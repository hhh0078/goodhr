// 文件作用说明：提供页面复用判断和日志 URL 脱敏公共方法。

/** safeURL 清除日志 URL 中的查询参数和锚点。 */
export function safeURL(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return rawURL.slice(0, 500);
  }
}

/** pageURLContainsTarget 判断已有标签页能否复用并保留用户设置的筛选条件。 */
export function pageURLContainsTarget(
  pageURL: string,
  targetURL: string,
): boolean {
  try {
    const current = new URL(pageURL.trim());
    const target = new URL(targetURL.trim());
    const currentPath = normalizeNavigationPath(current.pathname);
    const targetPath = normalizeNavigationPath(target.pathname);
    const pathMatches =
      currentPath === targetPath ||
      (targetPath !== "/" && currentPath.startsWith(`${targetPath}/`));
    return (
      current.origin.toLowerCase() === target.origin.toLowerCase() &&
      pathMatches &&
      (target.search === "" || current.search.includes(target.search))
    );
  } catch {
    const current = pageURL.trim().replace(/\/+$/, "");
    const target = targetURL.trim().replace(/\/+$/, "");
    return current !== "" && target !== "" && current.startsWith(target);
  }
}

/** normalizeNavigationPath 规范化标签页复用时参与比较的页面路径。 */
function normalizeNavigationPath(pathname: string): string {
  return pathname.replace(/\/+$/, "") || "/";
}
