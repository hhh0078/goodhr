/**
 * 应用版本号，优先从 Chrome Runtime 获取，其次从构建变量获取
 */

declare const __APP_VERSION__: string;

function resolveRuntimeManifestVersion(): string {
  if (
    typeof chrome !== "undefined" &&
    chrome?.runtime?.getManifest &&
    chrome.runtime.getManifest().version
  ) {
    return chrome.runtime.getManifest().version;
  }
  return "";
}

export const APP_VERSION: string =
  resolveRuntimeManifestVersion() ||
  (typeof __APP_VERSION__ !== "undefined" && __APP_VERSION__) ||
  "0.0.0";
