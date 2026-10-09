/* 文件作用：控制插件自身的登录、采集预览、人工查重、备注重试和推荐历史界面。 */
"use strict";
const extension = globalThis.browser || globalThis.chrome;
const $ = (id) => document.getElementById(id);
let mode = "password", currentResume = null, lastFingerprint = "", selectedCandidate = null, matches = [], positions = [], loggedIn = false, busy = false, pairPending = false, hiddenDuplicate = false;
let saveKey = GoodHRShared.newKey(), noteKey = GoodHRShared.newKey(), recommendationKey = GoodHRShared.newKey();
let avatarWarning = "", avatarKey = GoodHRShared.newKey();

/** send 向后台发出明确操作，界面永远不接触凭证。 */
async function send(type, payload = {}) { const result = await extension.runtime.sendMessage({ type, payload }); if (!result?.ok) { const unavailableLogin = ["LOGIN", "SEND_CODE"].includes(type) && result?.status === 404; const error = new Error(unavailableLogin ? "当前服务还没更新插件登录，请先用只读预览，稍后更新后登录" : result?.message || "没有处理成功，请再试一次"); error.status = result?.status; error.code = result?.code; throw error; } return result.data; }

/** showStatus 显示简短结果和下一步提示。 */
function showStatus(text, error = false) { $("status").textContent = text; $("status").classList.toggle("error", error); }

/** runAction 防止重复点击，同步保留发生失败时的表单。 */
async function runAction(action) {
  if (busy) return; busy = true;
  const buttons = Array.from(document.querySelectorAll("button")); buttons.forEach((button) => { button.disabled = true; });
  try { await action(); } catch (error) { showStatus(error.message, true); if (error.status === 409) { try { await checkDuplicates(); } catch { showStatus("保存遇到重复或版本冲突，请稍后重新查重。当前表单保留了", true); } } }
  finally { busy = false; buttons.forEach((button) => { button.disabled = false; }); refreshActions(); }
}

/** setMode 切换密码/验证码模式，不保存用户密码。 */
function setMode(next) { mode = next; $("password-row").hidden = next !== "password"; $("code-row").hidden = next !== "code"; $("password-mode").setAttribute("aria-pressed", String(next === "password")); $("code-mode").setAttribute("aria-pressed", String(next === "code")); }

/** formResume 将人工补填合并到只读采集结果，手机号不完整时拒绝保存。 */
function formResume() { return GoodHRShared.sanitizeResume({ ...currentResume, candidate_name: $("name").value, phone: $("phone").value, email: $("candidate-email").value, work_region: $("city").value, current_region: $("city").value, current_position: $("position").value, expected_position: $("expected-position").value }); }

/** refreshActions 按选择和手机号状态显示明确保存入口。 */
function refreshActions() {
  const unavailable = busy || !loggedIn || !$("consent").checked;
  $("save").hidden = Boolean(selectedCandidate); $("save-related").hidden = !selectedCandidate; $("update").hidden = !selectedCandidate || !selectedCandidate.row_version;
  for (const id of ["check", "save-related", "update", "retry-avatar", "job", "job-filter", "note"]) $(id).disabled = unavailable;
  $("save").disabled = unavailable || !currentResume || hiddenDuplicate;
}

/** requireAccount 在查重及写入前检查账号和同意状态，未登录预览不触发云请求。 */
function requireAccount() { if (!loggedIn) throw new Error("当前只是本地预览，登录后我再帮你查重和保存"); if (!$("consent").checked) throw new Error("请先同意把确认后的简历资料保存到 GoodHR"); }

/** previewLine 使用文字节点显示已读取资料，不渲染候选人的 HTML。 */
function previewLine(label, value) { if (!value) return; const paragraph = document.createElement("p"), title = document.createElement("strong"); title.textContent = `${label}：`; paragraph.append(title, document.createTextNode(String(value))); $("resume-preview").append(paragraph); }

/** renderPreview 以中文资料和经历显示本地读取结果，不展示程序 JSON。 */
function renderPreview(resume) {
  $("resume-preview").replaceChildren(); if (!resume) { previewLine("读取情况", "还没看到候选人详情，打开后再点重新读取"); return; }
  const fields = { birth_ym: "出生年月", age: "年龄", work_years: "工作年限", education_level: "学历", expected_region: "期望地区", current_salary_text: "当前薪资", expected_salary_text: "期望薪资", work_status: "求职状态", online_status: "活跃情况", personal_description: "个人简介", basic_info: "基本资料" };
  for (const [field, label] of Object.entries(fields)) previewLine(label, resume[field]);
  previewLine("性别", { male: "男", female: "女" }[resume.gender] || resume.gender); previewLine("技能标签", (resume.tags || []).join("、"));
  for (const [field, title] of Object.entries({ work_experiences: "工作经历", educations: "教育经历", project_experiences: "项目经历", certificates: "证书", honors: "荣誉" })) {
    for (const item of resume[field] || []) {
      const name = [item.company_name || item.school_name || item.project_name || item.certificate_name || item.honor_name, item.position_name || item.major_name || item.role_name, item.education_level].filter(Boolean).join(" · ");
      const time = item.start_ym ? `${item.start_ym} — ${item.end_ym || "至今"}` : item.issued_ym || "";
      previewLine(title, [name, time, item.content || item.description].filter(Boolean).join("\n"));
    }
  }
}

/** renderAvatar 仅显示 GoodHR 已保存的内嵌图，源头像预览不请求第三方图片。 */
function renderAvatar(candidate = selectedCandidate) {
  const saved = GoodHRShared.validStoredAvatar(candidate?.avatar_data_url), image = $("candidate-avatar");
  image.hidden = !saved; if (saved) image.src = candidate.avatar_data_url; else image.removeAttribute("src");
  $("avatar-status").textContent = avatarWarning || (saved ? "头像已保存到 GoodHR" : currentResume?.avatar_detected ? "已识别到当前头像，保存时一并读取" : "当前没有识别到头像，可先保存简历，稍后重新读取后重试头像");
  $("retry-avatar").hidden = !avatarWarning || !selectedCandidate?.id || !currentResume?.avatar_detected;
}

/** fillResume 在候选人变化时清除上一个人的备注、岗位和选择，避免错记。 */
function fillResume(resume) {
  currentResume = resume; selectedCandidate = null; matches = []; hiddenDuplicate = false;
  saveKey = GoodHRShared.newKey(); noteKey = GoodHRShared.newKey(); recommendationKey = GoodHRShared.newKey();
  avatarWarning = ""; avatarKey = GoodHRShared.newKey(); renderAvatar(null); $("candidate-link").hidden = true;
  for (const [id, field] of Object.entries({ name: "candidate_name", phone: "phone", "candidate-email": "email", city: "work_region", position: "current_position", "expected-position": "expected_position" })) $(id).value = resume?.[field] || "";
  $("note").value = ""; $("job").value = ""; $("duplicates").replaceChildren(); $("records").hidden = true;
  renderPreview(resume);
  $("source").textContent = resume ? `${{ liepin: "猎聘企业端", hliepin: "猎聘猎头端", zhaopin: "智联招聘", "51job": "前程无忧" }[resume.platform_id] || "招聘页面"} · 仅采集当前可见资料` : "打开猎聘、智联或前程无忧的候选人详情后，再点击重新读取。";
  refreshActions();
}

/** loadState 同步登录和详情变化，用户填写期间不覆盖当前候选人的表单。 */
async function loadState(force = false) {
  const state = await send("STATE");
  $("version").textContent = `v${state.version}`; $("consent").checked = state.data_use_accepted;
  $("local-port").value = state.local_port; pairPending = state.pairing_pending;
  $("revocation-panel").hidden = !state.revocation_pending; $("revocation-message").textContent = state.revocation_message || "";
  $("login-panel").hidden = state.logged_in; $("workspace").hidden = false; $("account-row").hidden = !state.logged_in;
  if (state.logged_in && !loggedIn) { loggedIn = true; await loadPositions(); }
  if (!state.logged_in) loggedIn = false;
  $("account").textContent = state.user?.email || "GoodHR 账号";
  const fingerprint = JSON.stringify(state.resume);
  if ((force || fingerprint !== lastFingerprint) && (!busy || force)) { lastFingerprint = fingerprint; fillResume(state.resume); if (state.logged_in) showStatus(state.resume ? (state.resume.phone ? "简历读到了，请确认后添加" : "简历读到了，手机号需要你补一下") : "还没看到候选人详情，打开后我再读取"); }
  if (state.revocation_pending) showStatus(state.revocation_message, true);
  else if (!state.logged_in) showStatus(state.resume ? "简历已读到，只在本地预览。登录并同意后可查重和保存" : "打开候选人详情后可直接预览，登录并同意后可查重和保存");
  refreshActions();
}

/** loadPreview 重新采集只调用本地消息，不登录、不查岗位、不发任何云请求。 */
async function loadPreview() { const result = await send("PREVIEW"); lastFingerprint = JSON.stringify(result.resume); fillResume(result.resume); showStatus(result.resume ? "简历已重新读取，资料只在本地预览" : "还没看到候选人详情，打开后再点重新读取"); }

/** loadPositions 获取完整岗位列表，在插件界面按关键词筛选。 */
async function loadPositions() {
  try { const result = await send("POSITIONS"); positions = result.positions || result.items || (Array.isArray(result) ? result : []); renderPositions(); }
  catch (error) { positions = []; renderPositions(); showStatus(`岗位暂时没加载出来：${error.message}，可先只添加简历`, true); }
}

/** renderPositions 使用文本节点渲染岗位，保留当前有效选择。 */
function renderPositions() {
  const selected = $("job").value, keyword = $("job-filter").value.trim().toLowerCase();
  $("job").replaceChildren(new Option("先放进简历库", ""));
  positions.filter((job) => `${job.name || job.title || job.position_name || ""} ${job.city || ""}`.toLowerCase().includes(keyword)).forEach((job) => $("job").append(new Option(job.name || job.title || job.position_name || job.id, job.id)));
  if (Array.from($("job").options).some((item) => item.value === selected)) $("job").value = selected;
}

/** checkDuplicates 将精确/相似结果分别提示，绝不自动合并或覆盖。 */
async function checkDuplicates() {
  requireAccount();
  const result = await send("DUPLICATES", { resume: formResume(), keyword: $("duplicate-keyword").value });
  matches = result.matches || []; selectedCandidate = null; hiddenDuplicate = Boolean(result.hidden_match); renderMatches(); refreshActions();
  showStatus(result.hidden_match ? "存在你无权查看的重复简历，请联系资料负责人" : matches.length ? "找到了这些线索，请人工确认是不是同一个人" : "没有查到重复线索，仍请确认姓名和手机号");
  if (result.hidden_match) $("save").disabled = true;
}

/** renderMatches 仅用文本节点显示后端资料，避免候选人文本成为 HTML。 */
function renderMatches() {
  $("duplicates").replaceChildren();
  for (const match of matches) {
    const candidate = match.candidate;
    const label = document.createElement("label"); label.className = "match" + (selectedCandidate?.id === candidate.id ? " selected" : "");
    const radio = document.createElement("input"); radio.type = "radio"; radio.name = "match"; radio.checked = selectedCandidate?.id === candidate.id;
    radio.addEventListener("change", () => runAction(async () => { selectedCandidate = candidate; avatarWarning = ""; renderAvatar(); renderMatches(); refreshActions(); await loadRecords(); showStatus("已选择已有简历，备注和岗位可单独保存"); }));
    const title = document.createElement("strong"); title.textContent = `${candidate.candidate_name || candidate.name || "未填写姓名"} · ${candidate.phone || "手机号待补充"}`;
    const reason = document.createElement("span"); reason.textContent = `${match.match_type === "exact" ? "精确线索" : "可能重复，需要你确认"}：${(match.reasons || []).join("、")}`;
    const link = document.createElement("a"); link.textContent = "查看完整简历"; link.href = `${GoodHRShared.CLOUD_BASE}/admin/resumes/detail?candidate_id=${encodeURIComponent(candidate.id)}`; link.target = "_blank"; link.rel = "noreferrer";
    label.append(radio, title, document.createElement("br"), reason, document.createElement("br"), link); $("duplicates").append(label);
  }
  if (matches.some((match) => match.match_type === "exact")) { const notice = document.createElement("p"); notice.className = "muted"; notice.textContent = "手机号相同需要使用已有简历；资料更新前请点“确认更新”。"; $("duplicates").append(notice); }
}

/** loadRecords 分别展示备注时间线和岗位推荐历史。 */
async function loadRecords() {
  if (!selectedCandidate?.id) return;
  const result = await send("DETAIL", { candidate_id: selectedCandidate.id });
  $("candidate-link").href = `${GoodHRShared.CLOUD_BASE}/admin/resumes/detail?candidate_id=${encodeURIComponent(selectedCandidate.id)}`; $("candidate-link").hidden = false;
  $("notes").replaceChildren(); $("recommendations").replaceChildren(); $("records").hidden = false;
  const notes = result.notes || result.items || [];
  if (!notes.length) { const empty = document.createElement("p"); empty.className = "muted"; empty.textContent = "暂无备注，第一次记账就交给你了。"; $("notes").append(empty); }
  for (const note of notes) $("notes").append(recordNode(note.content, note.author_name || note.author_email, note.created_at));
  for (const recommendation of result.recommendations || []) {
    const node = document.createElement("article"); node.className = "record";
    const title = document.createElement("strong"); title.textContent = `${recommendation.position_name || "岗位"} · ${GoodHRShared.STAGES[recommendation.stage] || recommendation.stage}`; node.append(title);
    for (const event of recommendation.events || []) node.append(recordNode(event.content || GoodHRShared.STAGES[event.to_stage] || event.to_stage, event.author_name || event.author_email, event.created_at));
    $("recommendations").append(node);
  }
}

/** recordNode 安全展示作者、时间和保留换行的记录内容。 */
function recordNode(content, author, time) { const node = document.createElement("article"); node.className = "record"; const meta = document.createElement("small"); meta.textContent = `${author || "GoodHR 用户"} · ${time ? new Date(time).toLocaleString("zh-CN") : ""}`; const text = document.createElement("p"); text.textContent = content || ""; node.append(meta, text); return node; }

/** saveFollowups 保留失败项输入和原幂等键，成功项清空后不再重复提交。 */
async function saveFollowups() {
  requireAccount();
  const result = await send("FOLLOWUPS", { candidate_id: selectedCandidate.id, note: $("note").value, position_id: $("job").value, note_key: noteKey, recommendation_key: recommendationKey });
  if (result.notes_saved) { $("note").value = ""; noteKey = GoodHRShared.newKey(); }
  if (result.recommendation_saved) { $("job").value = ""; recommendationKey = GoodHRShared.newKey(); }
  await loadRecords();
  showStatus(result.errors?.length ? `简历已在库，${result.errors.map((item) => item.message).join("；")}。输入保留了，可点保存重试` : "已经记好了，备注和岗位各自保存成功", Boolean(result.errors?.length));
}

/** saveCandidate 创建主体后再独立写备注和岗位，重复提交使用同一幂等键。 */
async function saveCandidate(updating = false) {
  requireAccount();
  const resume = formResume();
  if (!resume.candidate_name || !resume.phone) throw new Error("请补齐姓名和完整的 11 位手机号");
  if (updating) {
    const changes = GoodHRShared.describeResumeChanges(selectedCandidate, resume);
    if (currentResume?.avatar_detected) changes.push("头像：尝试同步当前头像，读取失败会保留已有头像");
    if (!changes.length) { showStatus("资料没有变化，已有内容都保留了"); return; }
    const summary = changes.join("\n");
    if (!confirm(`将更新以下资料：\n${summary}\n\n未读到和未填写的字段会保留已有内容。确认更新吗？`)) return;
  }
  const result = await send(updating ? "UPDATE" : "CREATE", { resume, capture_id: currentResume?.capture_id, existing_avatar_data_url: selectedCandidate?.avatar_data_url, idempotency_key: saveKey, candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version });
  selectedCandidate = result.candidate || result;
  if (!selectedCandidate?.id) throw new Error("没有拿到简历编号，请先查重确认保存结果，别急着重复添加");
  avatarWarning = result.avatar_warning || ""; avatarKey = GoodHRShared.newKey(); renderAvatar();
  refreshActions(); saveKey = GoodHRShared.newKey(); await saveFollowups();
  if (avatarWarning) showStatus(`${$("status").textContent}；${avatarWarning}`, true);
}

/** retryAvatar 独立重试头像，成功后刷新版本并保持备注及岗位输入。 */
async function retryAvatar() {
  requireAccount();
  const result = await send("AVATAR_RETRY", { candidate_id: selectedCandidate?.id, row_version: selectedCandidate?.row_version, capture_id: currentResume?.capture_id, existing_avatar_data_url: selectedCandidate?.avatar_data_url, idempotency_key: avatarKey });
  if (!result.unchanged) { selectedCandidate = result.candidate || result; if (!selectedCandidate?.id) throw new Error("头像结果暂未确认，请重新查重后再试"); }
  avatarWarning = ""; avatarKey = GoodHRShared.newKey(); renderAvatar(); showStatus(result.unchanged ? "头像和已保存的一样，原图保留了" : "头像已保存，备注和岗位输入都保留了");
}

/** pollPair 每两秒查询待确认授权，授权成功后只显示账号而不显示 token。 */
async function pollPair() {
  if (!pairPending || busy) return;
  try { const result = await send("PAIR_CHECK"); $("pair-status").textContent = result.status === "pending" ? "等你在控制台确认，我在这里接着等。" : result.status === "expired" ? "授权已过期，可以再连一次。" : ""; if (result.status !== "pending") { pairPending = false; await loadState(true); } }
  catch (error) { $("pair-status").textContent = error.message; }
}

$("password-mode").addEventListener("click", () => setMode("password"));
$("code-mode").addEventListener("click", () => setMode("code"));
$("consent").addEventListener("change", () => runAction(() => send("CONSENT", { accepted: $("consent").checked })));
$("login-form").addEventListener("submit", (event) => { event.preventDefault(); runAction(async () => { await send("LOGIN", { email: $("email").value, password: mode === "password" ? $("password").value : "", code: mode === "code" ? $("code").value : "", agreement_accepted: $("agreement").checked }); $("password").value = ""; $("code").value = ""; await loadState(true); }); });
$("send-code").addEventListener("click", () => runAction(async () => { await send("SEND_CODE", { email: $("email").value }); showStatus("验证码发出了，请去邮箱看看"); }));
$("pair").addEventListener("click", () => runAction(async () => { await send("PAIR_START", { port: Number($("local-port").value) }); pairPending = true; $("pair-status").textContent = "授权页打开了，请确认账号和简历权限。"; }));
$("restore").addEventListener("click", () => runAction(async () => { const result = await send("RESTORE"); if (!result.logged_in) throw new Error("还没恢复成功，请先连接授权或手动登录"); await loadState(true); }));
$("logout").addEventListener("click", () => runAction(async () => { const result = await send("LOGOUT"); loggedIn = false; await loadState(true); showStatus(result.message, result.revocation_pending); }));
$("revoke-retry").addEventListener("click", () => runAction(async () => { const result = await send("REVOKE_RETRY"); await loadState(); showStatus(result.message, result.revocation_pending); }));
$("reload").addEventListener("click", () => runAction(loadPreview));
$("check").addEventListener("click", () => runAction(checkDuplicates));
$("job-filter").addEventListener("input", renderPositions);
$("candidate-form").addEventListener("submit", (event) => { event.preventDefault(); runAction(() => saveCandidate()); });
$("update").addEventListener("click", () => runAction(() => saveCandidate(true)));
$("save-related").addEventListener("click", () => runAction(saveFollowups));
$("retry-avatar").addEventListener("click", () => runAction(retryAvatar));
$("sidebar").addEventListener("click", () => { if (extension.sidebarAction?.open) extension.sidebarAction.open().catch((error) => showStatus(error.message, true)); else runAction(() => send("OPEN_SIDEBAR")); });
loadState(true).catch((error) => showStatus(error.message, true));
setInterval(() => { pollPair(); if (!busy) loadState().catch(() => {}); }, 2500);
