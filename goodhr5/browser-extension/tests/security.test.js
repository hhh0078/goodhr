/* 文件作用：验证招聘脚本只读、消息权限隔离、受限凭证不泄漏及配对地址白名单。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), vm = require("node:vm"), crypto = require("node:crypto");
const src = path.join(__dirname, "..", "src");

/** backgroundFixture 构造后台执行环境和无真实网络的受限会话。 */
function backgroundFixture(fetchReply) {
  const values = { session: { token: "private-background-token", user: { email: "fixture@example.test", id: "fixture" }, expires_at: "2030-01-01" } }, requests = []; let listener;
  const extension = {
    runtime: { id: "test-extension", getURL: (value) => "chrome-extension://test-extension/" + value, getManifest: () => ({ version: require("../package.json").version }), onMessage: { addListener: (handler) => { listener = handler; } } },
    storage: { local: { get: async (keys) => Object.fromEntries(keys.filter((key) => key in values).map((key) => [key, values[key]])), set: async (input) => Object.assign(values, input), remove: async (keys) => { for (const key of Array.isArray(keys) ? keys : [keys]) delete values[key]; }, setAccessLevel: async () => {} } },
    tabs: { query: async () => [], create: async () => ({}), onRemoved: { addListener: () => {} }, onUpdated: { addListener: () => {} } },
    action: { setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {} }
  };
  const context = vm.createContext({ chrome: extension, URL, AbortSignal, crypto: crypto.webcrypto, Uint8Array, console, fetch: async (url, options) => { requests.push({ url, options }); return fetchReply ? fetchReply(url, options) : response({ ok: true, matches: [], token: "refreshed", user: { email: "fixture@example.test" } }); } });
  vm.runInContext(fs.readFileSync(path.join(src, "shared.js"), "utf8"), context); vm.runInContext(fs.readFileSync(path.join(src, "avatar.js"), "utf8"), context); vm.runInContext(fs.readFileSync(path.join(src, "background.js"), "utf8"), context);
  return { values, requests, context, extension, message: (message, sender) => new Promise((resolve) => { const result = listener(message, sender, resolve); if (result === false) resolve(undefined); }) };
}

/** response 构造明确状态码的 JSON 响应，避免真实外部请求。 */
function response(body = { ok: true }, status = 200) { return { ok: status >= 200 && status < 300, status, json: async () => body }; }
const uiSender = { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" };

/** avatarScene 构造当前头像快照及稳定采集标识，图片处理由后台测试替身负责。 */
async function avatarScene(value) {
  const tab = { id: 23, url: "https://h.liepin.com/resume/showresumedetail" }, resume = { candidate_name: "合成头像候选人", phone: "13800000001", platform_id: "hliepin", source_url: tab.url, avatar_source_url: "https://image0.lietou-static.com/fixture.png?signature=synthetic-value" };
  value.extension.tabs.query = async () => [tab]; value.extension.tabs.sendMessage = async () => value.message({ type: "CANDIDATE_SNAPSHOT", resume }, { id: "test-extension", frameId: 0, tab });
  const state = await value.message({ type: "PREVIEW" }, uiSender); return state.data.resume;
}

test("招聘页面只读脚本不写 DOM、不发请求、不读网站缓存、不模拟操作", () => {
  const code = ["content.js", "extraction.js"].map((name) => fs.readFileSync(path.join(src, name), "utf8")).join("\n");
  for (const forbidden of [/\.click\s*\(/, /\.focus\s*\(/, /\.scroll(?:To|By|IntoView)\s*\(/, /dispatchEvent\s*\(/, /createElement\s*\(/, /appendChild\s*\(/, /insertAdjacentHTML\s*\(/, /(?:innerHTML|textContent|outerHTML)\s*=/, /(?:fetch|XMLHttpRequest|WebSocket)\s*\(/, /localStorage|sessionStorage|document\.cookie/, /storage\.(?:local|sync)/, /\beval\s*\(/, /new\s+Function\s*\(/]) assert.equal(forbidden.test(code), false, String(forbidden));
});

test("内容脚本不能调用登录、查重、保存或读取凭证接口", async () => {
  const value = backgroundFixture(), sender = { id: "test-extension", frameId: 0, tab: { id: 4, url: "https://h.liepin.com/resume/showresumedetail" } };
  for (const type of ["STATE", "PREVIEW", "LOGIN", "CREATE", "AVATAR_RETRY", "DUPLICATES", "RESTORE", "FOLLOWUPS", "LOGOUT", "REVOKE_RETRY"]) assert.equal(await value.message({ type, payload: {} }, sender), undefined);
  assert.equal(value.requests.length, 0);
});

test("白名单详情上报可设徽标，iframe及站外上报被拒绝", async () => {
  const value = backgroundFixture(), resume = { candidate_name: "测试", phone: "13812345678", platform_id: "hliepin", source_url: "https://h.liepin.com/resume/showresumedetail" };
  const accepted = await value.message({ type: "CANDIDATE_SNAPSHOT", resume }, { id: "test-extension", frameId: 0, tab: { id: 2, url: resume.source_url } }); assert.equal(accepted.ok, true);
  assert.equal(await value.message({ type: "CANDIDATE_SNAPSHOT", resume }, { id: "test-extension", frameId: 1, tab: { id: 2, url: resume.source_url } }), undefined);
  assert.equal(await value.message({ type: "CANDIDATE_SNAPSHOT", resume }, { id: "test-extension", frameId: 0, tab: { id: 2, url: "https://evil.example/" } }), undefined);
});

test("插件 UI 的公开状态不包含 token、配对令牌或挑战值", async () => {
  const value = backgroundFixture(); value.values.pairing_token = "private-pairing"; value.values.pending_pair = { challenge: "private-challenge" };
  const state = await value.message({ type: "STATE" }, { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" });
  assert.equal(state.data.logged_in, true); const encoded = JSON.stringify(state); assert.equal(/private-background-token|private-pairing|private-challenge/.test(encoded), false);
});

test("未同意数据用途时禁止向云端查询个人资料", async () => {
  const value = backgroundFixture(); const result = await value.message({ type: "DUPLICATES", payload: { resume: { phone: "13812345678" } } }, { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" });
  assert.equal(result.ok, false); assert.match(result.message, /同意/); assert.equal(value.requests.length, 0);
});

test("业务请求仅向生产 HTTPS 域名，凭证在后台 Bearer 头且不走招聘页面", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const result = await value.message({ type: "DUPLICATES", payload: { resume: { candidate_name: "测试", phone: "13812345678", platform_id: "hliepin" } } }, { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" });
  assert.equal(result.ok, true); assert.equal(value.requests.length, 1); assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates/duplicates"); assert.equal(value.requests[0].options.headers.Authorization, "Bearer private-background-token"); assert.equal(value.requests[0].options.redirect, "error");
});

test("后台实际 CREATE 请求等于供 Go 路由验证的完整 JSON，备注不混入主体", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const input = require("./fixtures/create-input.json"), payload = require("./fixtures/plugin-candidate-create.json");
  const result = await value.message({ type: "CREATE", payload: { resume: { ...input, note: "不可混入的备注" }, idempotency_key: payload.idempotency_key } }, uiSender);
  assert.equal(result.ok, true); assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates");
  assert.deepEqual(JSON.parse(value.requests[0].options.body), payload); assert.equal(value.requests.length, 1);
});

test("更新空字段、空数组、null不会覆盖原库经历，仅非空资料带版本提交", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const result = await value.message({ type: "UPDATE", payload: { candidate_id: "existing-fixture", row_version: 3, idempotency_key: "safe-patch", resume: { candidate_name: "合成更新候选人", phone: "13800000001", current_position: "新测试职位", email: "", work_experiences: [], educations: null, tags: [], age: null, expected_salary_min: 0 } } }, uiSender);
  assert.equal(result.ok, true); const payload = JSON.parse(value.requests[0].options.body);
  assert.equal(payload.row_version, 3); assert.equal(value.requests[0].options.method, "PATCH"); assert.equal(payload.current_position, "新测试职位"); assert.equal(payload.expected_salary_min, 0);
  for (const field of ["email", "work_experiences", "educations", "tags", "age", "personal_description"]) assert.equal(field in payload, false, field);
});

test("未登录本地 PREVIEW读取当前简历不发云请求，也不恢复账号或重试撤销", async () => {
  const value = backgroundFixture(); delete value.values.session;
  value.values.pending_revoke = { cloud_tokens: ["private-not-to-be-used"], pairing_token: "private-pairing" };
  const tab = { id: 19, url: "https://lpt.liepin.com/resume/detail?resIdEncode=fixture" };
  value.extension.tabs.query = async () => [tab];
  value.extension.tabs.sendMessage = async () => value.message({ type: "CANDIDATE_SNAPSHOT", resume: { candidate_name: "合成未登录候选人", phone: "13800000001", platform_id: "liepin" } }, { id: "test-extension", frameId: 0, tab });
  const result = await value.message({ type: "PREVIEW" }, uiSender);
  assert.equal(result.data.resume.candidate_name, "合成未登录候选人"); assert.equal(result.data.resume.phone, "13800000001");
  assert.equal(value.requests.length, 0); assert.equal(value.values.session, undefined); assert.equal(JSON.stringify(result).includes("private-"), false);
  const save = await value.message({ type: "CREATE", payload: { resume: result.data.resume, idempotency_key: "not-logged-in" } }, uiSender);
  assert.equal(save.ok, false); assert.equal(value.requests.length, 0);
});

test("头像预览不公开图片源或请求外部图片，保存成功发送dataURL而不是外链", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true; const resume = await avatarScene(value);
  assert.equal(resume.avatar_detected, true); assert.equal(JSON.stringify(resume).includes("lietou-static"), false); assert.equal(value.requests.length, 0);
  let readCount = 0; value.context.GoodHRAvatar.fetchAvatar = async () => { readCount++; return "data:image/png;base64,AAAA"; };
  const result = await value.message({ type: "CREATE", payload: { resume, capture_id: resume.capture_id, idempotency_key: "avatar-main-create" } }, uiSender);
  assert.equal(result.ok, true); assert.equal(readCount, 1); const payload = JSON.parse(value.requests[0].options.body);
  assert.equal(payload.avatar_data_url, "data:image/png;base64,AAAA"); assert.equal(payload.avatar_source_url, undefined); assert.equal(payload.capture_id, undefined);
});

test("头像失败保存其它资料并明确提醒，独立头像重试只PATCH头像且保留幂等键", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true; const resume = await avatarScene(value);
  let failing = true; value.context.GoodHRAvatar.fetchAvatar = async () => { if (failing) throw new Error("合成图片读取失败"); return "data:image/png;base64,AAAA"; };
  const created = await value.message({ type: "CREATE", payload: { resume, capture_id: resume.capture_id, idempotency_key: "avatar-create" } }, uiSender);
  assert.equal(created.ok, true); assert.match(created.data.avatar_warning, /原有头像会保留/); assert.equal(JSON.parse(value.requests[0].options.body).avatar_data_url, undefined);
  const retry = { candidate_id: "candidate-avatar", row_version: 1, capture_id: resume.capture_id, idempotency_key: "same-avatar-key" };
  const failed = await value.message({ type: "AVATAR_RETRY", payload: retry }, uiSender); assert.equal(failed.ok, false); assert.equal(value.requests.length, 1);
  failing = false; const succeeded = await value.message({ type: "AVATAR_RETRY", payload: retry }, uiSender); assert.equal(succeeded.ok, true);
  assert.equal(value.requests[1].options.method, "PATCH"); assert.deepEqual(JSON.parse(value.requests[1].options.body), { avatar_data_url: "data:image/png;base64,AAAA", row_version: 1, idempotency_key: "same-avatar-key" });
  const unchanged = await value.message({ type: "AVATAR_RETRY", payload: { ...retry, existing_avatar_data_url: "data:image/png;base64,AAAA" } }, uiSender); assert.equal(unchanged.data.unchanged, true); assert.equal(value.requests.length, 2);
});

test("头像读取等待期间退出或手动切号会取消CREATE及头像PATCH，不使用新账号保存旧资料", async () => {
  for (const type of ["CREATE", "AVATAR_RETRY"]) for (const switchType of ["LOGOUT", "LOGIN"]) {
    const value = backgroundFixture(); value.values.data_use_accepted = true; const resume = await avatarScene(value);
    let release, started; const wait = new Promise((resolve) => { release = resolve; }), active = new Promise((resolve) => { started = resolve; });
    value.context.GoodHRAvatar.fetchAvatar = async () => { started(); await wait; return "data:image/png;base64,AAAA"; };
    const pending = value.message({ type, payload: { resume, candidate_id: "fixture-avatar", row_version: 1, capture_id: resume.capture_id, idempotency_key: "delayed-avatar" } }, uiSender);
    await active;
    await value.message({ type: switchType, payload: { email: "new-account@example.test", password: "synthetic", agreement_accepted: true } }, uiSender); release();
    const result = await pending; assert.equal(result.ok, false); assert.match(result.message, /账号已经变化/);
    assert.equal(value.requests.some((request) => request.url.includes("/api/candidates")), false);
  }
});

test("原账号请求发出后退出，迟到成功结果不能返回给新账号界面", async () => {
  let release, started; const wait = new Promise((resolve) => { release = resolve; }), active = new Promise((resolve) => { started = resolve; });
  const value = backgroundFixture(async (url) => { if (url.endsWith("/api/candidates")) { started(); await wait; return response({ ok: true, candidate: { id: "private-old-account-result" } }); } return response(); });
  value.values.data_use_accepted = true;
  const pending = value.message({ type: "CREATE", payload: { resume: { candidate_name: "合成候选人", phone: "13800000001" }, idempotency_key: "original-account-request" } }, uiSender);
  await active; await value.message({ type: "LOGOUT" }, uiSender); release();
  const result = await pending; assert.equal(result.ok, false); assert.match(result.message, /旧账号的结果已停止展示/); assert.equal(JSON.stringify(result).includes("private-old-account-result"), false);
});

test("配对 port 只接受范围内整数且不能变成任意主机地址", () => {
  const value = backgroundFixture(); assert.equal(vm.runInContext("localBase({local_port:43129})", value.context), "http://127.0.0.1:43129");
  for (const port of [-1, 0.5, 65536, "evil.example"]) assert.throws(() => vm.runInContext(`localBase({local_port:${JSON.stringify(port)}})`, value.context));
});

test("退出分别撤销本地授权和云会话，成功后清除私有恢复密钥", async () => {
  const value = backgroundFixture(); Object.assign(value.values, { pairing_token: "private-local-grant", installation_id: "fixture-install", local_port: 43129, pending_pair: { challenge: "private-challenge" } });
  const result = await value.message({ type: "LOGOUT" }, uiSender);
  assert.equal(result.ok, true); assert.equal(result.data.revocation_pending, false);
  assert.equal(value.values.session, undefined); assert.equal(value.values.pairing_token, undefined); assert.equal(value.values.pending_pair, undefined); assert.equal(value.values.pending_revoke, undefined);
  assert.equal(value.requests.length, 2);
  assert.equal(value.requests[0].url, "http://127.0.0.1:43129/api/v1/extensions/pair/revoke");
  assert.deepEqual(JSON.parse(value.requests[0].options.body), { installation_id: "fixture-install", pairing_token: "private-local-grant" });
  assert.equal(value.requests[1].url, "https://goodhr.58it.cn/api/auth/extension-logout");
  assert.equal(/private-/.test(JSON.stringify(result)), false);
});

test("本地离线退出保留待撤销密钥，公开提醒且禁止自动恢复，重试可完成撤销", async () => {
  let offline = true;
  const value = backgroundFixture((url) => { if (url.includes("127.0.0.1") && offline) throw new Error("fixture offline"); return response(); });
  Object.assign(value.values, { pairing_token: "private-local-grant", installation_id: "fixture-install", local_port: 43129 });
  const result = await value.message({ type: "LOGOUT" }, uiSender);
  assert.equal(result.data.revocation_pending, true); assert.match(result.data.message, /本地程序连接授权仍未撤销/);
  assert.equal(value.values.session, undefined); assert.equal(value.values.pairing_token, undefined); assert.equal(value.values.pending_revoke.pairing_token, "private-local-grant");
  assert.equal(value.values.pending_revoke.cloud_tokens.length, 0);
  const state = await value.message({ type: "RESTORE" }, uiSender);
  assert.equal(state.data.logged_in, false); assert.equal(state.data.revocation_pending, true); assert.equal(/private-local-grant/.test(JSON.stringify(state)), false);
  assert.equal(value.requests.some((request) => request.url.endsWith("/session/restore")), false);
  offline = false; const retried = await value.message({ type: "REVOKE_RETRY" }, uiSender);
  assert.equal(retried.data.revocation_pending, false); assert.equal(value.values.pending_revoke, undefined);
});

test("云端撤销失败不能宣称退出完成，401和服务失败凭证均私有保留供重试", async () => {
  for (const status of [401, 500]) {
    let failing = true;
    const value = backgroundFixture((url) => response(failing && url.endsWith("extension-logout") ? { error: { message: "fixture unavailable" } } : { ok: true }, failing && url.endsWith("extension-logout") ? status : 200));
    const result = await value.message({ type: "LOGOUT" }, uiSender);
    assert.equal(result.data.revocation_pending, true); assert.match(result.data.message, /云端会话仍未撤销/);
    assert.equal(value.values.session, undefined); assert.equal(value.values.pending_revoke.cloud_tokens[0], "private-background-token");
    const state = await value.message({ type: "STATE" }, uiSender); assert.equal(/private-background-token/.test(JSON.stringify(state)), false);
    failing = false; const retry = await value.message({ type: "REVOKE_RETRY" }, uiSender);
    assert.equal(retry.data.revocation_pending, false); assert.equal(value.values.pending_revoke, undefined);
  }
});

test("退出期间迟到的登录会话只进入撤销队列，不能恢复登录", async () => {
  let release; const wait = new Promise((resolve) => { release = resolve; });
  const value = backgroundFixture(async (url) => { if (url.endsWith("extension-login")) { await wait; return response({ token: "private-late-token", user: { email: "fixture@example.test" } }); } return response(); });
  const login = value.message({ type: "LOGIN", payload: { email: "fixture@example.test", password: "synthetic", agreement_accepted: true } }, uiSender);
  await value.message({ type: "LOGOUT" }, uiSender); release();
  const result = await login; assert.equal(result.ok, false); assert.match(result.message, /退出取消/);
  assert.equal(value.values.session, undefined); assert.equal(value.values.pending_revoke.cloud_tokens[0], "private-late-token");
  await value.message({ type: "REVOKE_RETRY" }, uiSender); assert.equal(value.values.pending_revoke, undefined);
});
