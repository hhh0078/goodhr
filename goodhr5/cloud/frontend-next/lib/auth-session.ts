/** 本文件负责判断何时清除登录凭证，防止临时故障或旧请求误退新会话。 */

/** shouldClearSession 仅在服务器明确判定本次请求凭证失效且凭证未被更新时返回 true。 */
export function shouldClearSession(
  status: number,
  code: string,
  requestToken: string,
  currentToken: string,
): boolean {
  return status === 401 && Boolean(requestToken) && requestToken === currentToken &&
    (code === "SESSION_REQUIRED" || code === "SESSION_EXPIRED");
}
