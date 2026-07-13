// 本文件负责识别当前浏览器所在系统平台，供本地程序下载和组件展示复用。

export type AgentDownloadPlatform = "mac" | "windows";

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
