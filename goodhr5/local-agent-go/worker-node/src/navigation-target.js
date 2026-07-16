// 本文件负责判断现有浏览器标签页是否可以复用为任务目标页面，避免刷新用户提前设置的筛选条件。

/**
 * normalizeNavigationURL 规范化用于包含匹配的页面地址。
 * @param {any} value - 原始页面地址。
 * @returns {string} 可用于稳定比较的页面地址。
 */
function normalizeNavigationURL(value) {
  const rawURL = String(value || "").trim();
  if (!rawURL) return "";
  try {
    const parsed = new URL(rawURL);
    const pathname = (parsed.pathname || "/").replace(/\/+$/, "") || "/";
    return `${parsed.origin.toLowerCase()}${pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return rawURL.replace(/\/+$/, "");
  }
}

/**
 * pageURLContainsTarget 判断现有标签页地址是否包含任务目标地址。
 * @param {any} pageURL - 现有标签页的完整地址，可以包含查询参数。
 * @param {any} targetURL - 平台配置的任务目标地址。
 * @returns {boolean} 是否可以直接复用现有标签页。
 */
export function pageURLContainsTarget(pageURL, targetURL) {
  const current = normalizeNavigationURL(pageURL);
  const target = normalizeNavigationURL(targetURL);
  return current !== "" && target !== "" && current.includes(target);
}
