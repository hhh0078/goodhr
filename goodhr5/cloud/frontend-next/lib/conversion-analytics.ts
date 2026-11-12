/** 文件作用：统一记录匿名注册步骤，只发送固定事件和分类，不发送邮箱、密码、验证码、令牌或错误原文。 */

export const CLARITY_PROJECT_ID = "z92qaybaap";
const ARRIVAL_KEY = "goodhr.auth.pending-arrival";
type AuthFlow = "new_account" | "existing_account" | "unknown";
type ConversionEvent = "site_visit" | "login_view" | "code_request" | "code_sent" | "code_failed" | "login_submit" | "agreement_opened" | "agreement_accepted" | "agreement_cancelled" | "validation_failed" | "login_failed" | "register_success" | "login_success" | "password_setup_required" | "password_setup_completed" | "admin_entered";
type ConversionDetail = { flow?: AuthFlow; method?: "code" | "password" | "wechat"; reason?: "email" | "code" | "password" | "password_confirmation" | "send_code_first" | "network" | "credentials" | "rate_limit" | "email_domain" | "other" };
type ClarityCommand = ((...args: unknown[]) => void) & { q?: unknown[][] };

declare global {
  interface Window {
    clarity?: ClarityCommand;
    _hmt?: unknown[][];
  }
}

/** isPrivateAnalyticsPath 排除含私人分享令牌的页面，保持原有统计边界。 */
export function isPrivateAnalyticsPath(pathname: string) {
  return ["/resumes/share", "/interview-invitation", "/diagnostics"].some((prefix) => pathname === prefix || pathname.startsWith(prefix + "/"));
}

/** canRecordClarity 只允许官网和登录页回放，后台及未知路由一律不采集。 */
export function canRecordClarity(pathname: string) {
  return ["/", "/login", "/features", "/pricing", "/videos", "/download", "/contact", "/articles", "/recruitment-automation", "/recruitment-auto-reply", "/hr-tools"].includes(pathname.replace(/\/$/, "") || "/") || pathname.startsWith("/articles/");
}

/** trackConversion 向百度和允许页面的Clarity发送固定分类，统计失败不阻断业务。 */
export function trackConversion(event: ConversionEvent, detail: ConversionDetail = {}) {
  if (typeof window === "undefined" || isPrivateAnalyticsPath(window.location.pathname)) return;
  try {
    const label = [detail.flow, detail.method, detail.reason].filter(Boolean).join(":") || "anonymous";
    (window._hmt ||= []).push(["_trackEvent", "registration_funnel", event, label]);
    if (canRecordClarity(window.location.pathname) && window.clarity) {
      for (const [key, value] of Object.entries(detail)) if (value) window.clarity("set", "auth_" + key, value);
      window.clarity("event", event);
    }
  } catch { /* 第三方统计不可用时继续原业务流程。 */ }
}

/** classifyAuthFailure 将错误归入固定分类，禁止上传可能带邮箱或个人信息的错误原文。 */
export function classifyAuthFailure(error: unknown): NonNullable<ConversionDetail["reason"]> {
  const message = error instanceof Error ? error.message : "";
  if (/网络|连接|超时/.test(message)) return "network";
  if (/验证码|密码|账号不存在/.test(message)) return "credentials";
  if (/频繁|稍后|次数|限制/.test(message)) return "rate_limit";
  if (/域名|白名单/.test(message)) return "email_domain";
  return "other";
}

/** markPendingAdminArrival 暂存匿名登录类型，等待后台真实读取账号成功后再记到达。 */
export function markPendingAdminArrival(flow: AuthFlow) {
  try { sessionStorage.setItem(ARRIVAL_KEY, JSON.stringify({ flow, at: Date.now() })); } catch { /* 禁用存储不影响登录。 */ }
}

/** trackAdminArrival 只在后台账号读取成功后记录一次，刷新和轮询不会重复计数。 */
export function trackAdminArrival() {
  try {
    const raw = sessionStorage.getItem(ARRIVAL_KEY);
    if (!raw) return;
    sessionStorage.removeItem(ARRIVAL_KEY);
    const pending = JSON.parse(raw);
    if (!["new_account", "existing_account", "unknown"].includes(pending.flow) || !Number.isFinite(pending.at) || Date.now() - pending.at > 30 * 60_000 || pending.at > Date.now()) return;
    trackConversion("admin_entered", { flow: pending.flow });
  } catch { /* 存储不可用或旧格式损坏时跳过统计。 */ }
}
