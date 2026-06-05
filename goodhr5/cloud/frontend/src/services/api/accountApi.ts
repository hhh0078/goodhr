// 本文件负责平台账号、平台配置和 Cookie 相关接口。
import { api } from "../apiClient";
import {
  createLocalProfile,
  deleteLocalProfile,
  listLocalProfiles,
  updateLocalProfile,
} from "../localAgentApi";

/**
 * 判断当前页面是否由本地 Local Agent 控制台提供。
 * @returns {boolean} 本地控制台返回 true。
 */
function isLocalConsole() {
  if (typeof window === "undefined") return false;
  const host = window.location.hostname;
  const port = Number(window.location.port || "0");
  return (host === "127.0.0.1" || host === "localhost") && port >= 9001 && port <= 9009;
}

/**
 * 返回当前本地控制台的 Local Agent 地址。
 * @returns {string} Local Agent HTTP 基础地址。
 */
function localAgentBase() {
  return window.location.origin;
}

/**
 * 将本地 profile 转换为前端原有账号结构。
 * @param {any} profile - Local Agent profile 记录。
 * @returns {any} 平台账号结构。
 */
function normalizeLocalAccount(profile: any) {
  return {
    ...profile,
    id: profile.id,
    platform_id: profile.platform_id,
    display_name: profile.display_name,
    local_profile_id: profile.local_profile_id || profile.id,
    status: profile.status || "available",
    updated_at: profile.updated_at || profile.created_at,
  };
}

/**
 * 读取平台账号列表。
 * @returns {Promise<any[]>} 返回平台账号数组。
 */
export async function listPlatformAccounts() {
  if (isLocalConsole()) {
    const profiles = await listLocalProfiles(localAgentBase());
    return profiles.map(normalizeLocalAccount);
  }
  const data = await api("/api/platform-accounts");
  return data.accounts;
}

/**
 * 创建平台账号。
 * @param {any} payload - 平台账号创建参数。
 * @returns {Promise<any>} 返回新建的平台账号。
 */
export async function createPlatformAccount(payload: any) {
  if (isLocalConsole()) {
    const profile = await createLocalProfile(localAgentBase(), {
      platform_id: payload.platform_id,
      display_name: payload.display_name,
      status: payload.status || "available",
    });
    return normalizeLocalAccount(profile);
  }
  const data = await api("/api/platform-accounts/create", { method: "POST", body: payload });
  return data.account;
}

/**
 * 删除平台账号。
 * @param {string} accountID - 平台账号 ID。
 * @returns {Promise<void>} 无返回值。
 */
export async function deletePlatformAccount(accountID: string) {
  if (isLocalConsole()) {
    await deleteLocalProfile(localAgentBase(), accountID);
    return;
  }
  await api(`/api/platform-accounts/${accountID}`, { method: "DELETE" });
}

/**
 * 读取平台登录和选择器配置。
 * @returns {Promise<any[]>} 返回平台配置数组。
 */
export async function listPlatformConfigs() {
  const data = await api("/api/platforms/config/");
  return data.configs;
}

/**
 * 读取 Cookie 账号列表。
 * @returns {Promise<any[]>} 返回 Cookie 账号数组。
 */
export async function listCookies() {
  if (isLocalConsole()) {
    const profiles = await listLocalProfiles(localAgentBase());
    return profiles.map(normalizeLocalAccount);
  }
  const data = await api("/api/cookies");
  return data.cookies;
}

/**
 * 创建 Cookie 账号。
 * @param {any} payload - Cookie 账号创建参数。
 * @returns {Promise<any>} 返回新建的 Cookie 账号。
 */
export async function createCookie(payload: any) {
  if (isLocalConsole()) {
    const profile = await createLocalProfile(localAgentBase(), {
      platform_id: payload.platform_id,
      display_name: payload.display_name,
      status: payload.status || "available",
    });
    return normalizeLocalAccount(profile);
  }
  const data = await api("/api/cookies/create", { method: "POST", body: payload });
  return data.cookie;
}

/**
 * 更新 Cookie 账号。
 * @param {string} cookieID - Cookie 账号 ID。
 * @param {any} payload - Cookie 更新参数。
 * @returns {Promise<any>} 返回更新后的 Cookie 账号。
 */
export async function updateCookie(cookieID: string, payload: any) {
  if (isLocalConsole()) {
    const profile = await updateLocalProfile(localAgentBase(), cookieID, {
      platform_id: payload.platform_id,
      display_name: payload.display_name,
      status: payload.status,
      local_profile_id: payload.local_profile_id,
    });
    return normalizeLocalAccount(profile);
  }
  const data = await api(`/api/cookies/${encodeURIComponent(cookieID)}`, {
    method: "PUT",
    body: payload,
  });
  return data.cookie;
}

/**
 * 更新平台账号 cookie 的登录状态。
 * @param {string} cookieID - Cookie 账号 ID。
 * @param {string} status - 目标状态，支持 available、expired、in_use。
 * @returns {Promise<any>} 返回后端状态更新结果。
 */
export async function updateCookieStatus(cookieID: string, status: string) {
  if (isLocalConsole()) {
    const profile = await updateLocalProfile(localAgentBase(), cookieID, { status });
    return { ok: true, cookie: normalizeLocalAccount(profile) };
  }
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/status`, {
    method: "PUT",
    body: { status },
  });
}

/**
 * 领取一个可用 Cookie 账号。
 * @param {string} cookieID - Cookie 账号 ID。
 * @param {any} payload - 领取参数。
 * @returns {Promise<any>} 返回领取结果。
 */
export async function claimCookie(cookieID: string, payload: any = {}) {
  if (isLocalConsole()) {
    return { ok: true, cookie: { id: cookieID, ...payload }, cookies: [] };
  }
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/claim`, {
    method: "POST",
    body: payload,
  });
}

/**
 * 释放 Cookie 账号。
 * @param {string} cookieID - Cookie 账号 ID。
 * @returns {Promise<any>} 返回释放结果。
 */
export async function releaseCookie(cookieID: string) {
  if (isLocalConsole()) {
    return { ok: true, cookie: { id: cookieID } };
  }
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/release`, { method: "POST" });
}
