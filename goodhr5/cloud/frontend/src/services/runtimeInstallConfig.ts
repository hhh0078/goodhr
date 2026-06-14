// 本文件负责从 system.onboarding_config 本地缓存生成运行组件安装配置。

type RuntimeAsset = {
  version?: string;
  url?: string;
  sha256?: string;
  note?: string;
};

type RuntimeManifest = {
  node_runtime: Record<string, RuntimeAsset>;
  node_worker: Record<string, RuntimeAsset>;
  cloakbrowser: Record<string, RuntimeAsset>;
  ocr: Record<string, RuntimeAsset>;
};

const ONBOARDING_CONFIG_CACHE_KEY = "system_onboarding_config";

const PLATFORM_ALIASES: Record<string, string[]> = {
  "win-x64": ["win-x64", "windows-x64", "win", "windows"],
  "darwin-arm64": ["darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"],
};

const COMPONENT_ALIASES: Record<keyof RuntimeManifest, string[]> = {
  node_runtime: ["node_runtime", "nodeRuntime", "node"],
  node_worker: ["node_worker", "nodeWorker", "worker", "browser_worker", "browserWorker"],
  cloakbrowser: ["cloakbrowser", "cloak_browser", "cloakBrowser", "browser"],
  ocr: ["ocr", "rapidocr", "rapidOCR"],
};

/**
 * 生成本地运行组件安装请求体。
 * @returns {Record<string, any>} 返回包含 manifest 的请求体。
 */
export function buildRuntimeInstallPayload() {
  const config = readOnboardingConfig();
  const source =
    objectValue(config.runtime_components) ||
    objectValue(config.runtimeComponents) ||
    objectValue(config.local_runtime_components) ||
    objectValue(config.localRuntimeComponents) ||
    objectValue(config.runtime) ||
    {};
  const manifest: RuntimeManifest = {
    node_runtime: normalizeComponent(source, "node_runtime"),
    node_worker: normalizeComponent(source, "node_worker"),
    cloakbrowser: normalizeComponent(source, "cloakbrowser"),
    ocr: normalizeComponent(source, "ocr"),
  };
  return { manifest };
}

/**
 * 读取本地缓存里的教学配置。
 * @returns {Record<string, any>} 返回教学配置对象。
 */
function readOnboardingConfig() {
  try {
    const raw = localStorage.getItem(ONBOARDING_CONFIG_CACHE_KEY) || "{}";
    const parsed = JSON.parse(raw);
    return objectValue(parsed) || {};
  } catch {
    return {};
  }
}

/**
 * 规范化单个运行组件配置。
 * @param {Record<string, any>} source - 运行组件原始配置。
 * @param {keyof RuntimeManifest} component - 组件键名。
 * @returns {Record<string, RuntimeAsset>} 返回按 Go 平台键整理后的资源配置。
 */
function normalizeComponent(source: Record<string, any>, component: keyof RuntimeManifest) {
  const componentConfig = pickComponentConfig(source, component);
  const result: Record<string, RuntimeAsset> = {};
  for (const [platform, aliases] of Object.entries(PLATFORM_ALIASES)) {
    const asset = pickPlatformAsset(componentConfig, aliases);
    if (asset.url) result[platform] = asset;
  }
  return result;
}

/**
 * 读取组件配置，兼容下划线和驼峰命名。
 * @param {Record<string, any>} source - 运行组件原始配置。
 * @param {keyof RuntimeManifest} component - 组件键名。
 * @returns {Record<string, any>} 返回组件配置。
 */
function pickComponentConfig(source: Record<string, any>, component: keyof RuntimeManifest) {
  for (const key of COMPONENT_ALIASES[component]) {
    const value = objectValue(source[key]);
    if (value) return value;
  }
  return {};
}

/**
 * 按平台别名读取资源配置。
 * @param {Record<string, any>} componentConfig - 单个组件配置。
 * @param {string[]} aliases - 平台别名列表。
 * @returns {RuntimeAsset} 返回资源配置。
 */
function pickPlatformAsset(componentConfig: Record<string, any>, aliases: string[]) {
  for (const alias of aliases) {
    const value = objectValue(componentConfig[alias]);
    if (value) return normalizeAsset(value);
  }
  return {};
}

/**
 * 规范化资源字段，兼容版本说明的不同命名。
 * @param {Record<string, any>} value - 原始资源配置。
 * @returns {RuntimeAsset} 返回标准资源配置。
 */
function normalizeAsset(value: Record<string, any>): RuntimeAsset {
  return {
    version: stringValue(value.version),
    url: stringValue(value.url),
    sha256: stringValue(value.sha256),
    note: stringValue(value.note || value.changelog || value.description || value.release_note),
  };
}

/**
 * 安全读取对象值。
 * @param {any} value - 待读取的值。
 * @returns {Record<string, any> | null} 对象值或空。
 */
function objectValue(value: any): Record<string, any> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, any>;
}

/**
 * 安全读取字符串值。
 * @param {any} value - 待读取的值。
 * @returns {string} 字符串值。
 */
function stringValue(value: any) {
  return String(value || "").trim();
}
