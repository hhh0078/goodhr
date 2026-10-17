/* 文件作用：持有受限凭证、管理本地配对与云端简历请求；凭证永不交给招聘页面或插件 UI。 */
"use strict";
if (typeof importScripts === "function") importScripts("shared.js", "avatar.js");
const extension = globalThis.browser || globalThis.chrome;
const shared = globalThis.GoodHRShared;
const avatar = globalThis.GoodHRAvatar;
const snapshots = new Map();
const snapshotSequences = new Map();
const attachmentImportRequests = new Map();
const attachmentTaskScopes = new Map();
let restoreAttempted = false;
let authGeneration = 0, revokeInFlight = null, lastRevokeAttempt = 0;
const duplicateCache = new Map(), captureTimers = new Map();
let draftQueue = Promise.resolve();
const refreshRequests = new Map();
let updateInFlight = null, updateInitialization = null, updateCheckedAt = 0, knownUpdate = null;
const VERSION_WRITES = new Set(["CREATE", "UPDATE", "AVATAR_RETRY", "FOLLOWUPS", "SUMMARY_START", "SUMMARY_RETRY", "SUMMARY_SAVE", "ATTACHMENT_START"]);

/** extensionBrowser 从浏览器自身清单识别 Firefox，版本检查不与 Chrome 的发布节奏互相锁定。 */
function extensionBrowser() { return extension.runtime.getManifest().browser_specific_settings?.gecko ? "firefox" : "chrome"; }

/** updateSummary 只把经过校验的公开版本和本站下载入口交给界面。 */
function updateSummary(release = {}) {
  const latest = shared.cleanText(release.version || release.latest_version, 50), available = /^\d+\.\d+\.\d+$/.test(latest) && shared.compareVersions(latest, extension.runtime.getManifest().version) > 0;
  return { current_version: extension.runtime.getManifest().version, latest_version: latest, note: shared.cleanText(release.note, 1000), update_available: available, update_required: available, url: shared.CLOUD_BASE + "/download#browser-extension" };
}

/** checkUpdate 合并公开版本检查；网络失败保留已确认的更新要求，重启后仍然有效。 */
async function checkUpdate(force = false) {
  if (!knownUpdate) {
    if (!updateInitialization) updateInitialization = (async () => {
      try { const stored = await extension.storage.local.get(["known_extension_release"]); knownUpdate = updateSummary(stored.known_extension_release); }
      catch { knownUpdate = updateSummary(); }
    })().finally(() => { updateInitialization = null; });
    await updateInitialization;
  }
  if (updateInFlight) return updateInFlight;
  if (!force && updateCheckedAt && Date.now() - updateCheckedAt < 60000) return knownUpdate;
  updateInFlight = (async () => {
    try {
      const result = await requestJSON(shared.CLOUD_BASE + "/api/system/browser-extension?browser=" + extensionBrowser(), { timeout_ms: 6000 }), release = result.release || {};
      if (/^\d+\.\d+\.\d+$/.test(String(release.version || ""))) {
        knownUpdate = updateSummary(release);
        await extension.storage.local.set({ known_extension_release: { version: knownUpdate.latest_version, note: knownUpdate.note } });
      }
    } catch { /* 公开配置暂不可达时，不抹掉已发现的新版，不阻止本地只读预览。 */ }
    finally { updateCheckedAt = Date.now(); }
    return knownUpdate;
  })().finally(() => { updateInFlight = null; });
  return updateInFlight;
}

/** requireCurrentVersion 在后台拦截旧版写操作，界面按钮或脚本均不能绕过强制更新。 */
async function requireCurrentVersion() {
  const update = await checkUpdate();
  if (update.update_required) throw Object.assign(new Error(`插件需要更新到 v${update.latest_version}，请前往官网下载新版后继续`), { code: "EXTENSION_UPDATE_REQUIRED", status: 426 });
}

/** readSettings 读取插件私有登录与配对信息，不读取任何网站缓存。 */
async function readSettings() { return extension.storage.local.get(["session", "installation_id", "pairing_token", "pairing_account_key", "local_port", "pending_pair", "pending_revoke", "data_use_accepted", "auto_close_after_save"]); }

/** requestJSON 请求固定白名单地址并限制超时，拒绝重定向至其他域名。 */
async function requestJSON(url, options = {}) {
  const { timeout_ms = 15000, ...fetchOptions } = options;
  const response = await fetch(url, { ...fetchOptions, redirect: "error", credentials: "omit", signal: AbortSignal.timeout(timeout_ms), headers: { "Content-Type": "application/json", ...fetchOptions.headers } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    const error = new Error(body.message || body.error?.message || (typeof body.error === "string" ? body.error : "") || `请求未成功（${response.status}）`);
    error.status = response.status; error.code = body.error_code || body.code || body.error?.code || ""; error.conflict = body.candidate ? { candidate: body.candidate } : null; throw error;
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
  await extension.storage.local.set({ session: { token: result.token, expires_at: result.expires_at, user: result.user }, ...(pairingGrant ? { pairing_token: pairingGrant.pairing_token, pairing_account_key: accountKey({ session: { user: result.user } }) } : {}) });
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
  duplicateCache.clear(); attachmentImportRequests.clear(); attachmentTaskScopes.clear(); for (const timer of captureTimers.values()) clearTimeout(timer); captureTimers.clear();
  if (revokeInFlight) await revokeInFlight;
  const settings = await readSettings(), pending = settings.pending_revoke || { cloud_tokens: [], created_at: Date.now() };
  if (settings.session?.token) pending.cloud_tokens = [...new Set([...(pending.cloud_tokens || []), settings.session.token])];
  if (settings.pairing_token) { pending.pairing_token = settings.pairing_token; pending.installation_id = settings.installation_id; pending.local_port = settings.local_port || 43129; }
  if (pending.pairing_token || pending.cloud_tokens?.length) await extension.storage.local.set({ pending_revoke: pending });
  await extension.storage.local.remove(["session", "pairing_token", "pairing_account_key", "pending_pair"]); snapshots.clear();
  return retryRevocation(true);
}

/** cloudRequest 请求插件受限接口，过期时最多刷新一次，不自动重放业务失败。 */
async function cloudRequest(path, body, method, retry = true, expectedGeneration = authGeneration) {
  const generation = expectedGeneration;
  const settings = await readSettings();
  if (generation !== authGeneration) throw new Error("账号已经变化，本次操作已停止，请确认账号后重新保存");
  if (!settings.session?.token) throw new Error("我先小声提醒一下，请先登录 GoodHR");
  try {
    const result = await requestJSON(shared.CLOUD_BASE + path, { method: method || (body ? "POST" : "GET"), headers: { Authorization: `Bearer ${settings.session.token}` }, ...(body ? { body: JSON.stringify(body) } : {}) });
    if (generation !== authGeneration) throw new Error("账号已经变化，旧账号的结果已停止展示，请重新确认保存结果");
    return result;
  } catch (error) {
    if (error.status === 401 && retry && path !== "/api/auth/extension-refresh") {
      if (generation !== authGeneration) throw new Error("账号已经变化，本次操作已停止，请确认账号后重新保存");
      try {
        await refreshSession(settings.session.token, generation);
      } catch { if ((await readSettings()).session?.token === settings.session.token) await extension.storage.local.remove("session"); throw new Error("登录已经过期，请重新登录，我保留了当前表单"); }
      return cloudRequest(path, body, method, false, generation);
    }
    throw error;
  }
}

/** refreshSession 同一账号代次和旧token只刷新一次，并发401复用已更新的受限会话。 */
async function refreshSession(oldToken, generation) {
  const settings = await readSettings();
  if (generation !== authGeneration) throw new Error("账号已经变化，不会恢复旧账号登录");
  if (settings.session?.token && settings.session.token !== oldToken) return;
  const key = JSON.stringify([generation, oldToken]);
  let pending = refreshRequests.get(key);
  if (!pending) {
    pending = (async () => { const result = await requestJSON(shared.CLOUD_BASE + "/api/auth/extension-refresh", { method: "POST", headers: { Authorization: `Bearer ${oldToken}` }, body: "{}" }); await storeSession(result, generation); })();
    refreshRequests.set(key, pending);
  }
  try { await pending; } finally { if (refreshRequests.get(key) === pending) refreshRequests.delete(key); }
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
  try { await storeSession(await localRequest("/api/v1/extensions/session/restore", { installation_id: await ensureInstallation(), pairing_token: settings.pairing_token }), generation, { pairing_token: settings.pairing_token, installation_id: settings.installation_id, local_port: settings.local_port || 43129 }); }
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
    const grant = result.pairing_token ? { pairing_token: result.pairing_token, installation_id: pending.installation_id, local_port: settings.local_port || 43129 } : null;
    if (generation === authGeneration && settings.session?.user && accountKey(settings) !== accountKey({ session: { user: result.user } })) {
      await extension.storage.local.remove("pending_pair"); await queueCancelledSession(result.token, grant); await retryRevocation(true);
      throw new Error("本地和插件账号不同，请先退出当前账号再连接。新连接授权已停止使用，撤销失败可点重试撤销");
    }
    const sessionGeneration = generation === authGeneration ? ++authGeneration : generation;
    await storeSession(result, sessionGeneration, grant);
    await extension.storage.local.remove("pending_pair");
    if (settings.session?.token && settings.session.token !== result.token && sessionGeneration === authGeneration) { await queueCancelledSession(settings.session.token); await retryRevocation(true); }
    restoreAttempted = false;
  }
  return { status: result.status };
}

/** activeTab 获取当前活动页面，用于重新采集当前候选人。 */
async function activeTab() { const tabs = await extension.tabs.query({ active: true, currentWindow: true }); return tabs[0]; }

/** currentSnapshot 主动询问只读脚本，确保服务 Worker 重启后不使用旧候选人缓存。 */
async function currentSnapshot(sourceTab = null) {
  const tab = sourceTab ? (extension.tabs.get ? await extension.tabs.get(sourceTab.id).catch(() => null) : sourceTab) : await activeTab();
  if (!tab?.id || !shared.platformForURL(tab.url)) return null;
  try { const result = await extension.tabs.sendMessage(tab.id, { type: "REQUEST_CAPTURE" }, { frameId: 0 }); if (result?.ok === false) return null; } catch { return null; }
  return snapshots.get(tab.id) || null;
}

/** requireCurrentCapture 校验悬浮面板自己的页面快照，切人或离开招聘页时停止迟到保存。 */
async function requireCurrentCapture(captureID, sourceTab) {
  if (!sourceTab) return;
  const snapshot = await currentSnapshot(sourceTab);
  if (!captureID || !snapshot || snapshot.capture_id !== captureID) throw Object.assign(new Error("候选人页面已经变化，请重新读取，确认当前人后再保存"), { code: "CAPTURE_SCOPE_CHANGED" });
}

/** publicResume 仅公开头像已识别状态，不把招聘图片外链和临时参数交给 UI。 */
function publicResume(snapshot) { if (!snapshot) return null; const { avatar_source_url, avatar_candidates, attachment, ...resume } = snapshot; return { ...resume, ...(attachment ? { attachment: Object.fromEntries(Object.entries(attachment).filter(([key]) => key !== "text")) } : {}), avatar_detected: Boolean(avatar_source_url || avatar_candidates?.length) }; }

/** requireActiveSession 在图片外部读取前检查插件账号，未登录预览不能触发图片请求。 */
async function requireActiveSession() { if (!(await readSettings()).session?.token) throw new Error("请先登录 GoodHR，再保存简历和头像"); }

/** prepareAvatar 绑定本次候选人快照，保存点击后才读取头像，切换详情时不串图。 */
async function prepareAvatar(captureID, sourceTab = null) {
  if (!captureID) return { data_url: "", warning: "" };
  const generation = authGeneration, boundTab = sourceTab || await activeTab(), snapshot = await currentSnapshot(boundTab);
  if (!snapshot || snapshot.capture_id !== captureID) throw Object.assign(new Error("候选人页面已经变化，请重新读取后再保存"), { code: "CAPTURE_SCOPE_CHANGED" });
  if (!snapshot.avatar_source_url && !snapshot.avatar_candidates?.length) return { data_url: "", warning: "" };
  let image;
  try { image = { data_url: snapshot.attachment ? await avatar.fetchAttachmentAvatar(snapshot.avatar_candidates, snapshot.source_url) : await avatar.fetchAvatar(snapshot.avatar_source_url, snapshot.source_url), warning: "" }; }
  catch (error) { image = { data_url: "", warning: `${error.message || "头像暂时没保存好"}。简历资料仍可保存，原有头像会保留` }; }
  if (generation !== authGeneration) throw new Error("账号已经变化，本次头像保存已停止，请确认账号后重试");
  const current = await currentSnapshot(boundTab);
  if (generation !== authGeneration) throw new Error("账号已经变化，本次头像保存已停止，请确认账号后重试");
  if (!current || current.capture_id !== captureID || current.avatar_source_url !== snapshot.avatar_source_url || JSON.stringify(current.avatar_candidates) !== JSON.stringify(snapshot.avatar_candidates)) throw Object.assign(new Error("候选人页面已经变化，头像未保存，请重新读取后再保存"), { code: "CAPTURE_SCOPE_CHANGED" });
  return image;
}

/** publicState 返回不含 token、配对令牌、挑战值的界面状态。 */
async function publicState(sourceTab = null) {
  await retryRevocation(); await tryRestore(); const settings = await readSettings();
  return { logged_in: Boolean(settings.session?.token), account_key: accountKey(settings), paired: Boolean(settings.pairing_token && !settings.pending_revoke), user: settings.session?.user ? { email: settings.session.user.email, id: settings.session.user.id } : null, expires_at: settings.session?.expires_at, local_port: settings.local_port || 43129, pairing_pending: Boolean(settings.pending_pair), revocation_pending: Boolean(settings.pending_revoke), revocation_message: settings.pending_revoke ? revocationSummary(settings.pending_revoke).message : "", data_use_accepted: Boolean(settings.data_use_accepted), auto_close_after_save: settings.auto_close_after_save !== false, resume: publicResume(await currentSnapshot(sourceTab)), version: extension.runtime.getManifest().version, update: await checkUpdate() };
}

/** accountKey 用公开账号标识隔离草稿和迟到界面请求，不包含任何凭证。 */
function accountKey(settings) { return settings.session?.user ? JSON.stringify([settings.session.user.id || "", settings.session.user.email || ""]) : "preview"; }

/** checkDuplicates 合并相同条件的并发查重，短期缓存不跨账号或不同候选人。 */
async function checkDuplicates(payload) {
  const generation = payload.expected_generation ?? authGeneration; await requireConsent(); await requireActiveSession();
  const input = shared.duplicateInput(payload.resume, payload.keyword, payload.exclude_id), key = JSON.stringify([generation, input]);
  const cached = duplicateCache.get(key); if (!payload.force && cached && Date.now() - cached.time < 15000) return cached.promise;
  const promise = cloudRequest("/api/candidates/duplicates", input, "POST", true, generation);
  duplicateCache.set(key, { time: Date.now(), promise }); if (duplicateCache.size > 40) duplicateCache.delete(duplicateCache.keys().next().value);
  try { return await promise; } catch (error) { if (duplicateCache.get(key)?.promise === promise) duplicateCache.delete(key); throw error; }
}

/** scheduleCaptureCheck 捕获后自动查重，只在已登录且同意数据用途时发出有限身份查询。 */
function scheduleCaptureCheck(tabID, resume) {
  clearTimeout(captureTimers.get(tabID)); if (!resume || resume.attachment) return;
  const generation = authGeneration;
  captureTimers.set(tabID, setTimeout(async () => {
    captureTimers.delete(tabID);
    try {
      const settings = await readSettings(); if (!settings.session?.token || !settings.data_use_accepted || generation !== authGeneration || snapshots.get(tabID)?.capture_id !== resume.capture_id) return;
      const result = await checkDuplicates({ resume }); if (generation !== authGeneration || snapshots.get(tabID)?.capture_id !== resume.capture_id) return;
      const state = shared.classifyDuplicates(result, resume.phone); extension.action.setBadgeText({ tabId: tabID, text: state.state === "existing" ? "✓" : state.state === "possible" || state.state === "hidden" ? "?" : "+" }); extension.action.setTitle({ tabId: tabID, title: `GoodHR：${state.title}` });
    } catch { /* 后台自动查询失败只保留可读取徽标；打开界面后会明确显示并允许重试。 */ }
  }, 450));
}

/** performDraftOperation 读取24小时内的私有草稿，切账号后拒绝旧界面写入。 */
async function performDraftOperation(payload, saving) {
  const generation = authGeneration, settings = await readSettings(), account = accountKey(settings), identity = shared.cleanText(payload.identity, 1500);
  if (!identity || payload.account_key !== account) throw Object.assign(new Error("账号或候选人已经变化，旧草稿不会带到当前账号"), { code: "DRAFT_SCOPE_CHANGED" });
  const stored = await extension.storage.local.get(["ui_drafts"]), drafts = Object.fromEntries(Object.entries(stored.ui_drafts || {}).filter(([, item]) => Date.now() - item.updated_at < 86400000));
  if (generation !== authGeneration) throw Object.assign(new Error("账号已经变化，旧草稿已停止读取"), { code: "DRAFT_SCOPE_CHANGED" });
  const key = JSON.stringify([account, identity]);
  if (!saving) { if (Object.keys(drafts).length !== Object.keys(stored.ui_drafts || {}).length) await extension.storage.local.set({ ui_drafts: drafts }); return { draft: drafts[key]?.data || null }; }
  if (generation !== authGeneration) throw Object.assign(new Error("账号已经变化，旧草稿已停止保存"), { code: "DRAFT_SCOPE_CHANGED" });
  const data = payload.draft || {}, fields = {};
  const frozenSource = data.summary_source ? shared.stripSummaryContacts(data.summary_source) : null;
  if (frozenSource && new TextEncoder().encode(JSON.stringify(frozenSource)).byteLength > 1024 * 1024) throw new Error("这份总结职业资料超过1MiB，本地草稿没保存，请先整理履历后再生成");
  for (const name of ["name", "phone", "candidate-email", "city", "position", "expected-position", "gender", "first-company", "first-work-start", "first-work-end", "first-school", "first-school-start", "first-school-end", "duplicate-keyword", "job-filter", "job", "folder-filter", "stage", "note", "note-time", "recommendation-note", "recommendation-time"]) fields[name] = shared.cleanText(data.fields?.[name], name.includes("note") ? 12000 : 500);
  drafts[key] = { updated_at: Date.now(), data: { fields, folder_ids: shared.sanitizeFolderIDs(data.folder_ids), folders_edited: data.folders_edited === true, edited: Array.isArray(data.edited) ? data.edited.filter((item) => Object.hasOwn(fields, item)) : [], keys: Object.fromEntries(["save", "note", "recommendation", "avatar", "summary"].map((item) => [item, shared.cleanText(data.keys?.[item], 100)])), ai_summary: shared.cleanText(data.ai_summary, 12000), summary_task: data.summary_task?.id ? { id: shared.cleanText(data.summary_task.id, 100), status: shared.cleanText(data.summary_task.status, 40), possibly_charged: data.summary_task.possibly_charged === true } : null, summary_unknown: data.summary_unknown === true, summary_source: frozenSource } };
  // 只保存任务回执和已识别结构化字段，不在磁盘草稿复制附件全文。
  drafts[key].data.attachment = data.attachment ? { capture_id: shared.cleanText(data.attachment.capture_id, 160), basic_key: shared.cleanText(data.attachment.basic_key, 128), full_key: shared.cleanText(data.attachment.full_key, 128), basic_task: attachmentTaskReceipt(data.attachment.basic_task), full_task: attachmentTaskReceipt(data.attachment.full_task), basic_unknown: data.attachment.basic_unknown === true, basic_error: shared.cleanText(data.attachment.basic_error, 500), save_request: shared.sanitizeAttachmentImport(data.attachment.save_request), save_task: shared.attachmentImportReceipt(data.attachment.save_task), save_unknown: data.attachment.save_unknown === true, save_not_found: data.attachment.save_not_found === true, save_applied: shared.cleanText(data.attachment.save_applied, 100) } : null;
  if (drafts[key].data.attachment?.full_task) delete drafts[key].data.attachment.full_task.resume;
  if (drafts[key].data.attachment) drafts[key].data.attachment.save_history = (Array.isArray(data.attachment.save_history) ? data.attachment.save_history : []).slice(0, 3).map(shared.attachmentImportReceipt).filter(Boolean);
  if (drafts[key].data.attachment) drafts[key].data.attachment.legacy_confirmed = shared.cleanText(data.attachment.legacy_confirmed, 100);
  if (drafts[key].data.attachment) drafts[key].data.attachment.full_unknown = data.attachment.full_unknown === true;
  const entries = Object.entries(drafts).sort(([left, a], [right, b]) => left === key ? -1 : right === key ? 1 : b.updated_at - a.updated_at).slice(0, 30);
  while (entries.length > 1 && new TextEncoder().encode(JSON.stringify(Object.fromEntries(entries))).byteLength > 3 * 1024 * 1024) entries.pop();
  await extension.storage.local.set({ ui_drafts: Object.fromEntries(entries) }); return { ok: true };
}

/** draftOperation 串行化不同弹窗的本地草稿读写，避免丢失另一个候选人的输入。 */
function draftOperation(payload, saving) { const operation = draftQueue.then(() => performDraftOperation(payload, saving)); draftQueue = operation.catch(() => {}); return operation; }

/** attachmentTaskReceipt 只向隔离 UI 和私有草稿提供任务结果白名单，不回显输入正文。 */
function attachmentTaskReceipt(task) {
  if (!task?.id) return null;
  return { id: shared.cleanText(task.id, 100), phase: task.phase === "full" ? "full" : "basic", capture_id: shared.cleanText(task.capture_id, 160), text_hash: shared.cleanText(task.text_hash, 64), request_key: shared.cleanText(task.request_key, 128), status: shared.cleanText(task.status, 40), possibly_charged: task.possibly_charged === true, error_code: shared.cleanText(task.error_code, 100), message: shared.cleanText(task.message, 1000), ...(task.phase !== "full" && task.resume ? { resume: shared.sanitizeResume(task.resume) } : {}) };
}

/** attachmentOperation 从当前页面的可信快照提交纯文本，界面不能指定正文或其它页面；迟到结果必须再次校验来源。 */
async function attachmentOperation(payload, action, sourceTab) {
  const generation = payload.expected_generation ?? authGeneration;
  await requireConsent(); await requireActiveSession();
  const account = accountKey(await readSettings());
  if (payload.account_key !== account) throw new Error("账号已经变化，请重新打开插件");
  if (action === "cancel") {
    const scope = attachmentTaskScopes.get(JSON.stringify([account, payload.task_id]));
    if (!scope || scope.generation !== generation || scope.capture_id !== payload.capture_id || scope.phase !== payload.phase) throw new Error("这条识别记录不属于当前账号的请求，请先查询原任务");
    const result = await cloudRequest("/api/candidates/attachment-parses/" + encodeURIComponent(scope.id) + "/cancel", {}, "POST", true, generation), task = result.task;
    if (task?.capture_id !== scope.capture_id || task?.text_hash !== scope.text_hash || task?.phase !== scope.phase) throw new Error("取消结果没有匹配原附件，请查询原任务确认");
    return { task: attachmentTaskReceipt(task) };
  }
  const boundTab = sourceTab || await activeTab();
  if (!boundTab?.id) throw new Error("请打开当前 BOSS 附件后再识别");
  const snapshot = await currentSnapshot(boundTab), attachment = snapshot?.attachment;
  if (!attachment || !payload.capture_id || snapshot.capture_id !== payload.capture_id) throw Object.assign(new Error("附件已切换，请重新读取当前候选人"), { code: "CAPTURE_SCOPE_CHANGED" });
  const phase = payload.phase;
  if (!["basic", "full"].includes(phase)) throw new Error("附件识别阶段不正确，请重新读取");
  let path = "/api/candidates/attachment-parses", body = null, method = "GET";
  if (action === "start") {
    if (phase !== "basic") throw new Error("完整解析已交给后台保存任务，请点击添加或更新");
    if (attachment.blocked || !attachment.text) throw new Error(attachment.warning || "附件还没加载完，请稍等");
    if (!/^[A-Za-z0-9_-]{8,128}$/.test(payload.request_key || "")) throw new Error("这次识别缺少标识，请重新读取");
    if (await shared.hashText(attachment.text) !== attachment.text_hash) throw new Error("附件内容校验没通过，请重新读取");
    body = { phase, request_key: payload.request_key, capture_id: snapshot.capture_id, text: attachment.text, text_hash: attachment.text_hash, source: { platform_id: "boss", source_url: shared.safeSourceURL(snapshot.source_url), ...(snapshot.platform_candidate_id ? { platform_candidate_id: snapshot.platform_candidate_id } : {}) } }; method = "POST";
  } else {
    if (!/^[A-Za-z0-9_-]{1,100}$/.test(payload.task_id || "")) throw new Error("识别记录没找到，请先查询上次请求");
    path += "/" + encodeURIComponent(payload.task_id) + (action === "cancel" ? "/cancel" : "");
    if (action === "cancel") { method = "POST"; body = {}; }
  }
  await requireCurrentCapture(snapshot.capture_id, boundTab);
  const result = await cloudRequest(path, body, method, true, generation), task = result.task;
  // 回执先与发起时的原文绑定，允许切人后取消原任务；展示前仍须校验当前页。
  if (task?.id && task?.capture_id === snapshot.capture_id && task?.text_hash === attachment.text_hash && task?.phase === phase) {
    attachmentTaskScopes.set(JSON.stringify([account, task.id]), { id: task.id, generation, capture_id: task.capture_id, text_hash: task.text_hash, phase });
    if (attachmentTaskScopes.size > 100) attachmentTaskScopes.delete(attachmentTaskScopes.keys().next().value);
  }
  await requireCurrentCapture(snapshot.capture_id, boundTab);
  if (generation !== authGeneration || task?.capture_id !== snapshot.capture_id || task?.text_hash !== attachment.text_hash || task?.phase !== phase || (action === "start" && task?.request_key !== payload.request_key)) throw Object.assign(new Error("识别结果与当前附件不一致，已停止填入"), { code: "CAPTURE_SCOPE_CHANGED" });
  const receipt = attachmentTaskReceipt(task);
  if (!receipt) throw new Error("还没拿到识别记录，请查询上次请求，不要重复发起");
  return { task: receipt };
}

/** summaryOperation 使用配对授权请求本地AI摘要，不把网页登录凭证或招聘缓存交给AI。 */
async function summaryOperation(payload, action) {
  const generation = payload.expected_generation ?? authGeneration; await requireConsent(); const settings = await readSettings();
  if (!settings.session?.token || !settings.pairing_token || settings.pending_revoke || settings.pairing_account_key !== accountKey(settings)) throw new Error("AI总结需要连接同账号的本地程序，请在上方“连接本地程序”完成一次配对，其它功能可以继续用");
  if (generation !== authGeneration) throw new Error("账号已经变化，本次AI请求已停止");
  const body = { installation_id: settings.installation_id, pairing_token: settings.pairing_token, request_key: shared.cleanText(payload.request_key, 100) };
  let path = "/api/v1/extensions/resume-summary";
  if (action === "start") { requireWritableResume(payload.resume); if (!body.request_key) throw new Error("这次总结缺少标识，请重新点击"); body.candidate = shared.stripSummaryContacts(payload.resume); }
  else { if (!payload.task_id || !/^[A-Za-z0-9_-]{1,100}$/.test(payload.task_id)) throw new Error("总结记录没有找到，请重新发起"); path += `/${encodeURIComponent(payload.task_id)}${action === "poll" ? "" : "/" + action}`; }
  const result = await requestJSON(localBase(settings) + path, { method: "POST", body: JSON.stringify(body) });
  if (generation !== authGeneration) throw new Error("账号已经变化，旧AI结果已停止展示"); return result;
}

/** requireWritableResume 在资料整理丢弃标记前拒绝只读内置示例，不按姓名或手机号误伤真实人。 */
function requireWritableResume(input = {}) {
  if (!input || typeof input !== "object") return;
  const builtinID = "00000000-0000-4000-8000-000000000001";
  if (["is_builtin", "readonly", "builtin_notice"].some((key) => Object.hasOwn(input, key)) || ["id", "candidate_id", "platform_candidate_id"].some((key) => input[key] === builtinID) || input.source_type === "builtin") throw new Error("这份是内置示例，不能生成AI总结；真实候选人的资料仍可正常使用");
}

/** requireConsent 在任何云端业务提交前检查用户的简历数据授权。 */
async function requireConsent() { if (!(await readSettings()).data_use_accepted) throw new Error("请先同意把确认后的简历资料保存到 GoodHR"); }

/** importTaskResult 核对后台回执属于原冻结请求，并移除任何完整解析数据。 */
function importTaskResult(result, request) {
  const task = shared.attachmentImportReceipt(result?.task);
  if (!task || task.request_key !== request.request_key || task.capture_id !== request.capture_id) throw new Error("后台回执还没确认，请查询原保存任务，不要重新发起");
  return { task };
}

/** pollAttachmentImport 仅按原编号恢复同账号保存状态，关闭或切人不会取消已经受理的任务。 */
async function pollAttachmentImport(payload) {
  const generation = payload.expected_generation ?? authGeneration;
  await requireConsent(); await requireActiveSession();
  if (!/^[A-Za-z0-9_-]{8,128}$/.test(payload.request_key || "") || !payload.capture_id) throw new Error("保存编号没找到，请重新打开原候选人卡片");
  const path = payload.task_id && /^[A-Za-z0-9_-]{1,100}$/.test(payload.task_id) ? "/api/candidates/attachment-imports/" + encodeURIComponent(payload.task_id) : "/api/candidates/attachment-imports?request_key=" + encodeURIComponent(payload.request_key);
  return importTaskResult(await cloudRequest(path, null, "GET", true, generation), payload);
}

/** startAttachmentImport 冻结人工输入，补入当前可信全文及头像，只提交一次后台任务而不等待完整AI。 */
async function startAttachmentImport(payload, updating, sourceTab) {
  const generation = payload.expected_generation ?? authGeneration;
  await requireConsent(); await requireActiveSession();
  const request = shared.sanitizeAttachmentImport(payload.attachment_save);
  if (!request || request.operation !== (updating ? "update" : "create") || request.capture_id !== payload.capture_id || !/^[A-Za-z0-9_-]{8,128}$/.test(request.request_key)) throw new Error("这次保存缺少原始编号，请重新读取");
  if (!request.review.candidate_name || !request.review.phone) throw new Error("请补齐姓名和完整手机号，再交给后台保存");
  if (updating && (!request.candidate_id || !request.row_version)) throw new Error("请先查重并选择要更新的简历");
  if ([request.note?.content, request.recommendation?.content].some((value) => Array.from(value || "").length > 1000)) throw new Error("备注超过1000字，请缩短或分次记录");
  const account = accountKey(await readSettings()), key = JSON.stringify([account, request.request_key]), signature = JSON.stringify(request);
  if (generation !== authGeneration) throw new Error("账号已经变化，本次保存未提交");
  let entry = attachmentImportRequests.get(key);
  if (entry && entry.signature !== signature) throw Object.assign(new Error("这次保存的资料已冻结，请查询原任务"), { status: 409, code: "REQUEST_CONFLICT" });
  if (entry?.promise) return entry.promise;
  if (entry?.task) return { task: entry.task };
  if (!entry) { entry = { signature, body: null, promise: null }; attachmentImportRequests.set(key, entry); if (attachmentImportRequests.size > 30) attachmentImportRequests.delete(attachmentImportRequests.keys().next().value); }
  entry.promise = (async () => {
    const boundTab = sourceTab || await activeTab(), snapshot = await currentSnapshot(boundTab), attachment = snapshot?.attachment;
    if (!boundTab || !attachment || snapshot.capture_id !== request.capture_id || attachment.blocked || !attachment.text) throw Object.assign(new Error("附件已切换或还没加载完，请重新读取当前人"), { code: "CAPTURE_SCOPE_CHANGED" });
    if (await shared.hashText(attachment.text) !== attachment.text_hash) throw new Error("附件内容校验没通过，请重新读取");
    if (!entry.body) {
      const image = await prepareAvatar(request.capture_id, boundTab);
      entry.body = { ...request, text: attachment.text, text_hash: attachment.text_hash, source: { platform_id: "boss", source_url: shared.safeSourceURL(snapshot.source_url), ...(snapshot.platform_candidate_id ? { platform_candidate_id: snapshot.platform_candidate_id } : {}) }, ...(image.data_url ? { avatar_data_url: image.data_url } : {}) };
    }
    await requireCurrentCapture(request.capture_id, boundTab);
    if (generation !== authGeneration) throw new Error("账号已经变化，本次保存未提交");
    const result = importTaskResult(await cloudRequest("/api/candidates/attachment-imports", entry.body, "POST", true, generation), request);
    entry.task = result.task; entry.body = null; return result;
  })();
  try { return await entry.promise; }
  catch (error) { if (shared.unacceptedImportError(error.code)) { attachmentImportRequests.delete(key); entry.body = null; } throw error; }
  finally { entry.promise = null; }
}

/** createCandidate 在后端再校验手机号，并用幂等键防止连续点击重复添加。 */
async function createCandidate(payload, updating = false, sourceTab = null) {
  const generation = payload.expected_generation ?? authGeneration;
  if (payload.attachment_save) return startAttachmentImport(payload, updating, sourceTab);
  await requireConsent(); await requireActiveSession(); const resume = shared.candidateWriteResume(payload.resume, updating, payload.resume?.platform_id === "boss");
  if (generation !== authGeneration) throw new Error("账号已经变化，本次保存已停止，请确认账号后重新保存");
  if (!resume.candidate_name || !resume.phone) throw new Error("姓名和完整手机号都填一下，我才能认真入库");
  const input = { ...resume, idempotency_key: shared.cleanText(payload.idempotency_key, 100) };
  if (!input.idempotency_key) throw new Error("这次保存缺少标识，请重新点击添加");
  const boundTab = sourceTab || await activeTab();
  if (generation !== authGeneration) throw new Error("账号已经变化，本次保存已停止，请确认账号后重新保存");
  await requireCurrentCapture(payload.capture_id, boundTab);
  const captured = boundTab ? await currentSnapshot(boundTab) : null;
  if (captured?.attachment || resume.platform_id === "boss") {
    if (!captured?.attachment || captured.capture_id !== payload.capture_id || captured.attachment.blocked || !captured.attachment.text) throw new Error("BOSS 附件已切换或页面不完整，请重新读取当前候选人");
    throw new Error("请重新读取后提交附件后台保存任务");
  }
  const image = await prepareAvatar(payload.capture_id, boundTab); if (image.data_url && (!updating || image.data_url !== payload.existing_avatar_data_url)) input.avatar_data_url = image.data_url;
  if (generation !== authGeneration) throw new Error("账号已经变化，本次保存已停止，请确认账号后重新保存");
  await requireCurrentCapture(payload.capture_id, boundTab);
  if (generation !== authGeneration) throw new Error("账号已经变化，本次保存已停止，请确认账号后重新保存");
  let result;
  if (updating) {
    if (!payload.candidate_id || !payload.row_version) throw new Error("请重新查重后再更新，避免覆盖别人刚改的资料");
    input.row_version = payload.row_version;
    result = await cloudRequest(`/api/candidates/${encodeURIComponent(payload.candidate_id)}`, input, "PATCH", true, generation);
  } else {
    result = await cloudRequest("/api/candidates", input, "POST", true, generation);
  }
  return { ...result, avatar_warning: image.warning };
}

/** retryAvatar 仅更新选定候选人的头像，独立幂等重试，不重建简历或备注。 */
async function retryAvatar(payload, sourceTab = null) {
  const generation = payload.expected_generation ?? authGeneration;
  await requireConsent(); await requireActiveSession();
  if (generation !== authGeneration) throw new Error("账号已经变化，本次头像保存已停止，请确认账号后重试");
  if (!payload.candidate_id || !payload.row_version || !payload.idempotency_key) throw new Error("请重新查重选择候选人，再重试头像");
  await requireCurrentCapture(payload.capture_id, sourceTab);
  const image = await prepareAvatar(payload.capture_id, sourceTab); if (!image.data_url) throw new Error(image.warning || "未识别到当前头像，可先保存简历，稍后重新读取后重试头像");
  await requireCurrentCapture(payload.capture_id, sourceTab);
  if (generation !== authGeneration) throw new Error("账号已经变化，本次头像保存已停止，请确认账号后重试");
  if (image.data_url === payload.existing_avatar_data_url) return { unchanged: true };
  return cloudRequest(`/api/candidates/${encodeURIComponent(payload.candidate_id)}`, { avatar_data_url: image.data_url, row_version: payload.row_version, idempotency_key: shared.cleanText(payload.idempotency_key, 100) }, "PATCH", true, generation);
}

/** setFloatingOpen 仅向原账号、原捕获和原候选人的顶层卡片转发展开或收起，不触发保存或 AI。 */
async function setFloatingOpen(payload, sourceTab, opening, generation) {
  const unchanged = opening ? { opened: false } : { closed: false };
  if (!sourceTab?.id) return unchanged;
  const settings = await readSettings();
  if (!payload.account_key || !settings.session?.token || generation !== authGeneration || payload.account_key !== accountKey(settings)) throw new Error("账号已经变化，本次卡片操作已停止");
  if (!opening && settings.auto_close_after_save === false) return unchanged;
  const identity = shared.cleanText(payload.identity, 3000), captureID = shared.cleanText(payload.capture_id, 160);
  if (!identity || identity !== payload.identity || !captureID || captureID !== payload.capture_id) throw Object.assign(new Error("原候选人标识不完整，卡片保持原状"), { code: "CAPTURE_SCOPE_CHANGED" });
  const snapshot = await currentSnapshot(sourceTab);
  if (!snapshot || snapshot.capture_id !== captureID || shared.resumeIdentity(snapshot) !== identity || (opening && (snapshot.platform_id !== "boss" || !snapshot.attachment))) throw Object.assign(new Error("候选人已经变化，旧卡片操作已停止"), { code: "CAPTURE_SCOPE_CHANGED" });
  const currentSettings = await readSettings(), current = snapshots.get(sourceTab.id);
  if (!currentSettings.session?.token || generation !== authGeneration || payload.account_key !== accountKey(currentSettings)) throw new Error("账号已经变化，本次卡片操作已停止");
  if (!current || current.capture_id !== captureID || shared.resumeIdentity(current) !== identity) throw Object.assign(new Error("候选人已经变化，旧卡片操作已停止"), { code: "CAPTURE_SCOPE_CHANGED" });
  const type = opening ? "OPEN_FLOATING" : "CLOSE_FLOATING", result = await extension.tabs.sendMessage(sourceTab.id, { type, identity }, { frameId: 0 });
  return opening ? { opened: Boolean(result?.opened) } : { closed: Boolean(result?.closed) };
}

/** handleUIMessage 接收插件界面的明确操作，所有接口固定而非通用代理。 */
async function handleUIMessage(message, sourceTab = null) {
  const generation = authGeneration, payload = { ...(message.payload || {}), expected_generation: generation };
  if (["POSITIONS", "FOLDERS", "DUPLICATES", "CREATE", "UPDATE", "AVATAR_RETRY", "DETAIL", "FOLLOWUPS", "SUMMARY_START", "SUMMARY_POLL", "SUMMARY_CANCEL", "SUMMARY_RETRY", "SUMMARY_SAVE", "ATTACHMENT_START", "ATTACHMENT_POLL", "ATTACHMENT_CANCEL", "ATTACHMENT_IMPORT_POLL"].includes(message.type) && payload.account_key) { const generation = authGeneration, settings = await readSettings(); if (generation !==authGeneration || payload.account_key !== accountKey(settings)) throw new Error("账号已经变化，本次操作已停止，请确认账号后继续"); }
  if (VERSION_WRITES.has(message.type)) { await requireConsent(); await requireActiveSession(); await requireCurrentVersion(); }
  switch (message.type) {
    case "STATE": return publicState(sourceTab);
    case "PREVIEW": return { resume: publicResume(await currentSnapshot(sourceTab)), version: extension.runtime.getManifest().version };
    case "CONSENT": if (!payload.accepted) { authGeneration++; duplicateCache.clear(); } await extension.storage.local.set({ data_use_accepted: payload.accepted === true }); if (payload.accepted) { const snapshot = await currentSnapshot(sourceTab), tab = sourceTab || await activeTab(); if (tab?.id && snapshot) scheduleCaptureCheck(tab.id, snapshot); } return { ok: true };
    case "SET_AUTO_CLOSE": await extension.storage.local.set({ auto_close_after_save: payload.enabled !== false }); return { enabled: payload.enabled !== false };
    case "SEND_CODE": return requestJSON(shared.CLOUD_BASE + "/api/auth/extension-send-code", { method: "POST", body: JSON.stringify({ email: shared.cleanText(payload.email, 120) }) });
    case "LOGIN": {
      const generation = ++authGeneration;
      const input = { email: shared.cleanText(payload.email, 120), agreement_accepted: payload.agreement_accepted === true };
      if (payload.code) input.code = shared.cleanText(payload.code, 20); else input.password = String(payload.password || "");
      await storeSession(await requestJSON(shared.CLOUD_BASE + "/api/auth/extension-login", { method: "POST", body: JSON.stringify(input) }), generation);
      return publicState(sourceTab);
    }
    case "LOGOUT": return logout();
    case "REVOKE_RETRY": return retryRevocation(true);
    case "PAIR_START": return beginPair(payload.port);
    case "PAIR_CHECK": return checkPair();
    case "RESTORE": if ((await readSettings()).session?.token) return { ...(await publicState(sourceTab)), existing_session: true }; restoreAttempted = false; await tryRestore(); return publicState(sourceTab);
    case "POSITIONS": return cloudRequest("/api/positions?purpose=recommendation", null, "GET", true, generation);
    case "FOLDERS": await requireConsent(); return cloudRequest("/api/candidates/folders", null, "GET", true, generation);
    case "DUPLICATES": return checkDuplicates(payload);
    case "DRAFT_GET": return draftOperation(payload, false);
    case "DRAFT_SAVE": return draftOperation(payload, true);
    case "CHECK_UPDATE": return checkUpdate(true);
    case "CLOSE_FLOATING": return setFloatingOpen(payload, sourceTab, false, generation);
    case "OPEN_FLOATING": return setFloatingOpen(payload, sourceTab, true, generation);
    case "SUMMARY_START": return summaryOperation(payload, "start");
    case "ATTACHMENT_START": return attachmentOperation(payload, "start", sourceTab);
    case "ATTACHMENT_POLL": return attachmentOperation(payload, "poll", sourceTab);
    case "ATTACHMENT_CANCEL": return attachmentOperation(payload, "cancel", sourceTab);
    case "ATTACHMENT_IMPORT_POLL": return pollAttachmentImport(payload);
    case "SUMMARY_POLL": return summaryOperation(payload, "poll");
    case "SUMMARY_CANCEL": return summaryOperation(payload, "cancel");
    case "SUMMARY_RETRY": return summaryOperation(payload, "retry");
    case "SUMMARY_SAVE": { await requireConsent(); if (!payload.candidate_id || !payload.row_version || !payload.ai_summary || !payload.idempotency_key) throw new Error("请先选择已有简历，再保存总结"); return cloudRequest(`/api/candidates/${encodeURIComponent(payload.candidate_id)}`, { ai_summary: shared.cleanText(payload.ai_summary, 12000), row_version: payload.row_version, idempotency_key: shared.cleanText(payload.idempotency_key, 100) }, "PATCH", true, generation); }
    case "CREATE": return createCandidate(payload, false, sourceTab);
    case "UPDATE": return createCandidate(payload, true, sourceTab);
    case "AVATAR_RETRY": return retryAvatar(payload, sourceTab);
    case "DETAIL": {
      await requireConsent();
      const path = `/api/candidates/${encodeURIComponent(payload.candidate_id)}`;
      const [notes, recommendations] = await Promise.allSettled([cloudRequest(path + "/notes", null, "GET", true, generation), cloudRequest(path + "/recommendations", null, "GET", true, generation)]);
      if (generation !== authGeneration) throw new Error("账号已经变化，旧记录已停止展示");
      return { ...(notes.status === "fulfilled" ? notes.value : { notes: [], notes_error: notes.reason.message }), ...(recommendations.status === "fulfilled" ? recommendations.value : { recommendations: [], recommendations_error: recommendations.reason.message }) };
    }
    case "FOLLOWUPS": {
      await requireConsent(); if (!payload.candidate_id) throw new Error("先选择候选人，再保存备注或岗位");
      return shared.saveFollowups((path, body) => cloudRequest(path, body, "POST", true, generation), payload.candidate_id, payload);
    }
    case "OPEN_SIDEBAR": {
      const tab = sourceTab || await activeTab();
      if (extension.sidePanel?.open && tab?.id) await extension.sidePanel.open({ tabId: tab.id });
      else if (extension.sidebarAction?.open) await extension.sidebarAction.open();
      else throw new Error("当前浏览器暂不支持侧栏，请继续用这个弹窗");
      return { ok: true };
    }
    default: throw new Error("这个操作暂时还不认识，请刷新插件");
  }
}

/** isTrustedUI 只接收本插件的固定界面页，拒绝网页URL、前缀伪装及其它扩展资源。 */
function isTrustedUI(sender) {
  try { const url = new URL(sender.url), ui = new URL(extension.runtime.getURL("src/ui.html")); return url.protocol === ui.protocol && url.host === ui.host && [ui.pathname, "/src/floating.html"].includes(url.pathname) && !url.search && !url.hash; }
  catch { return false; }
}

const costConfirmationPorts = new Map();
/** bindCostConfirmation 只连接本插件在招聘标签内的卡片，短通道仅用于收起时取消成本确认。 */
function bindCostConfirmation(port) {
  if (port.name !== "goodhr-cost-confirmation") return;
  const sender = port.sender;
  if (sender?.id !== extension.runtime.id || !isTrustedUI(sender) || new URL(sender.url).pathname !== "/src/floating.html" || !sender.tab?.id || !shared.platformForURL(sender.tab.url) || !Number.isInteger(sender.frameId) || sender.frameId < 1) { port.disconnect(); return; }
  const tabID = sender.tab.id, ports = costConfirmationPorts.get(tabID) || new Set(); ports.add(port); costConfirmationPorts.set(tabID, ports);
  port.onDisconnect.addListener(() => { ports.delete(port); if (!ports.size) costConfirmationPorts.delete(tabID); });
}

/** cancelFloatingCostConfirmation 只向同一浏览器标签的插件卡片发送取消，不能确认或发起任何 AI、保存请求。 */
function cancelFloatingCostConfirmation(tabID) {
  for (const port of costConfirmationPorts.get(tabID) || []) { try { port.postMessage({ type: "CANCEL_COST_CONFIRMATION" }); } catch { /* 已销毁的卡片不再接收消息。 */ } }
}

/** storeCandidateSnapshot 从可信顶层脚本整理快照，并在后台计算附件图片指纹，防止同文字换头像后沿用旧捕获。 */
async function storeCandidateSnapshot(message, sender) {
  const generation = authGeneration, tabID = sender.tab.id, sequence = (snapshotSequences.get(tabID) || 0) + 1; snapshotSequences.set(tabID, sequence);
  const resume = message.resume ? shared.sanitizeResume(message.resume) : null;
  const attachment = shared.platformForURL(sender.tab.url) === "boss" ? shared.sanitizeAttachment(message.resume?.attachment) : null;
  if (attachment && resume) {
    resume.platform_id = "boss"; resume.source_url = shared.safeSourceURL(sender.tab.url); resume.attachment = attachment;
    resume.avatar_candidates = shared.sanitizeAvatarCandidates(message.resume.avatar_candidates, sender.tab.url);
    attachment.avatar_hash = await shared.hashText(JSON.stringify(resume.avatar_candidates));
  }
  if (generation !== authGeneration || snapshotSequences.get(tabID) !== sequence) return { ok: false };
  if (resume?.platform_id && resume.platform_id !== shared.platformForURL(sender.tab.url)) return { ok: false };
  if (resume?.candidate_name || resume?.attachment) {
    const imageSource = !attachment && shared.safeAvatarSource(message.resume.avatar_source_url, sender.tab.url);
    if (imageSource) resume.avatar_source_url = imageSource;
    const old = snapshots.get(tabID), { capture_id, ...oldResume } = old || {};
    resume.capture_id = attachment ? "boss-" + attachment.text_hash + "-" + attachment.avatar_hash + (attachment.blocked ? "-blocked" : "-ready") : old && JSON.stringify(oldResume) === JSON.stringify(resume) ? capture_id : shared.newKey();
    snapshots.set(tabID, resume);
    if (resume.capture_id !== old?.capture_id) scheduleCaptureCheck(tabID, resume);
  } else snapshots.delete(tabID);
  extension.action.setBadgeText({ tabId: tabID, text: resume?.candidate_name || attachment ? "+" : "" });
  extension.action.setBadgeBackgroundColor({ tabId: tabID, color: "#0B8968" });
  extension.action.setTitle({ tabId: tabID, title: resume?.candidate_name ? "当前有简历，可添加到 GoodHR" : "GoodHR 简历助手" });
  return { ok: true };
}

/** onMessage 严格区分招聘内容脚本与插件 UI，页面脚本只能上报资料不能请求授权或 API。 */
function onMessage(message, sender, sendResponse) {
  if (sender.id !== extension.runtime.id) return false;
  if (isTrustedUI(sender)) {
    const floating = new URL(sender.url).pathname === "/src/floating.html";
    if (floating && ["LOGIN", "SEND_CODE", "CONSENT", "SET_AUTO_CLOSE", "PAIR_START", "PAIR_CHECK", "RESTORE", "LOGOUT", "REVOKE_RETRY", "OPEN_SIDEBAR"].includes(message.type)) return false;
    if (!floating && sender.tab) return false;
    // 嵌入UI的sender.tab由浏览器提供；任何payload里的tab编号都不能改变资料来源。
    if (sender.tab && (!shared.platformForURL(sender.tab.url) || !Number.isInteger(sender.frameId) || sender.frameId < 1)) return false;
    const sourceTab = sender.tab ? { id: sender.tab.id, url: sender.tab.url } : null;
    handleUIMessage(message, sourceTab).then((data) => sendResponse({ ok: true, data }), (error) => sendResponse({ ok: false, message: error.message, status: error.status || 0, code: error.code || "", conflict: error.conflict || null }));
    return true;
  }
  if (sender.tab) {
    if (message.type === "FLOATING_CLOSED" && sender.frameId === 0 && shared.platformForURL(sender.tab.url)) { cancelFloatingCostConfirmation(sender.tab.id); sendResponse({ ok: true }); return false; }
    if (message.type !== "CANDIDATE_SNAPSHOT" || sender.frameId !== 0 || !shared.platformForURL(sender.tab.url)) return false;
    storeCandidateSnapshot(message, sender).then(sendResponse, () => sendResponse({ ok: false })); return true;
  }
  return false;
}

extension.runtime.onMessage.addListener(onMessage);
extension.runtime.onConnect?.addListener(bindCostConfirmation);
extension.tabs.onRemoved.addListener((tabID) => { snapshotSequences.set(tabID, (snapshotSequences.get(tabID) || 0) + 1); snapshots.delete(tabID); clearTimeout(captureTimers.get(tabID)); captureTimers.delete(tabID); });
extension.tabs.onUpdated.addListener((tabID, change) => { if (change.url) { snapshotSequences.set(tabID, (snapshotSequences.get(tabID) || 0) + 1); snapshots.delete(tabID); clearTimeout(captureTimers.get(tabID)); captureTimers.delete(tabID); } });
if (extension.storage.local.setAccessLevel) extension.storage.local.setAccessLevel({ accessLevel: "TRUSTED_CONTEXTS" });
