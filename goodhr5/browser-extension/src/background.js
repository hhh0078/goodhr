/* 文件作用：持有受限凭证、管理本地配对与云端简历请求；凭证永不交给招聘页面或插件 UI。 */
"use strict";
if (typeof importScripts === "function") importScripts("shared.js");
const extension = globalThis.browser || globalThis.chrome;
const shared = globalThis.GoodHRShared;
const snapshots = new Map();
let restoreAttempted = false;
let authGeneration = 0, revokeInFlight = null, lastRevokeAttempt = 0;

/** readSettings 读取插件私有登录与配对信息，不读取任何网站缓存。 */
async function readSettings() { return extension.storage.local.get(["session", "installation_id", "pairing_token", "local_port", "pending_pair", "pending_revoke", "data_use_accepted"]); }

/** requestJSON 请求固定白名单地址并限制超时，拒绝重定向至其他域名。 */
async function requestJSON(url, options = {}) {
  const { timeout_ms = 15000, ...fetchOptions } = options;
  const response = await fetch(url, { ...fetchOptions, redirect: "error", credentials: "omit", signal: AbortSignal.timeout(timeout_ms), headers: { "Content-Type": "application/json", ...fetchOptions.headers } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    const error = new Error(body.message || body.error?.message || (typeof body.error === "string" ? body.error : "") || `请求未成功（${response.status}）`);
    error.status = response.status; error.code = body.code || body.error?.code || ""; error.conflict = body.candidate ? { candidate: body.candidate } : null; throw error;
  }
  return body.data || body;
}

/** storeSession 保存插件专属 token，绝不保存用户密码。 */
async function storeSession(result, expectedGeneration = authGeneration, pairingGrant = null) {
  if (!result?.token || !result?.user) throw new Error("没有拿到有效的插件登录，请重新登录");
  if (expectedGeneration !== authGeneration) {
    await queueCancelledSession(result.token, pairingGrant);
    throw new Error("这次登录已经被退出取消，不会自动恢复");
  }
  await extension.storage.local.set({ session: { token: result.token, expires_at: result.expires_at, user: result.user }, ...(pairingGrant ? { pairing_token: pairingGrant.pairing_token } : {}) });
  if (expectedGeneration !== authGeneration) {
    const settings = await readSettings();
    if (settings.session?.token === result.token) await extension.storage.local.remove("session");
    if (pairingGrant && settings.pairing_token === pairingGrant.pairing_token) await extension.storage.local.remove("pairing_token");
    await queueCancelledSession(result.token, pairingGrant);
    throw new Error("这次登录已经被退出取消，不会自动恢复");
  }
}

/** revocationSummary 返回不含任何凭证的撤销进度与明确未完成提示。 */
function revocationSummary(pending) {
  if (!pending) return { revocation_pending: false, message: "已退出，云端会话和本地连接授权已撤销" };
  const waiting = [];
  if (pending.pairing_token) waiting.push("本地程序连接授权");
  if (pending.cloud_tokens?.length) waiting.push("云端会话");
  return { revocation_pending: true, message: `插件已停止使用退出的账号；${waiting.join("和")}仍未撤销。请保持本地程序运行并重试，我不会自动恢复登录` };
}

/** queueCancelledSession 保存登出过程中迟到的刷新凭证，只用于随后撤销，不恢复账号。 */
async function queueCancelledSession(token, pairingGrant = null) {
  if (revokeInFlight) await revokeInFlight;
  const settings = await readSettings(), pending = settings.pending_revoke || { cloud_tokens: [], created_at: Date.now() };
  pending.cloud_tokens = [...new Set([...(pending.cloud_tokens || []), ...(token ? [token] : [])])];
  if (pairingGrant) Object.assign(pending, pairingGrant);
  await extension.storage.local.set({ pending_revoke: pending });
}

/** processPendingRevocation 逐项撤销本地授权及云会话，失败密钥私有保留供幂等重试。 */
async function processPendingRevocation() {
  const { pending_revoke: pending } = await readSettings();
  if (!pending) return revocationSummary(null);
  if (pending.pairing_token) {
    try {
      await requestJSON(localBase({ local_port: pending.local_port }) + "/api/v1/extensions/pair/revoke", { method: "POST", body: JSON.stringify({ installation_id: pending.installation_id, pairing_token: pending.pairing_token }), timeout_ms: 4000 });
      delete pending.pairing_token;
      await extension.storage.local.set({ pending_revoke: pending });
    } catch { /* 私有撤销密钥继续保留，不把连接错误或凭证暴露给页面。 */ }
  }
  const failed = [];
  for (const token of pending.cloud_tokens || []) {
    try { await requestJSON(shared.CLOUD_BASE + "/api/auth/extension-logout", { method: "POST", headers: { Authorization: `Bearer ${token}` }, body: "{}", timeout_ms: 6000 }); }
    catch { failed.push(token); }
  }
  pending.cloud_tokens = failed;
  if (pending.pairing_token || failed.length) { await extension.storage.local.set({ pending_revoke: pending }); return revocationSummary(pending); }
  await extension.storage.local.remove("pending_revoke"); return revocationSummary(null);
}

/** retryRevocation 合并同时打开的界面重试，自动检查至少间隔 30 秒。 */
async function retryRevocation(force = false) {
  if (revokeInFlight) return revokeInFlight;
  const settings = await readSettings();
  if (revokeInFlight) return revokeInFlight;
  if (!settings.pending_revoke) return revocationSummary(null);
  if (!force && Date.now() - lastRevokeAttempt < 30000) return revocationSummary(settings.pending_revoke);
  lastRevokeAttempt = Date.now();
  revokeInFlight = processPendingRevocation().finally(() => { revokeInFlight = null; });
  return revokeInFlight;
}

/** logout 先退出本机使用态，再独立撤销两端授权，任何失败均明确返回待撤销状态。 */
async function logout() {
  authGeneration++; restoreAttempted = true;
  if (revokeInFlight) await revokeInFlight;
  const settings = await readSettings(), pending = settings.pending_revoke || { cloud_tokens: [], created_at: Date.now() };
  if (settings.session?.token) pending.cloud_tokens = [...new Set([...(pending.cloud_tokens || []), settings.session.token])];
  if (settings.pairing_token) { pending.pairing_token = settings.pairing_token; pending.installation_id = settings.installation_id; pending.local_port = settings.local_port || 43129; }
  if (pending.pairing_token || pending.cloud_tokens?.length) await extension.storage.local.set({ pending_revoke: pending });
  await extension.storage.local.remove(["session", "pairing_token", "pending_pair"]); snapshots.clear();
  return retryRevocation(true);
}

/** cloudRequest 请求插件受限接口，过期时最多刷新一次，不自动重放业务失败。 */
async function cloudRequest(path, body, method, retry = true) {
  const generation = authGeneration;
  const settings = await readSettings();
  if (!settings.session?.token) throw new Error("我先小声提醒一下，请先登录 GoodHR");
  try {
    return await requestJSON(shared.CLOUD_BASE + path, { method: method || (body ? "POST" : "GET"), headers: { Authorization: `Bearer ${settings.session.token}` }, ...(body ? { body: JSON.stringify(body) } : {}) });
  } catch (error) {
    if (error.status === 401 && retry && path !== "/api/auth/extension-refresh") {
      try {
        const refreshed = await requestJSON(shared.CLOUD_BASE + "/api/auth/extension-refresh", { method: "POST", headers: { Authorization: `Bearer ${settings.session.token}` }, body: "{}" });
        await storeSession(refreshed, generation);
      } catch { if ((await readSettings()).session?.token === settings.session.token) await extension.storage.local.remove("session"); throw new Error("登录已经过期，请重新登录，我保留了当前表单"); }
      return cloudRequest(path, body, method, false);
    }
    throw error;
  }
}

/** localBase 仅允许本机 127.0.0.1 的合法端口作为配对目标。 */
function localBase(settings) { const port = Number(settings.local_port || 43129); if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("本地程序端口不正确"); return `http://127.0.0.1:${port}`; }

/** localRequest 调用 loopback 授权桥，不提供无认证读取已有云 token 的入口。 */
async function localRequest(path, body) { return requestJSON(localBase(await readSettings()) + path, { method: "POST", body: JSON.stringify(body) }); }

/** ensureInstallation 生成插件安装标识，账号切换不会复用云端业务会话。 */
async function ensureInstallation() {
  const settings = await readSettings(); if (settings.installation_id) return settings.installation_id;
  const id = shared.newKey(); await extension.storage.local.set({ installation_id: id }); return id;
}

/** tryRestore 尝试已配对本地程序自动恢复，失败时转人工配对或登录。 */
async function tryRestore() {
  const settings = await readSettings();
  if (settings.pending_revoke || restoreAttempted || settings.session?.token || !settings.pairing_token) return;
  const generation = authGeneration;
  restoreAttempted = true;
  try { await storeSession(await localRequest("/api/v1/extensions/session/restore", { installation_id: await ensureInstallation(), pairing_token: settings.pairing_token }), generation); }
  catch { /* 本地程序未运行或账号变化时，界面仍可手动登录。 */ }
}

/** beginPair 产生随机挑战并打开同域授权页，首次必须由用户在控制台确认。 */
async function beginPair(port) {
  if ((await readSettings()).pending_revoke) throw new Error("旧连接授权还没撤销，请先点重试撤销，再连接新的账号");
  if (port) { if (!Number.isInteger(Number(port)) || Number(port) < 1 || Number(port) > 65535) throw new Error("请填正确的本地端口"); await extension.storage.local.set({ local_port: Number(port) }); }
  const installationID = await ensureInstallation();
  const challenge = Array.from(crypto.getRandomValues(new Uint8Array(32)), (item) => item.toString(16).padStart(2, "0")).join("");
  const result = await localRequest("/api/v1/extensions/pair/start", { installation_id: installationID, challenge });
  const approve = new URL(result.approve_url);
  if (![shared.CLOUD_BASE, "http://127.0.0.1:5173"].includes(approve.origin) || approve.pathname !== "/admin/resumes") throw new Error("本地程序返回了不受支持的授权地址");
  await extension.storage.local.set({ pending_pair: { pairing_id: result.pairing_id, installation_id: installationID, challenge, expires_at: Date.now() + Math.min(Number(result.expires_in || 300), 600) * 1000 } });
  await extension.tabs.create({ url: approve.href });
  return { status: "pending" };
}

/** checkPair 查询同一短时挑战的授权结果，凭证仅在后台保存。 */
async function checkPair() {
  const generation = authGeneration;
  const { pending_pair: pending } = await readSettings(); if (!pending) return { status: "none" };
  if (Date.now() >= pending.expires_at) { await extension.storage.local.remove("pending_pair"); return { status: "expired" }; }
  const result = await localRequest("/api/v1/extensions/pair/status", { installation_id: pending.installation_id, challenge: pending.challenge, pairing_id: pending.pairing_id });
  if (result.status === "approved") {
    const settings = await readSettings();
    await storeSession(result, generation, result.pairing_token ? { pairing_token: result.pairing_token, installation_id: pending.installation_id, local_port: settings.local_port || 43129 } : null);
    await extension.storage.local.remove("pending_pair");
    restoreAttempted = false;
  }
  return { status: result.status };
}

/** activeTab 获取当前活动页面，用于重新采集当前候选人。 */
async function activeTab() { const tabs = await extension.tabs.query({ active: true, currentWindow: true }); return tabs[0]; }

/** currentSnapshot 主动询问只读脚本，确保服务 Worker 重启后不使用旧候选人缓存。 */
async function currentSnapshot() {
  const tab = await activeTab(); if (!tab?.id || !shared.platformForURL(tab.url)) return null;
  try { await extension.tabs.sendMessage(tab.id, { type: "REQUEST_CAPTURE" }); } catch { return null; }
  return snapshots.get(tab.id) || null;
}

/** publicState 返回不含 token、配对令牌、挑战值的界面状态。 */
async function publicState() {
  await retryRevocation(); await tryRestore(); const settings = await readSettings();
  return { logged_in: Boolean(settings.session?.token), user: settings.session?.user ? { email: settings.session.user.email, id: settings.session.user.id } : null, expires_at: settings.session?.expires_at, local_port: settings.local_port || 43129, pairing_pending: Boolean(settings.pending_pair), revocation_pending: Boolean(settings.pending_revoke), revocation_message: settings.pending_revoke ? revocationSummary(settings.pending_revoke).message : "", data_use_accepted: Boolean(settings.data_use_accepted), resume: await currentSnapshot(), version: extension.runtime.getManifest().version };
}

/** requireConsent 在任何云端业务提交前检查用户的简历数据授权。 */
async function requireConsent() { if (!(await readSettings()).data_use_accepted) throw new Error("请先同意把确认后的简历资料保存到 GoodHR"); }

/** createCandidate 在后端再校验手机号，并用幂等键防止连续点击重复添加。 */
async function createCandidate(payload, updating = false) {
  await requireConsent(); const resume = updating ? shared.nonEmptyResumePatch(payload.resume) : shared.sanitizeResume(payload.resume);
  if (!resume.candidate_name || !resume.phone) throw new Error("姓名和完整手机号都填一下，我才能认真入库");
  const input = { ...resume, idempotency_key: shared.cleanText(payload.idempotency_key, 100) };
  if (!input.idempotency_key) throw new Error("这次保存缺少标识，请重新点击添加");
  if (updating) {
    if (!payload.candidate_id || !payload.row_version) throw new Error("请重新查重后再更新，避免覆盖别人刚改的资料");
    input.row_version = payload.row_version;
    return cloudRequest(`/api/candidates/${encodeURIComponent(payload.candidate_id)}`, input, "PATCH");
  }
  return cloudRequest("/api/candidates", input);
}

/** handleUIMessage 接收插件界面的明确操作，所有接口固定而非通用代理。 */
async function handleUIMessage(message) {
  const payload = message.payload || {};
  switch (message.type) {
    case "STATE": return publicState();
    case "PREVIEW": return { resume: await currentSnapshot(), version: extension.runtime.getManifest().version };
    case "CONSENT": await extension.storage.local.set({ data_use_accepted: payload.accepted === true }); return { ok: true };
    case "SEND_CODE": return requestJSON(shared.CLOUD_BASE + "/api/auth/extension-send-code", { method: "POST", body: JSON.stringify({ email: shared.cleanText(payload.email, 120) }) });
    case "LOGIN": {
      const generation = authGeneration;
      const input = { email: shared.cleanText(payload.email, 120), agreement_accepted: payload.agreement_accepted === true };
      if (payload.code) input.code = shared.cleanText(payload.code, 20); else input.password = String(payload.password || "");
      await storeSession(await requestJSON(shared.CLOUD_BASE + "/api/auth/extension-login", { method: "POST", body: JSON.stringify(input) }), generation);
      return publicState();
    }
    case "LOGOUT": return logout();
    case "REVOKE_RETRY": return retryRevocation(true);
    case "PAIR_START": return beginPair(payload.port);
    case "PAIR_CHECK": return checkPair();
    case "RESTORE": restoreAttempted = false; await tryRestore(); return publicState();
    case "POSITIONS": return cloudRequest("/api/positions");
    case "DUPLICATES": {
      await requireConsent(); const resume = shared.sanitizeResume(payload.resume);
      return cloudRequest("/api/candidates/duplicates", { phone: resume.phone, candidate_name: resume.candidate_name, platform_id: resume.platform_id, platform_candidate_id: resume.platform_candidate_id, keyword: shared.cleanText(payload.keyword, 200), exclude_id: payload.exclude_id || "" });
    }
    case "CREATE": return createCandidate(payload);
    case "UPDATE": return createCandidate(payload, true);
    case "DETAIL": {
      const path = `/api/candidates/${encodeURIComponent(payload.candidate_id)}`;
      const [notes, recommendations] = await Promise.all([cloudRequest(path + "/notes"), cloudRequest(path + "/recommendations")]); return { ...notes, ...recommendations };
    }
    case "FOLLOWUPS": {
      await requireConsent(); if (!payload.candidate_id) throw new Error("先选择候选人，再保存备注或岗位");
      return shared.saveFollowups((path, body) => cloudRequest(path, body), payload.candidate_id, { note: payload.note, position_id: payload.position_id, note_key: payload.note_key, recommendation_key: payload.recommendation_key });
    }
    case "OPEN_SIDEBAR": {
      const tab = await activeTab();
      if (extension.sidePanel?.open && tab?.id) await extension.sidePanel.open({ tabId: tab.id });
      else if (extension.sidebarAction?.open) await extension.sidebarAction.open();
      else throw new Error("当前浏览器暂不支持侧栏，请继续用这个弹窗");
      return { ok: true };
    }
    default: throw new Error("这个操作暂时还不认识，请刷新插件");
  }
}

/** onMessage 严格区分招聘内容脚本与插件 UI，页面脚本只能上报资料不能请求授权或 API。 */
function onMessage(message, sender, sendResponse) {
  if (sender.id !== extension.runtime.id) return false;
  if (sender.tab) {
    if (message.type !== "CANDIDATE_SNAPSHOT" || sender.frameId !== 0 || !shared.platformForURL(sender.tab.url)) return false;
    const resume = message.resume ? shared.sanitizeResume(message.resume) : null;
    if (resume?.platform_id && resume.platform_id !== shared.platformForURL(sender.tab.url)) return false;
    if (resume?.candidate_name) snapshots.set(sender.tab.id, resume); else snapshots.delete(sender.tab.id);
    extension.action.setBadgeText({ tabId: sender.tab.id, text: resume?.candidate_name ? "+" : "" });
    extension.action.setBadgeBackgroundColor({ tabId: sender.tab.id, color: "#8A6721" });
    extension.action.setTitle({ tabId: sender.tab.id, title: resume?.candidate_name ? "当前有简历，可添加到 GoodHR" : "GoodHR 简历助手" });
    sendResponse({ ok: true }); return false;
  }
  if (!sender.url?.startsWith(extension.runtime.getURL(""))) return false;
  handleUIMessage(message).then((data) => sendResponse({ ok: true, data }), (error) => sendResponse({ ok: false, message: error.message, status: error.status || 0, code: error.code || "", conflict: error.conflict || null }));
  return true;
}

extension.runtime.onMessage.addListener(onMessage);
extension.tabs.onRemoved.addListener((tabID) => snapshots.delete(tabID));
extension.tabs.onUpdated.addListener((tabID, change) => { if (change.url) snapshots.delete(tabID); });
if (extension.storage.local.setAccessLevel) extension.storage.local.setAccessLevel({ accessLevel: "TRUSTED_CONTEXTS" });
