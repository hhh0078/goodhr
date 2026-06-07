// 本文件负责判断当前页面是否运行在 GoodHR 本地控制台环境中。

/**
 * 判断当前页面是否由本地程序控制台承载。
 * @returns {boolean} 本地控制台返回 true。
 */
export function isLocalConsole() {
  if (typeof window === "undefined") return false;
  const hostname = window.location.hostname;
  const port = Number(window.location.port || "0");
  return (hostname === "localhost" || hostname === "127.0.0.1") && port >= 9001 && port <= 9009;
}

/**
 * 返回当前本地控制台的 Local Agent 地址。
 * @returns {string} Local Agent HTTP 基础地址。
 */
export function localAgentBase() {
  if (typeof window === "undefined") return "";
  return window.location.origin;
}
