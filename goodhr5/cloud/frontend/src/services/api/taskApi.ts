// 本文件负责任务和任务日志相关接口。
import { api } from "../apiClient";

/**
 * 创建招聘任务。
 * @param {any} payload - 任务创建参数。
 * @returns {Promise<any>} 返回新建任务。
 */
export async function createTask(payload: any) {
  const data = await api("/api/tasks", { method: "POST", body: payload });
  return data.task;
}

/**
 * 更新招聘任务。
 * @param {string} taskID - 任务 ID。
 * @param {any} payload - 任务更新参数。
 * @returns {Promise<any>} 返回更新后的任务。
 */
export async function updateTask(taskID: string, payload: any) {
  const data = await api(`/api/tasks/${encodeURIComponent(taskID)}`, { method: "PUT", body: payload });
  return data.task;
}

/**
 * 删除招聘任务。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回删除结果。
 */
export async function deleteTask(taskID: string) {
  return api(`/api/tasks/${encodeURIComponent(taskID)}`, { method: "DELETE" });
}

/**
 * 读取招聘任务列表。
 * @returns {Promise<any[]>} 返回任务数组。
 */
export async function listTasks() {
  const data = await api("/api/tasks");
  return data.tasks;
}

/**
 * 通过云端接口启动任务。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回启动结果。
 */
export async function runTask(taskID: string) {
  return api(`/api/tasks/${taskID}/run`, { method: "POST" });
}

/**
 * 通过云端接口停止任务。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回停止结果。
 */
export async function stopTask(taskID: string) {
  return api(`/api/tasks/${taskID}/stop`, { method: "POST" });
}

/**
 * 读取任务日志摘要。
 * @param {string} taskID - 任务 ID。
 * @param {{ since?: string; before?: string; limit?: number }} params - 日志筛选参数。
 * @returns {Promise<any>} 返回日志列表和分页状态。
 */
export async function listTaskLogs(
  taskID: string,
  params: { since?: string; before?: string; limit?: number } = {},
) {
  const queryParams = new URLSearchParams();
  if (params.since) queryParams.set("since", params.since);
  if (params.before) queryParams.set("before", params.before);
  if (params.limit) queryParams.set("limit", String(params.limit));
  const query = queryParams.toString() ? `?${queryParams.toString()}` : "";
  return api(`/api/tasks/${taskID}/logs${query}`);
}

/**
 * 清空指定任务的云端日志摘要。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<void>} 无返回值。
 */
export async function clearTaskLogs(taskID: string) {
  await api(`/api/tasks/${taskID}/logs`, { method: "DELETE" });
}
