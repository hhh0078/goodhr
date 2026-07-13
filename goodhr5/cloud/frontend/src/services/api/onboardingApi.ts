// 本文件负责新手教学状态相关接口。
import { api } from "../apiClient";

/**
 * 读取当前用户教学状态和教学配置。
 * @returns {Promise<any>} 返回教学状态和配置。
 */
export async function getOnboardingStatus() {
  return api("/api/onboarding/status");
}

/**
 * 标记当前用户已完成教学。
 * @returns {Promise<any>} 返回后端保存后的教学状态。
 */
export async function completeOnboarding() {
  return api("/api/onboarding/complete", { method: "POST" });
}
