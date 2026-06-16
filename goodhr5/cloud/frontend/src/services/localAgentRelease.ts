// 本文件负责读取 system.onboarding_config 顶层 local_agent 最新版本配置。

import { detectAgentDownloadPlatform, type AgentDownloadPlatform } from "./agentPlatform";

export type LocalAgentRelease = {
  version: string;
  url: string;
  note: string;
  sha256: string;
  raw: Record<string, any>;
};

/**
 * latestLocalAgentRelease 读取当前平台的本地程序最新版本配置。
 * @param {any} onboardingConfig - system.onboarding_config 配置对象。
 * @param {AgentDownloadPlatform} platform - 目标平台。
 * @returns {LocalAgentRelease} 最新版本配置。
 */
export function latestLocalAgentRelease(
  onboardingConfig: any,
  platform: AgentDownloadPlatform = detectAgentDownloadPlatform(),
): LocalAgentRelease {
  const latest = firstLocalAgentItem(onboardingConfig);
  return {
    version: stringValue(latest.version),
    url: platform === "windows" ? stringValue(latest.url_win) : stringValue(latest.url_mac),
    note: stringValue(latest.note || latest.changelog || latest.description || latest.release_note),
    sha256: platform === "windows"
      ? stringValue(latest.sha256_win || latest.sha256)
      : stringValue(latest.sha256_mac || latest.sha256),
    raw: latest,
  };
}

/**
 * firstLocalAgentItem 返回 local_agent 数组第一项。
 * @param {any} onboardingConfig - system.onboarding_config 配置对象。
 * @returns {Record<string, any>} 最新本地程序配置。
 */
export function firstLocalAgentItem(onboardingConfig: any): Record<string, any> {
  const list = Array.isArray(onboardingConfig?.local_agent)
    ? onboardingConfig.local_agent
    : Array.isArray(onboardingConfig?.localAgent)
      ? onboardingConfig.localAgent
      : [];
  const first = list[0];
  if (first && typeof first === "object" && !Array.isArray(first)) {
    return first as Record<string, any>;
  }
  return {};
}

/**
 * localAgentRequiredVersion 返回当前要求的本地程序版本。
 * @param {any} onboardingConfig - system.onboarding_config 配置对象。
 * @returns {string} 要求版本号。
 */
export function localAgentRequiredVersion(onboardingConfig: any) {
  return latestLocalAgentRelease(onboardingConfig).version;
}

/**
 * stringValue 安全读取字符串值。
 * @param {any} value - 原始值。
 * @returns {string} 去空格后的字符串。
 */
function stringValue(value: any) {
  return String(value || "").trim();
}
