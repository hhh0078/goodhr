/* 文件作用：简历专用隔离浮窗处理查重、直接入库和独立记录，账号设置由工具栏页面负责。 */
"use strict";
const extension = globalThis.browser || globalThis.chrome, $ = (id) => document.getElementById(id), shared = GoodHRShared;
const resumeFields = { name: "candidate_name", phone: "phone", "candidate-email": "email", city: "work_region", position: "current_position", "expected-position": "expected_position" };
const attachmentFields = ["gender", "first-company", "first-work-start", "first-work-end", "first-school", "first-school-start", "first-school-end"];
const draftFields = [...Object.keys(resumeFields), ...attachmentFields, "duplicate-keyword", "job-filter", "job", "folder-filter", "stage", "note", "note-time", "recommendation-note", "recommendation-time"];
let currentResume = null, selectedCandidate = null, matches = [], groups = [], positions = [], loggedIn = false, dataUseAccepted = false, updateRequired = false, autoCloseAfterSave = true, busy = false, hiddenDuplicate = false;
let accountKey = "", identity = "", uiEpoch = 0, querySequence = 0, stateSequence = 0, duplicateTimer, draftTimer, autoCloseTimer, lastQuery = "", duplicateState = "idle", edited = new Set();
let keys = newKeys(), avatarWarning = "", aiSummary = "", summaryTask = null, summaryPolling = false, updateChecked = false, pendingJob = "", jobMenuOpen = false, summaryUnknown = false, summarySource = null;
let folders = [], selectedFolderIDs = [], foldersEdited = false, folderMenuOpen = false, folderLoading = false, folderLoadError = "", folderSequence = 0;
let attachmentState = null, attachmentPolling = false, attachmentSavePolling = false;
let costConfirmation = null;

/** sameCostScope 只允许同一账号和候选人快照接受本次成本确认，授权或版本变化时一律取消。 */
function sameCostScope(scope) { return scope.epoch === uiEpoch && scope.account === accountKey && scope.identity === identity && scope.capture === currentResume?.capture_id && loggedIn && dataUseAccepted && !updateRequired; }
/** settleCostConfirmation 关闭插件自己的确认区；取消不修改已有任务、请求编号或人工输入。 */
function settleCostConfirmation(accepted = false) {
  const pending = costConfirmation; if (!pending) return;
  costConfirmation = null; $("cost-confirmation").hidden = true;
  const allowed = accepted && sameCostScope(pending);
  try { pending.port?.disconnect(); } catch { /* 扩展卸载时仍结束本地等待，不放行付费。 */ }
  pending.resolve(allowed);
}
/** confirmAICost 在扩展内部明确确认可能再次计费，避免原生弹窗被浏览器抑制后静默取消。 */
function confirmAICost(message, label = "确认继续") {
  settleCostConfirmation(false);
  const scope = { epoch: uiEpoch, account: accountKey, identity, capture: currentResume?.capture_id };
  if (!sameCostScope(scope)) return Promise.resolve(false);
  return new Promise((resolve) => {
    const pending = { ...scope, resolve, port: null }; costConfirmation = pending;
    $("cost-confirmation-message").textContent = message; $("cost-confirmation-accept").textContent = label;
    $("cost-confirmation").hidden = false; $("cost-confirmation-accept").disabled = false; $("cost-confirmation-cancel").disabled = false;
    // 短连接仅接收可信后台的取消信号，收起本卡片不能放行任何付费或保存动作。
    if (extension.runtime.connect) {
      try {
        const port = extension.runtime.connect({ name: "goodhr-cost-confirmation" }); pending.port = port;
        port.onMessage.addListener((message) => { if (costConfirmation === pending && message?.type === "CANCEL_COST_CONFIRMATION") settleCostConfirmation(false); });
        port.onDisconnect.addListener(() => { if (costConfirmation === pending) settleCostConfirmation(false); });
      } catch { settleCostConfirmation(false); }
    }
  });
}

/** newKeys 为独立写操作生成幂等键，失败重试沿用原标识。 */
function newKeys() { return Object.fromEntries(["save", "note", "recommendation", "avatar", "summary"].map((key) => [key, shared.newKey()])); }
/** send 请求固定后台操作，UI不接触凭证。 */
async function send(type, payload = {}) { const guarded = ["POSITIONS", "FOLDERS", "DUPLICATES", "CREATE", "UPDATE", "AVATAR_RETRY", "DETAIL", "FOLLOWUPS", "SUMMARY_START", "SUMMARY_POLL", "SUMMARY_CANCEL", "SUMMARY_RETRY", "SUMMARY_SAVE", "ATTACHMENT_START", "ATTACHMENT_POLL", "ATTACHMENT_CANCEL", "ATTACHMENT_IMPORT_POLL"].includes(type); const result = await extension.runtime.sendMessage({ type, payload: { ...(guarded ? { account_key: accountKey } : {}), ...payload } }); if (!result?.ok) { const error = new Error(result?.message || "没有处理成功，请再试一次"); error.status = result?.status; error.code = result?.code; throw error; } return result.data; }
/** showStatus 显示简短结果和下一步提示。 */
function showStatus(text, error = false) { $("status").hidden = !text; $("status").textContent = text || ""; $("status").classList.toggle("error", error); }
/** runAction 防止重复写入，失败保留表单。 */
async function runAction(action) { if (busy) return; busy = true; refreshActions(); try { await action(); } catch (error) { showStatus(error.message, true); if (["UPDATE_REQUIRED", "EXTENSION_UPDATE_REQUIRED"].includes(error.code) || error.status === 426) { updateRequired = true; try { await checkUpdate(); } catch { renderUpdate({ update_required: true }); } } else if (error.status === 409) { lastQuery = ""; try { await checkDuplicates(true); } catch { /* 保留错误和草稿供明确重试。 */ } } } finally { busy = false; refreshActions(); queueDraft(); scheduleDuplicates(); } }
/** scheduleAutoClose 新简历完整入库后延迟收起；切人、关闭设置或迟到结果都不会关闭当前卡片。 */
function scheduleAutoClose(epoch, savedIdentity) {
  clearTimeout(autoCloseTimer); if (!autoCloseAfterSave) return;
  autoCloseTimer = setTimeout(() => { if (epoch !== uiEpoch || savedIdentity !== identity || !autoCloseAfterSave) return; send("CLOSE_FLOATING", { identity: savedIdentity }).catch(() => {}); }, 500);
}
/** requireAccount 未登录或未授权时引导工具栏设置，旧版本禁止写入。 */
function requireAccount() { if (!loggedIn || !dataUseAccepted) throw new Error("请点工具栏 GoodHR 图标登录并授权"); if (updateRequired) throw new Error("请先更新插件，再继续保存"); }
/** formResume 合并人工填写与当前只读快照。 */
function formResume() {
  const parsed = currentResume?.attachment ? attachmentState?.basic_task?.resume : null;
  const resume = shared.sanitizeResume({ ...currentResume, ...parsed, platform_id: currentResume?.platform_id, source_url: currentResume?.source_url, candidate_name: $("name").value, phone: $("phone").value, email: $("candidate-email").value, gender: currentResume?.attachment ? $("gender").value : currentResume?.gender, work_region: $("city").value, current_region: $("city").value, current_position: $("position").value, expected_position: $("expected-position").value, folder_ids: selectedFolderIDs }, Boolean(currentResume?.attachment));
  if (!currentResume?.attachment) return resume;
  const result = shared.mergeAttachmentReview(resume, resume, attachmentFieldValues(), edited, true); result.folder_ids = selectedFolderIDs; return result;
}
/** refreshActions 维护直接添加和独立记录权限，强制更新时禁用全部写按钮。 */
function refreshActions() {
  const unavailable = busy || !loggedIn || !dataUseAccepted || updateRequired, existing = Boolean(selectedCandidate?.id), phone = shared.normalizePhone($("phone").value);
  $("save").hidden = existing; $("update").hidden = !existing; $("save-note").hidden = !existing; $("save-recommendation").hidden = !existing || !$("job").value;
  $("save").textContent = "添加"; $("update").textContent = "更新";
  for (const id of ["save-note", "save-recommendation", "retry-avatar", "job", "job-filter", "folder-filter", "stage", "note", "note-time", "recommendation-note", "recommendation-time", "summary", "summary-cancel", "summary-retry", "summary-save"]) $(id).disabled = unavailable;
  $("check").disabled = busy || !loggedIn || !dataUseAccepted;
  for (const node of $("job-options").querySelectorAll("button")) node.disabled = unavailable;
  for (const node of document.querySelectorAll("#folder-options button, #selected-folders button")) node.disabled = unavailable;
  $("save").disabled = unavailable || !currentResume || !phone || hiddenDuplicate || ["idle", "checking", "failed", "hidden"].includes(duplicateState) || matches.some((item) => item.match_type === "exact");
  $("update").disabled = unavailable || !phone || !selectedCandidate?.row_version; $("save-note").disabled ||= !$("note").value.trim(); $("save-recommendation").disabled ||= !$("job").value;
  const summaryMode = shared.summaryAction(summaryTask, summaryUnknown, summarySource, formResume());
  $("summary").disabled ||= !currentResume || ["wait", "retry"].includes(summaryMode); $("summary").textContent = { recover: "查询总结", changed: "重新总结", new: "重新总结" }[summaryMode] || "AI总结";
  $("summary-save").hidden = !existing || !aiSummary; $("summary-save").disabled ||= !selectedCandidate?.row_version;
  $("summary-cancel").hidden = !["queued", "running"].includes(summaryTask?.status); $("summary-cancel").disabled = busy || !loggedIn || !dataUseAccepted; $("summary-retry").hidden = summaryUnknown || !["failed", "cancelled"].includes(summaryTask?.status);
  $("summary-retry").textContent = summaryMode === "changed" ? "按原资料重试" : "重试总结"; $("reload").disabled = busy;
  $("state-dot").classList.toggle("existing", existing); $("note").placeholder = existing ? "备注（填写后点备注按钮）" : "可选备注，添加后保存";
  $("recommendation-details").hidden = !$("job").value;
  if (currentResume?.attachment) {
    const ready = attachmentState?.basic_task?.status === "completed", waiting = ["queued", "running"].includes(attachmentState?.basic_task?.status) || attachmentPolling;
    $("save").disabled ||= !ready || currentResume.attachment.blocked; $("update").disabled ||= !ready || currentResume.attachment.blocked;
    $("attachment-start").disabled = unavailable || waiting || currentResume.attachment.blocked;
    $("attachment-cancel").disabled = !loggedIn || !dataUseAccepted;
    $("summary").disabled ||= !ready;
    const task = attachmentState?.save_task, pending = ["queued", "running"].includes(task?.status), recovering = attachmentState?.save_unknown || (task && task.status !== "completed" && !pending);
    $("save").disabled ||= pending; $("update").disabled ||= pending;
    if (recovering || attachmentState?.save_not_found) { const button = attachmentState.save_request?.operation === "update" ? $("update") : $("save"); $("save").hidden = button !== $("save"); $("update").hidden = button !== $("update"); button.textContent = attachmentState.save_not_found ? "重试这次提交" : "查询保存结果"; button.disabled = unavailable; }
  }
}
/** draftSnapshot 仅保留业务输入和幂等键，不包含凭证或密码。 */
function draftSnapshot() { return { fields: Object.fromEntries(draftFields.map((id) => [id, id === "job" ? $(id).value || pendingJob : $(id).value])), folder_ids: selectedFolderIDs, folders_edited: foldersEdited, edited: [...edited], keys, ai_summary: aiSummary, summary_task: summaryTask, summary_unknown: summaryUnknown, summary_source: summarySource, attachment: attachmentState }; }
/** persistDraft 保存当前人当前账号本地草稿，不请求云端。 */
async function persistDraft() { if (!identity || !accountKey) return true; const payload = { identity, account_key: accountKey, draft: draftSnapshot() }; try { await send("DRAFT_SAVE", payload); return true; } catch (error) { if (payload.identity === identity && payload.account_key === accountKey && error.code !== "DRAFT_SCOPE_CHANGED") showStatus(`本地草稿没保存：${error.message}。输入暂时还在，先别关闭插件，修改后我会重试`, true); return false; } }
/** queueDraft 输入后尽快保存，同一个人再次打开可恢复。 */
function queueDraft() { clearTimeout(draftTimer); draftTimer = setTimeout(persistDraft, 120); }
/** previewLine 用文本节点显示中文字段，不展示JSON。 */
function previewLine(label, value) { if (!value) return; const node = document.createElement("p"), title = document.createElement("strong"); title.textContent = label + "："; node.append(title, document.createTextNode(String(value))); $("resume-preview").append(node); }
/** renderPreview 显示履历原文，候选人HTML永远不会执行。 */
function renderPreview(resume) {
  $("resume-preview").replaceChildren(); if (!resume) { previewLine("读取情况", "还没看到候选人详情，打开后再点重新读取"); return; }
  for (const [field, label] of Object.entries({ birth_ym: "出生年月", age: "年龄", work_years: "工作年限", education_level: "学历", expected_region: "期望地区", current_salary_text: "当前薪资", expected_salary_text: "期望薪资", work_status: "求职状态", online_status: "活跃情况", personal_description: "个人简介", basic_info: "基本资料" })) previewLine(label, resume[field]);
  previewLine("性别", { male: "男", female: "女" }[resume.gender] || resume.gender); previewLine("技能标签", (resume.tags || []).join("、"));
  for (const [field, title] of Object.entries({ work_experiences: "工作经历", educations: "教育经历", project_experiences: "项目经历", certificates: "证书", honors: "荣誉" })) for (const item of resume[field] || []) { const name = [item.company_name || item.school_name || item.project_name || item.certificate_name || item.honor_name, item.position_name || item.major_name || item.role_name, item.education_level].filter(Boolean).join(" · "); const time = item.start_ym ? `${item.start_ym} — ${item.end_ym || "至今"}` : item.issued_ym || ""; previewLine(title, [name, time, item.content || item.description].filter(Boolean).join("\n")); }
}
/** renderAvatar 只显示已规范化内嵌头像，不加载招聘页面远程图片。 */
function renderAvatar() { const saved = shared.validStoredAvatar(selectedCandidate?.avatar_data_url), image = $("candidate-avatar"); image.hidden = !saved; if (saved) image.src = selectedCandidate.avatar_data_url; else image.removeAttribute("src"); $("retry-avatar").hidden = !avatarWarning || !selectedCandidate?.id || !currentResume?.avatar_detected; }
/** renderCandidateInfo 将候选人的简短职业资料集中展示。 */
function renderCandidateInfo(resume) { $("candidate-info").textContent = [resume?.age ? `${resume.age}岁` : "", { male: "男", female: "女" }[resume?.gender] || resume?.gender, resume?.education_level, resume?.current_position || resume?.expected_position, resume?.work_region || resume?.current_region].filter(Boolean).join(" · "); $("candidate-info").hidden = !$("candidate-info").textContent; }

/** attachmentFieldValues 读取已展示的查重线索，不触碰招聘页或隐藏数据。 */
function attachmentFieldValues() { return Object.fromEntries(attachmentFields.map((id) => [id, $(id).value])); }
/** newAttachmentState 为同一份原文冻结两个阶段的独立请求编号。 */
function newAttachmentState(captureID) { return { capture_id: captureID, observed_at: Date.now(), basic_key: shared.newKey(), basic_task: null, full_task: null, basic_unknown: false, basic_error: "", save_request: null, save_task: null, save_unknown: false, save_not_found: false, save_applied: "", save_history: [] }; }
/** applyBasicAttachment 只填未手改的字段；姓名、手机号和最早经历随后交给现有查重流程。 */
function applyBasicAttachment() {
  const parsed = attachmentState?.basic_task?.resume;
  if (!parsed || attachmentState.basic_task.status !== "completed") return;
  for (const [id, key] of Object.entries(resumeFields)) if (!edited.has(id)) $(id).value = parsed[key] || "";
  for (const [id, value] of Object.entries(shared.attachmentReviewFields(parsed))) if (!edited.has(id)) $(id).value = value;
  renderCandidateInfo(formResume()); renderPreview(formResume()); scheduleDuplicates();
}
/** renderAttachment 只展示当前一步与必要错误，正常完成后收起提示，不重复解释流程。 */
function renderAttachment() {
  const attachment = currentResume?.attachment; $("attachment-panel").hidden = !attachment;
  $("attachment-save-retry").hidden = !attachment || attachmentState?.save_task?.status !== "failed";
  $("attachment-save-history").hidden = !attachment || !attachmentState?.save_history?.length;
  $("attachment-full-query").hidden = !attachment || !attachmentState?.full_task?.id;
  $("attachment-save-actions").hidden = $("attachment-save-retry").hidden && $("attachment-save-history").hidden && $("attachment-full-query").hidden;
  $("attachment-save-retry").disabled = busy || !loggedIn || !dataUseAccepted || updateRequired;
  $("attachment-save-history").disabled = busy || !loggedIn || !dataUseAccepted;
  $("attachment-full-query").disabled = busy || !loggedIn || !dataUseAccepted;
  if (!attachment) return;
  const task = attachmentState?.basic_task, unknown = attachmentState?.basic_unknown;
  const labels = { queued: "识别已排队", running: task?.phase === "full" ? "正在解析完整简历…" : "正在识别…", completed: "", failed: "识别没完成，可重试", cancelled: "已取消识别", uncertain: "结果待确认，请查询" };
  const failed = ["failed", "cancelled", "uncertain"].includes(task?.status);
  $("attachment-status").textContent = attachment.blocked ? attachment.warning : unknown ? (attachmentPolling ? "正在识别…" : "结果待确认，请查询") : attachmentState?.basic_error || (failed ? task.message : "") || labels[task?.status] || (!task ? "正在读取附件…" : "");
  const saving = attachmentState?.save_task;
  if (attachmentState?.save_unknown) $("attachment-status").textContent = "保存回执待确认，正在查询原任务";
  else if (attachmentState?.save_not_found) $("attachment-status").textContent = "后台还没收到，可用原编号重试这次提交";
  else if (saving) $("attachment-status").textContent = ({ queued: "已交给后台处理", running: "已交给后台处理", completed: "已保存到简历库", partial_success: "简历已保存，部分记录没完成", failed: "后台未保存", uncertain: "保存结果待确认，请查询原任务" })[saving.status] + (["failed", "partial_success", "uncertain"].includes(saving.status) ? `：${saving.message || saving.followup_errors?.join("；") || "请到 GoodHR 查看"}${saving.possibly_charged ? "；可能已计费" : ""}` : "");
  if (task?.possibly_charged && ["failed", "cancelled", "uncertain"].includes(task.status)) $("attachment-status").textContent += "；上次可能已计费";
  $("attachment-status").hidden = !$("attachment-status").textContent;
  const running = [attachmentState?.basic_task].find((item) => ["queued", "running"].includes(item?.status));
  $("attachment-cancel").hidden = !running;
  $("attachment-start").hidden = Boolean(running) || (!attachmentState?.basic_task && !unknown && !attachmentState?.basic_error);
  $("attachment-start").textContent = attachmentState?.basic_unknown ? "查询结果" : "重新识别";
  $("attachment-start").parentElement.hidden = $("attachment-start").hidden && $("attachment-cancel").hidden;
  refreshActions();
}
/** attachmentPayload 固定当前捕获、阶段及请求标识，界面不持有附件全文。 */
function attachmentPayload(phase) { return { phase, capture_id: currentResume?.capture_id, request_key: attachmentState?.[phase + "_key"], task_id: attachmentState?.[phase + "_task"]?.id }; }
/** startAttachmentPhase 先落盘请求编号再发出一次识别，网络未知时只允许用户恢复原请求。 */
async function startAttachmentPhase(phase, explicit = false) {
  requireAccount(); if (!currentResume?.attachment || !attachmentState) return null;
  if (phase !== "basic") throw new Error("完整解析由后台保存任务处理");
  const taskKey = phase + "_task", unknownKey = phase + "_unknown", prior = attachmentState[taskKey], epoch = uiEpoch;
  if (prior && ["queued", "running"].includes(prior.status)) return prior;
  if (prior && ["completed", "failed", "cancelled", "uncertain"].includes(prior.status)) {
    if (!explicit) return prior;
    if (!await confirmAICost("会再次调用你当前的 AI，上次请求可能已计费。确认重新识别吗？", "确认重新识别") || epoch !== uiEpoch) return prior;
    attachmentState[phase + "_key"] = shared.newKey(); attachmentState[taskKey] = null;
  }
  attachmentState[unknownKey] = true; attachmentState.basic_error = ""; renderAttachment();
  if (!await persistDraft()) { attachmentState[unknownKey] = false; renderAttachment(); throw new Error("识别编号没保存，请先别关闭插件，稍后再试"); }
  if (epoch !== uiEpoch) return null;
  try {
    const result = await send("ATTACHMENT_START", attachmentPayload(phase));
    if (epoch !== uiEpoch) return null;
    attachmentState[taskKey] = result.task; attachmentState[unknownKey] = false;
    if (phase === "basic") applyBasicAttachment();
    return result.task;
  } catch (error) {
    if (epoch === uiEpoch) {
      if (error.code === "AI_NOT_CONFIGURED") { attachmentState[unknownKey] = false; attachmentState.basic_error = "还没配置 AI，请先到 GoodHR 的 AI 设置配置"; }
      else if (error.status >= 400 && error.status < 500) { attachmentState[unknownKey] = false; attachmentState.basic_error = error.message; }
      else attachmentState.basic_error = `${error.message}；先恢复上次请求，避免重复计费`;
    }
    throw error;
  } finally { if (epoch === uiEpoch) { renderAttachment(); await persistDraft(); } }
}
/** pollAttachmentPhase 只查询已创建任务，响应必须属于当前账号和捕获；不自动重试收费调用。 */
async function pollAttachmentPhase(phase) {
  const epoch = uiEpoch, id = attachmentState?.[phase + "_task"]?.id;
  if (!id) return null;
  const result = await send("ATTACHMENT_POLL", attachmentPayload(phase));
  if (epoch !== uiEpoch || attachmentState?.[phase + "_task"]?.id !== id) return null;
  attachmentState[phase + "_task"] = result.task;
  if (phase === "basic") applyBasicAttachment();
  renderAttachment(); queueDraft(); return result.task;
}
/** maintainAttachment 附件文字稳定后自动识别一次；缺页继续等加载，失败或结果不明不会自动重复计费。 */
async function maintainAttachment() {
  if (!currentResume?.attachment || !attachmentState || attachmentPolling || busy || !loggedIn || !dataUseAccepted || updateRequired) return;
  const attachment = currentResume.attachment;
  if (attachment.blocked || Date.now() - attachmentState.observed_at < 1500) return;
  const epoch = uiEpoch; attachmentPolling = true; refreshActions();
  try {
    if (["queued", "running"].includes(attachmentState.basic_task?.status)) await pollAttachmentPhase("basic");
    else if (!attachmentState.basic_task && !attachmentState.basic_unknown && !attachmentState.basic_error) await startAttachmentPhase("basic");
  } catch (error) { if (epoch === uiEpoch) showStatus(error.code === "AI_NOT_CONFIGURED" ? attachmentState.basic_error : error.message, true); }
  finally { attachmentPolling = false; if (epoch === uiEpoch) { renderAttachment(); scheduleDuplicates(); } }
}
/** freezeAttachmentSave 固定点击时的核对资料、收藏夹和新建附加记录，恢复时不使用后来修改的表单。 */
function freezeAttachmentSave(updating) {
  const resume = formResume();
  return shared.sanitizeAttachmentImport({ request_key: keys.save, capture_id: currentResume.capture_id, operation: updating ? "update" : "create", candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version, review: resume, review_fields: attachmentFieldValues(), edited_fields: [...edited], folder_ids: selectedFolderIDs, ...(!updating ? { note: { content: $("note").value, occurred_at: eventTime("note-time") }, recommendation: { position_id: $("job").value, stage: $("stage").value, content: $("recommendation-note").value, occurred_at: eventTime("recommendation-time") } } : {}) });
}

/** confirmLegacyAttachmentSave 先查询旧版完整解析，进行中绝不开新AI，失败或未知需明确确认成本后才另发新任务。 */
async function confirmLegacyAttachmentSave(request) {
  const previous = attachmentState?.full_task, epoch = uiEpoch;
  if (!previous?.id && attachmentState?.full_unknown && attachmentState.legacy_confirmed !== "request:" + attachmentState.full_key) {
    if (!await confirmAICost("上次完整解析的回执没有确认，可能已计费。本次新保存可能再次调用 AI 并计费，继续吗？") || epoch !== uiEpoch) return false;
    attachmentState.legacy_confirmed = "request:" + attachmentState.full_key; return true;
  }
  if (!previous?.id || attachmentState.legacy_confirmed === previous.id) return true;
  let task;
  try { task = await pollAttachmentPhase("full"); }
  catch (error) {
    if (epoch !== uiEpoch || error.status !== 404) throw error;
    if (!await confirmAICost("上次完整解析记录已过期或查不到，可能已计费。本次新保存可能再次调用 AI 并计费，继续吗？") || epoch !== uiEpoch) return false;
    attachmentState.legacy_confirmed = previous.id; return true;
  }
  if (epoch !== uiEpoch || !task) return false;
  if (task.status === "completed") { request.full_task_id = task.id; return true; }
  if (["queued", "running"].includes(task.status)) throw new Error("上次完整解析还在进行，请稍后查询，不会重复调用 AI");
  if (!await confirmAICost("上次完整解析可能已计费，结果没有确认。本次另建保存任务可能再次调用 AI 并计费，继续吗？") || epoch !== uiEpoch) return false;
  attachmentState.legacy_confirmed = task.id; return true;
}

/** queryLegacyAttachment 只展示旧版完整解析状态，始终不把完整履历结果交给当前表单。 */
async function queryLegacyAttachment() {
  requireAccount(); const epoch = uiEpoch, task = await pollAttachmentPhase("full"); if (epoch !== uiEpoch || !task) return;
  showStatus(`上次完整解析：${({ queued: "排队中", running: "仍在进行", completed: "已完成，可复用后保存", failed: "未完成", cancelled: "已取消", uncertain: "结果待确认" })[task.status] || "待查询"}${task.possibly_charged ? "；可能已计费" : ""}`);
}

/** acceptAttachmentImport 只显示属于本次冻结请求的状态，成功后不再由界面另发CREATE或附加记录。 */
async function acceptAttachmentImport(result, epoch) {
  if (epoch !== uiEpoch || !attachmentState?.save_request) return;
  const task = shared.attachmentImportReceipt(result.task), request = attachmentState.save_request;
  if (!task || task.request_key !== request.request_key || task.capture_id !== request.capture_id) throw new Error("保存回执还没确认，请查询原任务");
  attachmentState.save_task = task; attachmentState.save_unknown = false; attachmentState.save_not_found = false;
  if (task.candidate_id) { $("candidate-link").href = candidateLink(task.candidate_id); $("candidate-link").hidden = false; $("records").hidden = false; }
  if (task.status === "completed" && attachmentState.save_applied !== task.id) {
    attachmentState.save_applied = task.id;
    if (request.note && $("note").value.trim() === request.note.content) { $("note").value = ""; $("note-time").value = ""; keys.note = shared.newKey(); }
    if (request.recommendation && $("job").value === request.recommendation.position_id && $("recommendation-note").value.trim() === request.recommendation.content) { $("job").value = ""; $("job-filter").value = ""; $("recommendation-note").value = ""; $("recommendation-time").value = ""; keys.recommendation = shared.newKey(); }
    lastQuery = ""; try { await checkDuplicates(true); } catch { /* 已保存不回滚，查重失败仍可手动查询。 */ }
    if (epoch !== uiEpoch) return;
  }
  renderAttachment(); showStatus(""); const saved = await persistDraft();
  if (epoch === uiEpoch && task.status === "completed" && request.operation === "create" && saved && !task.followup_errors.length) scheduleAutoClose(epoch, identity);
}

/** submitAttachmentSave 先落盘原请求编号，再投递一次后台任务；关闭界面不会取消已受理任务。 */
async function submitAttachmentSave() {
  const epoch = uiEpoch, request = attachmentState?.save_request; if (!request) throw new Error("这次保存还没准备好，请重新点击");
  attachmentState.save_unknown = true; attachmentState.save_not_found = false; renderAttachment();
  if (!await persistDraft()) { attachmentState.save_unknown = false; renderAttachment(); throw new Error("保存编号没记住，请先别关闭插件，稍后重试"); }
  if (epoch !== uiEpoch) return;
  try { await acceptAttachmentImport(await send(request.operation === "update" ? "UPDATE" : "CREATE", { capture_id: request.capture_id, attachment_save: request }), epoch); }
  catch (error) {
    if (epoch === uiEpoch && (shared.unacceptedImportError(error.code) || (error.status >= 400 && error.status < 500 && error.status !== 409))) { attachmentState.save_unknown = false; attachmentState.save_request = null; attachmentState.save_task = null; keys.save = shared.newKey(); lastQuery = ""; }
    if (epoch === uiEpoch) { renderAttachment(); await persistDraft(); }
    throw error;
  }
}

/** pollAttachmentImport 只查询原保存编号；明确未受理时让用户用相同输入和编号重投，不自动再次计费。 */
async function pollAttachmentImport() {
  const epoch = uiEpoch, request = attachmentState?.save_request; if (!request) return;
  try { await acceptAttachmentImport(await send("ATTACHMENT_IMPORT_POLL", { request_key: request.request_key, capture_id: request.capture_id, task_id: attachmentState.save_task?.id }), epoch); }
  catch (error) {
    if (epoch !== uiEpoch) return;
    if (error.status === 404 && !attachmentState.save_task) { attachmentState.save_unknown = false; attachmentState.save_not_found = true; renderAttachment(); await persistDraft(); return; }
    throw error;
  }
}

/** maintainAttachmentImport 自动查询已提交任务，不读取旧人详情、不重投收费请求，切人后忽略迟到状态。 */
async function maintainAttachmentImport() {
  if (!attachmentState?.save_request || attachmentSavePolling || busy || !loggedIn || !dataUseAccepted || !(attachmentState.save_unknown || ["queued", "running"].includes(attachmentState.save_task?.status))) return;
  const epoch = uiEpoch; attachmentSavePolling = true;
  try { await pollAttachmentImport(); } catch (error) { if (epoch === uiEpoch) showStatus(`${error.message}；后台任务保留了，稍后继续查询`, true); }
  finally { attachmentSavePolling = false; }
}

/** resetFailedAttachmentImport 仅在明确失败后人工确认重新核对，旧编号仍可查询，不自动发出新收费任务。 */
async function resetFailedAttachmentImport() {
  requireAccount(); if (attachmentState?.save_task?.status !== "failed") return; const epoch = uiEpoch;
  if (!await confirmAICost("上次完整解析可能已计费。重新核对后再次点击保存，可能再次调用 AI 并计费，继续吗？", "确认重新核对") || epoch !== uiEpoch) return;
  attachmentState.save_history = [attachmentState.save_task, ...(attachmentState.save_history || [])].slice(0, 3);
  attachmentState.save_request = null; attachmentState.save_task = null; attachmentState.save_unknown = false; attachmentState.save_not_found = false; attachmentState.save_applied = ""; keys.save = shared.newKey(); lastQuery = "";
  await checkDuplicates(true); renderAttachment(); await persistDraft(); showStatus("已保留核对资料，请确认目标后再点击添加或更新");
}

/** pollPreviousAttachmentImport 只查询保留的旧任务状态，不替换当前输入或重新发起AI。 */
async function pollPreviousAttachmentImport() {
  requireAccount(); const epoch = uiEpoch, previous = attachmentState?.save_history?.[0]; if (!previous) return;
  const result = await send("ATTACHMENT_IMPORT_POLL", { task_id: previous.id, request_key: previous.request_key, capture_id: previous.capture_id });
  if (epoch !== uiEpoch) return;
  const task = shared.attachmentImportReceipt(result.task); if (task) attachmentState.save_history[0] = task;
  showStatus(`上次保存：${({ completed: "已入库", partial_success: "简历已保存，部分记录未完成", failed: "未入库", uncertain: "结果待确认", queued: "后台处理中", running: "后台处理中" })[task?.status] || "请稍后查询"}${task?.message ? "；" + task.message : ""}`); await persistDraft();
}

/** cancelAttachment 取消当前活动识别，不清空人工填写，也不自动发起替代任务。 */
async function cancelAttachment() {
  const phase = "basic", epoch = uiEpoch;
  const result = await send("ATTACHMENT_CANCEL", attachmentPayload(phase)); if (epoch !== uiEpoch) return;
  attachmentState[phase + "_task"] = result.task; renderAttachment(); await persistDraft();
}
/** applySnapshot 同人补读只更新未手改字段，切人恢复专属草稿。 */
async function applySnapshot(resume, switching = false) {
  if (switching) settleCostConfirmation(false);
  if (switching) { await persistDraft(); uiEpoch++; querySequence++; clearTimeout(duplicateTimer); clearTimeout(autoCloseTimer); identity = shared.resumeIdentity(resume); currentResume = resume; selectedCandidate = null; matches = []; groups = []; hiddenDuplicate = false; lastQuery = ""; duplicateState = "idle"; edited = new Set(); keys = newKeys(); avatarWarning = ""; aiSummary = ""; summaryTask = null; pendingJob = ""; jobMenuOpen = false; $("job-filter").removeAttribute("data-selected-value"); $("duplicates").replaceChildren(); $("duplicate-criteria").replaceChildren(); $("records").hidden = true; $("candidate-link").hidden = true; for (const id of draftFields) $(id).value = id === "stage" ? "draft" : ""; }
  if (switching) { selectedFolderIDs = []; foldersEdited = false; folderMenuOpen = false; attachmentState = resume?.attachment ? newAttachmentState(resume.capture_id) : null; }
  currentResume = resume; for (const [id, field] of Object.entries(resumeFields)) if (!edited.has(id)) $(id).value = resume?.[field] || "";
  if (switching) { summaryUnknown = false; summarySource = null; pendingJob = ""; }
  if (switching && identity) { const epoch = uiEpoch; try { const { draft } = await send("DRAFT_GET", { identity, account_key: accountKey }); if (epoch === uiEpoch && draft) { for (const id of draftFields) if (draft.fields && id in draft.fields) $(id).value = draft.fields[id]; pendingJob = draft.fields?.job || ""; selectedFolderIDs = shared.sanitizeFolderIDs(draft.folder_ids); foldersEdited = draft.folders_edited === true; edited = new Set(draft.edited || []); keys = { ...keys, ...Object.fromEntries(Object.entries(draft.keys || {}).filter(([, value]) => value)) }; aiSummary = draft.ai_summary || ""; summaryTask = draft.summary_task || null; summaryUnknown = Boolean(draft.summary_unknown); summarySource = draft.summary_source || null; if (resume?.attachment && draft.attachment?.capture_id === resume.capture_id) attachmentState = { ...newAttachmentState(resume.capture_id), ...draft.attachment }; } } catch { /* 草稿失效不阻碍本地预览。 */ } }
  if (resume?.attachment) applyBasicAttachment();
  renderPreview(resume?.attachment ? formResume() : resume); renderAvatar(); renderCandidateInfo(resume?.attachment ? formResume() : resume); renderSummary(); renderAttachment(); renderPositions(); renderFolders(); refreshActions(); scheduleDuplicates();
}
/** loadState 同步账号授权与当前人，切换账号清空上一人的表单草稿。 */
async function loadState() {
  const sequence = ++stateSequence, state = await send("STATE"); if (sequence !== stateSequence) return;
  const nextAccount = state.account_key || (state.logged_in ? JSON.stringify([state.user?.id || "", state.user?.email || ""]) : "preview"), changedAccount = nextAccount !== accountKey;
  if (changedAccount || !state.logged_in || !state.data_use_accepted) settleCostConfirmation(false);
  if (changedAccount) { identity = ""; accountKey = nextAccount; positions = []; folders = []; folderLoadError = ""; folderLoading = false; folderSequence++; }
  const wasLoggedIn = loggedIn, wasAccepted = dataUseAccepted; loggedIn = Boolean(state.logged_in); dataUseAccepted = Boolean(state.data_use_accepted); autoCloseAfterSave = state.auto_close_after_save !== false; renderUpdate(state.update);
  if (!dataUseAccepted || !loggedIn) { querySequence++; folderSequence++; clearTimeout(duplicateTimer); selectedCandidate = null; matches = []; groups = []; folders = []; folderLoading = false; lastQuery = ""; duplicateState = "idle"; $("records").hidden = true; $("duplicates").replaceChildren(); renderFolders(); }
  const switching = changedAccount || identity !== shared.resumeIdentity(state.resume); if (switching || JSON.stringify(state.resume) !== JSON.stringify(currentResume)) await applySnapshot(state.resume, switching);
  if (loggedIn && dataUseAccepted && (!wasLoggedIn || !wasAccepted || changedAccount)) await Promise.all([loadPositions(), loadFolders()]);
  if (!updateChecked) { updateChecked = true; checkUpdate().catch(() => {}); }
  if (!loggedIn || !dataUseAccepted) { $("duplicate-status").textContent = "简历预览"; showStatus("请点工具栏 GoodHR 图标登录并授权"); }
  else if (switching || !wasAccepted || !wasLoggedIn) { $("duplicate-status").textContent = state.resume ? "正在查重" : "等待简历"; showStatus(state.resume ? "" : "打开候选人详情后会自动读取"); }
  scheduleDuplicates(); refreshActions(); maintainAttachment(); maintainAttachmentImport();
  if (costConfirmation && !sameCostScope(costConfirmation)) settleCostConfirmation(false);
}
/** loadPreview 只重读当前可见资料，不提交简历到云端。 */
async function loadPreview() { const result = await send("PREVIEW"); await applySnapshot(result.resume, identity !== shared.resumeIdentity(result.resume)); showStatus(result.resume ? "已重新读取" : "还没看到简历，请打开候选人详情"); }
/** renderUpdate 强制更新时醒目展示当前及最新版本，并锁住全部写操作。 */
function renderUpdate(update) { if (!update) return; updateRequired = Boolean(update.update_required || update.required); const available = updateRequired || Boolean(update.update_available); $("update-notice").hidden = !available; $("update-notice").classList.toggle("optional", !updateRequired); $("update-title").textContent = updateRequired ? "请先更新插件，才能继续保存简历" : "插件有新版本"; $("update-versions").textContent = `当前 ${update.current_version || extension.runtime.getManifest?.().version || ""} · 最新 ${update.latest_version || ""}`; $("update-link").href = shared.CLOUD_BASE + "/download#browser-extension"; refreshActions(); }
/** checkUpdate 读取正式发布状态，固定指向官网下载页。 */
async function checkUpdate() { const result = await send("CHECK_UPDATE"); renderUpdate(result); }
/** loadPositions 读取完整岗位，旧账号返回值丢弃。 */
async function loadPositions() { const epoch = uiEpoch; try { const result = await send("POSITIONS"); if (epoch !== uiEpoch) return; positions = result.positions || result.items || (Array.isArray(result) ? result : []); renderPositions(); } catch (error) { if (epoch === uiEpoch) showStatus(`岗位暂时没加载出来：${error.message}，可先只添加简历`, true); } }
/** loadFolders 读取当前账号可见的已有收藏夹，旧账号响应不能污染当前候选人的选择。 */
async function loadFolders() {
  if (!loggedIn || !dataUseAccepted) return;
  const account = accountKey, sequence = ++folderSequence; folderLoading = true; folderLoadError = ""; renderFolders();
  try { const result = await send("FOLDERS"); if (account !== accountKey || sequence !== folderSequence) return; folders = (Array.isArray(result.folders) ? result.folders : []).filter((folder) => shared.sanitizeFolderIDs([folder?.id]).length && typeof folder.name === "string" && folder.name.trim() && ["team", "personal"].includes(folder.scope)).map((folder) => ({ ...folder, id: folder.id.toLowerCase(), name: shared.cleanText(folder.name, 50) })); }
  catch (error) { if (account !== accountKey || sequence !== folderSequence) return; folderLoadError = `收藏夹没加载出来，点搜索框重试：${error.message}`; }
  finally { if (account === accountKey && sequence === folderSequence) { folderLoading = false; renderFolders(); refreshActions(); } }
}
/** syncCandidateFolders 只回填候选人手动加入的收藏夹，不从原技能标签推断分类。 */
function syncCandidateFolders() { if (!foldersEdited && selectedCandidate) selectedFolderIDs = shared.sanitizeFolderIDs(selectedCandidate.folder_ids || selectedCandidate.folder_details?.map((folder) => folder.id)); renderFolders(); }
/** toggleFolder 多选已有收藏夹或移除已选分类，搜索文字始终不会创建收藏夹。 */
function toggleFolder(id) {
  if (busy || !loggedIn || !dataUseAccepted || updateRequired) return;
  if (selectedFolderIDs.includes(id)) selectedFolderIDs = selectedFolderIDs.filter((value) => value !== id);
  else { if (!folders.some((folder) => folder.id === id)) return; if (selectedFolderIDs.length >= 30) { showStatus("最多选 30 个收藏夹，先移除一个再选吧", true); return; } selectedFolderIDs.push(id); }
  foldersEdited = true; folderMenuOpen = false; $("folder-filter").value = ""; renderFolders(); refreshActions(); queueDraft();
}
/** renderFolders 在插件自身界面显示可搜索收藏夹和手动选择，明确团队与个人的可见范围。 */
function renderFolders() {
  const filter = $("folder-filter"), options = $("folder-options"), selected = $("selected-folders"), keyword = filter.value.trim().toLowerCase(); options.replaceChildren(); selected.replaceChildren();
  const shown = keyword && folderMenuOpen ? folders.filter((folder) => folder.name.toLowerCase().includes(keyword)).slice(0, 8) : [];
  options.hidden = !shown.length; filter.setAttribute("aria-expanded", String(Boolean(shown.length)));
  for (const folder of shown) { const option = document.createElement("button"), meta = document.createElement("small"); option.type = "button"; option.className = "position-option"; option.setAttribute("role", "option"); option.setAttribute("aria-selected", String(selectedFolderIDs.includes(folder.id))); option.dataset.value = folder.id; option.append(document.createTextNode(`${selectedFolderIDs.includes(folder.id) ? "✓ " : ""}${folder.name}`)); meta.textContent = `${folder.scope === "team" ? "团队可见" : "仅自己可见"} · ${Number(folder.candidate_count) || 0} 人`; option.append(meta); option.addEventListener("click", () => toggleFolder(folder.id)); options.append(option); }
  for (const id of selectedFolderIDs) { const folder = folders.find((item) => item.id === id) || selectedCandidate?.folder_details?.find((item) => item.id === id), badge = document.createElement("button"); badge.type = "button"; badge.dataset.value = id; badge.className = "selected-folder"; badge.textContent = `${folder?.name || "收藏夹已不可用"} ×`; badge.setAttribute("aria-label", `移出收藏夹 ${folder?.name || "已不可用收藏夹"}`); badge.title = folder?.scope === "team" ? "团队可见" : "仅自己可见"; badge.addEventListener("click", () => toggleFolder(id)); selected.append(badge); }
  $("folder-hint").textContent = folderLoadError || (keyword && folderMenuOpen && !shown.length ? "没找到，可到简历库新建" : ""); $("folder-hint").hidden = !$("folder-hint").textContent;
  refreshActions();
}
/** positionLabel 生成岗位搜索和选中后共用的短标签，保留公司和城市方便确认。 */
function positionLabel(job) { return [job?.name || job?.title || job?.position_name || job?.id, job?.company_name, job?.city].filter(Boolean).join(" / "); }
/** selectPosition 保存岗位编号并收起候选，下次清空搜索框即可取消推荐。 */
function selectPosition(value) {
  const selected = positions.find((job) => String(job.id) === String(value));
  $("job").value = selected ? String(selected.id) : ""; pendingJob = ""; jobMenuOpen = false;
  $("job-filter").value = selected ? positionLabel(selected) : "";
  if (selected) $("job-filter").dataset.selectedValue = String(selected.id); else $("job-filter").removeAttribute("data-selected-value");
  edited.add("job"); edited.add("job-filter"); renderPositions(); refreshActions(); queueDraft();
}
/** renderPositions 仅在输入搜索词后显示最多八个下拉候选，不再默认铺满岗位列表。 */
function renderPositions() {
  const selected = String($("job").value || pendingJob || ""), filter = $("job-filter"), options = $("job-options");
  $("job").replaceChildren(new Option("暂不推荐", "")); options.replaceChildren();
  for (const job of positions) $("job").append(new Option(job.name || job.title || job.position_name || job.id, String(job.id)));
  const chosen = positions.find((job) => String(job.id) === selected);
  if (chosen) {
    $("job").value = String(chosen.id); pendingJob = "";
    if (!filter.value.trim() || filter.dataset.selectedValue !== String(chosen.id)) filter.value = positionLabel(chosen);
    filter.dataset.selectedValue = String(chosen.id);
  }
  const keyword = filter.value.trim().toLowerCase();
  const filtered = keyword && jobMenuOpen ? positions.filter((job) => `${positionLabel(job)} ${job.department || ""}`.toLowerCase().includes(keyword)).slice(0, 8) : [];
  options.hidden = filtered.length === 0; filter.setAttribute("aria-expanded", String(filtered.length > 0));
  for (const job of filtered) {
    const option = document.createElement("button"), meta = document.createElement("small"); option.type = "button"; option.className = "position-option"; option.setAttribute("role", "option"); option.setAttribute("aria-selected", String(String(job.id) === $("job").value)); option.dataset.value = String(job.id); option.append(document.createTextNode(job.name || job.title || job.position_name || String(job.id))); meta.textContent = [job.company_name, job.city].filter(Boolean).join(" · "); if (meta.textContent) option.append(meta); option.disabled = busy || !loggedIn || !dataUseAccepted || updateRequired; option.addEventListener("click", () => selectPosition(job.id)); options.append(option);
  }
}
/** scheduleDuplicates 对身份与手机变更防抖，未登录未同意不查询。 */
function scheduleDuplicates() { if (!loggedIn || !dataUseAccepted || !currentResume || busy || (currentResume.attachment && attachmentState?.basic_task?.status !== "completed")) return; const signature = JSON.stringify(shared.duplicateInput(formResume(), $("duplicate-keyword").value)); if (signature === lastQuery) return; clearTimeout(duplicateTimer); const epoch = uiEpoch; duplicateTimer = setTimeout(() => { if (epoch === uiEpoch && !busy) checkDuplicates().catch(() => {}); }, 450); }
/** checkDuplicates 用账号候选人查询序号隔离结果，仅唯一exact自动选择。 */
async function checkDuplicates(force = false) {
  if (!loggedIn || !dataUseAccepted) throw new Error("请点工具栏 GoodHR 图标登录并授权"); clearTimeout(duplicateTimer); const epoch = uiEpoch, sequence = ++querySequence, resume = formResume(), keyword = $("duplicate-keyword").value, signature = JSON.stringify(shared.duplicateInput(resume, keyword)); lastQuery = signature; duplicateState = "checking"; selectedCandidate = null; $("records").hidden = true; $("duplicate-status").textContent = resume.phone ? "正在查重……" : "手机号还没读到，先按已有资料找找……"; refreshActions();
  try { const result = await send("DUPLICATES", { resume, keyword, force }); if (epoch !== uiEpoch || sequence !== querySequence || signature !== JSON.stringify(shared.duplicateInput(formResume(), $("duplicate-keyword").value))) return; matches = result.matches || []; groups = result.groups || []; hiddenDuplicate = Boolean(result.hidden_match); const state = shared.classifyDuplicates(result, resume.phone); duplicateState = state.state; selectedCandidate = state.selected; syncCandidateFolders(); $("duplicate-status").textContent = { existing: "已入库", possible: "可能重复，请选择已有简历", hidden: "已有重复资料，无权查看", new: "未入库", "no-phone": "未入库，请补填手机号" }[state.state] || state.title; renderCriteria(result.criteria); renderMatches(); renderAvatar(); refreshActions(); if (selectedCandidate) await loadRecords(); }
  catch (error) { if (epoch !== uiEpoch || sequence !== querySequence) return; duplicateState = "failed"; matches = []; groups = []; renderMatches(); $("duplicate-status").textContent = `查重没完成：${error.message}。输入保留了，点查重重试`; refreshActions(); throw error; }
}
/** renderCriteria 展示后端实际查询条件，不猜测缺失字段。 */
function renderCriteria(criteria) { $("duplicate-criteria").replaceChildren(); if (!criteria) return; const values = [["姓氏", criteria.surname], ["性别", { male: "男", female: "女" }[criteria.gender] || criteria.gender], ["年龄范围", criteria.age_min ? `${criteria.age_min}—${criteria.age_max}岁` : ""], ["最早学校", [criteria.earliest_school, criteria.earliest_school_year].filter(Boolean).join(" · ")], ["最早公司", [criteria.earliest_company, criteria.earliest_company_year].filter(Boolean).join(" · ")]]; for (const [label, value] of values) if (value) { const line = document.createElement("p"); line.textContent = `${label}：${value}`; $("duplicate-criteria").append(line); } }
/** candidateLink 生成GoodHR完整详情地址。 */
function candidateLink(id) { return `${shared.CLOUD_BASE}/admin/resumes/detail?candidate_id=${encodeURIComponent(id)}`; }
/** renderMatches 展示可读分组，多个或相似候选人必须用户选择。 */
function renderMatches() { $("duplicates").replaceChildren(); const shown = new Set(); for (const group of groups) { if (!(group.matches || []).length) continue; const header = document.createElement("p"); header.className = "group-title"; header.textContent = [group.title, group.summary].filter(Boolean).join(" · "); $("duplicates").append(header); for (const match of group.matches) { shown.add(match.candidate?.id); renderMatch(match); } } for (const match of matches) if (!shown.has(match.candidate?.id)) renderMatch(match); }
/** renderMatch 绑定人工选择，候选人内容全部安全文本显示。 */
function renderMatch(match) { const candidate = match.candidate; if (!candidate?.id) return; const label = document.createElement("label"); label.className = "match" + (selectedCandidate?.id === candidate.id ? " selected" : ""); const radio = document.createElement("input"); radio.type = "radio"; radio.name = "match"; radio.checked = selectedCandidate?.id === candidate.id; radio.addEventListener("change", () => runAction(async () => { selectedCandidate = candidate; foldersEdited = false; syncCandidateFolders(); avatarWarning = ""; renderMatches(); renderAvatar(); refreshActions(); await loadRecords(); $("duplicate-status").textContent = "已入库"; showStatus(""); })); const title = document.createElement("strong"); title.textContent = `${candidate.candidate_name || "未填写姓名"} · ${candidate.phone || "手机号待补充"}`; const info = document.createElement("span"); info.textContent = `${match.match_type === "exact" ? "精确线索" : "相似线索"}：${(match.reasons || []).join("、")}`; const link = document.createElement("a"); link.textContent = "查看完整简历"; link.href = candidateLink(candidate.id); link.target = "_blank"; link.rel = "noreferrer"; label.append(radio, title, document.createElement("br"), info, document.createElement("br"), link); $("duplicates").append(label); }
/** loadRecords 分开展示备注和推荐事件，迟到旧人记录不显示。 */
async function loadRecords() {
  if (!selectedCandidate?.id) return;
  const epoch = uiEpoch, id = selectedCandidate.id, result = await send("DETAIL", { candidate_id: id }); if (epoch !== uiEpoch || selectedCandidate?.id !== id) return;
  $("candidate-link").href = candidateLink(id); $("candidate-link").hidden = false; $("records").hidden = false; $("notes").replaceChildren(); $("recommendations").replaceChildren();
  const notes = result.notes || result.items || [];
  if (result.notes_error) $("notes").append(recordNode(`备注记录没读到：${result.notes_error}，点查重重新读取`, "", ""));
  else if (!notes.length) $("notes").append(recordNode("暂无备注", "", ""));
  for (const note of notes) $("notes").append(recordNode(note.content, note.author_name || note.author_email, note.occurred_at || note.created_at));
  if (result.recommendations_error) $("recommendations").append(recordNode(`推荐记录没读到：${result.recommendations_error}，点查重重新读取`, "", ""));
  for (const recommendation of result.recommendations || []) {
    const node = document.createElement("article"), title = document.createElement("strong"); node.className = "record"; title.textContent = `${recommendation.position_name || "岗位"} · ${shared.STAGES[recommendation.stage] || recommendation.stage}`; node.append(title);
    for (const event of recommendation.events || []) node.append(recordNode([`${shared.STAGES[event.from_stage] || event.from_stage || "新建"} → ${shared.STAGES[event.to_stage] || event.to_stage || recommendation.stage}`, event.content].filter(Boolean).join("\n"), event.author_name || event.author_email, event.occurred_at || event.created_at)); $("recommendations").append(node);
  }
}
/** recordNode 展示作者、发生时间和原换行内容。 */
function recordNode(content, author, time) { const node = document.createElement("article"), meta = document.createElement("small"), text = document.createElement("p"); node.className = "record"; const date = time ? new Date(time) : null; meta.textContent = [author || "", date && !Number.isNaN(date.getTime()) ? date.toLocaleString("zh-CN", { hour12: false }) : ""].filter(Boolean).join(" · "); text.textContent = content || ""; node.append(meta, text); return node; }

/** eventTime 将本地选择时间转为RFC3339，留空交给服务端记录。 */
function eventTime(id) { const value = $(id).value; if (!value) return ""; const date = new Date(value); if (Number.isNaN(date.getTime())) throw new Error("记录时间没选好，请重新选择"); return date.toISOString(); }
/** saveFollowups 分开提交备注和推荐，失败保留正文、时间和幂等键。 */
async function saveFollowups(kind = "both") {
  requireAccount(); if (!selectedCandidate?.id) throw new Error("先选已有简历，或先把新人添加到库里"); const epoch = uiEpoch, candidateID = selectedCandidate.id;
  const result = await send("FOLLOWUPS", { candidate_id: candidateID, note: kind === "recommendation" ? "" : $("note").value, note_time: kind === "recommendation" ? "" : eventTime("note-time"), position_id: kind === "note" ? "" : $("job").value, stage: $("stage").value, recommendation_note: $("recommendation-note").value, recommendation_time: kind === "note" ? "" : eventTime("recommendation-time"), note_key: keys.note, recommendation_key: keys.recommendation });
  if (epoch !== uiEpoch || selectedCandidate?.id !== candidateID) return null;
  if (kind !== "recommendation" && result.notes_saved) { $("note").value = ""; $("note-time").value = ""; keys.note = shared.newKey(); }
  if (kind !== "note" && result.recommendation_saved) { $("job").value = ""; $("job-filter").value = ""; $("job-filter").removeAttribute("data-selected-value"); jobMenuOpen = false; $("recommendation-note").value = ""; $("recommendation-time").value = ""; keys.recommendation = shared.newKey(); }
  await loadRecords(); showStatus(result.errors?.length ? `${result.errors.map((item) => item.message).join("；")}。输入保留了，请重试` : kind === "note" ? "备注已保存" : kind === "recommendation" ? "推荐已保存" : "已添加", Boolean(result.errors?.length)); renderPositions(); refreshActions(); await persistDraft(); return result;
}
/** saveCandidate 一次点击直接提交，保留查重保护、幂等键和更新版本检查。 */
async function saveCandidate(updating = false) {
  requireAccount();
  if (currentResume?.attachment && attachmentState?.save_request) {
    if (attachmentState.save_not_found) return submitAttachmentSave();
    if (attachmentState.save_task?.status !== "completed") return pollAttachmentImport();
    attachmentState.save_request = null; attachmentState.save_task = null; attachmentState.save_applied = ""; keys.save = shared.newKey();
  }
  const saveEpoch = uiEpoch, resume = formResume(); if (!resume.candidate_name || !resume.phone) throw new Error("请补齐姓名和完整手机号，国际号码请带 + 区号");
  if (!updating && ["idle", "checking", "failed"].includes(duplicateState)) await checkDuplicates(true);
  if (saveEpoch !== uiEpoch) return;
  if (!updating && (hiddenDuplicate || matches.some((item) => item.match_type === "exact") || selectedCandidate)) throw new Error("已有重复简历，请选择后更新");
  if (currentResume?.attachment) {
    if (currentResume.attachment.blocked) throw new Error("附件还没加载完，请稍等");
    if (attachmentState?.basic_task?.status !== "completed") throw new Error("先识别并核对基本资料，再添加或更新");
    if (!updating && JSON.stringify(shared.duplicateInput(formResume(), $("duplicate-keyword").value)) !== lastQuery) throw new Error("核对资料有变化，请等查重完成后再保存");
    if (updating && (!selectedCandidate?.id || !selectedCandidate?.row_version)) throw new Error("请先查重并选择要更新的简历");
    const frozen = freezeAttachmentSave(updating), chosenID = selectedCandidate?.id;
    if (!await confirmLegacyAttachmentSave(frozen) || saveEpoch !== uiEpoch) return;
    if (updating && chosenID !== selectedCandidate?.id) throw new Error("目标简历有变化，请重新确认后保存");
    attachmentState.save_request = frozen; return submitAttachmentSave();
  }
  if (updating) { if (!selectedCandidate?.id || !selectedCandidate?.row_version) throw new Error("请先选择已有简历"); const changes = shared.describeResumeChanges(selectedCandidate, resume); if (!changes.length && !currentResume?.avatar_detected) { showStatus("资料没有变化"); return; } }
  const epoch = uiEpoch, result = await send(updating ? "UPDATE" : "CREATE", { resume: shared.candidateWriteResume(resume, updating, Boolean(currentResume?.attachment)), capture_id: currentResume?.capture_id, existing_avatar_data_url: selectedCandidate?.avatar_data_url, idempotency_key: keys.save, candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version }); if (epoch !== uiEpoch) return;
  selectedCandidate = result.candidate || result; if (!selectedCandidate?.id) throw new Error("还没拿到简历编号，请先重读并查重"); foldersEdited = false; syncCandidateFolders(); avatarWarning = result.avatar_warning || ""; keys.save = shared.newKey(); keys.avatar = shared.newKey(); duplicateState = "existing"; $("duplicate-status").textContent = "已入库"; if (!updating) { matches = []; groups = []; hiddenDuplicate = false; renderMatches(); } renderAvatar(); refreshActions();
  const followups = updating ? null : await saveFollowups(); if (epoch !== uiEpoch) return;
  if (updating) { await loadRecords(); showStatus("已更新"); }
  if (avatarWarning) showStatus(`${$("status").textContent}；${avatarWarning}`, true); const draftSaved = await persistDraft();
  if (!updating && !avatarWarning && !followups?.errors?.length && draftSaved) scheduleAutoClose(epoch, identity);
}
/** retryAvatar 独立重试头像，不重建主体或顺带提交备注。 */
async function retryAvatar() { requireAccount(); const epoch = uiEpoch, result = await send("AVATAR_RETRY", { candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version, capture_id: currentResume?.capture_id, existing_avatar_data_url: selectedCandidate?.avatar_data_url, idempotency_key: keys.avatar }); if (epoch !== uiEpoch) return; if (!result.unchanged) selectedCandidate = result.candidate || result; avatarWarning = ""; keys.avatar = shared.newKey(); renderAvatar(); showStatus(result.unchanged ? "头像没有变化" : "头像已保存"); }
/** renderSummary 仅在总结有任务或结果时展开，不占用默认浮窗空间。 */
function renderSummary() { $("summary-panel").hidden = !summaryTask && !summaryUnknown && !aiSummary; $("summary-content").textContent = aiSummary; const labels = { queued: "总结已排队", running: "正在总结", completed: "总结已生成", failed: "总结失败，草稿保留", cancelled: "总结已取消" }; $("summary-status").textContent = [summaryUnknown ? "结果待确认，点查询总结恢复" : labels[summaryTask?.status] || "", summaryTask?.error, summaryTask?.possibly_charged ? "上次可能已计费，重试将再次调用AI" : ""].filter(Boolean).join("；"); refreshActions(); }
/** summarize 通过明确点击启动或重试本地任务，切人切账号丢弃旧结果。 */
async function summarize(retrying = false) {
  requireAccount(); const epoch = uiEpoch, action = shared.summaryAction(summaryTask, summaryUnknown, summarySource, formResume());
  if (!retrying && ["wait", "retry"].includes(action)) throw new Error("上次任务保留了，失败或取消后请点明确重试；资料改变后可重新生成");
  if ((retrying || action === "changed") && (!await confirmAICost("可能再次调用 AI 并计费，上次请求也可能已计费。确认继续吗？", "确认重新总结") || epoch !== uiEpoch)) return;
  if (!retrying && action !== "recover") { if (["new", "changed"].includes(action)) keys.summary = shared.newKey(); summarySource = shared.stripSummaryContacts(formResume()); summaryTask = null; }
  summaryUnknown = true; renderSummary(); if (!await persistDraft()) { summaryUnknown = false; renderSummary(); throw new Error("总结草稿还没保存，先不要关闭插件，整理履历后再试"); } if (epoch !== uiEpoch) return;
  try { const result = await send(retrying ? "SUMMARY_RETRY" : "SUMMARY_START", { task_id: summaryTask?.id, resume: summarySource || formResume(), request_key: keys.summary }); if (epoch !== uiEpoch) return; summaryTask = result.task; if (!summaryTask?.id) throw new Error("还没拿到总结记录，请查询上次总结确认结果"); summaryUnknown = false; if (summaryTask.ai_summary) aiSummary = summaryTask.ai_summary; }
  finally { if (epoch === uiEpoch) { renderSummary(); await persistDraft(); } }
}
/** pollSummary 只查询已有任务，不因关闭弹窗或网络失败再次生成。 */
async function pollSummary() { if (summaryPolling || busy || !loggedIn || !["queued", "running"].includes(summaryTask?.status)) return; summaryPolling = true; const epoch = uiEpoch, id = summaryTask.id; try { const result = await send("SUMMARY_POLL", { task_id: id }); if (epoch !== uiEpoch || summaryTask?.id !== id) return; summaryTask = result.task; if (summaryTask?.ai_summary) aiSummary = summaryTask.ai_summary; renderSummary(); queueDraft(); } catch (error) { if (epoch === uiEpoch) $("summary-status").textContent = `${error.message}，本地任务保留了，稍后继续查询`; } finally { summaryPolling = false; } }
/** cancelSummary 明确取消本地任务，保留可能计费提示。 */
async function cancelSummary() { const epoch = uiEpoch, result = await send("SUMMARY_CANCEL", { task_id: summaryTask?.id }); if (epoch !== uiEpoch) return; summaryTask = result.task; renderSummary(); await persistDraft(); }
/** saveSummary 单独保存摘要并校验版本，失败可独立重试。 */
async function saveSummary() { requireAccount(); if (!selectedCandidate?.id || !aiSummary) return; const epoch = uiEpoch, result = await send("SUMMARY_SAVE", { candidate_id: selectedCandidate.id, row_version: selectedCandidate.row_version, ai_summary: aiSummary, idempotency_key: keys.summary }); if (epoch !== uiEpoch) return; selectedCandidate = result.candidate || result; keys.summary = shared.newKey(); showStatus("总结已保存"); refreshActions(); }
$("reload").addEventListener("click", () => runAction(loadPreview).finally(scheduleDuplicates));
$("check").addEventListener("click", () => runAction(() => checkDuplicates(true)));
for (const id of draftFields) $(id).addEventListener("input", () => { edited.add(id); queueDraft(); if (["name", "phone", "duplicate-keyword", ...attachmentFields].includes(id)) { querySequence++; selectedCandidate = null; $("records").hidden = true; duplicateState = "idle"; lastQuery = ""; renderAvatar(); scheduleDuplicates(); } if (id === "job-filter") { const chosen = positions.find((job) => String(job.id) === $("job").value), label = chosen ? positionLabel(chosen) : ""; if ($("job").value && $("job-filter").value.trim() !== label) { $("job").value = ""; pendingJob = ""; $("job-filter").removeAttribute("data-selected-value"); } jobMenuOpen = Boolean($("job-filter").value.trim()); renderPositions(); } else if (id === "job") renderPositions(); else if (id === "folder-filter") { folderMenuOpen = Boolean($("folder-filter").value.trim()); renderFolders(); } if (["name", "phone", "city", "position", "expected-position", "gender"].includes(id)) renderCandidateInfo(formResume()); refreshActions(); });
$("attachment-start").addEventListener("click", () => runAction(() => startAttachmentPhase("basic", true)));
$("attachment-cancel").addEventListener("click", () => cancelAttachment().catch((error) => showStatus(error.message, true)));
$("attachment-save-retry").addEventListener("click", () => runAction(resetFailedAttachmentImport));
$("attachment-save-history").addEventListener("click", () => runAction(pollPreviousAttachmentImport));
$("attachment-full-query").addEventListener("click", () => runAction(queryLegacyAttachment));
document.addEventListener("pointerdown", (event) => { if (jobMenuOpen && !event.target.closest(".jobs")) { jobMenuOpen = false; renderPositions(); } if (folderMenuOpen && !event.target.closest(".folder-picker")) { folderMenuOpen = false; renderFolders(); } });
$("folder-filter").addEventListener("focus", () => { folderMenuOpen = Boolean($("folder-filter").value.trim()); loadFolders(); });
$("folder-filter").addEventListener("keydown", (event) => { if (event.key === "Enter") { event.preventDefault(); const first = $("folder-options").querySelector("button"); if (folderMenuOpen && first) toggleFolder(first.dataset.value); } else if (event.key === "ArrowDown") { const first = $("folder-options").querySelector("button"); if (first) { event.preventDefault(); first.focus(); } } else if (event.key === "Escape") { folderMenuOpen = false; renderFolders(); } });
$("candidate-form").addEventListener("submit", (event) => { event.preventDefault(); runAction(() => saveCandidate()); });
$("update").addEventListener("click", () => runAction(() => saveCandidate(true)));
$("save-note").addEventListener("click", () => runAction(() => saveFollowups("note")));
$("save-recommendation").addEventListener("click", () => runAction(() => saveFollowups("recommendation")));
$("retry-avatar").addEventListener("click", () => runAction(retryAvatar));
$("summary").addEventListener("click", () => runAction(() => summarize()));
$("summary-retry").addEventListener("click", () => runAction(() => summarize(true)));
$("summary-cancel").addEventListener("click", () => runAction(cancelSummary));
$("summary-save").addEventListener("click", () => runAction(saveSummary));
$("cost-confirmation-accept").addEventListener("click", () => settleCostConfirmation(true));
$("cost-confirmation-cancel").addEventListener("click", () => settleCostConfirmation(false));
document.addEventListener("keydown", (event) => { if (event.key === "Escape" && costConfirmation) { event.preventDefault(); settleCostConfirmation(false); return; } if (event.altKey && event.key === "Enter" && !busy && !$("save").disabled && !$("save").hidden) { event.preventDefault(); runAction(() => saveCandidate()); } });
document.addEventListener("visibilitychange", () => { if (document.hidden) settleCostConfirmation(false); });
window.addEventListener("pagehide", () => { settleCostConfirmation(false); persistDraft(); }); loadState().catch((error) => showStatus(error.message, true));
setInterval(() => { pollSummary(); if (!busy || costConfirmation) loadState().catch(() => {}); }, 2500);
