// 本文件负责超级管理员专用接口。
import { api } from "../apiClient";

/**
 * 读取超级管理员可见的激活码列表。
 * @returns {Promise<any[]>} 返回激活码数组。
 */
export async function listAdminActivationCodes() {
  const data = await api("/api/admin/activation-codes");
  return data.codes || [];
}

/**
 * 超级管理员批量生成激活码。
 * @param {any} payload - 包含天数、备注和数量。
 * @returns {Promise<any[]>} 返回生成的激活码数组。
 */
export async function createAdminActivationCodes(payload: any) {
  const data = await api("/api/admin/activation-codes", { method: "POST", body: payload });
  return data.codes || [];
}

/**
 * 读取超级管理员可见的全部支付记录。
 * @returns {Promise<any[]>} 返回全部支付记录数组。
 */
export async function listAdminPaymentOrders() {
  const data = await api("/api/admin/payment/orders");
  return data.orders || [];
}

/**
 * 读取超级管理员可见的用户分页列表。
 * @param {{ page?: number; page_size?: number; q?: string }} params - 分页和搜索参数。
 * @returns {Promise<any>} 返回用户、分页和统计数据。
 */
export async function listAdminUsers(params: { page?: number; page_size?: number; q?: string } = {}) {
  const query = new URLSearchParams();
  if (params.page) query.set("page", String(params.page));
  if (params.page_size) query.set("page_size", String(params.page_size));
  if (params.q?.trim()) query.set("q", params.q.trim());
  const suffix = query.toString() ? `?${query.toString()}` : "";
  const data = await api(`/api/admin/users${suffix}`);
  return {
    users: data.users || [],
    total: Number(data.total || 0),
    page: Number(data.page || 1),
    page_size: Number(data.page_size || params.page_size || 20),
    stats: data.stats || {},
  };
}

/**
 * 超级管理员调整指定用户会员天数。
 * @param {any} payload - 包含 email、days 和 reason。
 * @returns {Promise<any>} 返回新的订阅状态。
 */
export async function adjustAdminUserSubscription(payload: any) {
  return api("/api/admin/users", { method: "POST", body: payload });
}

/**
 * 超级管理员解除指定用户的本地程序机器绑定。
 * @param {string} email - 要解除绑定的用户邮箱。
 * @returns {Promise<any>} 返回解除结果。
 */
export async function unbindAdminUserAgent(email: string) {
  return api("/api/admin/users/unbind-agent", { method: "POST", body: { email } });
}

/**
 * 读取管理员可见的系统原始配置 JSON。
 * @returns {Promise<any[]>} 返回系统配置列表。
 */
export async function listAdminSystemConfigs() {
  const data = await api("/api/admin/system/configs/");
  return data.configs;
}

/**
 * 保存管理员可见的系统原始配置 JSON。
 * @param {string} configKey - 系统配置键。
 * @param {string} configValue - JSON 字符串形式的配置值。
 * @returns {Promise<any>} 返回保存后的系统配置。
 */
export async function updateAdminSystemConfig(configKey: string, configValue: string) {
  const data = await api(`/api/admin/system/configs/${encodeURIComponent(configKey)}`, {
    method: "PUT",
    body: { config_value: configValue },
  });
  return data.config;
}
