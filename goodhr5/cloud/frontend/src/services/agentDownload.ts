// 本文件负责根据用户当前系统选择 GoodHR 本地程序下载链接。

import { latestLocalAgentRelease } from "./localAgentRelease";
import { detectAgentDownloadPlatform, type AgentDownloadPlatform } from "./agentPlatform";

export { detectAgentDownloadPlatform, type AgentDownloadPlatform } from "./agentPlatform";

export type AgentDownloadOption = {
  platform: AgentDownloadPlatform;
  label: string;
  url: string;
};

/**
 * getAgentDownloadURL 获取指定系统的本地程序下载链接。
 * @param {any} config - 新手教学系统配置。
 * @param {AgentDownloadPlatform} platform - 目标系统。
 * @returns {string} 下载链接。
 */
export function getAgentDownloadURL(config: any, platform: AgentDownloadPlatform): string {
  return latestLocalAgentRelease(config, platform).url;
}

/**
 * buildAgentDownloadOptions 生成主下载入口和备用下载入口。
 * @param {any} config - 新手教学系统配置。
 * @returns {{primary: AgentDownloadOption, secondary: AgentDownloadOption}} 下载入口。
 */
export function buildAgentDownloadOptions(config: any): {
  primary: AgentDownloadOption;
  secondary: AgentDownloadOption;
} {
  const currentPlatform = detectAgentDownloadPlatform();
  const secondaryPlatform = currentPlatform === "windows" ? "mac" : "windows";
  return {
    primary: {
      platform: currentPlatform,
      label: currentPlatform === "windows" ? "下载 Windows 本地程序" : "下载 Mac 本地程序",
      url: getAgentDownloadURL(config, currentPlatform),
    },
    secondary: {
      platform: secondaryPlatform,
      label: secondaryPlatform === "windows" ? "下载 Windows 版本" : "下载 Mac 版本",
      url: getAgentDownloadURL(config, secondaryPlatform),
    },
  };
}
