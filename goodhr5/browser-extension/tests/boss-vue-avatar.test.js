/* 文件作用：用合成主世界验证 BOSS Vue 头像的当前人绑定、可见性、附件地址校验和最小读取范围，不访问真实招聘网站。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), vm = require("node:vm"), path = require("node:path"), fs = require("node:fs"), os = require("node:os");
const reader = require("../src/boss-vue-avatar.js"), shared = require("../src/shared.js");
const PAGE = "https://www.zhipin.com/web/chat/index", CANDIDATE = "synthetic_encrypted_uid_A", PHOTO = "https://img.bosszhipin.com/beijin/upload/avatar/20261017/synthetic-photo.jpg.webp";

/** visibleNode 构造只有只读尺寸和祖先样式的可见节点，不包含真实候选人资料。 */
function visibleNode(extra = {}) {
  return { parentElement: null, hidden: false, style: { display: "block", visibility: "visible", opacity: "1" }, getClientRects() { return [{ width: 200, height: 120 }]; }, getAttribute() { return ""; }, ...extra };
}

/** probeScene 将固定 MAIN 函数放入独立主世界执行，序列化可发现偷偷依赖后台闭包的实现。 */
function probeScene() {
  const id = CANDIDATE, geek = { encryptUid: id, uid: "100001", avatar: PHOTO }, resume = { encryptUid: id, uid: "100001" }, detail = { encryptGeekId: id };
  const conversation = visibleNode({ __vue__: { geek } }), currentResume = visibleNode({ __vue__: { resume, detail } });
  const inner = new URL("/wflow/zpgeek/download/preview4boss/" + id, PAGE); inner.searchParams.set("geekId", id);
  const outer = new URL("/bzl-office/pdf-viewer-b", PAGE); outer.searchParams.set("url", inner.href);
  const frame = visibleNode({ src: outer.href, getAttribute(name) { return name === "src" ? this.src : ""; } });
  let forbiddenReads = 0;
  Object.defineProperty(frame, "contentDocument", { get() { forbiddenReads++; throw new Error("不能遍历附件内部页面"); } });
  for (const item of [conversation.__vue__, currentResume.__vue__, geek, resume, detail]) Object.defineProperty(item, "privateUnrelatedState", { enumerable: true, get() { forbiddenReads++; throw new Error("不能枚举全量Vue"); } });
  const groups = { ".conversation-message": [conversation], ".resume-content": [currentResume], "iframe[src]": [frame] };
  const doc = { querySelectorAll(selector) { assert.ok(Object.hasOwn(groups, selector), "只能读取固定节点：" + selector); return groups[selector]; } };
  Object.defineProperty(doc, "cookie", { get() { forbiddenReads++; throw new Error("不能读取Cookie"); } });
  const globals = { document: doc, location: new URL(PAGE), URL, getComputedStyle: (node) => node.style };
  globals.window = globals; globals.top = globals; doc.defaultView = globals;
  for (const key of ["localStorage", "sessionStorage", "fetch", "XMLHttpRequest"]) Object.defineProperty(globals, key, { get() { forbiddenReads++; throw new Error("不能读取网站缓存或请求网络"); } });
  return { conversation, currentResume, frame, geek, resume, detail, groups, doc, globals, get forbiddenReads() { return forbiddenReads; }, run() { const result = vm.runInNewContext("(" + reader.probeCurrentBossAvatar.toString() + ")()", globals); return result === null ? null : JSON.parse(JSON.stringify(result)); } };
}

test("固定MAIN探针只返回当前附件精确绑定的候选人加密编号和头像，不读全量Vue或缓存", () => {
  const scene = probeScene(); assert.equal(reader.probeCurrentBossAvatar.length, 0);
  assert.deepEqual(scene.run(), { candidate_id: CANDIDATE, avatar_url: PHOTO }); assert.equal(scene.forbiddenReads, 0);
});

test("隐藏、透明、祖先隐藏和零尺寸组件不提供头像；隐藏旧组件不会污染唯一当前人", () => {
  for (const change of [
    (scene) => { scene.conversation.hidden = true; },
    (scene) => { scene.currentResume.style.display = "none"; },
    (scene) => { scene.conversation.style.visibility = "hidden"; },
    (scene) => { scene.currentResume.style.opacity = "0"; },
    (scene) => { scene.conversation.parentElement = visibleNode({ style: { display: "none", visibility: "visible", opacity: "1" } }); },
    (scene) => { scene.currentResume.getClientRects = () => [{ width: 0, height: 120 }]; },
    (scene) => { scene.frame.style.display = "none"; }
  ]) { const scene = probeScene(); change(scene); assert.equal(scene.run(), null); }
  const scene = probeScene(); scene.groups[".conversation-message"].push(visibleNode({ hidden: true, __vue__: { geek: { encryptUid: "old_candidate", uid: "old", avatar: PHOTO } } })); assert.deepEqual(scene.run(), { candidate_id: CANDIDATE, avatar_url: PHOTO });
});

test("聊天详情、简历详情或附件同时出现多个可见实例时留空，不能取第一个", () => {
  for (const selector of Object.keys(probeScene().groups)) { const scene = probeScene(); scene.groups[selector].push(scene.groups[selector][0]); assert.equal(scene.run(), null, selector); }
});

test("所有身份字段均非空且精确匹配，加载旧详情、切聊天与缺少Vue时留空", () => {
  for (const change of [
    (scene) => { scene.geek.encryptUid = "synthetic_encrypted_uid_B"; },
    (scene) => { scene.resume.encryptUid = "synthetic_encrypted_uid_B"; },
    (scene) => { scene.detail.encryptGeekId = "synthetic_encrypted_uid_B"; },
    (scene) => { scene.geek.uid = "100002"; },
    (scene) => { scene.resume.uid = ""; },
    (scene) => { scene.geek.encryptUid = ""; },
    (scene) => { scene.detail.encryptGeekId = ""; },
    (scene) => { delete scene.conversation.__vue__; },
    (scene) => { delete scene.currentResume.__vue__; },
    (scene) => { scene.globals.location = new URL("https://www.zhipin.com.evil.test/web/chat/index"); }
  ]) { const scene = probeScene(); change(scene); assert.equal(scene.run(), null); }
});

test("附件外层和内层只接受同站固定路径与唯一精确geekId，跨源或错误候选人不取头像", () => {
  const urls = [
    "https://evil.test/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/" + CANDIDATE + "?geekId=" + CANDIDATE),
    PAGE + "?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/" + CANDIDATE + "?geekId=" + CANDIDATE),
    "/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("https://evil.test/wflow/zpgeek/download/preview4boss/" + CANDIDATE + "?geekId=" + CANDIDATE),
    "/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/other_candidate?geekId=" + CANDIDATE),
    "/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/" + CANDIDATE + "?geekId=other_candidate"),
    "/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/" + CANDIDATE),
    "/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/" + CANDIDATE + "?geekId=" + CANDIDATE + "&geekId=" + CANDIDATE),
    "/bzl-office/pdf-viewer-b?url=" + encodeURIComponent("/wflow/zpgeek/download/preview4boss/" + CANDIDATE + "?geekId=" + CANDIDATE) + "&url=another"
  ];
  for (const source of urls) { const scene = probeScene(); scene.frame.src = new URL(source, PAGE).href; assert.equal(scene.run(), null, source); assert.equal(scene.forbiddenReads, 0); }
});

test("真实上传目录头像有精确域约束，默认卡通头像、危险地址和其它图片目录留空", () => {
  assert.equal(shared.safeBossVueAvatarSource(PHOTO, PAGE), PHOTO);
  for (const value of ["https://img.bosszhipin.com/boss/avatar/avatar_13.png", "https://img.bosszhipin.com/beijin/upload/avatar/../../boss/avatar/avatar_13.png", "https://img.bosszhipin.com/beijin/upload/other/photo.jpg", "https://sub.img.bosszhipin.com/beijin/upload/avatar/photo.jpg", "https://img.bosszhipin.com.evil.test/beijin/upload/avatar/photo.jpg", "https://user:secret@img.bosszhipin.com/beijin/upload/avatar/photo.jpg", "http://img.bosszhipin.com/beijin/upload/avatar/photo.jpg", "data:image/png;base64,AAAA", "javascript:alert(1)"]) { const scene = probeScene(); scene.geek.avatar = value; assert.equal(scene.run(), null, value); assert.equal(shared.safeBossVueAvatarSource(value, PAGE), "", value); }
  assert.equal(shared.safeBossVueAvatarSource(PHOTO, "https://h.liepin.com/resume/showresumedetail"), "");
});

test("真实Chrome隔离合成页面可读取MAIN头像并按实际CSS排除隐藏旧组件，所有请求均在测试内响应", async () => {
  const { chromium } = require(path.join(__dirname, "..", "..", "cloud", "frontend-next", "node_modules", "@playwright", "test")), browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const context = await browser.newContext(), inner = new URL("/wflow/zpgeek/download/preview4boss/" + CANDIDATE, PAGE), outer = new URL("/bzl-office/pdf-viewer-b", PAGE); inner.searchParams.set("geekId", CANDIDATE); outer.searchParams.set("url", inner.href);
    const html = `<div class="conversation-message" style="width:200px;height:120px"></div><div class="resume-wrapper"><div class="resume-content" style="width:200px;height:120px"></div></div><div style="display:none"><div class="conversation-message">合成隐藏旧人</div></div><iframe width="200" height="200" src="${outer.href.replaceAll("&", "&amp;")}"></iframe><script>document.querySelector('.conversation-message').__vue__={geek:${JSON.stringify({ encryptUid: CANDIDATE, uid: "100001", avatar: PHOTO })}};document.querySelector('.resume-content').__vue__={resume:${JSON.stringify({ encryptUid: CANDIDATE, uid: "100001" })},detail:${JSON.stringify({ encryptGeekId: CANDIDATE })}};</script>`;
    await context.route("**/*", (route) => route.fulfill({ status: 200, contentType: "text/html", body: route.request().url() === PAGE ? html : "<!doctype html><title>仅合成附件</title>" }));
    const page = await context.newPage(); await page.goto(PAGE);
    assert.deepEqual(await page.evaluate(reader.probeCurrentBossAvatar), { candidate_id: CANDIDATE, avatar_url: PHOTO });
    await page.locator(".resume-wrapper").evaluate((node) => { node.style.display = "none"; }); assert.equal(await page.evaluate(reader.probeCurrentBossAvatar), null);
    await page.locator(".resume-wrapper").evaluate((node) => { node.style.display = "block"; }); await page.locator(".conversation-message").first().evaluate((node) => { node.__vue__.geek.encryptUid = "synthetic_encrypted_uid_B"; }); assert.equal(await page.evaluate(reader.probeCurrentBossAvatar), null);
  } finally { await browser.close(); }
});

test("真实Chrome扩展后台通过实际scripting MAIN接口读取Vue，隔离世界读不到且全程不登录或发送网络", { timeout: 20000 }, async () => {
  const { chromium } = require(path.join(__dirname, "..", "..", "cloud", "frontend-next", "node_modules", "@playwright", "test")), { preparePackageDirectory } = require("../scripts/build.js");
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "goodhr-vue-main-")), extensionDirectory = path.join(temporary, "extension"), profileDirectory = path.join(temporary, "profile"); let context;
  try {
    preparePackageDirectory(extensionDirectory, "chrome", true);
    context = await chromium.launchPersistentContext(profileDirectory, { channel: "chrome", headless: true, ignoreDefaultArgs: ["--disable-extensions"], args: ["--enable-unsafe-extension-debugging", "--disable-background-networking"] });
    const inner = new URL("/wflow/zpgeek/download/preview4boss/" + CANDIDATE, PAGE), outer = new URL("/bzl-office/pdf-viewer-b", PAGE); inner.searchParams.set("geekId", CANDIDATE); outer.searchParams.set("url", inner.href);
    const html = `<div class="conversation-message" style="width:200px;height:120px"></div><div class="resume-content" style="width:200px;height:120px"></div><iframe width="200" height="200" src="${outer.href.replaceAll("&", "&amp;")}"></iframe><script>document.querySelector('.conversation-message').__vue__={geek:${JSON.stringify({ encryptUid: CANDIDATE, uid: "100001", avatar: PHOTO })}};document.querySelector('.resume-content').__vue__={resume:${JSON.stringify({ encryptUid: CANDIDATE, uid: "100001" })},detail:${JSON.stringify({ encryptGeekId: CANDIDATE })}};</script>`;
    await context.route("**/*", (route) => { if (route.request().url().startsWith("chrome-extension://")) return route.continue(); if (route.request().url() === PAGE) return route.fulfill({ status: 200, contentType: "text/html", body: html }); return route.abort(); });
    const cdp = await context.browser().newBrowserCDPSession(), installed = await cdp.send("Extensions.loadUnpacked", { path: extensionDirectory }); assert.ok(installed.id);
    const worker = context.serviceWorkers()[0] || await context.waitForEvent("serviceworker");
    await worker.evaluate(() => { globalThis.syntheticNetworkAttempts = []; globalThis.fetch = async (url) => { syntheticNetworkAttempts.push(String(url)); throw new Error("隔离测试禁止后台网络"); }; });
    const page = context.pages()[0] || await context.newPage(); await page.goto(PAGE);
    const result = await worker.evaluate(async (url) => {
      const tabs = await chrome.tabs.query({ url }); if (tabs.length !== 1) throw new Error("合成目标页面必须唯一");
      const isolated = await chrome.scripting.executeScript({ target: { tabId: tabs[0].id, frameIds: [0] }, world: "ISOLATED", func: () => Boolean(document.querySelector(".conversation-message")?.__vue__) });
      return { isolated_sees_vue: isolated[0].result, result: await probeBossAttachmentAvatar({ id: tabs[0].id, url: tabs[0].url }), session: (await chrome.storage.local.get(["session"])).session || null, network: syntheticNetworkAttempts };
    }, PAGE);
    assert.equal(result.isolated_sees_vue, false); assert.deepEqual(result.result, { candidate_id: CANDIDATE, avatar_url: PHOTO }); assert.equal(result.session, null); assert.deepEqual(result.network, []);
    await page.locator(".conversation-message").evaluate((node) => { node.__vue__.geek.avatar = "https://img.bosszhipin.com/boss/avatar/avatar_13.png"; });
    const rejected = await worker.evaluate(async (url) => { const [tab] = await chrome.tabs.query({ url }); return probeBossAttachmentAvatar({ id: tab.id, url: tab.url }); }, PAGE); assert.equal(rejected, null);
    console.log(JSON.stringify({ boss_vue_actual_extension_main: true, isolated_world_has_no_vue: true, default_avatar_rejected: true, background_network_attempts: 0, chrome: context.browser().version() }));
  } finally {
    await context?.close();
    if (path.dirname(path.resolve(temporary)) !== path.resolve(os.tmpdir()) || !path.basename(temporary).startsWith("goodhr-vue-main-")) throw new Error("合成扩展临时目录超出预定范围，停止清理");
    fs.rmSync(temporary, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
  }
});
