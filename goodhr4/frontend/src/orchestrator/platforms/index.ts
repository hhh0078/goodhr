/**
 * 平台注册表与 URL 检测
 *
 * 集中管理所有平台配置，提供根据 URL 自动识别平台的能力。
 * 支持远程动态配置（从后端 API 拉取）和本地硬编码兜底。
 * bridge.ts 通过此模块获取当前平台的配置信息。
 *
 * 页面校验：
 *   每个平台有 pages 数组，定义了有效页面。
 *   如果当前页面不在有效页面中，需要引导用户跳转。
 */

import type { PlatformConfig, PlatformPage } from "./types.js";
import { bossConfig } from "./boss.js";
import { lagouConfig } from "./lagou.js";
import { liepinConfig } from "./liepin.js";
import { hliepinConfig } from "./hliepin.js";
import { zhilianConfig } from "./zhilian.js";
import { employer58Config } from "./employer58.js";

/** 本地硬编码兜底配置（网络异常时使用） */
const LOCAL_PLATFORMS: PlatformConfig[] = [
  bossConfig,
  lagouConfig,
  liepinConfig,
  hliepinConfig,
  zhilianConfig,
  employer58Config,
];

/** 当前生效的平台配置列表（优先使用远程配置） */
let activePlatforms: PlatformConfig[] = [...LOCAL_PLATFORMS];

/**
 * 用远程配置替换当前平台列表
 * @param configs - 从后端拉取的平台配置数组
 */
export function applyRemoteConfigs(configs: PlatformConfig[]): void {
  if (configs && configs.length > 0) {
    activePlatforms = configs;
  }
}

/**
 * 获取当前生效的平台配置列表
 * @returns 平台配置数组
 */
export function getActivePlatforms(): PlatformConfig[] {
  return activePlatforms;
}

/**
 * 根据 URL 匹配平台配置
 * 使用 domain 字段进行 includes 匹配
 * @param url - 当前页面 URL
 * @returns 匹配的平台配置，未匹配返回 null
 */
export function detectPlatform(url: string): PlatformConfig | null {
  for (const platform of activePlatforms) {
    if (!platform.domain) continue;
    if (url.includes(platform.domain)) {
      return platform;
    }
  }
  return null;
}

/**
 * 校验当前页面是否在平台的有效页面列表中
 * pages 为空数组时表示该平台不限制页面，始终返回 true
 * @param url - 当前页面 URL
 * @param platform - 平台配置
 * @returns true 表示在有效页面上，false 表示需要引导跳转
 */
export function isOnValidPage(url: string, platform: PlatformConfig): boolean {
  if (!platform.pages || platform.pages.length === 0) {
    return true;
  }
  return platform.pages.some((page) => url.includes(page.url));
}

/**
 * 获取平台第一个有效页面（用于引导跳转）
 * @param platform - 平台配置
 * @returns 第一个有效页面配置，无则返回 null
 */
export function getFirstPage(platform: PlatformConfig): PlatformPage | null {
  if (!platform.pages || platform.pages.length === 0) {
    return null;
  }
  return platform.pages[0];
}

/**
 * 根据平台 ID 获取配置
 * @param id - 平台唯一标识
 * @returns 平台配置，未找到返回 null
 */
export function getPlatformById(id: string): PlatformConfig | null {
  return activePlatforms.find((p) => p.id === id) || null;
}

/**
 * 获取所有已注册平台的 ID 和名称列表
 * @returns 平台摘要列表
 */
export function listPlatforms(): Array<{ id: string; name: string }> {
  return activePlatforms.map((p) => ({ id: p.id, name: p.name }));
}
