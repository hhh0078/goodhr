function resolveRuntimeManifestVersion() {
  if (
    typeof chrome !== "undefined" &&
    chrome?.runtime?.getManifest &&
    chrome.runtime.getManifest().version
  ) {
    return chrome.runtime.getManifest().version;
  }
  return "";
}

export const APP_VERSION =
  resolveRuntimeManifestVersion() ||
  (typeof __APP_VERSION__ !== "undefined" && __APP_VERSION__) ||
  "0.0.0";
