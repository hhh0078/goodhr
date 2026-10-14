/* 文件作用：验证招聘脚本只读、消息权限隔离、受限凭证不泄漏及配对地址白名单。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), vm = require("node:vm"), crypto = require("node:crypto");
const src = path.join(__dirname, "..", "src");

/** backgroundFixture 构造后台执行环境和无真实网络的受限会话。 */
function backgroundFixture(fetchReply) {
  const values = { session: { token: "private-background-token", user: { email: "fixture@example.test", id: "fixture" }, expires_at: "2030-01-01" } }, requests = [], versionRequests = []; let listener;
  const extension = {
    runtime: { id: "test-extension", getURL: (value) => "chrome-extension://test-extension/" + value, getManifest: () => ({ version: require("../package.json").version }), onMessage: { addListener: (handler) => { listener = handler; } } },
    storage: { local: { get: async (keys) => Object.fromEntries(keys.filter((key) => key in values).map((key) => [key, values[key]])), set: async (input) => Object.assign(values, input), remove: async (keys) => { for (const key of Array.isArray(keys) ? keys : [keys]) delete values[key]; }, setAccessLevel: async () => {} } },
    tabs: { query: async () => [], create: async () => ({}), onRemoved: { addListener: () => {} }, onUpdated: { addListener: () => {} } },
    action: { setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {} }
  };
  const context = vm.createContext({ chrome: extension, URL, AbortSignal, crypto: crypto.webcrypto, Uint8Array, TextEncoder, console, setTimeout, clearTimeout, fetch: async (url, options) => { if (new URL(url).pathname === "/api/system/browser-extension") { versionRequests.push({ url, options }); return response({ release: { version: extension.runtime.getManifest().version } }); } requests.push({ url, options }); return fetchReply ? fetchReply(url, options) : response({ ok: true, matches: [], token: "refreshed", user: { email: "fixture@example.test" } }); } });
  vm.runInContext(fs.readFileSync(path.join(src, "shared.js"), "utf8"), context); vm.runInContext(fs.readFileSync(path.join(src, "avatar.js"), "utf8"), context); vm.runInContext(fs.readFileSync(path.join(src, "background.js"), "utf8"), context);
  return { values, requests, versionRequests, context, extension, message: (message, sender) => new Promise((resolve) => { const result = listener(message, sender, resolve); if (result === false) resolve(undefined); }) };
}

/** response 构造明确状态码的 JSON 响应，避免真实外部请求。 */
function response(body = { ok: true }, status = 200) { return { ok: status >= 200 && status < 300, status, json: async () => body }; }
const uiSender = { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" };
const floatingSender = { id: "test-extension", url: "chrome-extension://test-extension/src/floating.html" };

test("标签只由授权插件界面读取已有列表，招聘网页与旧账号不能请求标签管理", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const result = await value.message({ type: "TAGS" }, floatingSender);
  assert.equal(result.ok, true); assert.equal(value.requests.length, 1);
  assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates/tags"); assert.equal(value.requests[0].options.method, "GET");
  const webSender = { id: "test-extension", frameId: 0, tab: { id: 11, url: "https://h.liepin.com/resume/show" }, url: "https://h.liepin.com/resume/show" };
  assert.equal(await value.message({ type: "TAGS" }, webSender), undefined);
  const stale = await value.message({ type: "TAGS", payload: { account_key: "old-account" } }, floatingSender);
  assert.equal(stale.ok, false); assert.match(stale.message, /账号已经变化/);
  assert.equal((await value.message({ type: "TAG_CREATE", payload: { name: "任意新增" } }, floatingSender)).ok, false);
  assert.equal(value.requests.length, 1);
  value.values.data_use_accepted = false;
  assert.equal((await value.message({ type: "TAGS" }, floatingSender)).ok, false); assert.equal(value.requests.length, 1);
});

test("插件保存仅提交已选标签编号，不再把采集技能建成标签，更新可显式移除标签", async () => {
  const tagID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", value = backgroundFixture(); value.values.data_use_accepted = true;
  const resume = { candidate_name: "合成标签候选人", phone: "13800000001", tags: ["招聘页面技能", "不能自由新建"], tag_ids: [tagID, tagID, "任意输入"] };
  assert.equal((await value.message({ type: "CREATE", payload: { resume, idempotency_key: "create-tags" } }, uiSender)).ok, true);
  const create = JSON.parse(value.requests[0].options.body); assert.deepEqual(create.tag_ids, [tagID]); assert.equal(Object.hasOwn(create, "tags"), false);
  assert.equal((await value.message({ type: "UPDATE", payload: { resume: { ...resume, tag_ids: [] }, candidate_id: "existing", row_version: 1, idempotency_key: "clear-tags" } }, uiSender)).ok, true);
  const update = JSON.parse(value.requests[1].options.body); assert.deepEqual(update.tag_ids, []); assert.equal(Object.hasOwn(update, "tags"), false);
  assert.equal((await value.message({ type: "POSITIONS" }, floatingSender)).ok, true); assert.equal(value.requests[2].url, "https://goodhr.58it.cn/api/positions?purpose=recommendation");
});

test("标签草稿只保存有限编号和人工选择标记，切账号不能恢复他人的标签", async () => {
  const value = backgroundFixture(), account = JSON.stringify(["fixture", "fixture@example.test"]), tagID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const draft = { fields: { "tag-filter": "合成标签" }, tag_ids: [tagID, "搜索词"], tags_edited: true };
  assert.equal((await value.message({ type: "DRAFT_SAVE", payload: { identity: "synthetic", account_key: account, draft } }, floatingSender)).ok, true);
  const loaded = await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic", account_key: account } }, floatingSender);
  assert.deepEqual(Array.from(loaded.data.draft.tag_ids), [tagID]); assert.equal(loaded.data.draft.tags_edited, true); assert.equal(loaded.data.draft.fields["tag-filter"], "合成标签");
  assert.equal((await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic", account_key: "other" } }, floatingSender)).ok, false);
});

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

test("悬浮入口只使用固定扩展iframe，既不接网页消息，也不访问网站缓存或主世界", () => {
  const code = fs.readFileSync(path.join(src, "floating.js"), "utf8");
  assert.match(code, /attachShadow\(\{ mode: "closed" \}\)/);
  assert.match(code, /getURL\("src\/floating\.html"\)/);
  for (const forbidden of [/addEventListener\(["']message/, /postMessage\s*\(/, /(?:fetch|XMLHttpRequest|WebSocket)\s*\(/, /localStorage|sessionStorage|document\.cookie/, /storage\.(?:local|sync)/, /\.focus\s*\(/, /\.scroll(?:To|By|IntoView)\s*\(/, /dispatchEvent\s*\(/, /innerHTML\s*=/]) assert.equal(forbidden.test(code), false, String(forbidden));
});

test("固定UI路径和网页iframe来源校验，拒绝相似路径、网页伪造与站外嵌入", async () => {
  const value = backgroundFixture();
  for (const url of ["chrome-extension://test-extension/src/ui.html.evil", "chrome-extension://test-extension/src/privacy.html", "https://h.liepin.com/chrome-extension://test-extension/src/ui.html", "chrome-extension://other/src/ui.html", "chrome-extension://test-extension/src/ui.html?surface=evil", "chrome-extension://test-extension/src/ui.html?surface=floating", "chrome-extension://test-extension/src/floating.html.evil", "chrome-extension://test-extension/src/floating.html?surface=floating", "chrome-extension://other/src/floating.html"]) {
    assert.equal(await value.message({ type: "STATE" }, { ...uiSender, url }), undefined);
  }
  const iframe = { ...floatingSender, frameId: 3, tab: { id: 3, url: "https://evil.example/" } };
  assert.equal(await value.message({ type: "STATE" }, iframe), undefined); assert.equal(value.requests.length, 0);
});

test("独立悬浮界面不能登录、配对、退出或改变资料授权，工具栏仍可设置授权", async () => {
  const value = backgroundFixture(), sender = { ...floatingSender, frameId: 1, tab: { id: 13, url: "https://h.liepin.com/resume/showresumedetail" } };
  for (const type of ["LOGIN", "SEND_CODE", "CONSENT", "PAIR_START", "PAIR_CHECK", "RESTORE", "LOGOUT", "REVOKE_RETRY", "OPEN_SIDEBAR"]) {
    assert.equal(await value.message({ type, payload: { accepted: true, email: "other@example.test", password: "synthetic", agreement_accepted: true } }, sender), undefined, type);
  }
  assert.equal(value.values.data_use_accepted, undefined); assert.equal(value.values.session.user.id, "fixture"); assert.equal(value.requests.length, 0); assert.equal(value.versionRequests.length, 0);
  const accepted = await value.message({ type: "CONSENT", payload: { accepted: true } }, uiSender);
  assert.equal(accepted.ok, true); assert.equal(value.values.data_use_accepted, true); assert.equal(value.requests.length, 0);
});

test("入库自动关闭默认开启并持久化，只有工具栏能设置且只向来源页主框架发收起请求", async () => {
  const value = backgroundFixture(), tab = { id: 31, url: "https://h.liepin.com/resume/showresumedetail" }, sender = { ...floatingSender, frameId: 2, tab };
  const initial = await value.message({ type: "STATE" }, uiSender); assert.equal(initial.data.auto_close_after_save, true);
  assert.equal(await value.message({ type: "SET_AUTO_CLOSE", payload: { enabled: false } }, sender), undefined); assert.equal(value.values.auto_close_after_save, undefined);
  const disabled = await value.message({ type: "SET_AUTO_CLOSE", payload: { enabled: false } }, uiSender); assert.equal(disabled.ok, true); assert.equal(value.values.auto_close_after_save, false);
  const restored = await value.message({ type: "STATE" }, uiSender); assert.equal(restored.data.auto_close_after_save, false);
  const deliveries = []; value.extension.tabs.sendMessage = async (...args) => { deliveries.push(args); return { closed: true }; };
  const skipped = await value.message({ type: "CLOSE_FLOATING", payload: { identity: "disabled" } }, sender); assert.equal(skipped.data.closed, false); assert.equal(deliveries.length, 0);
  await value.message({ type: "SET_AUTO_CLOSE", payload: { enabled: true } }, uiSender);
  const longIdentity = JSON.stringify(["hliepin", "fixture", `https://h.liepin.com/${"a".repeat(700)}`, "合成长链接候选人"]);
  const closed = await value.message({ type: "CLOSE_FLOATING", payload: { identity: longIdentity } }, sender); assert.equal(closed.data.closed, true);
  assert.equal(deliveries.length, 1); assert.equal(deliveries[0][0], tab.id); assert.equal(deliveries[0][1].type, "CLOSE_FLOATING"); assert.equal(deliveries[0][1].identity, longIdentity); assert.equal(deliveries[0][2].frameId, 0);
});

test("悬浮UI读取和头像绑定自身tab，活动tab变化不串人，旧快照不能新增", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const tabA = { id: 23, url: "https://h.liepin.com/resume/showresumedetail" }, tabB = { id: 24, url: "https://rd6.zhaopin.com/app" };
  let resumeA = { candidate_name: "合成悬浮甲", phone: "13800000101", platform_id: "hliepin", source_url: tabA.url, avatar_source_url: "https://image0.lietou-static.com/fixture.png" };
  const resumeB = { candidate_name: "合成活动乙", phone: "13800000102", platform_id: "zhaopin", source_url: tabB.url };
  value.extension.tabs.query = async () => [tabB]; value.extension.tabs.get = async (id) => id === tabA.id ? tabA : tabB;
  value.extension.tabs.sendMessage = async (id) => value.message({ type: "CANDIDATE_SNAPSHOT", resume: id === tabA.id ? resumeA : resumeB }, { id: "test-extension", frameId: 0, tab: id === tabA.id ? tabA : tabB });
  const sender = { ...floatingSender, frameId: 1, tab: tabA };
  const read = await value.message({ type: "PREVIEW", payload: { tab_id: tabB.id } }, sender);
  assert.equal(read.data.resume.candidate_name, "合成悬浮甲");
  const popup = await value.message({ type: "PREVIEW" }, uiSender); assert.equal(popup.data.resume.candidate_name, "合成活动乙");
  let imagePage; value.context.GoodHRAvatar.fetchAvatar = async (_source, pageURL) => { imagePage = pageURL; return "data:image/png;base64,YQ=="; };
  const created = await value.message({ type: "CREATE", payload: { resume: read.data.resume, capture_id: read.data.resume.capture_id, idempotency_key: "floating-synthetic-create" } }, sender);
  assert.equal(created.ok, true); assert.equal(imagePage, tabA.url);
  resumeA = { ...resumeA, candidate_name: "合成悬浮新候选人", phone: "13800000103" };
  const stale = await value.message({ type: "CREATE", payload: { resume: read.data.resume, capture_id: read.data.resume.capture_id, idempotency_key: "floating-stale" } }, sender);
  assert.equal(stale.ok, false); assert.equal(stale.code, "CAPTURE_SCOPE_CHANGED");
  assert.equal(value.requests.filter(({ url }) => url.endsWith("/api/candidates")).length, 1);
  value.values.data_use_accepted = false;
});

test("悬浮面板头像读取期间切人会终止CREATE，既有简历不被新头像串写", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const tab = { id: 51, url: "https://h.liepin.com/resume/showresumedetail" }; let candidate = { candidate_name: "合成旧候选人", phone: "13800000121", platform_id: "hliepin", source_url: tab.url, avatar_source_url: "https://image0.lietou-static.com/fixture.png" }, release, started;
  const startedPromise = new Promise((resolve) => { started = resolve; });
  value.extension.tabs.sendMessage = async () => value.message({ type: "CANDIDATE_SNAPSHOT", resume: candidate }, { id: "test-extension", frameId: 0, tab });
  value.context.GoodHRAvatar.fetchAvatar = async () => { started(); return new Promise((resolve) => { release = resolve; }); };
  const sender = { ...floatingSender, frameId: 2, tab };
  const snapshot = (await value.message({ type: "PREVIEW" }, sender)).data.resume;
  const saving = value.message({ type: "CREATE", payload: { resume: snapshot, capture_id: snapshot.capture_id, idempotency_key: "change-during-image" } }, sender);
  await startedPromise; candidate = { ...candidate, candidate_name: "合成新候选人", phone: "13800000122" }; release("data:image/png;base64,YQ==");
  const result = await saving; assert.equal(result.ok, false); assert.equal(result.code, "CAPTURE_SCOPE_CHANGED");
  assert.equal(value.requests.some(({ url }) => url.endsWith("/api/candidates")), false);
  value.values.data_use_accepted = false;
});

test("内容脚本不能调用登录、查重、保存或读取凭证接口", async () => {
  const value = backgroundFixture(), sender = { id: "test-extension", frameId: 0, tab: { id: 4, url: "https://h.liepin.com/resume/showresumedetail" } };
  for (const type of ["STATE", "PREVIEW", "LOGIN", "CREATE", "AVATAR_RETRY", "DUPLICATES", "RESTORE", "FOLLOWUPS", "LOGOUT", "REVOKE_RETRY", "DRAFT_GET", "DRAFT_SAVE", "CHECK_UPDATE", "SET_AUTO_CLOSE", "CLOSE_FLOATING", "SUMMARY_START", "SUMMARY_SAVE"]) assert.equal(await value.message({ type, payload: {} }, sender), undefined);
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

test("已同意捕获后自动查重合并相同请求，不把头像或履历全文发到查重", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true; const resume = await avatarScene(value);
  const explicit = await value.message({ type: "DUPLICATES", payload: { resume } }, uiSender); assert.equal(explicit.ok, true);
  await new Promise((resolve) => setTimeout(resolve, 550)); assert.equal(value.requests.length, 1);
  const input = JSON.parse(value.requests[0].options.body); assert.equal(input.avatar_data_url, undefined); assert.ok(Array.isArray(input.work_experiences)); assert.ok(Array.isArray(input.educations));
  delete value.values.session; value.requests.length = 0; await avatarScene(value); await new Promise((resolve) => setTimeout(resolve, 550)); assert.equal(value.requests.length, 0);
});

test("本地草稿24小时和30条限制，切账号不能读取旧人草稿且并发写不丢人", async () => {
  const value = backgroundFixture(), account = JSON.stringify(["fixture", "fixture@example.test"]);
  await Promise.all(["synthetic-a", "synthetic-b"].map((identity) => value.message({ type: "DRAFT_SAVE", payload: { identity, account_key: account, draft: { fields: { note: identity }, edited: ["note"], keys: { note: "same-note" } } } }, uiSender)));
  assert.equal(Object.keys(value.values.ui_drafts).length, 2);
  const loaded = await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic-a", account_key: account } }, uiSender); assert.equal(loaded.data.draft.fields.note, "synthetic-a"); assert.equal(value.requests.length, 0);
  value.values.session.user = { id: "other", email: "other@example.test" };
  const stale = await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic-a", account_key: account } }, uiSender); assert.equal(stale.ok, false);
  const other = await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic-a", account_key: JSON.stringify(["other", "other@example.test"]) } }, uiSender); assert.equal(other.data.draft, null);
  const staleSave = await value.message({ type: "CREATE", payload: { account_key: account, resume: { candidate_name: "旧人", phone: "13800000001" }, idempotency_key: "old" } }, uiSender); assert.equal(staleSave.ok, false); assert.equal(value.requests.length, 0);
});

test("冻结摘要单份不超过1MiB，总草稿UTF8不超过3MiB且优先保留当前候选人", async () => {
  const value = backgroundFixture(), account = JSON.stringify(["fixture", "fixture@example.test"]);
  const source = { work_experiences: Array.from({ length: 30 }, () => ({ company_name: "合成公司", content: "测".repeat(5000) })) };
  for (let index = 0; index < 9; index++) { const result = await value.message({ type: "DRAFT_SAVE", payload: { account_key: account, identity: `budget-${index}`, draft: { summary_source: source, summary_unknown: true } } }, uiSender); assert.equal(result.ok, true); }
  assert.ok(new TextEncoder().encode(JSON.stringify(value.values.ui_drafts)).byteLength <= 3 * 1024 * 1024); assert.ok(Object.keys(value.values.ui_drafts).length < 9); assert.equal(value.values.ui_drafts[JSON.stringify([account, "budget-8"])].data.summary_unknown, true);
  const huge = { work_experiences: Array.from({ length: 40 }, () => ({ content: "测".repeat(6000) })), project_experiences: Array.from({ length: 40 }, () => ({ content: "测".repeat(6000) })) };
  const rejected = await value.message({ type: "DRAFT_SAVE", payload: { account_key: account, identity: "too-large", draft: { summary_source: huge } } }, uiSender); assert.equal(rejected.ok, false); assert.match(rejected.message, /1MiB/); assert.equal(value.values.ui_drafts[JSON.stringify([account, "too-large"])], undefined);
});

test("本地AI仅配对授权且去联系方式，任务不执行云AI；摘要单独带版本PATCH", async () => {
  const value = backgroundFixture((url) => response(url.includes("resume-summary") ? { ok: true, data: { task: { id: "synthetic-task", status: "completed", ai_summary: "合成总结" } } } : { ok: true, candidate: { id: "synthetic" } })); value.values.data_use_accepted = true;
  const payload = { resume: { candidate_name: "合成姓名", phone: "13800000001", email: "fake@example.test", work_experiences: [{ company_name: "合成履历公司" }] }, request_key: "same-ai" };
  const unavailable = await value.message({ type: "SUMMARY_START", payload }, uiSender); assert.equal(unavailable.ok, false); assert.match(unavailable.message, /配对/); assert.equal(value.requests.length, 0);
  value.values.installation_id = "fixture-installation"; value.values.pairing_token = "private-grant"; value.values.pairing_account_key = JSON.stringify(["fixture", "fixture@example.test"]);
  const result = await value.message({ type: "SUMMARY_START", payload }, uiSender); assert.equal(result.data.task.ai_summary, "合成总结"); assert.equal(value.requests[0].url, "http://127.0.0.1:43129/api/v1/extensions/resume-summary");
  const body = JSON.parse(value.requests[0].options.body); assert.equal(body.pairing_token, "private-grant"); assert.equal(body.candidate.phone, undefined); assert.equal(body.candidate.candidate_name, undefined); assert.equal(body.candidate.work_experiences[0].company_name, "合成履历公司");
  await value.message({ type: "SUMMARY_SAVE", payload: { candidate_id: "synthetic", row_version: 8, ai_summary: "合成总结", idempotency_key: "summary-save" } }, uiSender);
  assert.equal(value.requests[1].options.method, "PATCH"); assert.deepEqual(JSON.parse(value.requests[1].options.body), { ai_summary: "合成总结", row_version: 8, idempotency_key: "summary-save" });
  value.values.session.user = { id: "new-account", email: "other@example.test" }; const stalePair = await value.message({ type: "SUMMARY_START", payload }, uiSender); assert.equal(stalePair.ok, false); assert.match(stalePair.message, /同账号/); assert.equal(value.requests.length, 2);
});

test("AI整理前拒绝预留ID和内置只读标记，不调用本地任务且不按真实同名同号拒绝", async () => {
  const value = backgroundFixture((url) => response({ ok: true, data: { task: { id: "synthetic-writable-task", status: "queued" } } }));
  Object.assign(value.values, { data_use_accepted: true, installation_id: "fixture-install", pairing_token: "private-grant", pairing_account_key: JSON.stringify(["fixture", "fixture@example.test"]) });
  const resume = { candidate_name: "合成同名候选人", phone: "13800000181", work_experiences: [{ company_name: "合成公司" }] }, builtinID = "00000000-0000-4000-8000-000000000001";
  for (const marker of [{ id: builtinID }, { candidate_id: builtinID }, { platform_candidate_id: builtinID }, { is_builtin: true }, { is_builtin: false }, { readonly: true }, { readonly: false }, { builtin_notice: "合成只读" }, { source_type: "builtin" }]) {
    const result = await value.message({ type: "SUMMARY_START", payload: { resume: { ...resume, ...marker }, request_key: "blocked-built-in" } }, uiSender);
    assert.equal(result.ok, false); assert.match(result.message, /内置示例/); assert.equal(value.requests.length, 0);
  }
  const allowed = await value.message({ type: "SUMMARY_START", payload: { resume, request_key: "same-contacts-real-candidate" } }, uiSender);
  assert.equal(allowed.ok, true); assert.equal(value.requests.length, 1); assert.equal(allowed.data.task.id, "synthetic-writable-task");
});

test("同账号手动登录可配对并撤销旧token，不同账号新授权被撤销且不悄悄切号", async () => {
  for (const same of [true, false]) {
    const user = same ? { id: "fixture", email: "fixture@example.test" } : { id: "other", email: "other@example.test" };
    const value = backgroundFixture((url) => response(url.endsWith("/pair/status") ? { ok: true, data: { status: "approved", token: "new-pair-token", pairing_token: "new-pair-grant", user, expires_at: "2030-01-01" } } : { ok: true }));
    value.values.pending_pair = { installation_id: "installation", pairing_id: "pair", challenge: "synthetic-challenge", expires_at: Date.now() + 30000 };
    const result = await value.message({ type: "PAIR_CHECK" }, uiSender);
    assert.equal(result.ok, same); assert.equal(value.values.pending_pair, undefined); assert.equal(value.values.pending_revoke, undefined);
    const logoutCalls = value.requests.filter((item) => item.url.endsWith("/extension-logout")); assert.equal(logoutCalls.length, 1);
    if (same) { assert.equal(value.values.session.token, "new-pair-token"); assert.equal(value.values.pairing_token, "new-pair-grant"); assert.equal(value.values.pairing_account_key, JSON.stringify(["fixture", "fixture@example.test"])); assert.equal(logoutCalls[0].options.headers.Authorization, "Bearer private-background-token"); }
    else { assert.equal(value.values.session.token, "private-background-token"); assert.equal(value.values.pairing_token, undefined); assert.match(result.message, /本地和插件账号不同/); assert.equal(logoutCalls[0].options.headers.Authorization, "Bearer new-pair-token"); assert.equal(value.requests.some((item) => item.url.endsWith("/pair/revoke")), true); }
  }
});

test("本地AI返回前退出丢弃旧摘要，不向新账号展示", async () => {
  let release, started; const captured = new Promise((resolve) => { started = resolve; });
  const value = backgroundFixture(async (url) => { if (url.endsWith("/resume-summary")) { started(); return new Promise((resolve) => { release = () => resolve(response({ ok: true, data: { task: { id: "task", status: "completed", ai_summary: "旧账号合成摘要" } } })); }); } return response({ ok: true }); });
  value.values.data_use_accepted = true; value.values.installation_id = "installation"; value.values.pairing_token = "grant"; value.values.pairing_account_key = JSON.stringify(["fixture", "fixture@example.test"]);
  const pending = value.message({ type: "SUMMARY_START", payload: { request_key: "ai", resume: { work_experiences: [{ company_name: "合成公司" }] } } }, uiSender); await captured; await value.message({ type: "LOGOUT" }, uiSender); release(); const result = await pending; assert.equal(result.ok, false); assert.match(result.message, /账号已经变化/); assert.equal(JSON.stringify(result).includes("旧账号合成摘要"), false);
});

test("业务请求仅向生产 HTTPS 域名，凭证在后台 Bearer 头且不走招聘页面", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const result = await value.message({ type: "DUPLICATES", payload: { resume: { candidate_name: "测试", phone: "13812345678", platform_id: "hliepin" } } }, { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" });
  assert.equal(result.ok, true); assert.equal(value.requests.length, 1); assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates/duplicates"); assert.equal(value.requests[0].options.headers.Authorization, "Bearer private-background-token"); assert.equal(value.requests[0].options.redirect, "error");
});

test("备注和推荐并发401只消费旧token一次，两个请求共用新session", async () => {
  let refreshCount = 0;
  const value = backgroundFixture(async (url, options) => {
    if (url.endsWith("/extension-refresh")) { refreshCount++; await new Promise((resolve) => setTimeout(resolve, 20)); return refreshCount === 1 ? response({ token: "private-new-token", user: { id: "fixture", email: "fixture@example.test" } }) : response({ message: "old token consumed" }, 401); }
    if (options.headers.Authorization === "Bearer private-background-token") return response({ message: "expired" }, 401);
    return response(url.endsWith("/notes") ? { ok: true, notes: [{ content: "合成备注" }] } : { ok: true, recommendations: [{ id: "recommendation" }] });
  }); value.values.data_use_accepted = true;
  const result = await value.message({ type: "DETAIL", payload: { candidate_id: "synthetic" } }, uiSender);
  assert.equal(result.ok, true); assert.equal(refreshCount, 1); assert.equal(result.data.notes.length, 1); assert.equal(result.data.recommendations.length, 1); assert.equal(result.data.notes_error, undefined); assert.equal(result.data.recommendations_error, undefined); assert.equal(value.values.session.token, "private-new-token");
});

test("并发续期途中退出或撤销同意不恢复旧会话，迟到token仅进入撤销队列", async () => {
  for (const type of ["LOGOUT", "CONSENT"]) {
    let release, started; const active = new Promise((resolve) => { started = resolve; }), wait = new Promise((resolve) => { release = resolve; });
    const value = backgroundFixture(async (url, options) => {
      if (url.endsWith("/extension-refresh")) { started(); await wait; return response({ token: "private-delayed-refresh", user: { id: "fixture", email: "fixture@example.test" } }); }
      if (url.includes("/api/candidates/") && options.headers.Authorization === "Bearer private-background-token") return response({ message: "expired" }, 401);
      return response({ ok: true });
    }); value.values.data_use_accepted = true;
    const pending = value.message({ type: "DETAIL", payload: { candidate_id: "synthetic" } }, uiSender); await active; await value.message({ type, payload: { accepted: false } }, uiSender); release(); const result = await pending;
    assert.equal(result.ok, false); assert.notEqual(value.values.session?.token, "private-delayed-refresh"); assert.equal(value.values.pending_revoke.cloud_tokens.includes("private-delayed-refresh"), true); assert.equal(value.requests.filter((request) => request.url.endsWith("/extension-refresh")).length, 1);
  }
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

test("头像等待期间退出、切号或撤销同意会取消CREATE及头像PATCH", async () => {
  for (const type of ["CREATE", "AVATAR_RETRY"]) for (const switchType of ["LOGOUT", "LOGIN", "CONSENT"]) {
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
