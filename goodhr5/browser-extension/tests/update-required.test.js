/* 文件作用：用真实后台消息执行验证强制版本更新、并发检查缓存与跨重启持久化，不访问外部网络。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), vm = require("node:vm"), crypto = require("node:crypto");
const src = path.join(__dirname, "..", "src");
const uiSender = { id: "update-fixture", url: "chrome-extension://update-fixture/src/ui.html" };
const officialMetadata = "https://goodhr.58it.cn/api/system/browser-extension", officialDownload = "https://goodhr.58it.cn/download#browser-extension";

/** response 构造固定状态的 JSON 响应，全部网络仅在测试内模拟。 */
function response(body = { ok: true }, status = 200) { return { ok: status >= 200 && status < 300, status, json: async () => body }; }

/** updateFixture 保留私有存储并可重启真实后台，分别记录公开版本请求和个人资料请求。 */
function updateFixture({ values = {}, currentVersion = "1.0.3", release = { version: "1.0.3" }, metadataReply, businessReply, clock = { now: Date.now() } } = {}) {
  Object.assign(values, { ...(!Object.hasOwn(values, "session") ? { session: { token: "private-update-token", user: { id: "fixture", email: "fixture@example.test" }, expires_at: "2030-01-01" } } : {}), ...(!Object.hasOwn(values, "data_use_accepted") ? { data_use_accepted: true } : {}) });
  const requests = [], versionRequests = [];
  let listener;
  const extension = {
    runtime: { id: "update-fixture", getURL: (name) => "chrome-extension://update-fixture/" + name, getManifest: () => ({ version: currentVersion }), onMessage: { addListener: (handler) => { listener = handler; } } },
    storage: { local: { get: async (keys) => Object.fromEntries(keys.filter((key) => key in values).map((key) => [key, values[key]])), set: async (input) => Object.assign(values, input), remove: async (keys) => { for (const key of Array.isArray(keys) ? keys : [keys]) delete values[key]; }, setAccessLevel: async () => {} } },
    tabs: { query: async () => [], create: async () => ({}), onRemoved: { addListener: () => {} }, onUpdated: { addListener: () => {} } },
    action: { setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {} }
  };
  /** FixtureDate 只替换后台缓存时钟，不等待真实一分钟。 */
  class FixtureDate extends Date { static now() { return clock.now; } }
  const context = vm.createContext({ chrome: extension, URL, AbortSignal, crypto: crypto.webcrypto, Uint8Array, TextEncoder, Date: FixtureDate, console, setTimeout, clearTimeout, fetch: async (url, options) => {
    if (url === officialMetadata) { versionRequests.push({ url, options }); return metadataReply ? metadataReply(url, options) : response({ release }); }
    requests.push({ url, options });
    return businessReply ? businessReply(url, options) : response({ ok: true, candidate: { id: "synthetic-existing", row_version: 4 }, matches: [], notes: [], recommendations: [], data: url.includes("resume-summary") ? { task: { id: "synthetic-task", status: "cancelled" } } : undefined });
  } });
  for (const name of ["shared.js", "avatar.js", "background.js"]) vm.runInContext(fs.readFileSync(path.join(src, name), "utf8"), context);
  return { values, requests, versionRequests, context, extension, clock, message: (type, payload = {}, sender = uiSender) => new Promise((resolve) => { const accepted = listener({ type, payload }, sender, resolve); if (accepted === false) resolve(undefined); }) };
}

/** writablePayload 为各类提交提供完整合成资料，不让缺字段校验冒充版本阻止。 */
function writablePayload() { return { resume: { candidate_name: "合成版本候选人", phone: "13800000191", work_experiences: [{ company_name: "合成职业公司" }] }, idempotency_key: "synthetic-save", candidate_id: "synthetic-existing", row_version: 3, note: "合成备注", note_key: "synthetic-note", position_id: "synthetic-position", stage: "draft", recommendation_key: "synthetic-recommendation", request_key: "synthetic-ai", task_id: "synthetic-task", ai_summary: "合成职业摘要" }; }

/** assertBlocked 验证后台明确返回更新状态且没有任何个人资料网络提交。 */
function assertBlocked(result, value) { assert.equal(result.ok, false); assert.equal(result.status, 426); assert.equal(result.code, "EXTENSION_UPDATE_REQUIRED"); assert.match(result.message, /更新/); assert.equal(value.requests.length, 0); }

test("新版要求在后台阻止所有候选人、头像、备注推荐和AI写操作，不发送个人资料", async () => {
  const value = updateFixture({ release: { version: "1.0.4", note: "合成更新说明", url: "https://evil.example/" } });
  Object.assign(value.values, { installation_id: "synthetic-install", pairing_token: "private-grant", pairing_account_key: JSON.stringify(["fixture", "fixture@example.test"]) });
  let avatarReads = 0; value.context.GoodHRAvatar.fetchAvatar = async () => { avatarReads++; return "data:image/png;base64,YQ=="; };
  for (const type of ["CREATE", "UPDATE", "AVATAR_RETRY", "FOLLOWUPS", "SUMMARY_START", "SUMMARY_RETRY", "SUMMARY_SAVE"]) assertBlocked(await value.message(type, writablePayload()), value);
  assert.equal(avatarReads, 0); assert.equal(value.versionRequests.length, 1);
  const call = value.versionRequests[0]; assert.equal(call.url, officialMetadata); assert.equal(call.options.headers.Authorization, undefined); assert.equal(call.options.credentials, "omit"); assert.equal(call.options.redirect, "error"); assert.equal(call.options.body, undefined);
});

test("强制更新仍允许本地预览、云端查重和记录读取，以及取消正在运行的AI任务", async () => {
  const value = updateFixture({ release: { version: "1.0.4" } });
  Object.assign(value.values, { installation_id: "synthetic-install", pairing_token: "private-grant", pairing_account_key: JSON.stringify(["fixture", "fixture@example.test"]) });
  const state = await value.message("STATE"); assert.equal(state.data.update.update_required, true);
  const preview = await value.message("PREVIEW"); assert.equal(preview.ok, true); assert.equal(value.requests.length, 0);
  for (const type of ["POSITIONS", "DUPLICATES", "DETAIL", "SUMMARY_POLL", "SUMMARY_CANCEL"]) { const result = await value.message(type, writablePayload()); assert.equal(result.ok, true, type); }
  assert.equal(value.requests.some(({ url }) => url.endsWith("/synthetic-task/cancel")), true);
  assert.equal(value.requests.some(({ url, options }) => url.endsWith("/api/candidates") && options.method === "POST"), false);
});

test("公开配置的恶意链接不能改下载目标，STATE和主动检查都返回固定本站链接", async () => {
  const value = updateFixture({ release: { version: "1.0.4", url: "javascript:alert(1)", download_url: "https://evil.example/", note: "合成更新" } });
  const result = await value.message("CHECK_UPDATE"); assert.equal(result.data.url, officialDownload); assert.equal(result.data.update_required, true); assert.equal(result.data.current_version, "1.0.3");
  const state = await value.message("STATE"); assert.equal(state.data.update.url, officialDownload);
  assert.equal(value.requests.length, 0); assert.equal(value.versionRequests.length, 1); assert.equal(JSON.stringify(state).includes("private-update-token"), false);
});

test("同版本、较旧版本和无效版本不误阻止实际CREATE，首次配置离线也可继续", async () => {
  const releases = [{ version: "1.0.3" }, { version: "1.0.2" }, { version: "0.9.99" }, { version: "garbage" }, { version: "v1.0.4" }, { version: "1.0.4.0" }, { version: "" }, {}, { version: null }];
  for (const release of releases) {
    const value = updateFixture({ release }); const result = await value.message("CREATE", writablePayload()); assert.equal(result.ok, true, JSON.stringify(release));
    assert.equal(value.requests.length, 1); assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates"); assert.equal(JSON.parse(value.requests[0].options.body).phone, "13800000191");
    const state = await value.message("STATE"); assert.equal(state.data.update.update_required, false);
  }
  const offline = updateFixture({ metadataReply: async () => { throw new Error("合成配置离线"); } }); assert.equal((await offline.message("CREATE", writablePayload())).ok, true); assert.equal(offline.requests.length, 1);
});

test("并发版本检查合并一次请求，STATE一分钟缓存到期再读，主动检查可刷新", async () => {
  let started, release; const active = new Promise((resolve) => { started = resolve; });
  const value = updateFixture({ metadataReply: async () => { started(); return new Promise((resolve) => { release = () => resolve(response({ release: { version: "1.0.4" } })); }); } });
  const checks = Promise.all(Array.from({ length: 8 }, () => value.message("CHECK_UPDATE"))); await active; assert.equal(value.versionRequests.length, 1); release();
  const results = await checks; for (const result of results) assert.equal(result.data.update_required, true); assert.equal(value.versionRequests.length, 1);
  for (let index = 0; index < 4; index++) assert.equal((await value.message("STATE")).data.update.update_required, true);
  value.clock.now += 59999; await value.message("STATE"); assert.equal(value.versionRequests.length, 1);
  value.clock.now += 1; const expired = value.message("STATE"); await new Promise((resolve) => setImmediate(resolve)); assert.equal(value.versionRequests.length, 2); release(); await expired;
  const forced = value.message("CHECK_UPDATE"); await new Promise((resolve) => setImmediate(resolve)); assert.equal(value.versionRequests.length, 3); release(); await forced;
});

test("并发首次读取的迟到旧存储不能覆盖刚确认的新版，写操作继续被拦截", async () => {
  const value = updateFixture({ release: { version: "1.0.4" } }), originalGet = value.extension.storage.local.get;
  let firstRead, secondRead, readCount = 0, secondStarted; const active = new Promise((resolve) => { secondStarted = resolve; });
  value.extension.storage.local.get = (keys) => {
    if (!keys.includes("known_extension_release")) return originalGet(keys);
    readCount++;
    if (readCount === 1) return new Promise((resolve) => { firstRead = () => resolve({}); });
    if (readCount === 2) { secondStarted(); return new Promise((resolve) => { secondRead = () => resolve({}); }); }
    return originalGet(keys);
  };
  const checking = value.message("CHECK_UPDATE"), saving = value.message("CREATE", writablePayload());
  // 单次初始化应共用存储读取；旧实现会进入第二个慢读取，故两种路径均可继续验证。
  const secondWasStarted = await Promise.race([active.then(() => true), new Promise((resolve) => setTimeout(() => resolve(false), 20))]);
  firstRead(); assert.equal((await checking).data.update_required, true); if (secondWasStarted) secondRead();
  assertBlocked(await saving, value); assert.equal(value.versionRequests.length, 1);
});

test("已知新版持久化后后台重启且网络失败仍禁止旧版写入，无效回应也不抹掉更新要求", async () => {
  const first = updateFixture({ release: { version: "1.0.4", note: "合成必须更新" } }); await first.message("CHECK_UPDATE"); assert.equal(first.values.known_extension_release.version, "1.0.4");
  const restarted = updateFixture({ values: first.values, metadataReply: async () => { throw new Error("合成重启后离线"); } });
  assertBlocked(await restarted.message("CREATE", writablePayload()), restarted); assert.equal(restarted.versionRequests.length, 1);
  const state = await restarted.message("STATE"); assert.equal(state.data.update.latest_version, "1.0.4"); assert.equal(state.data.update.update_required, true);
  const invalid = updateFixture({ values: first.values, release: { version: "invalid" } }); assertBlocked(await invalid.message("FOLLOWUPS", writablePayload()), invalid); assert.equal(invalid.values.known_extension_release.version, "1.0.4");
});

test("用户已更新到所需版本或更高版本后，重启即使配置离线也不会继续锁住", async () => {
  for (const currentVersion of ["1.0.4", "1.0.5", "1.1.0"]) {
    const value = updateFixture({ currentVersion, values: { known_extension_release: { version: "1.0.4", note: "旧缓存合成说明" } }, metadataReply: async () => { throw new Error("合成离线"); } });
    const state = await value.message("STATE"); assert.equal(state.data.update.update_required, false, currentVersion); assert.equal(state.data.update.current_version, currentVersion);
    const created = await value.message("CREATE", writablePayload()); assert.equal(created.ok, true); assert.equal(value.requests.length, 1); assert.equal(value.versionRequests.length, 1);
  }
});

test("未登录或未授权的写请求先拒绝账号和用途，不为被拒绝写请求读取版本或个人数据", async () => {
  for (const values of [{ session: null }, { data_use_accepted: false }]) {
    const value = updateFixture({ values, release: { version: "1.0.4" } }); const result = await value.message("CREATE", writablePayload());
    assert.equal(result.ok, false); assert.notEqual(result.status, 426); assert.match(result.message, values.session === null ? /登录/ : /同意/); assert.equal(value.requests.length, 0); assert.equal(value.versionRequests.length, 0);
  }
});
