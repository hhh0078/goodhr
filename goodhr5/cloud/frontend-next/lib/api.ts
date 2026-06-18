/** 本文件负责新版前端访问 GoodHR 云端 API 和统一错误处理。 */

export const TOKEN_KEY = "goodhr5_access_token";
export const SESSION_EMAIL_KEY = "goodhr5_session_email";
export const INVITE_CACHE_KEY = "goodhr5_invite_id";

/** cloudAPIBase 返回浏览器应访问的云端 API 地址。 */
export function cloudAPIBase() {
  return (process.env.NEXT_PUBLIC_CLOUD_API_BASE || "https://goodhr5.58it.cn").replace(/\/$/, "");
}

/** legacyAdminURL 返回登录成功后暂时进入旧后台的地址。 */
export function legacyAdminURL() {
  return process.env.NEXT_PUBLIC_LEGACY_ADMIN_URL || "/admin/";
}

/** apiRequest 请求云端 JSON 接口并将错误转换成中文。 */
export async function apiRequest(path: string, init: RequestInit = {}) {
  let response: Response;
  try {
    response = await fetch(`${cloudAPIBase()}${path}`, {
      ...init,
      cache: "no-store",
      headers: { "Content-Type": "application/json", ...(init.headers || {}) },
    });
  } catch {
    throw new Error("无法连接云端服务，请检查网络后重试");
  }
  const text = await response.text();
  let data: any = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    throw new Error("云端返回的数据格式不正确");
  }
  if (!response.ok || data.ok === false) {
    throw new Error(normalizeAPIError(data.error || data.msg));
  }
  return data;
}

/** normalizeAPIError 将后端常见错误转换成用户可读的中文。 */
function normalizeAPIError(value: unknown) {
  const message = String(value || "").trim();
  const messages: Record<string, string> = {
    "invalid email": "邮箱格式不正确",
    "invalid code": "验证码格式不正确",
    "failed to send code": "验证码发送失败，请稍后重试",
    "failed to save session": "登录状态保存失败，请稍后重试",
    "session is invalid or expired": "登录状态已过期，请重新登录",
  };
  return messages[message] || message || "请求失败，请稍后重试";
}
