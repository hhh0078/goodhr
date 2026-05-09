import { deepClone } from "../utils/clone.js";
import { APP_VERSION } from "../constants/appVersion.js";

function hasChrome() {
  return typeof chrome !== "undefined";
}

export function getManifestVersion() {
  if (hasChrome() && chrome.runtime?.getManifest) {
    return chrome.runtime.getManifest().version;
  }
  return APP_VERSION;
}

export async function storageGet(keys) {
  if (hasChrome() && chrome.storage?.local) {
    return chrome.storage.local.get(keys);
  }
  const raw = globalThis.localStorage?.getItem("__goodhr4_fallback__");
  const parsed = raw ? JSON.parse(raw) : {};
  if (Array.isArray(keys)) {
    return keys.reduce((acc, key) => {
      acc[key] = parsed[key];
      return acc;
    }, {});
  }
  return { [keys]: parsed[keys] };
}

export async function storageSet(payload) {
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

export async function queryActiveTab() {
  if (!hasChrome() || !chrome.tabs?.query) return null;
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  return tabs[0] || null;
}

export async function sendMessageToActiveTab(message) {
  if (!hasChrome() || !chrome.tabs?.sendMessage) {
    return { status: "mock" };
  }
  const tab = await queryActiveTab();
  if (!tab?.id) {
    throw new Error("未找到当前标签页");
  }
  return chrome.tabs.sendMessage(tab.id, message);
}

export async function startRunOnPage(settings, currentPosition) {
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

export async function stopRunOnPage() {
  return sendMessageToActiveTab({ action: "STOP_SCROLL" });
}

export async function pushSettingsToPage(settings, currentPosition) {
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

export function attachRuntimeLogListener(onMessage) {
  if (!hasChrome() || !chrome.runtime?.onMessage) {
    return () => {};
  }
  const handler = (message) => {
    if (message?.type === "LOG_MESSAGE" && message.data) {
      onMessage(message.data);
    }
  };
  chrome.runtime.onMessage.addListener(handler);
  return () => chrome.runtime.onMessage.removeListener(handler);
}
