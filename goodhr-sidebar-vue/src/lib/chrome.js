export function getConfig() {
  return globalThis.GOODHR_CONFIG || {};
}

export function getApiBase() {
  return getConfig().API_BASE || "https://goodhr.58it.cn";
}

export function getApiRequest() {
  return globalThis.apiRequest || null;
}

export function getManifestVersion() {
  if (globalThis.chrome?.runtime?.getManifest) {
    return chrome.runtime.getManifest().version;
  }
  return "0.0.0";
}

export async function storageGet(key) {
  if (!globalThis.chrome?.storage?.local) {
    return {};
  }

  return chrome.storage.local.get(key);
}

export async function storageSet(payload) {
  if (!globalThis.chrome?.storage?.local) {
    return;
  }

  await chrome.storage.local.set(payload);
}
