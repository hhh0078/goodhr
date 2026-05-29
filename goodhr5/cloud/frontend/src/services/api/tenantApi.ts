// 本文件负责团队成员和租户管理接口。
import { api } from "../apiClient";

/**
 * 读取团队成员列表。
 * @returns {Promise<any[]>} 返回团队成员数组。
 */
export async function listTenantMembers() {
  const data = await api("/api/tenants/members");
  return data.members;
}

/**
 * 邀请团队成员。
 * @param {any} payload - 邀请邮箱和角色参数。
 * @returns {Promise<any>} 返回邀请结果。
 */
export async function inviteTenantMember(payload: any) {
  return api("/api/tenants/invite", { method: "POST", body: payload });
}

/**
 * 更新团队成员角色。
 * @param {string} email - 成员邮箱。
 * @param {any} payload - 更新参数。
 * @returns {Promise<any>} 返回更新结果。
 */
export async function updateTenantMember(email: string, payload: any) {
  return api(`/api/tenants/members/${encodeURIComponent(email)}`, { method: "PUT", body: payload });
}

/**
 * 删除团队成员。
 * @param {string} email - 成员邮箱。
 * @returns {Promise<any>} 返回删除结果。
 */
export async function deleteTenantMember(email: string) {
  return api(`/api/tenants/members/${encodeURIComponent(email)}`, { method: "DELETE" });
}
