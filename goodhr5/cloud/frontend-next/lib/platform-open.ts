/** 本文件负责统一判断招聘平台配置中的开放开关。 */
"use client";

export type PlatformConfigLike = {
  config_key?: string;
  key?: string;
  platform_id?: string;
  id?: string;
  config_value?: unknown;
  value?: unknown;
  open?: boolean;
};

/** parsePlatformConfig 解析平台配置，兼容 system_configs 包装和直接 JSON。 */
function parsePlatformConfig(config: PlatformConfigLike | null | undefined) {
  if (!config) return null;
  const value = config.config_value ?? config.value ?? config;
  if (typeof value !== "string") return value as PlatformConfigLike;
  try {
    return JSON.parse(value) as PlatformConfigLike;
  } catch {
    return null;
  }
}

/** platformConfigID 读取平台配置对应的平台 ID。 */
function platformConfigID(config: PlatformConfigLike) {
  const parsed = parsePlatformConfig(config);
  const key = String(config.config_key || config.key || "");
  return String(
    config.platform_id ||
      config.id ||
      parsed?.platform_id ||
      parsed?.id ||
      key.replace(/^platform\./, ""),
  ).toLowerCase();
}

/** findPlatformConfig 从平台配置列表中找到指定平台配置。 */
export function findPlatformConfig(configs: PlatformConfigLike[], platformID: string) {
  const target = String(platformID || "").toLowerCase();
  if (!target) return null;
  return (configs || []).find((config) => platformConfigID(config) === target) || null;
}

/** isPlatformOpen 判断平台是否开放；配置存在但未写 open 时默认开放。 */
export function isPlatformOpen(configs: PlatformConfigLike[], platformID: string) {
  const config = findPlatformConfig(configs, platformID);
  if (!config) return false;
  const parsed = parsePlatformConfig(config);
  if (!parsed) return false;
  return parsed?.open !== false;
}
