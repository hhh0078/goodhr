// 本文件负责根据用户当前系统选择 GoodHR 本地程序下载链接。

export type AgentDownloadPlatform = "mac" | "windows";

export type AgentDownloadOption = {
  platform: AgentDownloadPlatform;
  label: string;
  url: string;
};

/**
 * detectAgentDownloadPlatform 识别当前浏览器所在系统，识别失败时默认返回 Mac。
 * @returns {AgentDownloadPlatform} 当前优先展示的系统。
 */
export function detectAgentDownloadPlatform(): AgentDownloadPlatform {
  const nav = window.navigator as Navigator & { userAgentData?: { platform?: string } };
  const platform = `${nav.userAgentData?.platform || nav.platform || nav.userAgent || ""}`.toLowerCase();
  if (platform.includes("win")) return "windows";
  return "mac";
}

/**
 * getAgentDownloadURL 获取指定系统的本地程序下载链接，并使用旧配置字段兜底。
 * @param {any} config - 新手教学系统配置。
 * @param {AgentDownloadPlatform} platform - 目标系统。
 * @returns {string} 下载链接。
 */
export function getAgentDownloadURL(config: any, platform: AgentDownloadPlatform): string {
  const macURL = String(config?.local_agent_download_url_mac || "").trim();
  const windowsURL = String(config?.local_agent_download_url_windows || "").trim();
  const fallbackURL = String(config?.local_agent_download_url || "").trim();
  if (!macURL && !windowsURL) return fallbackURL;
  if (platform === "windows") return windowsURL;
  return macURL;
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
