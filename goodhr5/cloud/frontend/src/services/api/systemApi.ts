// 本文件负责前端公共系统配置接口。
import { api } from "../apiClient";

/**
 * 读取前端公共系统配置。
 * @returns {Promise<any>} 返回本地执行器版本要求和公告列表。
 */
export async function getSystemAppConfig() {
  const data = await api("/api/system/app-config");
  return data.config || {};
}
