// 本文件负责简历库和候选人详情接口。
import { api } from "../apiClient";
import {
  clearLocalCandidates,
  getLocalCandidate,
  listLocalCandidatesPaged,
} from "../localAgentApi";
import { isLocalConsole, localAgentBase } from "../localConsole";

/**
 * 读取简历库候选人列表。
 * @param {{ taskId?: string; positionId?: string; keyword?: string; page?: number; pageSize?: number }} params - 搜索和分页条件。
 * @returns {Promise<any>} 返回候选人简历分页结果。
 */
export async function listCandidates(
  params: { taskId?: string; positionId?: string; keyword?: string; page?: number; pageSize?: number } = {},
) {
  if (isLocalConsole()) {
    return listLocalCandidatesPaged(localAgentBase(), params);
  }
  const query = new URLSearchParams();
  if (params.taskId) query.set("task_id", params.taskId);
  if (params.positionId) query.set("position_id", params.positionId);
  if (params.keyword) query.set("keyword", params.keyword);
  if (params.page) query.set("page", String(params.page));
  if (params.pageSize) query.set("page_size", String(params.pageSize));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  const data = await api(`/api/candidates${suffix}`);
  return {
    items: data.candidates || [],
    total: Number(data.total || 0),
    page: Number(data.page || params.page || 1),
    pageSize: Number(data.page_size || params.pageSize || 20),
  };
}

/**
 * 读取候选人详情。
 * @param {string} candidateID - 候选人 ID。
 * @param {string} engagementID - 触达上下文 ID，传入后按本次任务读取分析记录。
 * @returns {Promise<any>} 返回候选人详情。
 */
export async function getCandidate(candidateID: string, engagementID = "", taskID = "") {
  if (isLocalConsole()) {
    return getLocalCandidate(localAgentBase(), candidateID, taskID);
  }
  const query = engagementID ? `?engagement_id=${encodeURIComponent(engagementID)}` : "";
  const data = await api(`/api/candidates/${encodeURIComponent(candidateID)}${query}`);
  return data.candidate;
}

/**
 * 清空当前团队的全部候选人数据。
 * @returns {Promise<number>} 返回删除的候选人数量。
 */
export async function clearTeamCandidates() {
  if (isLocalConsole()) {
    return clearLocalCandidates(localAgentBase());
  }
  const data = await api("/api/candidates", { method: "DELETE" });
  return Number(data.deleted || 0);
}
