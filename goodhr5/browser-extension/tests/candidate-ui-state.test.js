/* 文件作用：在无浏览器、无真实网络的合成界面中验证附件查重时序与后台保存任务恢复。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), vm = require("node:vm"), crypto = require("node:crypto");

/** element 提供插件自身控件的最小 DOM 替身，不包含招聘页面或个人资料。 */
function element() { const listeners = new Map(); return { value: "", hidden: false, disabled: false, textContent: "", dataset: {}, children: [], parentElement: { hidden: false }, classList: { toggle: () => {}, add: () => {}, remove: () => {} }, addEventListener(type, handler) { const items = listeners.get(type) || []; items.push(handler); listeners.set(type, items); }, click() { if (!this.disabled) for (const handler of listeners.get("click") || []) handler({}); }, setAttribute: () => {}, removeAttribute: () => {}, querySelectorAll: () => [], append(...nodes) { this.children.push(...nodes); }, replaceChildren(...nodes) { this.children = nodes; } }; }

/** optionFixture 提供切换候选人时重绘岗位所需的原生 Option 替身。 */
function optionFixture(text, value) { return Object.assign(element(), { textContent: text, value }); }

/** uiFixture 加载实际插件界面脚本，用可控时钟观察真实查重调用而不等待页面轮询。 */
function uiFixture(reply) {
  const elements = new Map(), calls = [], timers = new Map(), windowEvents = new Map(), documentEvents = new Map(), intervals = [], ports = []; let clock = 0, sequence = 0, stateReply = null;
  const document = { hidden: false, getElementById: (id) => { if (!elements.has(id)) elements.set(id, element()); return elements.get(id); }, querySelectorAll: () => [], createElement: element, createTextNode: (value) => value, addEventListener: (type, handler) => documentEvents.set(type, handler) };
  const context = vm.createContext({ Option: optionFixture, document, window: { addEventListener: (type, handler) => windowEvents.set(type, handler) }, URL, crypto: crypto.webcrypto, TextEncoder, Uint8Array, console, confirm: () => { throw new Error("native confirm must not be used"); }, setInterval: (handler) => { intervals.push(handler); return intervals.length; }, setTimeout: (handler, delay) => { const id = ++sequence; timers.set(id, { at: clock + delay, handler }); return id; }, clearTimeout: (id) => timers.delete(id), chrome: { runtime: { connect: () => { let onMessage, onDisconnect; const port = { onMessage: { addListener: (handler) => { onMessage = handler; } }, onDisconnect: { addListener: (handler) => { onDisconnect = handler; } }, disconnect: () => onDisconnect?.(), emit: (message) => onMessage?.(message) }; ports.push(port); return port; }, sendMessage: async (message) => { if (message.type === "STATE") return stateReply ? { ok: true, data: stateReply } : new Promise(() => {}); calls.push(message); return reply ? reply(message) : { ok: true, data: { matches: [], groups: [] } }; } } } });
  for (const filename of ["shared.js", "candidate-ui.js"]) vm.runInContext(fs.readFileSync(path.join(__dirname, "..", "src", filename), "utf8"), context, { filename });
  vm.runInContext('loggedIn = true; dataUseAccepted = true; accountKey = "synthetic-account"; identity = "synthetic-attachment"; currentResume = { platform_id: "boss", source_url: "https://www.zhipin.com/web/chat/index", capture_id: "synthetic-capture", attachment: { text_hash: "a".repeat(64), blocked: false } }; attachmentState = newAttachmentState(currentResume.capture_id); attachmentState.basic_task = { id: "synthetic-basic", phase: "basic", status: "completed", resume: { candidate_name: "合成候选人", phone: "13800000991", work_experiences: [{ company_name: "合成首份公司", start_ym: "2017-01" }] } };', context);
  return { context, calls, elements, ports, intervals, windowEvents, documentEvents, setState: (state) => { stateReply = state; }, click: (id) => elements.get(id).click(), run: (code) => vm.runInContext(code, context), advance: async (duration) => { clock += duration; for (const [id, timer] of [...timers]) if (timer.at <= clock) { timers.delete(id); await timer.handler(); } await new Promise((resolve) => setImmediate(resolve)); } };
}

/** finishCostAction 只在合成测试中明确点击确认或取消，生产代码不自动批准成本。 */
async function finishCostAction(value, pending, accepted) { await new Promise((resolve) => setImmediate(resolve)); if (value.run("Boolean(costConfirmation)")) value.click(accepted ? "cost-confirmation-accept" : "cost-confirmation-cancel"); return pending; }

test("附件基础识别完成后450ms自动复用查重，不等待下一次STATE轮询", async () => {
  const value = uiFixture(); value.run("applyBasicAttachment()");
  await value.advance(449); assert.equal(value.calls.some((item) => item.type === "DUPLICATES"), false);
  await value.advance(1); const duplicate = value.calls.find((item) => item.type === "DUPLICATES"); assert.equal(duplicate.payload.resume.candidate_name, "合成候选人"); assert.equal(duplicate.payload.resume.phone, "13800000991");
  value.run("scheduleDuplicates()"); await value.advance(450); assert.equal(value.calls.filter((item) => item.type === "DUPLICATES").length, 1);
});

test("手动识别完成时busy阻止的查重，在runAction结束后补发同一查重流程", async () => {
  const value = uiFixture(); await value.run("runAction(async () => applyBasicAttachment())");
  assert.equal(value.calls.some((item) => item.type === "DUPLICATES"), false); await value.advance(450);
  assert.equal(value.calls.filter((item) => item.type === "DUPLICATES").length, 1);
  value.run('edited.add("name"); document.getElementById("name").value = "人工核对姓名"; applyBasicAttachment()'); await value.advance(450);
  assert.equal(value.calls.filter((item) => item.type === "DUPLICATES").at(-1).payload.resume.candidate_name, "人工核对姓名");
});

test("点击添加冻结核对和followups，只提交CREATE保存任务，不发full阶段也不等待完整结果", async () => {
  const value = uiFixture(({ type, payload }) => ({ ok: true, data: type === "CREATE" ? { task: { ...payload.attachment_save, id: "synthetic-save-task", status: "queued", stage: "queued", resume: { private: "完整结果不得进入UI" } } } : { matches: [], groups: [] } }));
  value.run('applyBasicAttachment(); document.getElementById("note").value = "冻结备注"; document.getElementById("job").value = "synthetic-job"; document.getElementById("recommendation-note").value = "冻结岗位记录"; document.getElementById("first-work-end").value = ""; edited.add("first-work-end")'); await value.advance(450);
  await value.run("runAction(() => saveCandidate())");
  const saved = value.calls.find((item) => item.type === "CREATE"); assert.equal(saved.payload.attachment_save.review_fields["first-work-end"], ""); assert.equal(saved.payload.attachment_save.note.content, "冻结备注"); assert.equal(saved.payload.attachment_save.recommendation.content, "冻结岗位记录");
  assert.equal(value.run("attachmentState.save_task.resume"), undefined); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START" || item.type === "FOLLOWUPS"), false); assert.equal(value.elements.get("attachment-status").textContent, "已交给后台处理");
});

// 验证新增投递后500毫秒收起早于云端回执，迟到完成不会重复收起。
test("附件新增500毫秒收起不等待CREATE回执且不会再次收起", async () => {
  let release, posted; const started = new Promise((resolve) => { posted = resolve; });
  const value = uiFixture(async ({ type, payload }) => { if (type === "CREATE") { posted(); await new Promise((resolve) => { release = resolve; }); return { ok: true, data: { task: { ...payload.attachment_save, id: "late-import", status: "queued" } } }; } return { ok: true, data: type === "CLOSE_FLOATING" ? { closed: true } : { matches: [], groups: [] } }; });
  value.run("applyBasicAttachment()"); await value.advance(450); const pending = value.run("runAction(() => saveCandidate())"); await started;
  assert.equal(value.run("attachmentState.save_unknown"), true); assert.ok(value.calls.some((item) => item.type === "DRAFT_SAVE" && item.payload.draft.attachment.save_request));
  await value.advance(499); assert.equal(value.calls.some((item) => item.type === "CLOSE_FLOATING"), false);
  await value.advance(1); const close = value.calls.find((item) => item.type === "CLOSE_FLOATING"); assert.equal(close.payload.capture_id, "synthetic-capture"); assert.equal(close.payload.account_key, "synthetic-account");
  release(); await pending; await value.advance(1000); assert.equal(value.calls.filter((item) => item.type === "CLOSE_FLOATING").length, 1); assert.equal(value.calls.filter((item) => item.type === "CREATE").length, 1);
});

// 验证快速拒绝保留浮窗，收起后的迟到失败只重新展开原人，切人后不打扰新人。
test("附件新增提交失败取消收起，迟到失败只恢复原候选人", async () => {
  for (const phase of ["early", "late", "switched"]) {
    let release, posted; const started = new Promise((resolve) => { posted = resolve; });
    const value = uiFixture(async ({ type }) => { if (type === "CREATE") { posted(); await new Promise((resolve) => { release = resolve; }); return { ok: false, message: "合成提交失败" }; } return { ok: true, data: type === "CLOSE_FLOATING" ? { closed: true } : { matches: [], groups: [] } }; });
    value.run("applyBasicAttachment()"); await value.advance(450); const pending = value.run("runAction(() => saveCandidate())"); await started;
    if (phase !== "early") await value.advance(500);
    if (phase === "switched") value.run('uiEpoch++; identity = "new-person"; currentResume.capture_id = "new-capture"');
    release(); await pending; await value.advance(600);
    assert.equal(value.calls.filter((item) => item.type === "CLOSE_FLOATING").length, phase === "early" ? 0 : 1);
    assert.equal(value.calls.filter((item) => item.type === "OPEN_FLOATING").length, phase === "late" ? 1 : 0);
    assert.equal(value.calls.filter((item) => item.type === "CREATE").length, 1);
  }
});

// 验证精简首屏不丢失性别字段，取消自动收起和候选人切换仍受原设置保护。
test("首屏隐藏性别与成功后的重新识别，更新或关闭自动收起均不自动关窗", async () => {
  for (const mode of ["disabled", "switched", "update"]) {
    const value = uiFixture(({ type, payload }) => ({ ok: true, data: ["CREATE", "UPDATE"].includes(type) ? { task: { ...payload.attachment_save, id: "synthetic-import", status: "queued" } } : { matches: [], groups: [] } }));
    value.run('attachmentState.basic_task.resume.gender = "女"; applyBasicAttachment(); renderCandidateInfo({gender:"female"}); renderAttachment()'); await value.advance(450);
    assert.equal(value.elements.get("candidate-info").hidden, true); assert.equal(value.elements.get("gender").value, "female"); assert.equal(value.elements.get("attachment-start").hidden, true);
    if (mode === "disabled") value.run("autoCloseAfterSave = false");
    if (mode === "update") value.run('selectedCandidate = { id: "existing", row_version: 1 }');
    await value.run(`runAction(() => saveCandidate(${mode === "update"}))`);
    if (mode === "switched") value.run('uiEpoch++; identity = "another-person"');
    await value.advance(600); assert.equal(value.calls.some((item) => item.type === "CLOSE_FLOATING"), false);
  }
});

test("未收到保存回执先查询原任务，404后人工重投同key和冻结字段，不把后来改名带入", async () => {
  let posts = 0;
  const value = uiFixture(({ type, payload }) => type === "CREATE" ? (++posts === 1 ? { ok: false, message: "synthetic lost response" } : { ok: true, data: { task: { ...payload.attachment_save, id: "synthetic-recovered-task", status: "queued" } } }) : type === "ATTACHMENT_IMPORT_POLL" ? { ok: false, status: 404, message: "not received" } : { ok: true, data: { matches: [], groups: [] } });
  value.run("applyBasicAttachment()"); await value.advance(450); await value.run("runAction(() => saveCandidate())");
  const first = value.calls.find((item) => item.type === "CREATE").payload.attachment_save;
  value.run('document.getElementById("name").value = "后来改名"; edited.add("name")'); await value.run("runAction(() => saveCandidate())"); assert.equal(posts, 1); assert.equal(value.run("attachmentState.save_not_found"), true);
  await value.run("runAction(() => saveCandidate())"); const second = value.calls.filter((item) => item.type === "CREATE")[1].payload.attachment_save;
  assert.equal(second.request_key, first.request_key); assert.equal(second.review.candidate_name, "合成候选人"); assert.equal(second.review.candidate_name, first.review.candidate_name); assert.equal(posts, 2);
});

test("业务预检409清除未受理冻结并查重，选择现有记录后UPDATE可用新key提交", async () => {
  let rejected = false;
  const candidate = { id: "synthetic-existing", row_version: 9, candidate_name: "合成候选人", phone: "13800000991" };
  const value = uiFixture(({ type, payload }) => { if (type === "CREATE") { rejected = true; return { ok: false, status: 409, code: "CANDIDATE_DUPLICATE", message: "已有简历" }; } if (type === "UPDATE") return { ok: true, data: { task: { ...payload.attachment_save, id: "synthetic-update-task", status: "queued" } } }; return { ok: true, data: { matches: rejected ? [{ match_type: "exact", candidate }] : [], groups: [] } }; });
  value.run("applyBasicAttachment()"); await value.advance(450); await value.run("runAction(() => saveCandidate())"); assert.equal(value.run("attachmentState.save_request"), null); assert.equal(value.run("selectedCandidate.row_version"), 9);
  await value.run("runAction(() => saveCandidate(true))"); const create = value.calls.find((item) => item.type === "CREATE"), update = value.calls.find((item) => item.type === "UPDATE");
  assert.equal(update.payload.attachment_save.operation, "update"); assert.equal(update.payload.attachment_save.candidate_id, candidate.id); assert.equal(update.payload.attachment_save.row_version, 9); assert.notEqual(update.payload.attachment_save.request_key, create.payload.attachment_save.request_key);
});

test("已受理failed可明确重新核对且保留原任务可查，partial和uncertain不自动开启新AI任务", async () => {
  for (const status of ["failed", "partial_success", "uncertain"]) {
    const value = uiFixture(({ type, payload }) => ({ ok: true, data: type === "CREATE" ? { task: { ...payload.attachment_save, id: "synthetic-" + status, status, possibly_charged: true, candidate_id: status === "partial_success" ? "synthetic-saved" : "" } } : { matches: [], groups: [] } }));
    value.run("applyBasicAttachment()"); await value.advance(450); await value.run("runAction(() => saveCandidate())"); const original = value.run("attachmentState.save_request.request_key");
    await finishCostAction(value, value.run("runAction(resetFailedAttachmentImport)"), true);
    if (status === "failed") { assert.equal(value.run("attachmentState.save_request"), null); assert.equal(value.run("attachmentState.save_history[0].request_key"), original); assert.equal(value.elements.get("attachment-save-history").hidden, false); assert.equal(value.elements.get("save").textContent, "添加"); }
    else { assert.equal(value.run("attachmentState.save_request.request_key"), original); assert.equal(value.elements.get("attachment-save-retry").hidden, true); }
    assert.equal(value.calls.filter((item) => item.type === "CREATE").length, 1);
  }
});

test("旧full任务先查询，只有completed复用，running不投递新任务，失败需明确成本确认", async () => {
  for (const status of ["running", "completed", "failed", "uncertain", "missing"]) {
    const value = uiFixture(({ type, payload }) => type === "ATTACHMENT_POLL" && status === "missing" ? { ok: false, status: 404, message: "expired" } : ({ ok: true, data: type === "ATTACHMENT_POLL" ? { task: { id: "legacy-full", phase: "full", status } } : type === "CREATE" ? { task: { ...payload.attachment_save, id: "new-import", status: "queued" } } : { matches: [], groups: [] } }));
    value.run(`applyBasicAttachment(); attachmentState.full_task = { id: "legacy-full", phase: "full", status: ${JSON.stringify(status)} }`); await value.advance(450);
    const pending = value.run("runAction(() => saveCandidate())"); await new Promise((resolve) => setImmediate(resolve)); const confirmations = value.run("Boolean(costConfirmation)") ? 1 : 0; await finishCostAction(value, pending, false);
    assert.equal(value.calls.filter((item) => item.type === "ATTACHMENT_POLL").length, 1); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START"), false);
    if (status === "completed") { assert.equal(value.calls.find((item) => item.type === "CREATE").payload.attachment_save.full_task_id, "legacy-full"); assert.equal(confirmations, 0); }
    else { assert.equal(value.calls.some((item) => item.type === "CREATE"), false); assert.equal(confirmations, status === "running" ? 0 : 1); }
  }
});

test("切人后的迟到受理不覆盖新人卡片，不发送取消或第二次完整解析", async () => {
  let release, started; const active = new Promise((resolve) => { started = resolve; });
  const value = uiFixture(async ({ type, payload }) => { if (type === "CREATE") { started(); await new Promise((resolve) => { release = resolve; }); return { ok: true, data: { task: { ...payload.attachment_save, id: "accepted-old-person", status: "queued" } } }; } return { ok: true, data: { matches: [], groups: [] } }; });
  value.run("applyBasicAttachment()"); await value.advance(450); const pending = value.run("runAction(() => saveCandidate())"); await active;
  value.run('uiEpoch++; currentResume = { platform_id: "boss", capture_id: "new-person", attachment: {} }; attachmentState = newAttachmentState("new-person"); document.getElementById("name").value = "新人卡片"'); release(); await pending;
  assert.equal(value.run("attachmentState.save_task"), null); assert.equal(value.elements.get("name").value, "新人卡片"); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_CANCEL" || item.type === "ATTACHMENT_START"), false);
});

test("重新识别先显示内部成本确认，取消零AI请求且保留原key，明确确认只发送一次", async () => {
  const value = uiFixture(({ type }) => ({ ok: true, data: type === "ATTACHMENT_START" ? { task: { id: "new-basic-task", status: "queued", phase: "basic" } } : {} }));
  const original = value.run("attachmentState.basic_key");
  let pending = value.run('runAction(() => startAttachmentPhase("basic", true))');
  assert.equal(value.run("busy"), true); assert.equal(value.elements.get("cost-confirmation").hidden, false);
  assert.equal(value.elements.get("cost-confirmation-accept").disabled, false); assert.equal(value.elements.get("cost-confirmation-cancel").disabled, false);
  value.ports.at(-1).emit({ type: "CONFIRM_COST_CONFIRMATION" }); assert.equal(value.run("Boolean(costConfirmation)"), true);
  value.click("cost-confirmation-cancel"); await pending;
  assert.equal(value.run("busy"), false); assert.equal(value.run("attachmentState.basic_key"), original); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START"), false);
  pending = value.run('runAction(() => startAttachmentPhase("basic", true))'); value.click("cost-confirmation-accept"); value.click("cost-confirmation-accept"); await pending;
  assert.equal(value.calls.filter((item) => item.type === "ATTACHMENT_START").length, 1); assert.notEqual(value.run("attachmentState.basic_key"), original); assert.equal(value.run("busy"), false);
});

test("切人、收起、文档隐藏或销毁均取消待确认，busy释放且不会发出识别请求", async () => {
  for (const reason of ["candidate", "collapsed", "hidden", "destroyed"]) {
    const value = uiFixture(); const pending = value.run('runAction(() => startAttachmentPhase("basic", true))');
    if (reason === "candidate") await value.run('applySnapshot({ platform_id: "boss", candidate_name: "另一合成候选人", capture_id: "other-capture", attachment: { blocked: false } }, true)');
    if (reason === "collapsed") value.ports.at(-1).emit({ type: "CANCEL_COST_CONFIRMATION" });
    if (reason === "hidden") { value.context.document.hidden = true; value.documentEvents.get("visibilitychange")(); }
    if (reason === "destroyed") value.windowEvents.get("pagehide")();
    await pending;
    assert.equal(value.run("busy"), false, reason); assert.equal(value.run("Boolean(costConfirmation)"), false, reason); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START"), false, reason);
  }
});

test("成本确认期间STATE仍轮询，发现切账号立即取消，不挂busy也不向新人发送旧动作", async () => {
  const value = uiFixture(); const pending = value.run('runAction(() => startAttachmentPhase("basic", true))');
  value.setState({ account_key: "next-account", logged_in: true, data_use_accepted: true, resume: value.run("currentResume"), update: {} });
  value.intervals[0](); await new Promise((resolve) => setImmediate(resolve)); await pending;
  assert.equal(value.run("accountKey"), "next-account"); assert.equal(value.run("busy"), false); assert.equal(value.run("Boolean(costConfirmation)"), false); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START"), false);
});

test("确认瞬间账号、快照、epoch或授权变化时均拒绝旧同意", async () => {
  for (const mutation of ['accountKey = "other"', 'currentResume.capture_id = "other"', 'uiEpoch++', 'loggedIn = false', 'dataUseAccepted = false', 'updateRequired = true']) {
    const value = uiFixture(); const pending = value.run('runAction(() => startAttachmentPhase("basic", true))'); value.run(mutation); value.click("cost-confirmation-accept"); await pending;
    assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START"), false, mutation); assert.equal(value.run("busy"), false, mutation);
  }
});

test("旧full失败在内部确认前不新建，取消保留原任务，确认后只提交一个后台保存", async () => {
  const value = uiFixture(({ type, payload }) => ({ ok: true, data: type === "ATTACHMENT_POLL" ? { task: { id: "legacy-full", phase: "full", status: "failed" } } : type === "CREATE" ? { task: { ...payload.attachment_save, id: "new-import", status: "queued" } } : { matches: [], groups: [] } }));
  value.run('applyBasicAttachment(); attachmentState.full_task = { id: "legacy-full", phase: "full", status: "failed" }'); await value.advance(450);
  await finishCostAction(value, value.run("runAction(() => saveCandidate())"), false);
  assert.equal(value.calls.some((item) => item.type === "CREATE"), false); assert.equal(value.run("attachmentState.full_task.id"), "legacy-full");
  await finishCostAction(value, value.run("runAction(() => saveCandidate())"), true);
  assert.equal(value.calls.filter((item) => item.type === "CREATE").length, 1); assert.equal(value.calls.some((item) => item.type === "ATTACHMENT_START"), false);
});

test("保存失败重新核对与总结重试取消时不重置任务，确认后只重试一次", async () => {
  const value = uiFixture(({ type }) => ({ ok: true, data: type === "SUMMARY_RETRY" ? { task: { id: "summary-after", status: "completed", ai_summary: "合成总结" } } : { matches: [], groups: [] } }));
  value.run('attachmentState.save_task = { id: "failed-save", status: "failed" }; attachmentState.save_request = { request_key: "old-save" }');
  await finishCostAction(value, value.run("runAction(resetFailedAttachmentImport)"), false);
  assert.equal(value.run("attachmentState.save_task.id"), "failed-save"); assert.equal(value.run("attachmentState.save_request.request_key"), "old-save");
  value.run('summaryTask = { id: "failed-summary", status: "failed" }; summarySource = shared.stripSummaryContacts(formResume())');
  await finishCostAction(value, value.run("runAction(() => summarize(true))"), false); assert.equal(value.calls.some((item) => item.type === "SUMMARY_RETRY"), false);
  await finishCostAction(value, value.run("runAction(() => summarize(true))"), true); assert.equal(value.calls.filter((item) => item.type === "SUMMARY_RETRY").length, 1);
});

test("全部六处成本确认复用内部helper，HTML确认按钮不会提交表单", () => {
  const code = fs.readFileSync(path.join(__dirname, "..", "src", "candidate-ui.js"), "utf8"), html = fs.readFileSync(path.join(__dirname, "..", "src", "floating.html"), "utf8");
  assert.doesNotMatch(code, /\bconfirm\s*\(/); assert.equal((code.match(/await confirmAICost\(/g) || []).length, 6);
  for (const id of ["cost-confirmation-accept", "cost-confirmation-cancel"]) assert.match(html, new RegExp(`<button id="${id}"[^>]*type="button"`));
});
