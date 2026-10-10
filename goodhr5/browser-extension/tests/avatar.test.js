/* 文件作用：验证头像域白名单、文件头、体积/像素边界、离页面画布输出及严格内嵌图片合同。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), avatar = require("../src/avatar.js"), shared = require("../src/shared.js");
const png = Uint8Array.from([137,80,78,71,13,10,26,10]);

/** imageEnvironment 构造无招聘 DOM 和真实网络的后台图片环境，用于边界验证。 */
function imageEnvironment(bytes = png, dimensions = { width: 800, height: 400 }) {
  const calls = [], sizes = []; let closed = false;
  class TestCanvas {
    /** constructor 记录缩图尺寸，模拟插件后台离页面画布。 */
    constructor(width, height) { this.width = width; this.height = height; sizes.push({ width, height }); }
    /** getContext 提供仅处理测试图片的画图接口。 */
    getContext() { return { drawImage: () => {}, fillRect: () => {} }; }
    /** convertToBlob 返回可识别文件头，用于编码和体积合同验证。 */
    async convertToBlob() { return new Blob([png], { type: "image/png" }); }
  }
  return { calls, sizes, get closed() { return closed; }, environment: { fetch: async (url, options) => { calls.push({ url, options }); return { ok: true, headers: { get: () => "" }, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) }; }, createImageBitmap: async () => ({ ...dimensions, close: () => { closed = true; } }), OffscreenCanvas: TestCanvas } };
}

test("头像仅可从当前平台及有限CDN读取，拒绝相似域、用户名、HTTP与跨平台地址", () => {
  assert.equal(shared.safeAvatarSource("https://image0.lietou-static.com/fixture.png", "https://lpt.liepin.com/resume/detail"), "https://image0.lietou-static.com/fixture.png");
  assert.equal(shared.safeAvatarSource("https://mypics.zhaopin.cn/fixture.jpg", "https://rd6.zhaopin.com/app"), "https://mypics.zhaopin.cn/fixture.jpg");
  for (const source of ["https://lietou-static.com.evil.test/a.png", "http://image0.lietou-static.com/a.png", "https://user:secret@image0.lietou-static.com/a.png", "https://mypics.zhaopin.cn/a.png", "javascript:alert(1)", "blob:https://lpt.liepin.com/fixture"]) assert.equal(shared.safeAvatarSource(source, "https://lpt.liepin.com/resume/detail"), "", source);
  assert.equal(shared.safeAvatarSource("https://unverified.51jobcdn.com/a.png", "https://ehire.51job.com/feature/im"), "");
  assert.equal(shared.safeAvatarSource("", "https://h.liepin.com/resume/showresumedetail"), "");
});

test("保存请求头像不发送cookie、拒绝重定向、有超时且缩图不触碰页面DOM", async () => {
  const fixture = imageEnvironment(); const dataURL = await avatar.fetchAvatar("https://image0.lietou-static.com/fixture.png", "https://h.liepin.com/resume/showresumedetail", fixture.environment);
  assert.equal(fixture.calls.length, 1); assert.equal(fixture.calls[0].options.credentials, "omit"); assert.equal(fixture.calls[0].options.redirect, "error"); assert.equal(fixture.calls[0].options.referrerPolicy, "no-referrer"); assert.ok(fixture.calls[0].options.signal instanceof AbortSignal);
  assert.deepEqual(fixture.sizes, [{ width: 256, height: 128 }]); assert.equal(fixture.closed, true); assert.equal(shared.validStoredAvatar(dataURL), true);
});

test("图片伪装、超过1MiB响应和超过8百万像素解码均拒绝，超限流会取消", async () => {
  for (const bytes of [Uint8Array.from(Buffer.from("<svg><script>bad</script></svg>")), new Uint8Array(1024 * 1024 + 1)]) {
    const fixture = imageEnvironment(bytes); await assert.rejects(() => avatar.fetchAvatar("https://h.liepin.com/avatar", "https://h.liepin.com/resume/showresumedetail", fixture.environment)); assert.equal(fixture.sizes.length, 0);
  }
  const bigPixels = imageEnvironment(png, { width: 4000, height: 3000 }); await assert.rejects(() => avatar.fetchAvatar("https://h.liepin.com/avatar", "https://h.liepin.com/resume/showresumedetail", bigPixels.environment), /尺寸太大/); assert.equal(bigPixels.closed, true);
  let cancelled = false; const reader = { read: async () => ({ done: false, value: new Uint8Array(1024 * 1024 + 1) }), cancel: async () => { cancelled = true; } };
  await assert.rejects(() => avatar.readLimitedResponse({ headers: { get: () => "" }, body: { getReader: () => reader } }), /超过/); assert.equal(cancelled, true);
});

test("图片域外地址在fetch之前拒绝，WebP魔数可识别，输出只允许PNG或JPEG严格base64", async () => {
  const fixture = imageEnvironment(); await assert.rejects(() => avatar.fetchAvatar("https://evil.test/a.png", "https://h.liepin.com/resume/showresumedetail", fixture.environment), /不在支持范围/); assert.equal(fixture.calls.length, 0);
  assert.equal(avatar.detectImageType(Uint8Array.from(Buffer.from("RIFF1234WEBP"))), "image/webp");
  const prefix = "data:image/png;base64,"; assert.equal(shared.validStoredAvatar(prefix + "AAAA"), true);
  for (const value of [prefix + "AAAA=", prefix + "A===", prefix, "data:image/svg+xml;base64,AAAA", prefix + "A".repeat(131072), "https://image0.lietou-static.com/a.png"]) assert.equal(shared.validStoredAvatar(value), false);
  assert.equal(shared.sanitizeResume({ avatar_data_url: prefix + "AAAA" }).avatar_data_url, prefix + "AAAA");
  const changes = shared.describeResumeChanges({ source_type: "browser_extension" }, { avatar_data_url: prefix + "AAAA" }); assert.deepEqual(changes, ["头像：未填写 → 已保存头像"]);
});
