/* 文件作用：在插件自身弹窗和侧栏完成自动查重、隔离草稿、独立备注推荐和本地AI总结，不操作招聘页面。 */
"use strict";
const extension = globalThis.browser || globalThis.chrome, $ = (id) => document.getElementById(id), shared = GoodHRShared;
const resumeFields = { name: "candidate_name", phone: "phone", "candidate-email": "email", city: "work_region", position: "current_position", "expected-position": "expected_position" };
const draftFields = [...Object.keys(resumeFields), "duplicate-keyword", "job-filter", "job", "stage", "note", "note-time", "recommendation-note", "recommendation-time"];
let mode = "password", currentResume = null, selectedCandidate = null, matches = [], groups = [], positions = [], loggedIn = false, busy = false, pairPending = false, hiddenDuplicate = false;
let accountKey = "", identity = "", uiEpoch = 0, querySequence = 0, stateSequence = 0, duplicateTimer, draftTimer, lastQuery = "", duplicateState = "idle", edited = new Set();
let keys = newKeys(), avatarWarning = "", aiSummary = "", summaryTask = null, summaryPolling = false, updateChecked = false, pendingJob = "", summaryUnknown = false, summarySource = null;

/** newKeys 为独立写操作生成幂等键，失败重试沿用原标识。 */
function newKeys() { return Object.fromEntries(["save", "note", "recommendation", "avatar", "summary"].map((key) => [key, shared.newKey()])); }
/** send 请求固定后台操作，UI不接触凭证。 */
async function send(type, payload = {}) { const guarded = ["POSITIONS", "DUPLICATES", "CREATE", "UPDATE", "AVATAR_RETRY", "DETAIL", "FOLLOWUPS", "SUMMARY_START", "SUMMARY_POLL", "SUMMARY_CANCEL", "SUMMARY_RETRY", "SUMMARY_SAVE"].includes(type); const result = await extension.runtime.sendMessage({ type, payload: { ...(guarded ? { account_key: accountKey } : {}), ...payload } }); if (!result?.ok) { const unavailable = ["LOGIN", "SEND_CODE"].includes(type) && result?.status === 404; const error = new Error(unavailable ? "当前服务还没更新插件登录，请先用只读预览，稍后更新后登录" : result?.message || "没有处理成功，请再试一次"); error.status = result?.status; error.code = result?.code; throw error; } return result.data; }
/** showStatus 显示简短结果和下一步提示。 */
function showStatus(text, error = false) { $("status").textContent = text; $("status").classList.toggle("error", error); }
/** runAction 防止重复写入，失败保留表单。 */
async function runAction(action) { if (busy) return; busy = true; refreshActions(); try { await action(); } catch (error) { showStatus(error.message, true); if (error.status === 409) { lastQuery = ""; try { await checkDuplicates(true); } catch { /* 保留错误和草稿供明确重试。 */ } } } finally { busy = false; refreshActions(); queueDraft(); } }
/** requireAccount 未登录或未同意时不发送候选人云请求。 */
function requireAccount() { if (!loggedIn) throw new Error("当前只是本地预览，登录后我再帮你查重和保存"); if (!$("consent").checked) throw new Error("请先同意把确认后的简历资料保存到 GoodHR"); }
/** setMode 切换登录方式，密码和验证码不进入草稿。 */
function setMode(next) { mode = next; $("password-row").hidden = next !== "password"; $("code-row").hidden = next !== "code"; $("password-mode").setAttribute("aria-pressed", String(next === "password")); $("code-mode").setAttribute("aria-pressed", String(next === "code")); }
/** formResume 合并人工填写与当前只读快照。 */
function formResume() { return shared.sanitizeResume({ ...currentResume, candidate_name: $("name").value, phone: $("phone").value, email: $("candidate-email").value, work_region: $("city").value, current_region: $("city").value, current_position: $("position").value, expected_position: $("expected-position").value }); }
/** refreshActions 区分更新、备注、推荐和摘要，已有无号记录仍可备注和推荐。 */
function refreshActions() {
  const unavailable = busy || !loggedIn || !$("consent").checked, existing = Boolean(selectedCandidate?.id), phone = shared.normalizePhone($("phone").value);
  $("save").hidden = existing; $("update").hidden = !existing; $("save-note").hidden = !existing; $("save-recommendation").hidden = !existing;
  for (const id of ["check", "save-note", "save-recommendation", "retry-avatar", "job", "job-filter", "stage", "note", "note-time", "recommendation-note", "recommendation-time", "summary", "summary-cancel", "summary-retry", "summary-save"]) $(id).disabled = unavailable;
  $("save").disabled = unavailable || !currentResume || !phone || hiddenDuplicate || ["idle", "checking", "failed", "hidden"].includes(duplicateState) || matches.some((item) => item.match_type === "exact");
  $("update").disabled = unavailable || !phone || !selectedCandidate?.row_version; $("save-note").disabled ||= !$("note").value.trim(); $("save-recommendation").disabled ||= !$("job").value;
  const summaryMode = shared.summaryAction(summaryTask, summaryUnknown, summarySource, formResume());
  $("summary").disabled ||= !currentResume || ["wait", "retry"].includes(summaryMode); $("summary").textContent = { recover: "查询上次总结", changed: "按当前资料重新生成", new: "重新生成总结" }[summaryMode] || "生成总结"; $("summary-save").hidden = !existing || !aiSummary; $("summary-save").disabled ||= !selectedCandidate?.row_version;
  $("summary-cancel").hidden = !["queued", "running"].includes(summaryTask?.status); $("summary-retry").hidden = summaryUnknown || !["failed", "cancelled"].includes(summaryTask?.status);
  $("summary-retry").textContent = summaryMode === "changed" ? "按原资料重试" : "确认重试总结";
  for (const id of ["reload", "sidebar", "logout", "revoke-retry", "pair", "restore", "send-code"]) $(id).disabled = busy;
  if (existing && !phone) $("duplicate-status").textContent = "已选择已有简历：可以先加备注或推荐，更新资料前再补完整手机号";
}
/** draftSnapshot 仅保留业务输入和幂等键，不包含凭证或密码。 */
function draftSnapshot() { return { fields: Object.fromEntries(draftFields.map((id) => [id, id === "job" ? $(id).value || pendingJob : $(id).value])), edited: [...edited], keys, ai_summary: aiSummary, summary_task: summaryTask, summary_unknown: summaryUnknown, summary_source: summarySource }; }
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
/** renderAvatar 仅显示GoodHR已保存内嵌图，预览不请求招聘图片。 */
function renderAvatar() { const saved = shared.validStoredAvatar(selectedCandidate?.avatar_data_url), image = $("candidate-avatar"); image.hidden = !saved; if (saved) image.src = selectedCandidate.avatar_data_url; else image.removeAttribute("src"); $("avatar-status").textContent = avatarWarning || (saved ? "头像已保存到 GoodHR" : currentResume?.avatar_detected ? "已识别到当前头像，保存时一并读取" : "当前没有识别到头像，可先保存简历，稍后重新读取后重试头像"); $("retry-avatar").hidden = !avatarWarning || !selectedCandidate?.id || !currentResume?.avatar_detected; }
/** applySnapshot 同人补读只更新未手改字段，切人恢复专属草稿。 */
async function applySnapshot(resume, switching = false) {
  if (switching) { await persistDraft(); uiEpoch++; querySequence++; clearTimeout(duplicateTimer); identity = shared.resumeIdentity(resume); currentResume = resume; selectedCandidate = null; matches = []; groups = []; hiddenDuplicate = false; lastQuery = ""; duplicateState = "idle"; edited = new Set(); keys = newKeys(); avatarWarning = ""; aiSummary = ""; summaryTask = null; $("duplicates").replaceChildren(); $("duplicate-criteria").replaceChildren(); $("records").hidden = true; $("candidate-link").hidden = true; for (const id of draftFields) $(id).value = id === "stage" ? "draft" : ""; }
  currentResume = resume; for (const [id, field] of Object.entries(resumeFields)) if (!edited.has(id)) $(id).value = resume?.[field] || "";
  if (switching) { summaryUnknown = false; summarySource = null; pendingJob = ""; }
  if (switching && identity) { const epoch = uiEpoch; try { const { draft } = await send("DRAFT_GET", { identity, account_key: accountKey }); if (epoch === uiEpoch && draft) { for (const id of draftFields) if (draft.fields && id in draft.fields) $(id).value = draft.fields[id]; pendingJob = draft.fields?.job || ""; edited = new Set(draft.edited || []); keys = { ...keys, ...Object.fromEntries(Object.entries(draft.keys || {}).filter(([, value]) => value)) }; aiSummary = draft.ai_summary || ""; summaryTask = draft.summary_task || null; summaryUnknown = Boolean(draft.summary_unknown); summarySource = draft.summary_source || null; } } catch { /* 草稿失效不阻碍本地预览。 */ } }
  renderPreview(resume); renderAvatar(); renderSummary(); renderPositions(); $("source").textContent = resume ? `${{ liepin: "猎聘企业端", hliepin: "猎聘猎头端", zhaopin: "智联招聘", "51job": "前程无忧" }[resume.platform_id] || "招聘页面"} · 仅采集当前可见资料` : "打开候选人详情，再点重新读取。"; refreshActions(); scheduleDuplicates();
}
/** loadState 同步账号和当前人，迟到状态不会覆盖新界面。 */
async function loadState() {
  const sequence = ++stateSequence, state = await send("STATE"); if (sequence !== stateSequence) return;
  const nextAccount = state.account_key || (state.logged_in ? JSON.stringify([state.user?.id || "", state.user?.email || ""]) : "preview"), changedAccount = nextAccount !== accountKey;
  if (changedAccount) { identity = ""; accountKey = nextAccount; updateChecked = false; positions = []; }
  const wasLoggedIn = loggedIn; loggedIn = state.logged_in; $("version").textContent = `v${state.version}`; $("consent").checked = state.data_use_accepted; $("local-port").value = state.local_port; pairPending = state.pairing_pending;
  $("revocation-panel").hidden = !state.revocation_pending; $("revocation-message").textContent = state.revocation_message || ""; $("login-panel").hidden = loggedIn; $("workspace").hidden = false; $("account-row").hidden = !loggedIn; $("account").textContent = state.user?.email || "GoodHR 账号";
  const switching = changedAccount || identity !== shared.resumeIdentity(state.resume); if (switching || JSON.stringify(state.resume) !== JSON.stringify(currentResume)) await applySnapshot(state.resume, switching);
  if (loggedIn && (!wasLoggedIn || changedAccount)) await loadPositions(); if (loggedIn && !updateChecked) { updateChecked = true; checkUpdate().catch(() => {}); }
  if (state.revocation_pending) showStatus(state.revocation_message, true); else if (!loggedIn) { $("duplicate-status").textContent = "当前只是本地预览，登录并同意后自动查重"; showStatus(state.resume ? "简历已读到，只在本地预览。登录并同意后可查重和保存" : "打开候选人详情后可直接预览"); } else if (switching) showStatus(state.resume ? "简历已读到，查重后我会告诉你是否在库" : "还没看到候选人详情，打开后我再读取"); scheduleDuplicates(); refreshActions();
}
/** loadPreview 重新采集只调用本地消息，未登录不查云端。 */
async function loadPreview() { const result = await send("PREVIEW"); await applySnapshot(result.resume, identity !== shared.resumeIdentity(result.resume)); showStatus(result.resume ? "简历已重新读取，未手改资料已补齐，输入保留了" : "还没看到候选人详情，打开后再点重新读取"); }
/** checkUpdate 仅线上版本较新时提示，不阻止开发版本测试。 */
async function checkUpdate() { const epoch = uiEpoch, result = await send("CHECK_UPDATE"); if (epoch !== uiEpoch) return; $("update-notice").hidden = !result.update_available; $("update-notice").href = shared.CLOUD_BASE + "/download#browser-extension"; $("update-notice").textContent = `插件有新版 v${result.latest_version}，点这里更新${result.note ? "：" + result.note : ""}`; }
/** loadPositions 读取完整岗位，旧账号返回值丢弃。 */
async function loadPositions() { const epoch = uiEpoch; try { const result = await send("POSITIONS"); if (epoch !== uiEpoch) return; positions = result.positions || result.items || (Array.isArray(result) ? result : []); renderPositions(); } catch (error) { if (epoch === uiEpoch) showStatus(`岗位暂时没加载出来：${error.message}，可先只添加简历`, true); } }
/** renderPositions 在插件筛选所有岗位，已选岗位不因筛选而丢失。 */
function renderPositions() { const selected = $("job").value || pendingJob, keyword = $("job-filter").value.trim().toLowerCase(); $("job").replaceChildren(new Option("暂不推荐", "")); positions.filter((job) => job.id === selected || `${job.name || job.title || job.position_name || ""} ${job.company_name || ""} ${job.city || ""}`.toLowerCase().includes(keyword)).forEach((job) => $("job").append(new Option(job.name || job.title || job.position_name || job.id, job.id))); if (Array.from($("job").options).some((item) => item.value === selected)) { $("job").value = selected; pendingJob = ""; } }
/** scheduleDuplicates 对身份与手机变更防抖，未登录未同意不查询。 */
function scheduleDuplicates() { if (!loggedIn || !$("consent").checked || !currentResume || busy) return; const signature = JSON.stringify(shared.duplicateInput(formResume(), $("duplicate-keyword").value)); if (signature === lastQuery) return; clearTimeout(duplicateTimer); const epoch = uiEpoch; duplicateTimer = setTimeout(() => { if (epoch === uiEpoch && !busy) checkDuplicates().catch(() => {}); }, 450); }
/** checkDuplicates 用账号候选人查询序号隔离结果，仅唯一exact自动选择。 */
async function checkDuplicates(force = false) {
  requireAccount(); clearTimeout(duplicateTimer); const epoch = uiEpoch, sequence = ++querySequence, resume = formResume(), keyword = $("duplicate-keyword").value, signature = JSON.stringify(shared.duplicateInput(resume, keyword)); lastQuery = signature; duplicateState = "checking"; selectedCandidate = null; $("records").hidden = true; $("duplicate-status").textContent = resume.phone ? "正在查重……" : "手机号还没读到，先按已有资料找找……"; refreshActions();
  try { const result = await send("DUPLICATES", { resume, keyword, force }); if (epoch !== uiEpoch || sequence !== querySequence || signature !== JSON.stringify(shared.duplicateInput(formResume(), $("duplicate-keyword").value))) return; matches = result.matches || []; groups = result.groups || []; hiddenDuplicate = Boolean(result.hidden_match); const state = shared.classifyDuplicates(result, resume.phone); duplicateState = state.state; selectedCandidate = state.selected; $("duplicate-status").textContent = state.title; renderCriteria(result.criteria); renderMatches(); renderAvatar(); refreshActions(); if (selectedCandidate) await loadRecords(); }
  catch (error) { if (epoch !== uiEpoch || sequence !== querySequence) return; duplicateState = "failed"; matches = []; groups = []; renderMatches(); $("duplicate-status").textContent = `查重没完成：${error.message}。输入保留了，点查重重试`; refreshActions(); throw error; }
}
/** renderCriteria 展示后端实际查询条件，不猜测缺失字段。 */
function renderCriteria(criteria) { $("duplicate-criteria").replaceChildren(); if (!criteria) return; const values = [["姓氏", criteria.surname], ["性别", { male: "男", female: "女" }[criteria.gender] || criteria.gender], ["年龄范围", criteria.age_min ? `${criteria.age_min}—${criteria.age_max}岁` : ""], ["最早学校", [criteria.earliest_school, criteria.earliest_school_year].filter(Boolean).join(" · ")], ["最早公司", [criteria.earliest_company, criteria.earliest_company_year].filter(Boolean).join(" · ")]]; for (const [label, value] of values) if (value) { const line = document.createElement("p"); line.textContent = `${label}：${value}`; $("duplicate-criteria").append(line); } }
/** candidateLink 生成GoodHR完整详情地址。 */
function candidateLink(id) { return `${shared.CLOUD_BASE}/admin/resumes/detail?candidate_id=${encodeURIComponent(id)}`; }
/** renderMatches 展示可读分组，多个或相似候选人必须用户选择。 */
function renderMatches() { $("duplicates").replaceChildren(); const shown = new Set(); for (const group of groups) { if (!(group.matches || []).length) continue; const header = document.createElement("p"); header.className = "group-title"; header.textContent = [group.title, group.summary].filter(Boolean).join(" · "); $("duplicates").append(header); for (const match of group.matches) { shown.add(match.candidate?.id); renderMatch(match); } } for (const match of matches) if (!shown.has(match.candidate?.id)) renderMatch(match); }
/** renderMatch 绑定人工选择，候选人内容全部安全文本显示。 */
function renderMatch(match) { const candidate = match.candidate; if (!candidate?.id) return; const label = document.createElement("label"); label.className = "match" + (selectedCandidate?.id === candidate.id ? " selected" : ""); const radio = document.createElement("input"); radio.type = "radio"; radio.name = "match"; radio.checked = selectedCandidate?.id === candidate.id; radio.addEventListener("change", () => runAction(async () => { selectedCandidate = candidate; avatarWarning = ""; renderMatches(); renderAvatar(); refreshActions(); await loadRecords(); showStatus("已选择已有简历，可以单独加备注或保存推荐记录"); })); const title = document.createElement("strong"); title.textContent = `${candidate.candidate_name || "未填写姓名"} · ${candidate.phone || "手机号待补充"}`; const info = document.createElement("span"); info.textContent = `${match.match_type === "exact" ? "精确线索" : "可能重复，需要你确认"}：${(match.reasons || []).join("、")}`; const link = document.createElement("a"); link.textContent = "查看完整简历"; link.href = candidateLink(candidate.id); link.target = "_blank"; link.rel = "noreferrer"; label.append(radio, title, document.createElement("br"), info, document.createElement("br"), link); $("duplicates").append(label); }
/** loadRecords 分开展示备注和推荐事件，迟到旧人记录不显示。 */
async function loadRecords() {
  if (!selectedCandidate?.id) return;
  const epoch = uiEpoch, id = selectedCandidate.id, result = await send("DETAIL", { candidate_id: id }); if (epoch !== uiEpoch || selectedCandidate?.id !== id) return;
  $("candidate-link").href = candidateLink(id); $("candidate-link").hidden = false; $("records").hidden = false; $("notes").replaceChildren(); $("recommendations").replaceChildren();
  const notes = result.notes || result.items || [];
  if (result.notes_error) $("notes").append(recordNode(`备注记录没读到：${result.notes_error}，点查重重新读取`, "", ""));
  else if (!notes.length) $("notes").append(recordNode("暂无备注，第一次记账就交给你了。", "", ""));
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
  if (epoch !== uiEpoch || selectedCandidate?.id !== candidateID) return;
  if (kind !== "recommendation" && result.notes_saved) { $("note").value = ""; $("note-time").value = ""; keys.note = shared.newKey(); }
  if (kind !== "note" && result.recommendation_saved) { $("job").value = ""; $("recommendation-note").value = ""; $("recommendation-time").value = ""; keys.recommendation = shared.newKey(); }
  await loadRecords(); showStatus(result.errors?.length ? `${result.errors.map((item) => item.message).join("；")}。输入保留了，可点对应按钮重试` : kind === "note" ? "备注记好了，简历资料没有改动" : kind === "recommendation" ? "推荐记录记好了，状态、备注和时间各归其位" : "简历添加成功，备注和推荐分别记好了", Boolean(result.errors?.length)); refreshActions(); await persistDraft();
}
/** saveCandidate 更新只改主体，新建后才分开保存可选备注推荐和摘要。 */
async function saveCandidate(updating = false) {
  requireAccount(); const resume = formResume(); if (!resume.candidate_name || !resume.phone) throw new Error("请补齐姓名和完整手机号，国际号码请带 + 区号");
  if (!updating && ["idle", "checking", "failed"].includes(duplicateState)) { await checkDuplicates(true); if (selectedCandidate || matches.length || hiddenDuplicate) throw new Error("先确认查重结果，再选择添加或更新"); }
  if (!updating && matches.length && !selectedCandidate && !confirm("有相似候选人，请先看一眼查重线索。确认是另一个人，继续添加吗？")) return;
  if (updating) { const changes = shared.describeResumeChanges(selectedCandidate, resume); if (currentResume?.avatar_detected) changes.push("头像：尝试同步当前头像，失败会保留原图"); if (!changes.length) { showStatus("资料没有变化，已有内容都保留了"); return; } if (!confirm(`将更新以下资料：\n${changes.join("\n")}\n\n未读到和未填写的字段会保留已有内容。备注和推荐请单独保存。确认更新吗？`)) return; }
  const epoch = uiEpoch, result = await send(updating ? "UPDATE" : "CREATE", { resume, capture_id: currentResume?.capture_id, existing_avatar_data_url: selectedCandidate?.avatar_data_url, idempotency_key: keys.save, candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version }); if (epoch !== uiEpoch) return;
  selectedCandidate = result.candidate || result; if (!selectedCandidate?.id) throw new Error("没有拿到简历编号，请先查重确认结果，别急着重复添加"); avatarWarning = result.avatar_warning || ""; keys.save = shared.newKey(); keys.avatar = shared.newKey(); duplicateState = "existing"; $("duplicate-status").textContent = updating ? "已更新，空字段保留了原内容" : "已入库"; renderAvatar(); refreshActions();
  if (updating) { await loadRecords(); showStatus("简历资料更新好了，备注和推荐输入保留了，请单独保存"); } else { await saveFollowups(); if (aiSummary) showStatus(`${$("status").textContent}；AI总结保留在草稿，请确认后点保存总结`); }
  if (avatarWarning) showStatus(`${$("status").textContent}；${avatarWarning}`, true); await persistDraft();
}
/** retryAvatar 独立重试头像，不重建主体或顺带提交备注。 */
async function retryAvatar() { requireAccount(); const epoch = uiEpoch, result = await send("AVATAR_RETRY", { candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version, capture_id: currentResume?.capture_id, existing_avatar_data_url: selectedCandidate?.avatar_data_url, idempotency_key: keys.avatar }); if (epoch !== uiEpoch) return; if (!result.unchanged) selectedCandidate = result.candidate || result; avatarWarning = ""; keys.avatar = shared.newKey(); renderAvatar(); showStatus(result.unchanged ? "头像和已保存的一样，原图保留了" : "头像已保存，备注和岗位输入都保留了"); }
/** renderSummary 展示当前任务和可能计费提醒，不自动重试AI。 */
function renderSummary() { $("summary-content").textContent = aiSummary; const labels = { queued: "总结已排队，稍等我整理一下", running: "本地程序正在整理", completed: "总结已生成，请看一眼再保存", failed: "总结没完成，草稿保留了", cancelled: "总结已取消" }; $("summary-status").textContent = [summaryUnknown ? "上次结果还没确认，点查询上次总结，用原资料和原编号恢复，不重复调用AI" : labels[summaryTask?.status] || "通过已配对的本地程序整理，不代替你的判断。", summaryTask?.error, summaryTask?.possibly_charged ? "上次请求可能已计费，重试或重新生成都会调用AI，请先确认。" : ""].filter(Boolean).join("；"); refreshActions(); }
/** summarize 通过明确点击启动或重试本地任务，切人切账号丢弃旧结果。 */
async function summarize(retrying = false) {
  requireAccount(); const action = shared.summaryAction(summaryTask, summaryUnknown, summarySource, formResume());
  if (!retrying && ["wait", "retry"].includes(action)) throw new Error("上次任务保留了，失败或取消后请点明确重试；资料改变后可重新生成");
  if ((retrying || action === "changed") && !confirm("会再次调用AI，上次请求可能已计费。确认继续吗？")) return;
  const epoch = uiEpoch;
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
async function saveSummary() { requireAccount(); if (!selectedCandidate?.id || !aiSummary) return; const epoch = uiEpoch, result = await send("SUMMARY_SAVE", { candidate_id: selectedCandidate.id, row_version: selectedCandidate.row_version, ai_summary: aiSummary, idempotency_key: keys.summary }); if (epoch !== uiEpoch) return; selectedCandidate = result.candidate || result; keys.summary = shared.newKey(); showStatus("总结已单独保存到简历，备注和推荐输入保留了"); refreshActions(); }
/** pollPair 查询短时配对授权，成功后隔离账号草稿。 */
async function pollPair() { if (!pairPending || busy) return; try { const result = await send("PAIR_CHECK"); $("pair-status").textContent = result.status === "pending" ? "等你在控制台确认，我在这里接着等。" : result.status === "expired" ? "授权已过期，可以再连一次。" : ""; if (result.status !== "pending") { pairPending = false; await loadState(); } } catch (error) { $("pair-status").textContent = error.message; } }

$("password-mode").addEventListener("click", () => setMode("password")); $("code-mode").addEventListener("click", () => setMode("code"));
$("consent").addEventListener("change", () => { querySequence++; if (!$("consent").checked) { clearTimeout(duplicateTimer); selectedCandidate = null; $("records").hidden = true; $("duplicates").replaceChildren(); $("duplicate-status").textContent = "未同意数据用途，当前仅在本地预览"; } runAction(async () => { await send("CONSENT", { accepted: $("consent").checked }); lastQuery = ""; }).finally(scheduleDuplicates); });
$("login-form").addEventListener("submit", (event) => { event.preventDefault(); runAction(async () => { await persistDraft(); await send("LOGIN", { email: $("email").value, password: mode === "password" ? $("password").value : "", code: mode === "code" ? $("code").value : "", agreement_accepted: $("agreement").checked }); $("password").value = ""; $("code").value = ""; await loadState(); }).finally(scheduleDuplicates); });
$("send-code").addEventListener("click", () => runAction(async () => { await send("SEND_CODE", { email: $("email").value }); showStatus("验证码发出了，请去邮箱看看"); }));
$("pair").addEventListener("click", () => runAction(async () => { await send("PAIR_START", { port: Number($("local-port").value) }); pairPending = true; $("pair-status").textContent = "授权页打开了，请确认账号和简历权限。"; }));
$("restore").addEventListener("click", () => runAction(async () => { const result = await send("RESTORE"); if (!result.logged_in) throw new Error("还没恢复成功，请先连接授权或手动登录"); await loadState(); if (result.existing_session) $("pair-status").textContent = "当前插件账号保留了。需要AI总结请点连接并授权；要切换本地账号，请先退出当前账号。"; }).finally(scheduleDuplicates));
$("logout").addEventListener("click", () => runAction(async () => { await persistDraft(); uiEpoch++; querySequence++; clearTimeout(duplicateTimer); const result = await send("LOGOUT"); loggedIn = false; await loadState(); showStatus(result.message, result.revocation_pending); }));
$("revoke-retry").addEventListener("click", () => runAction(async () => { const result = await send("REVOKE_RETRY"); await loadState(); showStatus(result.message, result.revocation_pending); }));
$("reload").addEventListener("click", () => runAction(loadPreview).finally(scheduleDuplicates)); $("check").addEventListener("click", () => runAction(() => checkDuplicates(true)));
for (const id of draftFields) $(id).addEventListener("input", () => { edited.add(id); queueDraft(); if (["name", "phone", "duplicate-keyword"].includes(id)) { querySequence++; selectedCandidate = null; $("records").hidden = true; duplicateState = "idle"; lastQuery = ""; renderAvatar(); scheduleDuplicates(); } if (id === "job-filter") renderPositions(); refreshActions(); });
$("candidate-form").addEventListener("submit", (event) => { event.preventDefault(); runAction(() => saveCandidate()); }); $("update").addEventListener("click", () => runAction(() => saveCandidate(true)));
$("save-note").addEventListener("click", () => runAction(() => saveFollowups("note"))); $("save-recommendation").addEventListener("click", () => runAction(() => saveFollowups("recommendation"))); $("retry-avatar").addEventListener("click", () => runAction(retryAvatar));
$("summary").addEventListener("click", () => runAction(() => summarize())); $("summary-retry").addEventListener("click", () => runAction(() => summarize(true))); $("summary-cancel").addEventListener("click", () => runAction(cancelSummary)); $("summary-save").addEventListener("click", () => runAction(saveSummary));
$("sidebar").addEventListener("click", () => { if (extension.sidebarAction?.open) extension.sidebarAction.open().catch((error) => showStatus(error.message, true)); else runAction(() => send("OPEN_SIDEBAR")); });
document.addEventListener("keydown", (event) => { if (event.altKey && event.key === "Enter" && !busy && !$("save").disabled && !$("save").hidden) { event.preventDefault(); runAction(() => saveCandidate()); } });
window.addEventListener("pagehide", persistDraft); loadState().catch((error) => showStatus(error.message, true));
setInterval(() => { pollPair(); pollSummary(); if (!busy) loadState().catch(() => {}); }, 2500);
