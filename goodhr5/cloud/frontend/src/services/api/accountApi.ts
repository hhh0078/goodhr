// 本文件负责平台账号、平台配置和 Cookie 相关接口。
import { api } from "../apiClient";

/**
 * 读取平台账号列表。
 * @returns {Promise<any[]>} 返回平台账号数组。
 */
export async function listPlatformAccounts() {
  const data = await api("/api/platform-accounts");
  return data.accounts;
}

/**
 * 创建平台账号。
 * @param {any} payload - 平台账号创建参数。
 * @returns {Promise<any>} 返回新建的平台账号。
 */
export async function createPlatformAccount(payload: any) {
  const data = await api("/api/platform-accounts/create", { method: "POST", body: payload });
  return data.account;
}

/**
 * 删除平台账号。
 * @param {string} accountID - 平台账号 ID。
 * @returns {Promise<void>} 无返回值。
 */
export async function deletePlatformAccount(accountID: string) {
  await api(`/api/platform-accounts/${accountID}`, { method: "DELETE" });
}

/**
 * 读取平台登录和选择器配置。
 * @returns {Promise<any[]>} 返回平台配置数组。
 */
export async function listPlatformConfigs() {
  const data = await api("/api/platforms/config/", { auth: false });
  return data.configs;
}

/**
 * 读取 Cookie 账号列表。
 * @returns {Promise<any[]>} 返回 Cookie 账号数组。
 */
export async function listCookies() {
  const data = await api("/api/cookies");
  return data.cookies;
}

/**
 * 创建 Cookie 账号。
 * @param {any} payload - Cookie 账号创建参数。
 * @returns {Promise<any>} 返回新建的 Cookie 账号。
 */
export async function createCookie(payload: any) {
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
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/release`, { method: "POST" });
}
