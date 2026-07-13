// 本文件负责用户邀请相关接口。
import { api } from "../apiClient";

/**
 * 读取当前用户邀请信息。
 * @returns {Promise<any>} 返回邀请配置、邀请 ID 和邀请列表。
 */
export async function getInvitationSummary() {
  return api("/api/invitations/summary");
}
