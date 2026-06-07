// 本文件负责岗位模板和系统默认提示词接口。
import { api } from "../apiClient";
import {
  deleteLocalPosition,
  getLocalPositionDefaultPrompts,
  listLocalPositions,
  optimizeLocalPositionRequirement,
  saveLocalPosition,
} from "../localAgentApi";
import { isLocalConsole, localAgentBase } from "../localConsole";

/**
 * 读取岗位模板列表。
 * @returns {Promise<any[]>} 返回岗位模板数组。
 */
export async function listPositions() {
  if (isLocalConsole()) {
    return listLocalPositions(localAgentBase());
  }
  const data = await api("/api/positions");
  return data.positions;
}

/**
 * 保存岗位模板。
 * @param {any} payload - 岗位模板表单数据。
 * @returns {Promise<any>} 返回保存后的岗位模板。
 */
export async function savePosition(payload: any) {
  if (isLocalConsole()) {
    return saveLocalPosition(localAgentBase(), payload);
  }
  const data = await api("/api/positions", { method: "POST", body: payload });
  return data.position;
}

/**
 * 删除岗位模板。
 * @param {string} positionID - 岗位模板 ID。
 * @returns {Promise<void>} 无返回值。
 */
export async function deletePosition(positionID: string) {
  if (isLocalConsole()) {
    await deleteLocalPosition(localAgentBase(), positionID);
    return;
  }
  await api(`/api/positions/${positionID}`, { method: "DELETE" });
}

/**
 * 读取系统默认 AI 提示词。
 * @returns {Promise<any>} 返回 filter_prompt、open_detail_prompt 和 review_prompt。
 */
export async function getDefaultPrompts() {
  if (isLocalConsole()) {
    return getLocalPositionDefaultPrompts(localAgentBase());
  }
  const data = await api("/api/system/default-prompts");
  return data.prompts || {};
}

/**
 * 使用当前用户个人 AI 配置优化岗位要求。
 * @param {string} text - 用户输入的原始岗位要求。
 * @returns {Promise<string>} 返回优化后的岗位要求。
 */
export async function optimizePositionRequirement(text: string) {
  if (isLocalConsole()) {
    return optimizeLocalPositionRequirement(localAgentBase(), text);
  }
  const data = await api("/api/positions/optimize-requirement", {
    method: "POST",
    body: { text },
  });
  return String(data.optimized || "");
}
