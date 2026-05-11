/**
 * 平台注册表与 URL 检测
 *
 * 集中管理所有平台配置，提供根据 URL 自动识别平台的能力。
 * bridge.ts 通过此模块获取当前平台的配置信息。
 */

import type { PlatformConfig } from "./types.js";
import { bossConfig } from "./boss.js";
import { lagouConfig } from "./lagou.js";
import { liepinConfig } from "./liepin.js";
import { hliepinConfig } from "./hliepin.js";
import { zhilianConfig } from "./zhilian.js";
import { employer58Config } from "./employer58.js";

/** 所有平台配置列表（优先匹配排前面的） */
const ALL_PLATFORMS: PlatformConfig[] = [
  bossConfig,
  lagouConfig,
  liepinConfig,
  hliepinConfig,
  zhilianConfig,
  employer58Config,
];

/**
 * 根据 URL 匹配平台配置
 * @param url - 当前页面 URL
 * @returns 匹配的平台配置，未匹配返回 null
 */
export function detectPlatform(url: string): PlatformConfig | null {
  for (const platform of ALL_PLATFORMS) {
    if (platform.urlPattern.test(url)) {
      return platform;
    }
  }
  return null;
}

/**
 * 根据平台 ID 获取配置
 * @param id - 平台唯一标识
 * @returns 平台配置，未找到返回 null
 */
export function getPlatformById(id: string): PlatformConfig | null {
  return ALL_PLATFORMS.find((p) => p.id === id) || null;
}

/**
 * 获取所有已注册平台的 ID 和名称列表
 * @returns 平台摘要列表
 */
export function listPlatforms(): Array<{ id: string; name: string }> {
  return ALL_PLATFORMS.map((p) => ({ id: p.id, name: p.name }));
}
