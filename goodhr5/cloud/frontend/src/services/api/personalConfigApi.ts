// 本文件负责个人 AI 配置和个人偏好配置接口。
import { api } from "../apiClient";

/**
 * 读取当前用户 AI 配置。
 * @returns {Promise<any>} 返回用户 AI 配置。
 */
export async function getUserAIConfig() {
  const data = await api("/api/config/user-ai");
  return data.config;
}

/**
 * 更新当前用户 AI 配置。
 * @param {any} payload - AI 配置表单数据。
 * @returns {Promise<any>} 返回保存后的 AI 配置。
 */
export async function updateUserAIConfig(payload: any) {
  const data = await api("/api/config/user-ai", { method: "PUT", body: payload });
  return data.config;
}

/**
 * 通过云端后端测试 AI 配置，避免浏览器跨域限制。
 * @param {any} payload - 待测试的 AI 地址、模型和 Key。
 * @returns {Promise<any>} 返回测试结果。
 */
export async function testUserAIConfig(payload: any) {
  return api("/api/config/test-ai", { method: "POST", body: payload });
}

/**
 * 读取当前用户操作偏好。
 * @returns {Promise<any>} 返回用户偏好配置。
 */
export async function getUserPreferences() {
  const data = await api("/api/config/user-preferences");
  return data.config;
}

/**
 * 更新当前用户操作偏好。
 * @param {any} payload - 偏好配置表单数据。
 * @returns {Promise<any>} 返回保存后的偏好配置。
 */
export async function updateUserPreferences(payload: any) {
  const data = await api("/api/config/user-preferences", { method: "PUT", body: payload });
  return data.config;
}
