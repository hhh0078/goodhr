// 本文件负责平台账号和平台配置相关接口。
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
