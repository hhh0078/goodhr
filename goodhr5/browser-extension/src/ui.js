/* 文件作用：工具栏账号设置页处理登录、用途授权、本地程序配对和更新提示，不包含候选人表单。 */
"use strict";
const extension = globalThis.browser || globalThis.chrome, $ = (id) => document.getElementById(id), shared = GoodHRShared;
let mode = "password", busy = false, pairPending = false, loggedIn = false, paired = false, stateSequence = 0, updateChecked = false;

/** send 通过固定后台消息操作账号和设置，界面不保存凭证。 */
async function send(type, payload = {}) { const result = await extension.runtime.sendMessage({ type, payload }); if (!result?.ok) { const unavailable = ["LOGIN", "SEND_CODE"].includes(type) && result?.status === 404; throw new Error(unavailable ? "插件登录服务暂不可用，请稍后重试" : result?.message || "没处理成功，请再试一次"); } return result.data; }
/** showStatus 展示必要操作结果，空提示不占空间。 */
function showStatus(text, error = false) { $("status").hidden = !text; $("status").textContent = text || ""; $("status").classList.toggle("error", error); }
/** showPairStatus 只在连接有进展时显示提示。 */
function showPairStatus(text) { $("pair-status").hidden = !text; $("pair-status").textContent = text || ""; }
/** refreshActions 阻止重复登录或设置写入。 */
function refreshActions() { for (const node of document.querySelectorAll("button, #consent, #auto-close")) node.disabled = busy; }
/** runAction 保留失败输入，完成后恢复操作。 */
async function runAction(action) { if (busy) return; busy = true; refreshActions(); try { await action(); } catch (error) { showStatus(error.message, true); } finally { busy = false; refreshActions(); } }
/** setMode 切换三种登录方式；本地程序复用现有安全配对流程，不保存密码。 */
function setMode(next) {
  mode = ["password", "code", "local"].includes(next) ? next : "password";
  const cloudMode = mode !== "local";
  $("login-form").hidden = !cloudMode;
  $("email").required = cloudMode;
  $("agreement").required = cloudMode;
  $("password-row").hidden = mode !== "password";
  $("password").required = mode === "password";
  $("code-row").hidden = mode !== "code";
  $("code").required = mode === "code";
  $("pair-panel").hidden = !(mode === "local" || loggedIn);
  $("pair-panel").classList.toggle("compact", loggedIn);
  for (const name of ["password", "code", "local"]) $(name + "-mode").setAttribute("aria-pressed", String(mode === name));
}
/** renderUpdate 显示固定官网下载入口，强制更新与普通更新有不同醒目程度。 */
function renderUpdate(update) { if (!update) return; const required = Boolean(update.update_required || update.required), available = required || Boolean(update.update_available); $("update-notice").hidden = !available; $("update-notice").classList.toggle("optional", !required); $("update-title").textContent = required ? "请先更新插件，才能继续保存简历" : "插件有新版本"; $("update-versions").textContent = `当前 ${update.current_version || $("version").textContent.replace(/^v/, "")} · 最新 ${update.latest_version || ""}`; $("update-link").href = shared.CLOUD_BASE + "/download#browser-extension"; }
/** loadState 同步账号与一次用途授权，旧异步响应不能覆盖新状态。 */
async function loadState() {
  const sequence = ++stateSequence, state = await send("STATE"); if (sequence !== stateSequence) return;
  loggedIn = Boolean(state.logged_in); paired = Boolean(state.paired); pairPending = Boolean(state.pairing_pending);
  $("version").textContent = `v${state.version}`; $("consent").checked = Boolean(state.data_use_accepted); $("auto-close").checked = state.auto_close_after_save !== false; $("local-port").value = state.local_port || 43129;
  $("revocation-panel").hidden = !state.revocation_pending; $("revocation-message").textContent = state.revocation_message || "";
  $("login-panel").hidden = loggedIn; $("account-row").hidden = !loggedIn; $("account").textContent = state.user?.email || "GoodHR 账号";
  $("pair-heading").textContent = loggedIn ? "本地程序" : "本地程序登录";
  $("pair-state").textContent = paired ? "已连接" : loggedIn ? "AI总结需要连接" : "本地程序已登录即可使用";
  $("pair").textContent = paired ? "重新连接" : loggedIn ? "连接本地程序" : "连接并登录";
  setMode(mode);
  if (state.revocation_pending) showStatus(state.revocation_message, true);
  renderUpdate(state.update); if (!updateChecked) { updateChecked = true; send("CHECK_UPDATE").then(renderUpdate).catch(() => {}); }
}
/** pollPair 查询既有配对申请，不重复申请授权。 */
async function pollPair() { if (!pairPending || busy) return; try { const result = await send("PAIR_CHECK"); showPairStatus(result.status === "pending" ? "等待控制台确认" : result.status === "expired" ? "授权已过期，请重新连接" : ""); if (result.status !== "pending") { pairPending = false; await loadState(); } } catch (error) { showPairStatus(error.message); } }

$("password-mode").addEventListener("click", () => setMode("password"));
$("code-mode").addEventListener("click", () => setMode("code"));
$("local-mode").addEventListener("click", () => setMode("local"));
$("consent").addEventListener("change", () => runAction(async () => { await send("CONSENT", { accepted: $("consent").checked }); showStatus($("consent").checked ? "已允许，打开候选人即可使用" : "已关闭，简历仅在本地预览"); await loadState(); }));
$("auto-close").addEventListener("change", () => runAction(async () => { const enabled = $("auto-close").checked; await send("SET_AUTO_CLOSE", { enabled }); showStatus(enabled ? "新简历入库后会自动收起" : "已保留入库后的页面卡片"); await loadState(); }));
$("login-form").addEventListener("submit", (event) => { event.preventDefault(); runAction(async () => { await send("LOGIN", { email: $("email").value, password: mode === "password" ? $("password").value : "", code: mode === "code" ? $("code").value : "", agreement_accepted: $("agreement").checked }); $("password").value = ""; $("code").value = ""; await loadState(); showStatus("已登录，打开候选人即可使用"); }); });
$("send-code").addEventListener("click", () => runAction(async () => { await send("SEND_CODE", { email: $("email").value }); showStatus("验证码已发送，请查看邮箱"); }));
$("pair").addEventListener("click", () => runAction(async () => { await send("PAIR_START", { port: Number($("local-port").value) }); pairPending = true; showPairStatus("请在控制台确认账号与授权"); }));
$("restore").addEventListener("click", () => runAction(async () => { const result = await send("RESTORE"); if (!result.logged_in) throw new Error("还没恢复登录，请连接授权或手动登录"); await loadState(); showPairStatus(result.existing_session ? "已保留当前账号；AI总结需连接本地程序" : "已恢复登录"); }));
$("logout").addEventListener("click", () => runAction(async () => { const result = await send("LOGOUT"); await loadState(); showStatus(result.message, result.revocation_pending); }));
$("revoke-retry").addEventListener("click", () => runAction(async () => { const result = await send("REVOKE_RETRY"); await loadState(); showStatus(result.message, result.revocation_pending); }));
setMode("password"); loadState().catch((error) => showStatus(error.message, true));
setInterval(() => { pollPair(); if (!busy) loadState().catch(() => {}); }, 2500);
