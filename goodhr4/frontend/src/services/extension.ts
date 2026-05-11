/**
 * Chrome 扩展 API 封装
 * 提供存储、标签页、消息通信等功能的统一接口
 */

import { deepClone } from "../utils/clone.js";
import { APP_VERSION } from "../constants/appVersion.js";
import type { Settings, Position } from "../constants/defaults.js";

function hasChrome(): boolean {
  return typeof chrome !== "undefined";
}

/** 获取扩展版本号 */
export function getManifestVersion(): string {
  if (hasChrome() && chrome.runtime?.getManifest) {
    return chrome.runtime.getManifest().version;
  }
  return APP_VERSION;
}

/** 从 chrome.storage 或 localStorage 读取数据 */
export async function storageGet(keys: string | string[]): Promise<Record<string, any>> {
  if (hasChrome() && chrome.storage?.local) {
    return chrome.storage.local.get(keys);
  }
  const raw = globalThis.localStorage?.getItem("__goodhr4_fallback__");
  const parsed = raw ? JSON.parse(raw) : {};
  if (Array.isArray(keys)) {
    return keys.reduce<Record<string, any>>((acc, key) => {
      acc[key] = parsed[key];
      return acc;
    }, {});
  }
  return { [keys]: parsed[keys] };
}

/** 写入 chrome.storage 或 localStorage */
export async function storageSet(payload: Record<string, any>): Promise<void> {
  if (hasChrome() && chrome.storage?.local) {
    await chrome.storage.local.set(payload);
    return;
  }
  const raw = globalThis.localStorage?.getItem("__goodhr4_fallback__");
  const parsed = raw ? JSON.parse(raw) : {};
  globalThis.localStorage?.setItem(
    "__goodhr4_fallback__",
    JSON.stringify({ ...parsed, ...payload }),
  );
}

/** 查询当前活跃标签页 */
export async function queryActiveTab(): Promise<chrome.tabs.Tab | null> {
  if (!hasChrome() || !chrome.tabs?.query) return null;
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  return tabs[0] || null;
}

/** 向当前活跃标签页发送消息 */
export async function sendMessageToActiveTab(message: any): Promise<any> {
  if (!hasChrome() || !chrome.tabs?.sendMessage) {
    return { status: "mock" };
  }
  const tab = await queryActiveTab();
  if (!tab?.id) {
    throw new Error("未找到当前标签页");
  }
  return chrome.tabs.sendMessage(tab.id, message);
}

/** 启动页面端运行 */
export async function startRunOnPage(settings: Settings, currentPosition: Position): Promise<any> {
  const shared = {
    matchLimit: settings.matchLimit,
    scrollDelayMin: settings.scrollDelayMin,
    scrollDelayMax: settings.scrollDelayMax,
    clickFrequency: settings.clickFrequency,
    enableSound: settings.enableSound,
    communicationEnabled: settings.runModeConfig.communicationEnabled,
    communicationConfig: deepClone(settings.communicationConfig),
  };

  if (settings.runMode === "ai") {
    return sendMessageToActiveTab({
      action: "START_AI_SCROLL",
      data: {
        ...shared,
        positionName: currentPosition.name,
        jobDescription: currentPosition.description,
        aiConfig: deepClone(settings.aiConfig),
      },
    });
  }

  return sendMessageToActiveTab({
    action: "START_SCROLL",
    data: {
      ...shared,
      keywords: [...currentPosition.keywords],
      excludeKeywords: [...currentPosition.excludeKeywords],
      isAndMode: settings.isAndMode,
    },
  });
}

/** 停止页面端运行 */
export async function stopRunOnPage(): Promise<any> {
  return sendMessageToActiveTab({ action: "STOP_SCROLL" });
}

/** 推送设置更新到页面端 */
export async function pushSettingsToPage(settings: Settings, currentPosition: Position): Promise<any> {
  return sendMessageToActiveTab({
    action: "SETTINGS_UPDATED",
    data: {
      ...deepClone(currentPosition),
      isAndMode: settings.isAndMode,
      matchLimit: settings.matchLimit,
      scrollDelayMin: settings.scrollDelayMin,
      scrollDelayMax: settings.scrollDelayMax,
      clickFrequency: settings.clickFrequency,
      enableSound: settings.enableSound,
      communicationConfig: deepClone(settings.communicationConfig),
    },
  }).catch(() => null);
}

/** 注册运行时日志监听器，返回取消监听函数 */
export function attachRuntimeLogListener(onMessage: (data: any) => void): () => void {
  if (!hasChrome() || !chrome.runtime?.onMessage) {
    return () => {};
  }
  const handler = (message: any) => {
    if (message?.type === "LOG_MESSAGE" && message.data) {
      onMessage(message.data);
    }
  };
  chrome.runtime.onMessage.addListener(handler);
  return () => chrome.runtime.onMessage.removeListener(handler);
}
