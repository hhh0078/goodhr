/* 文件作用：验证招聘脚本只读、消息权限隔离、受限凭证不泄漏及配对地址白名单。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), vm = require("node:vm"), crypto = require("node:crypto");
const src = path.join(__dirname, "..", "src");

/** backgroundFixture 构造后台执行环境和无真实网络的受限会话。 */
function backgroundFixture(fetchReply) {
  const values = { session: { token: "private-background-token", user: { email: "fixture@example.test", id: "fixture" }, expires_at: "2030-01-01" } }, requests = [], versionRequests = []; let listener, connectListener;
  const extension = {
    runtime: { id: "test-extension", getURL: (value) => "chrome-extension://test-extension/" + value, getManifest: () => ({ version: require("../package.json").version }), onMessage: { addListener: (handler) => { listener = handler; } }, onConnect: { addListener: (handler) => { connectListener = handler; } } },
    storage: { local: { get: async (keys) => Object.fromEntries(keys.filter((key) => key in values).map((key) => [key, values[key]])), set: async (input) => Object.assign(values, input), remove: async (keys) => { for (const key of Array.isArray(keys) ? keys : [keys]) delete values[key]; }, setAccessLevel: async () => {} } },
    tabs: { query: async () => [], create: async () => ({}), onRemoved: { addListener: () => {} }, onUpdated: { addListener: () => {} } },
    action: { setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {} }
  };
  const context = vm.createContext({ chrome: extension, URL, AbortSignal, crypto: crypto.webcrypto, Uint8Array, TextEncoder, console, setTimeout, clearTimeout, fetch: async (url, options) => { if (new URL(url).pathname === "/api/system/browser-extension") { versionRequests.push({ url, options }); return response({ release: { version: extension.runtime.getManifest().version } }); } requests.push({ url, options }); return fetchReply ? fetchReply(url, options) : response({ ok: true, matches: [], token: "refreshed", user: { email: "fixture@example.test" } }); } });
  for (const file of ["shared.js", "avatar.js", "boss-vue-avatar.js", "background.js"]) vm.runInContext(fs.readFileSync(path.join(src, file), "utf8"), context);
  return { values, requests, versionRequests, context, extension, connect: (port) => connectListener(port), message: (message, sender) => new Promise((resolve) => { const result = listener(message, sender, resolve); if (result === false) resolve(undefined); }) };
}

test("卡片成本短连接只接受可信扩展iframe，收起信号仅取消同tab且不请求网络", async () => {
  const value = backgroundFixture(), tabA = { id: 70, url: "https://www.zhipin.com/web/chat/index" }, tabB = { ...tabA, id: 71 };
  /** portFixture 只记录本插件内部取消信号，不连接真实浏览器。 */
  function portFixture(sender) { return { name: "goodhr-cost-confirmation", sender, messages: [], disconnected: false, onDisconnect: { addListener: () => {} }, disconnect() { this.disconnected = true; }, postMessage(message) { this.messages.push(message); } }; }
  const first = portFixture({ ...floatingSender, frameId: 2, tab: tabA }), second = portFixture({ ...floatingSender, frameId: 2, tab: tabB }); value.connect(first); value.connect(second);
  for (const sender of [{ id: "test-extension", url: tabA.url, frameId: 0, tab: tabA }, { ...floatingSender, frameId: 0, tab: tabA }, { ...floatingSender, frameId: 2, tab: { id: 72, url: "https://example.com/" } }]) { const port = portFixture(sender); value.connect(port); assert.equal(port.disconnected, true); }
  await value.message({ type: "FLOATING_CLOSED" }, { id: "test-extension", frameId: 0, tab: tabA });
  assert.equal(first.messages.length, 1); assert.equal(first.messages[0].type, "CANCEL_COST_CONFIRMATION"); assert.equal(second.messages.length, 0);
  for (const sender of [{ id: "test-extension", frameId: 2, tab: tabB }, { id: "other-extension", frameId: 0, tab: tabB }, { id: "test-extension", frameId: 0, tab: { id: 71, url: "https://example.com/" } }]) await value.message({ type: "FLOATING_CLOSED" }, sender);
  assert.equal(second.messages.length, 0); assert.equal(value.requests.length, 0); assert.equal(value.versionRequests.length, 0);
});

/** response 构造明确状态码的 JSON 响应，避免真实外部请求。 */
function response(body = { ok: true }, status = 200) { return { ok: status >= 200 && status < 300, status, json: async () => body }; }
const uiSender = { id: "test-extension", url: "chrome-extension://test-extension/src/ui.html" };
const floatingSender = { id: "test-extension", url: "chrome-extension://test-extension/src/floating.html" };

/** attachmentScene 创建可变的合成 BOSS 页面，后台只从受信内容脚本取得原文。 */
async function attachmentScene(value) {
  const tab = { id: 57, url: "https://www.zhipin.com/web/chat/index?token=must-not-upload" }, account = JSON.stringify(["fixture", "fixture@example.test"]), sender = { ...floatingSender, frameId: 2, tab };
  const scene = { tab, sender, account, raw: null };
  /** replaceText 用新的合成附件替换页面，验证迟到任务与当前捕获隔离。 */
  scene.replaceText = async (text = "合成附件正文\n姓名和联系方式仅用于测试", overrides = {}) => { const text_hash = await crypto.webcrypto.subtle.digest("SHA-256", new TextEncoder().encode(text)); scene.raw = { platform_id: "boss", source_url: tab.url, attachment: { text, text_hash: Buffer.from(text_hash).toString("hex"), page_count: 1, total_pages: 0, complete: false, blocked: false, ...overrides } }; };
  await scene.replaceText(); value.values.data_use_accepted = true;
  value.extension.tabs.query = async () => [tab]; value.extension.tabs.get = async () => tab;
  value.extension.tabs.sendMessage = async () => value.message({ type: "CANDIDATE_SNAPSHOT", resume: scene.raw }, { id: "test-extension", frameId: 0, tab });
  const state = await value.message({ type: "PREVIEW" }, uiSender); scene.capture = state.data.resume;
  scene.payload = (phase = "basic") => ({ account_key: account, capture_id: scene.capture.capture_id, phase, acknowledged: true, request_key: "synthetic-" + phase + "-request" });
  return scene;
}

/** importInput 创建不含全文的人工核对请求，全文只能由后台绑定的内容脚本补入。 */
function importInput(scene, extra = {}) { return { request_key: "synthetic-import-0001", capture_id: scene.capture.capture_id, operation: "create", review: { candidate_name: "人工核对姓名", phone: "13800000991", gender: "female" }, review_fields: { "first-company": "人工核对首份公司", "first-work-start": "2017-01", "first-work-end": "", "first-school": "人工核对学校", "first-school-start": "2010-09", "first-school-end": "2014-06" }, edited_fields: ["name", "phone", "first-work-end"], folder_ids: ["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"], ...extra }; }

test("BOSS 基本识别自动提交可信纯文本，去参数来源；UI/磁盘回执不复制原文，缺页仍不能提交", async () => {
  const value = backgroundFixture((url, options) => {
    const body = JSON.parse(options.body); return response({ ok: true, task: { ...body, id: "parse-basic", status: "completed", resume: { candidate_name: "合成候选人", phone: "13800000991" } } });
  }), scene = await attachmentScene(value), payload = scene.payload();
  assert.equal(Object.hasOwn(scene.capture.attachment, "text"), false); assert.equal(value.requests.length, 0);
  await scene.replaceText(undefined, { blocked: true });
  const rejected = await value.message({ type: "ATTACHMENT_START", payload }, scene.sender); assert.equal(rejected.ok, false); assert.equal(value.requests.length, 0);
  await scene.replaceText();
  const result = await value.message({ type: "ATTACHMENT_START", payload: { ...payload, acknowledged: false, text: "UI 伪造正文", html: "<html>拒绝</html>" } }, scene.sender);
  assert.equal(result.ok, true); assert.equal(result.data.task.resume.phone, "13800000991"); assert.equal(JSON.stringify(result).includes(scene.raw.attachment.text), false);
  const body = JSON.parse(value.requests[0].options.body); assert.equal(body.text, scene.raw.attachment.text); assert.equal(body.phase, "basic"); assert.equal(body.source.source_url, "https://www.zhipin.com/web/chat/index"); assert.equal(Object.hasOwn(body, "html"), false); assert.equal(Object.hasOwn(body, "cookies"), false);
  assert.equal(value.requests[0].options.headers.Authorization, "Bearer private-background-token"); assert.equal(value.requests[0].options.credentials, "omit");
  assert.equal((await value.message({ type: "ATTACHMENT_START", payload: { ...payload, account_key: "other" } }, scene.sender)).ok, false);
  assert.equal((await value.message({ type: "CREATE", payload: { ...payload, resume: { candidate_name: "合成候选人", phone: "13800000991" }, idempotency_key: "no-full-yet" } }, scene.sender)).ok, false);
  assert.equal((await value.message({ type: "CREATE", payload: { ...payload, resume: { candidate_name: "合成候选人", phone: "13800000991" }, idempotency_key: "toolbar-no-full-yet" } }, uiSender)).ok, false, "无 sender.tab 的工具栏不能绕过完整解析检查");
});

test("BOSS 添加只投递后台保存任务，可信全文及人工字段冻结，202回执不泄漏完整结果", async () => {
  const value = backgroundFixture((url, options) => response({ task: { ...JSON.parse(options.body), id: "synthetic-import", status: "queued", stage: "queued", resume: { personal_description: "私有完整结果不能给UI" } } }, 202)), scene = await attachmentScene(value);
  const forbidden = await value.message({ type: "ATTACHMENT_START", payload: scene.payload("full") }, scene.sender); assert.equal(forbidden.ok, false); assert.equal(value.requests.length, 0);
  const request = importInput(scene, { text: "UI伪造正文", source: { source_url: "https://evil.test" }, full_task_id: "legacy-completed-full", note: { content: "合成备注" }, recommendation: { position_id: "synthetic-position", stage: "interview", content: "合成岗位记录" } });
  const saved = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender); assert.equal(saved.ok, true); assert.equal(saved.data.task.status, "queued"); assert.equal(saved.data.task.resume, undefined);
  const body = JSON.parse(value.requests[0].options.body); assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates/attachment-imports");
  assert.equal(body.text, scene.raw.attachment.text); assert.equal(body.source.source_url, "https://www.zhipin.com/web/chat/index"); assert.equal(body.review.candidate_name, "人工核对姓名"); assert.equal(body.review.gender, "female"); assert.equal(body.review_fields["first-work-end"], ""); assert.equal(body.full_task_id, "legacy-completed-full"); assert.deepEqual(body.folder_ids, request.folder_ids); assert.equal(body.note.content, request.note.content); assert.equal(body.recommendation.stage, "interview");
  assert.equal(JSON.stringify(saved).includes(scene.raw.attachment.text), false); assert.equal(JSON.stringify(saved).includes("私有完整结果"), false);
  const savedDraft = await value.message({ type: "DRAFT_SAVE", payload: { identity: "synthetic-attachment", account_key: scene.account, draft: { attachment: { capture_id: request.capture_id, save_request: request, save_task: { ...saved.data.task, resume: { raw_text: "不得落盘" } } } } } }, scene.sender); assert.equal(savedDraft.ok, true);
  const draft = Object.values(value.values.ui_drafts)[0].data.attachment; assert.equal(Object.hasOwn(draft.save_task, "resume"), false); assert.equal(Object.hasOwn(draft.save_request, "text"), false);
});

test("BOSS 切人后迟到结果拒绝展示，可信任务仍可取消；别人的任务编号不能代取消", async () => {
  let release, original;
  const value = backgroundFixture(async (url, options) => {
    if (url.endsWith("/cancel")) return response({ task: { ...original, status: "cancelled" } });
    original = { ...JSON.parse(options.body), id: "late-basic", status: "running" }; await new Promise((resolve) => { release = resolve; }); return response({ task: original });
  }), scene = await attachmentScene(value), payload = scene.payload();
  const pending = value.message({ type: "ATTACHMENT_START", payload }, scene.sender);
  while (!release) await new Promise((resolve) => setTimeout(resolve, 1));
  await scene.replaceText("另一份合成附件，不能被旧结果填入"); release();
  const late = await pending; assert.equal(late.ok, false); assert.equal(late.code, "CAPTURE_SCOPE_CHANGED");
  const cancelled = await value.message({ type: "ATTACHMENT_CANCEL", payload: { ...payload, task_id: "late-basic" } }, scene.sender); assert.equal(cancelled.ok, true); assert.equal(cancelled.data.task.status, "cancelled");
  assert.equal((await value.message({ type: "ATTACHMENT_CANCEL", payload: { ...payload, task_id: "unknown-other-task" } }, scene.sender)).ok, false);
});

test("BOSS 未配置 AI 返回明确错误码；文本哈希与任务内容不符均停止且不自动重发", async () => {
  const value = backgroundFixture(() => response({ ok: false, error: "请先配置当前 AI", error_code: "AI_NOT_CONFIGURED" }, 422)), scene = await attachmentScene(value);
  const result = await value.message({ type: "ATTACHMENT_START", payload: scene.payload() }, scene.sender); assert.equal(result.ok, false); assert.equal(result.code, "AI_NOT_CONFIGURED"); assert.equal(value.requests.length, 1);
  scene.raw.attachment.text_hash = "a".repeat(64); const preview = await value.message({ type: "PREVIEW" }, uiSender); scene.capture = preview.data.resume;
  const mismatch = await value.message({ type: "ATTACHMENT_START", payload: scene.payload() }, scene.sender); assert.equal(mismatch.ok, false); assert.match(mismatch.message, /校验/); assert.equal(value.requests.length, 1);
});

test("BOSS 图片源只留后台，头像变更独立于正文并由后台重算指纹，忽略聊天头像外链", async () => {
  const value = backgroundFixture(), scene = await attachmentScene(value);
  scene.raw.avatar_source_url = "https://www.zhipin.com/wrong-chat-avatar.png";
  scene.raw.avatar_candidates = [{ source_url: "/photo-a.png", width: 120, height: 160 }]; scene.raw.attachment.avatar_hash = "UI cannot supply the fingerprint";
  const first = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
  assert.equal(first.avatar_detected, true); assert.equal(first.avatar_candidates, undefined); assert.equal(first.avatar_source_url, undefined); assert.equal(JSON.stringify(first).includes("photo-a"), false); assert.equal(first.capture_id.length <= 160, true);
  scene.raw.avatar_candidates[0].source_url = "/photo-b.png";
  const second = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
  assert.equal(first.attachment.text_hash, second.attachment.text_hash); assert.notEqual(first.attachment.avatar_hash, second.attachment.avatar_hash); assert.notEqual(first.capture_id, second.capture_id); assert.equal(value.requests.length, 0);
  scene.raw.avatar_candidates = []; const none = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume; assert.equal(none.avatar_detected, false, "不能回退到聊天/列表头像");
});

test("BOSS 旧图片指纹异步迟到不能覆盖更新后的附件快照", async () => {
  const value = backgroundFixture(), scene = await attachmentScene(value), originalHash = value.context.GoodHRShared.hashText;
  let release, started; const active = new Promise((resolve) => { started = resolve; });
  value.context.GoodHRShared.hashText = async (text) => { if (text.includes("photo-delayed")) { started(); await new Promise((resolve) => { release = resolve; }); } return originalHash(text); };
  const contentSender = { id: "test-extension", frameId: 0, tab: scene.tab };
  const pending = value.message({ type: "CANDIDATE_SNAPSHOT", resume: { ...scene.raw, avatar_candidates: [{ source_url: "/photo-delayed.png", width: 120, height: 160 }] } }, contentSender);
  await active;
  const current = await value.message({ type: "CANDIDATE_SNAPSHOT", resume: { ...scene.raw, avatar_candidates: [{ source_url: "/photo-current.png", width: 120, height: 160 }] } }, contentSender); assert.equal(current.ok, true);
  release(); assert.equal((await pending).ok, false);
  const stored = vm.runInContext("snapshots.get(57)", value.context); assert.match(stored.avatar_candidates[0].source_url, /photo-current/); assert.equal(JSON.stringify(vm.runInContext("publicResume(snapshots.get(57))", value.context)).includes("photo-current"), false);
});

test("BOSS 同文字换头像在下载前拒绝旧捕获，下载期间变更也不PATCH已有候选人", async () => {
  const value = backgroundFixture(), scene = await attachmentScene(value);
  scene.raw.avatar_candidates = [{ source_url: "/photo-a.png", width: 120, height: 160 }];
  const first = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
  let calls = 0, release, started; const active = new Promise((resolve) => { started = resolve; });
  value.context.GoodHRAvatar.fetchAttachmentAvatar = async () => { calls++; started(); return new Promise((resolve) => { release = resolve; }); };
  const payload = { candidate_id: "synthetic-avatar-owner", row_version: 1, capture_id: first.capture_id, idempotency_key: "synthetic-boss-avatar" };
  scene.raw.avatar_candidates[0].source_url = "/photo-b.png";
  const stale = await value.message({ type: "AVATAR_RETRY", payload }, scene.sender); assert.equal(stale.ok, false); assert.equal(stale.code, "CAPTURE_SCOPE_CHANGED"); assert.equal(calls, 0);
  const current = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
  const pending = value.message({ type: "AVATAR_RETRY", payload: { ...payload, capture_id: current.capture_id } }, scene.sender);
  await active; scene.raw.avatar_candidates[0].source_url = "/photo-c.png"; release("data:image/png;base64,AAAA");
  const result = await pending; assert.equal(result.ok, false); assert.equal(result.code, "CAPTURE_SCOPE_CHANGED"); assert.equal(value.requests.length, 0);
});

test("BOSS 无独立图片仍可投递保存，只有当前唯一图片随任务存成内嵌头像", async () => {
  for (const withPhoto of [false, true]) {
    const value = backgroundFixture((url, options) => response({ task: { ...JSON.parse(options.body), id: "synthetic-photo-import", status: "queued" } }, 202)), scene = await attachmentScene(value);
    let calls = 0; value.context.GoodHRAvatar.fetchAttachmentAvatar = async () => { calls++; return "data:image/png;base64,AAAA"; };
    scene.raw.avatar_candidates = withPhoto ? [{ source_url: "/photo.png", width: 120, height: 160 }] : [];
    scene.capture = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
    const request = importInput(scene), saved = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender); assert.equal(saved.ok, true);
    const body = JSON.parse(value.requests[0].options.body);
    assert.equal(calls, withPhoto ? 1 : 0); assert.equal(body.avatar_data_url, withPhoto ? "data:image/png;base64,AAAA" : undefined); assert.equal(body.avatar_candidates, undefined); assert.equal(body.avatar_source_url, undefined);
  }
});

/** bossVueScene 设置固定主世界回复与离页面图片替身，记录每次限定权限调用，不运行真实网页代码。 */
async function bossVueScene(value) {
  const scene = await attachmentScene(value), calls = [], photos = [], source = { candidate_id: "synthetic_encrypted_uid_A", avatar_url: "https://img.bosszhipin.com/beijin/upload/avatar/20261017/synthetic-photo.jpg.webp" };
  scene.vue = { source, calls, photos, reply: async () => [{ frameId: 0, result: { ...source } }] };
  value.extension.scripting = { executeScript: async (options) => { calls.push(options); return scene.vue.reply(options); } };
  value.context.GoodHRAvatar.fetchBossVueAvatar = async (url, pageURL) => { photos.push({ url, pageURL }); return "data:image/png;base64,AAAA"; };
  return scene;
}

test("BOSS头像只在提交时用固定零参数MAIN探针读取，前后校验后仅发送内嵌图片，捕获与AI不重启", async () => {
  const value = backgroundFixture((url, options) => response({ task: { ...JSON.parse(options.body), id: "synthetic-vue-import", status: "queued" } }, 202)), scene = await bossVueScene(value);
  const before = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
  assert.equal(scene.vue.calls.length, 0); assert.equal(scene.vue.photos.length, 0); assert.equal(value.requests.length, 0);
  const request = importInput(scene), result = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender);
  assert.equal(result.ok, true); assert.equal(scene.vue.calls.length, 2); assert.equal(scene.vue.photos.length, 1);
  for (const options of scene.vue.calls) { assert.equal(options.world, "MAIN"); assert.deepEqual(JSON.parse(JSON.stringify(options.target)), { tabId: scene.tab.id, frameIds: [0] }); assert.equal(options.func, value.context.GoodHRBossVueAvatar.probeCurrentBossAvatar); assert.equal(options.args, undefined); assert.equal(options.files, undefined); }
  const body = JSON.parse(value.requests[0].options.body); assert.equal(body.avatar_data_url, "data:image/png;base64,AAAA"); assert.equal(JSON.stringify(body).includes("bosszhipin"), false); assert.equal(JSON.stringify(result).includes("bosszhipin"), false); assert.equal(JSON.stringify(value.values).includes("bosszhipin"), false);
  const after = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume; assert.equal(after.capture_id, before.capture_id); assert.equal(after.attachment.text_hash, before.attachment.text_hash); assert.equal(scene.vue.calls.length, 2); assert.equal(value.requests.filter((item) => item.url.includes("attachment-parses")).length, 0);
});

test("BOSS附件独立头像优先，不借Vue替换有效附件头像或改变原捕获", async () => {
  const value = backgroundFixture((url, options) => response({ task: { ...JSON.parse(options.body), id: "synthetic-independent-photo", status: "queued" } }, 202)), scene = await bossVueScene(value);
  scene.raw.avatar_candidates = [{ source_url: "/synthetic-independent.png", width: 120, height: 160 }]; scene.capture = (await value.message({ type: "PREVIEW" }, scene.sender)).data.resume;
  let attachmentCalls = 0; value.context.GoodHRAvatar.fetchAttachmentAvatar = async () => { attachmentCalls++; return "data:image/png;base64,AQID"; };
  const request = importInput(scene), result = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender);
  assert.equal(result.ok, true); assert.equal(attachmentCalls, 1); assert.equal(scene.vue.calls.length, 0); assert.equal(scene.vue.photos.length, 0); assert.equal(JSON.parse(value.requests[0].options.body).avatar_data_url, "data:image/png;base64,AQID");
});

test("BOSS缺少脚本能力、主世界失败或结果无效时保持正文可保存，不把不可信图片传给下载器", async () => {
  for (const failure of ["missing", "throw", "empty", "subframe", "duplicate", "default", "wrong-domain"]) {
    const value = backgroundFixture((url, options) => response({ task: { ...JSON.parse(options.body), id: "synthetic-no-vue-photo", status: "queued" } }, 202)), scene = await bossVueScene(value);
    if (failure === "missing") delete value.extension.scripting;
    else scene.vue.reply = async () => { if (failure === "throw") throw new Error("synthetic MAIN unavailable"); if (failure === "empty") return []; if (failure === "subframe") return [{ frameId: 2, result: scene.vue.source }]; if (failure === "duplicate") return [{ frameId: 0, result: scene.vue.source }, { frameId: 0, result: scene.vue.source }]; return [{ frameId: 0, result: { ...scene.vue.source, avatar_url: failure === "default" ? "https://img.bosszhipin.com/boss/avatar/avatar_13.png" : "https://img.bosszhipin.com.evil.test/beijin/upload/avatar/photo.jpg" } }]; };
    const request = importInput(scene), result = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender);
    assert.equal(result.ok, true, failure); assert.equal(scene.vue.photos.length, 0, failure); assert.equal(value.requests.length, 1); assert.equal(JSON.parse(value.requests[0].options.body).avatar_data_url, undefined, failure);
  }
});

test("BOSS Vue头像下载失败不阻断正文保存，后台不回传或落盘外部地址", async () => {
  const value = backgroundFixture((url, options) => response({ task: { ...JSON.parse(options.body), id: "synthetic-failed-vue-photo", status: "queued" } }, 202)), scene = await bossVueScene(value);
  value.context.GoodHRAvatar.fetchBossVueAvatar = async () => { throw new Error("synthetic image unavailable"); };
  const request = importInput(scene), result = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender);
  assert.equal(result.ok, true); assert.equal(JSON.parse(value.requests[0].options.body).avatar_data_url, undefined); assert.equal(value.requests.length, 1); assert.equal(JSON.stringify(result).includes("bosszhipin"), false); assert.equal(JSON.stringify(value.values).includes("bosszhipin"), false);
});

test("BOSS同正文同capture切候选人或头像源时，下载迟到的头像不能提交简历或PATCH", async () => {
  for (const changed of ["candidate", "avatar", "capture"]) for (const type of ["CREATE", "AVATAR_RETRY"]) {
    const value = backgroundFixture(), scene = await bossVueScene(value); let release, started;
    const active = new Promise((resolve) => { started = resolve; });
    value.context.GoodHRAvatar.fetchBossVueAvatar = async () => { started(); return new Promise((resolve) => { release = resolve; }); };
    const request = importInput(scene), payload = type === "CREATE" ? { capture_id: request.capture_id, attachment_save: request } : { candidate_id: "synthetic-existing-avatar-owner", row_version: 1, capture_id: request.capture_id, idempotency_key: "synthetic-vue-avatar-retry" };
    const pending = value.message({ type, payload }, scene.sender); await active;
    if (changed === "candidate") scene.vue.source.candidate_id = "synthetic_encrypted_uid_B";
    else if (changed === "avatar") scene.vue.source.avatar_url = "https://img.bosszhipin.com/beijin/upload/avatar/20261017/synthetic-other-photo.jpg.webp";
    else await scene.replaceText("合成换人后的另一份附件正文");
    release("data:image/png;base64,AAAA"); const result = await pending;
    assert.equal(result.ok, false, type + ":" + changed); assert.equal(result.code, "CAPTURE_SCOPE_CHANGED", type + ":" + changed); assert.equal(value.requests.length, 0);
  }
});

test("BOSS Vue头像等待期间退出、切账号或撤销同意，旧头像和保存请求都停止", async () => {
  for (const type of ["CREATE", "AVATAR_RETRY"]) for (const switchType of ["LOGOUT", "LOGIN", "CONSENT"]) {
    const value = backgroundFixture(), scene = await bossVueScene(value); let release, started;
    const active = new Promise((resolve) => { started = resolve; }); value.context.GoodHRAvatar.fetchBossVueAvatar = async () => { started(); return new Promise((resolve) => { release = resolve; }); };
    const request = importInput(scene), payload = type === "CREATE" ? { capture_id: request.capture_id, attachment_save: request } : { candidate_id: "synthetic-existing-avatar-owner", row_version: 1, capture_id: request.capture_id, idempotency_key: "synthetic-vue-auth-retry" };
    const pending = value.message({ type, payload }, scene.sender); await active;
    await value.message({ type: switchType, payload: { email: "new-account@example.test", password: "synthetic", agreement_accepted: true } }, uiSender); release("data:image/png;base64,AAAA");
    const result = await pending; assert.equal(result.ok, false, type + ":" + switchType); assert.match(result.message, /账号已经变化/); assert.equal(value.requests.some((item) => item.url.includes("/api/candidates")), false);
  }
});

test("后台保存预检409尚未受理时清除本地冻结，选择update和新row_version后能正常提交", async () => {
  let attempts = 0;
  const value = backgroundFixture((url, options) => ++attempts === 1 ? response({ error_code: "CANDIDATE_DUPLICATE", error: "已有简历，请更新" }, 409) : response({ task: { ...JSON.parse(options.body), id: "synthetic-update-import", status: "queued" } }, 202)), scene = await attachmentScene(value), request = importInput(scene);
  assert.equal((await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender)).code, "CANDIDATE_DUPLICATE");
  const update = { ...request, operation: "update", candidate_id: "synthetic-existing", row_version: 9, note: { content: "更新不能隐式保存备注" }, recommendation: { position_id: "synthetic-position", content: "不能隐式推荐" } };
  const result = await value.message({ type: "UPDATE", payload: { capture_id: request.capture_id, attachment_save: update } }, scene.sender); assert.equal(result.ok, true); assert.equal(attempts, 2);
  const body = JSON.parse(value.requests[1].options.body); assert.equal(body.operation, "update"); assert.equal(body.row_version, 9); assert.equal(body.note, undefined); assert.equal(body.recommendation, undefined);
});

test("保存回包未知沿原编号GET恢复，切人不取消已受理任务，同key更改输入被拒且无第二次POST", async () => {
  let accepted;
  const value = backgroundFixture((url, options) => { if (options.method === "POST") { accepted = { ...JSON.parse(options.body), id: "synthetic-accepted-import", status: "running", stage: "parsing" }; throw new Error("synthetic response lost"); } return response({ task: { ...accepted, resume: { private: "不得返回完整结果" } } }); }), scene = await attachmentScene(value), request = importInput(scene);
  assert.equal((await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: request } }, scene.sender)).ok, false);
  const changed = await value.message({ type: "CREATE", payload: { capture_id: request.capture_id, attachment_save: { ...request, review: { ...request.review, candidate_name: "后来修改不能混入原任务" } } } }, scene.sender); assert.equal(changed.code, "REQUEST_CONFLICT");
  await scene.replaceText("切到另一个合成候选人");
  const recovered = await value.message({ type: "ATTACHMENT_IMPORT_POLL", payload: { request_key: request.request_key, capture_id: request.capture_id } }, scene.sender); assert.equal(recovered.ok, true); assert.equal(recovered.data.task.status, "running"); assert.equal(recovered.data.task.resume, undefined);
  assert.equal(value.requests.filter((item) => item.options.method === "POST").length, 1); assert.match(value.requests[1].url, /attachment-imports\?request_key=synthetic-import-0001$/); assert.equal(value.requests[1].options.body, undefined);
});

test("收藏夹只由授权插件界面读取已有列表，招聘网页与旧账号不能请求分类管理", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const result = await value.message({ type: "FOLDERS" }, floatingSender);
  assert.equal(result.ok, true); assert.equal(value.requests.length, 1);
  assert.equal(value.requests[0].url, "https://goodhr.58it.cn/api/candidates/folders"); assert.equal(value.requests[0].options.method, "GET");
  const webSender = { id: "test-extension", frameId: 0, tab: { id: 11, url: "https://h.liepin.com/resume/show" }, url: "https://h.liepin.com/resume/show" };
  assert.equal(await value.message({ type: "FOLDERS" }, webSender), undefined);
  const stale = await value.message({ type: "FOLDERS", payload: { account_key: "old-account" } }, floatingSender);
  assert.equal(stale.ok, false); assert.match(stale.message, /账号已经变化/);
  assert.equal((await value.message({ type: "FOLDER_CREATE", payload: { name: "任意新增" } }, floatingSender)).ok, false);
  assert.equal(value.requests.length, 1);
  value.values.data_use_accepted = false;
  assert.equal((await value.message({ type: "FOLDERS" }, floatingSender)).ok, false); assert.equal(value.requests.length, 1);
});

test("原技能标签正常保存，收藏夹仅提交手动编号，清空分类不会清空原技能", async () => {
  const tagID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", value = backgroundFixture(); value.values.data_use_accepted = true;
  const resume = { candidate_name: "合成标签候选人", phone: "13800000001", tags: ["招聘页面技能", "不能自由新建"], folder_ids: [tagID, tagID, "任意输入"] };
  assert.equal((await value.message({ type: "CREATE", payload: { resume, idempotency_key: "create-tags" } }, uiSender)).ok, true);
  const create = JSON.parse(value.requests[0].options.body); assert.deepEqual(create.folder_ids, [tagID]); assert.deepEqual(create.tags, resume.tags);
  assert.equal((await value.message({ type: "UPDATE", payload: { resume: { ...resume, folder_ids: [] }, candidate_id: "existing", row_version: 1, idempotency_key: "clear-tags" } }, uiSender)).ok, true);
  const update = JSON.parse(value.requests[1].options.body); assert.deepEqual(update.folder_ids, []); assert.deepEqual(update.tags, resume.tags);
  assert.equal((await value.message({ type: "POSITIONS" }, floatingSender)).ok, true); assert.equal(value.requests[2].url, "https://goodhr.58it.cn/api/positions?purpose=recommendation");
});

test("未选择收藏夹的采集技能不会形成分类，空采集标签更新保留原库技能", async () => {
  const value = backgroundFixture(); value.values.data_use_accepted = true;
  const resume = { candidate_name: "合成技能候选人", phone: "13800000001", tags: ["Java", "招聘流程"] };
  assert.equal((await value.message({ type: "CREATE", payload: { resume, idempotency_key: "skills-only" } }, uiSender)).ok, true);
  const create = JSON.parse(value.requests[0].options.body); assert.deepEqual(create.tags, resume.tags); assert.deepEqual(create.folder_ids, []);
  assert.equal((await value.message({ type: "UPDATE", payload: { resume: { ...resume, tags: [], folder_ids: [] }, candidate_id: "existing", row_version: 1, idempotency_key: "folders-clear-only" } }, uiSender)).ok, true);
  const update = JSON.parse(value.requests[1].options.body); assert.deepEqual(update.folder_ids, []); assert.equal(Object.hasOwn(update, "tags"), false);
  assert.equal(value.requests.some((item) => item.url.includes("/folders")), false, "保存技能不能调用收藏夹管理接口");
});

test("收藏夹草稿只保存有限编号和人工选择标记，切账号不能恢复他人的分类", async () => {
  const value = backgroundFixture(), account = JSON.stringify(["fixture", "fixture@example.test"]), tagID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const draft = { fields: { "folder-filter": "合成标签" }, folder_ids: [tagID, "搜索词"], folders_edited: true };
  assert.equal((await value.message({ type: "DRAFT_SAVE", payload: { identity: "synthetic", account_key: account, draft } }, floatingSender)).ok, true);
  const loaded = await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic", account_key: account } }, floatingSender);
  assert.deepEqual(Array.from(loaded.data.draft.folder_ids), [tagID]); assert.equal(loaded.data.draft.folders_edited, true); assert.equal(loaded.data.draft.fields["folder-filter"], "合成标签");
  assert.equal((await value.message({ type: "DRAFT_GET", payload: { identity: "synthetic", account_key: "other" } }, floatingSender)).ok, false);
});

/** avatarScene 构造当前头像快照及稳定采集标识，图片处理由后台测试替身负责。 */
async function avatarScene(value) {
  const tab = { id: 23, url: "https://h.liepin.com/resume/showresumedetail" }, resume = { candidate_name: "合成头像候选人", phone: "13800000001", platform_id: "hliepin", source_url: tab.url, avatar_source_url: "https://image0.lietou-static.com/fixture.png?signature=synthetic-value" };
  value.extension.tabs.query = async () => [tab]; value.extension.tabs.sendMessage = async () => value.message({ type: "CANDIDATE_SNAPSHOT", resume }, { id: "test-extension", frameId: 0, tab });
  const state = await value.message({ type: "PREVIEW" }, uiSender); return state.data.resume;
}

test("招聘页面只读脚本不写 DOM、不发请求、不读网站缓存、不模拟操作", () => {
  const code = ["content.js", "extraction.js", "boss-attachment.js"].map((name) => fs.readFileSync(path.join(src, name), "utf8")).join("\n");
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
  const account = JSON.stringify(["fixture", "fixture@example.test"]), deliveries = [], resume = { platform_id: "hliepin", platform_candidate_id: "fixture", candidate_name: "合成长链接候选人", source_url: "https://h.liepin.com/" + "a".repeat(700) };
  value.extension.tabs.get = async () => tab;
  value.extension.tabs.sendMessage = async (id, message, options) => {
    if (message.type === "REQUEST_CAPTURE") return value.message({ type: "CANDIDATE_SNAPSHOT", resume }, { id: "test-extension", frameId: 0, tab });
    deliveries.push([id, message, options]); return { closed: true };
  };
  const snapshot = (await value.message({ type: "PREVIEW" }, sender)).data.resume;
  const identity = value.context.GoodHRShared.resumeIdentity(snapshot), payload = { account_key: account, capture_id: snapshot.capture_id, identity };
  const initial = await value.message({ type: "STATE" }, uiSender); assert.equal(initial.data.auto_close_after_save, true);
  assert.equal(await value.message({ type: "SET_AUTO_CLOSE", payload: { enabled: false } }, sender), undefined); assert.equal(value.values.auto_close_after_save, undefined);
  const disabled = await value.message({ type: "SET_AUTO_CLOSE", payload: { enabled: false } }, uiSender); assert.equal(disabled.ok, true); assert.equal(value.values.auto_close_after_save, false);
  const restored = await value.message({ type: "STATE" }, uiSender); assert.equal(restored.data.auto_close_after_save, false);
  const skipped = await value.message({ type: "CLOSE_FLOATING", payload }, sender); assert.equal(skipped.data.closed, false); assert.equal(deliveries.length, 0);
  await value.message({ type: "SET_AUTO_CLOSE", payload: { enabled: true } }, uiSender);
  const longIdentity = JSON.stringify(["hliepin", "fixture", `https://h.liepin.com/${"a".repeat(700)}`, "合成长链接候选人"]);
  const closed = await value.message({ type: "CLOSE_FLOATING", payload }, sender); assert.equal(closed.data.closed, true);
  assert.equal(deliveries.length, 1); assert.equal(deliveries[0][0], tab.id); assert.equal(deliveries[0][1].type, "CLOSE_FLOATING"); assert.equal(deliveries[0][1].identity, identity); assert.equal(deliveries[0][2].frameId, 0);
  assert.equal(longIdentity, identity);
  const rejectedOpen = await value.message({ type: "OPEN_FLOATING", payload }, sender); assert.equal(rejectedOpen.ok, false); assert.equal(deliveries.length, 1);
});

test("BOSS失败恢复只允许原账号原捕获原候选人的可信iframe，旧状态不投递显隐动作", async () => {
  const value = backgroundFixture(), scene = await attachmentScene(value), capture = value.extension.tabs.sendMessage, deliveries = [];
  const payload = { account_key: scene.account, capture_id: scene.capture.capture_id, identity: value.context.GoodHRShared.resumeIdentity(scene.capture) };
  value.extension.tabs.sendMessage = async (id, message, options) => {
    if (message.type === "REQUEST_CAPTURE") return capture(id, message, options);
    deliveries.push({ id, message, options }); return { opened: true, closed: true };
  };
  for (const type of ["OPEN_FLOATING", "CLOSE_FLOATING"]) {
    for (const changed of [{ account_key: "old-account" }, { capture_id: "old-capture" }, { identity: "old-candidate" }, { account_key: "" }, { capture_id: "" }, { identity: "" }]) {
      const rejected = await value.message({ type, payload: { ...payload, ...changed } }, scene.sender); assert.equal(rejected.ok, false); assert.equal(deliveries.length, 0);
    }
    const result = await value.message({ type, payload }, scene.sender); assert.equal(result.ok, true); assert.equal(result.data[type === "OPEN_FLOATING" ? "opened" : "closed"], true);
    const delivery = deliveries.pop(); assert.equal(delivery.id, scene.tab.id); assert.equal(delivery.options.frameId, 0); assert.equal(delivery.message.type, type); assert.equal(delivery.message.identity, payload.identity); assert.equal(Object.hasOwn(delivery.message, "account_key"), false);
  }
  await scene.replaceText("合成另一份附件，不能接收上一人的失败恢复");
  const stale = await value.message({ type: "OPEN_FLOATING", payload }, scene.sender); assert.equal(stale.ok, false); assert.equal(deliveries.length, 0);
  assert.equal(value.requests.length, 0); assert.equal(value.versionRequests.length, 0); value.values.data_use_accepted = false;
});

test("读取原捕获期间切换账号会停止迟到的失败展开请求", async () => {
  const value = backgroundFixture(), scene = await attachmentScene(value), capture = value.extension.tabs.sendMessage, deliveries = [];
  const payload = { account_key: scene.account, capture_id: scene.capture.capture_id, identity: value.context.GoodHRShared.resumeIdentity(scene.capture) };
  let release, started; const waiting = new Promise((resolve) => { started = resolve; });
  value.extension.tabs.sendMessage = async (id, message, options) => {
    if (message.type === "REQUEST_CAPTURE") { started(); await new Promise((resolve) => { release = resolve; }); return capture(id, message, options); }
    deliveries.push(message); return { opened: true };
  };
  const pending = value.message({ type: "OPEN_FLOATING", payload }, scene.sender); await waiting;
  value.values.session = { token: "new-account-token", user: { id: "other", email: "other@example.test" } }; release();
  const result = await pending; assert.equal(result.ok, false); assert.equal(deliveries.length, 0); assert.equal(value.requests.length, 0); value.values.data_use_accepted = false;
});

/** floatingContentFixture 使用合成插件容器加载真实显隐逻辑，不连接招聘页面或浏览器。 */
async function floatingContentFixture() {
  const nodes = [], messages = []; let listener;
  /** node 为插件自身 Shadow DOM 提供显隐测试所需的最小节点。 */
  function node(tag) {
    const value = { tag, hidden: false, isConnected: true, children: [], style: { setProperty: () => {} }, addEventListener: () => {}, setAttribute: () => {}, getBoundingClientRect: () => ({ left: 0, top: 0, width: 248, height: 460 }), append(...items) { this.children.push(...items); }, appendChild(item) { this.children.push(item); }, attachShadow() { return node("shadow"); } };
    nodes.push(value); return value;
  }
  const document = { createElement: node, documentElement: node("html") };
  const context = vm.createContext({ document, location: { href: "https://www.zhipin.com/web/chat/index" }, URL, crypto: crypto.webcrypto, Uint8Array, TextEncoder, innerWidth: 1100, innerHeight: 900, addEventListener: () => {}, MutationObserver: class { observe() {} }, setInterval: () => 1, setTimeout: () => 1, clearTimeout: () => {}, GoodHRExtraction: { extractCurrentCandidate: () => null }, chrome: { runtime: { id: "test-extension", getURL: (file) => "chrome-extension://test-extension/" + file, sendMessage: async (message) => { messages.push(message); return { ok: true }; }, onMessage: { addListener: (handler) => { listener = handler; } } } } });
  for (const file of ["shared.js", "floating.js", "content.js"]) vm.runInContext(fs.readFileSync(path.join(src, file), "utf8"), context);
  await new Promise((resolve) => setImmediate(resolve));
  return { context, nodes, messages, message: (message, sender = { id: "test-extension" }) => new Promise((resolve) => { const pending = listener(message, sender, resolve); if (pending === false) resolve(undefined); }) };
}

test("顶层卡片仅显隐同一identity，迟到失败不能展开新人且网页没有新增动作桥", async () => {
  const value = await floatingContentFixture(), first = { platform_id: "boss", source_url: "https://www.zhipin.com/web/chat/index", attachment: { text_hash: "a".repeat(64) } };
  value.context.GoodHRFloating.update(first);
  const firstIdentity = value.context.GoodHRShared.resumeIdentity(first), host = value.nodes.find((item) => item.tag === "aside");
  assert.equal((await value.message({ type: "CLOSE_FLOATING", identity: firstIdentity })).closed, true); assert.equal(host.hidden, true);
  assert.equal((await value.message({ type: "OPEN_FLOATING", identity: "wrong-candidate" })).opened, false); assert.equal(host.hidden, true);
  assert.equal((await value.message({ type: "OPEN_FLOATING", identity: firstIdentity })).opened, true); assert.equal(host.hidden, false);
  const second = { ...first, attachment: { text_hash: "b".repeat(64) } }; value.context.GoodHRFloating.update(second);
  const secondIdentity = value.context.GoodHRShared.resumeIdentity(second);
  assert.equal((await value.message({ type: "CLOSE_FLOATING", identity: firstIdentity })).closed, false); assert.equal(host.hidden, false);
  assert.equal((await value.message({ type: "CLOSE_FLOATING", identity: secondIdentity })).closed, true);
  assert.equal((await value.message({ type: "OPEN_FLOATING", identity: firstIdentity })).opened, false); assert.equal(host.hidden, true);
  assert.equal(await value.message({ type: "OPEN_FLOATING", identity: secondIdentity }, { id: "webpage" }), undefined); assert.equal(host.hidden, true);
  assert.equal((await value.message({ type: "OPEN_FLOATING" })).opened, false); assert.equal(host.hidden, true);
  assert.equal((await value.message({ type: "OPEN_FLOATING", identity: secondIdentity })).opened, true);
  assert.equal(value.messages.some((item) => !["CANDIDATE_SNAPSHOT", "FLOATING_CLOSED"].includes(item.type)), false);
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
  for (const type of ["STATE", "PREVIEW", "LOGIN", "CREATE", "AVATAR_RETRY", "DUPLICATES", "RESTORE", "FOLLOWUPS", "LOGOUT", "REVOKE_RETRY", "DRAFT_GET", "DRAFT_SAVE", "CHECK_UPDATE", "SET_AUTO_CLOSE", "CLOSE_FLOATING", "OPEN_FLOATING", "SUMMARY_START", "SUMMARY_SAVE"]) assert.equal(await value.message({ type, payload: {} }, sender), undefined);
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
  const payload = { resume: { candidate_name: "合成姓名", phone: "13800000001", email: "fake@example.test", tags: ["原始技能"], folder_ids: ["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"], work_experiences: [{ company_name: "合成履历公司" }] }, request_key: "same-ai" };
  const unavailable = await value.message({ type: "SUMMARY_START", payload }, uiSender); assert.equal(unavailable.ok, false); assert.match(unavailable.message, /配对/); assert.equal(value.requests.length, 0);
  value.values.installation_id = "fixture-installation"; value.values.pairing_token = "private-grant"; value.values.pairing_account_key = JSON.stringify(["fixture", "fixture@example.test"]);
  const result = await value.message({ type: "SUMMARY_START", payload }, uiSender); assert.equal(result.data.task.ai_summary, "合成总结"); assert.equal(value.requests[0].url, "http://127.0.0.1:43129/api/v1/extensions/resume-summary");
  const body = JSON.parse(value.requests[0].options.body); assert.equal(body.pairing_token, "private-grant"); assert.equal(body.candidate.phone, undefined); assert.equal(body.candidate.candidate_name, undefined); assert.equal(body.candidate.work_experiences[0].company_name, "合成履历公司");
  assert.deepEqual(body.candidate.tags, ["原始技能"]); assert.equal(Object.hasOwn(body.candidate, "folder_ids"), false, "人工收藏分类不进入AI职业资料");
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
