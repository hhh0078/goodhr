/**
 * 后端 API 封装
 * 包含本地服务 (localhost:8787) 和远程认证服务 (ai.58it.cn) 的接口
 */

const DEFAULT_BASE = "http://127.0.0.1:8787";
const AUTH_BASE = "https://ai.58it.cn";

/** 获取 API 基础地址 */
export function getApiBase(): string {
  return (globalThis as any).GOODHR_CONFIG?.API_BASE || DEFAULT_BASE;
}

/** 通用请求方法 */
async function request(path: string, options: RequestInit & { headers?: Record<string, string> } = {}): Promise<any> {
  const response = await fetch(`${getApiBase()}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
    ...options,
  });

  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `请求失败: ${response.status}`);
  }
  return data;
}

/** 绑定身份标识 */
export function bindIdentity(identifier: string): Promise<any> {
  return request("/api/v1/account/bind", {
    method: "POST",
    body: JSON.stringify({ identifier }),
  });
}

/** 拉取用户设置 */
export function fetchSettings(identifier: string): Promise<any> {
  return request(`/api/v1/account/${encodeURIComponent(identifier)}/settings`);
}

/** 保存用户设置 */
export function saveSettings(identifier: string, settings: any): Promise<any> {
  return request(`/api/v1/account/${encodeURIComponent(identifier)}/settings`, {
    method: "POST",
    body: JSON.stringify({ settings }),
  });
}

/** 拉取系统配置 */
export function fetchSystemConfig(key = "frontend"): Promise<any> {
  return request(`/api/v1/system/config?key=${encodeURIComponent(key)}`);
}

/** 注册认证用户（ai.58it.cn） */
export async function registerAuthUser(email: string): Promise<any> {
  const response = await fetch(`${AUTH_BASE}/api/auth/register`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      email,
      inviter_id: 1,
      key_name: "goodhr",
    }),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok || data?.code !== 0) {
    throw new Error(data?.message || `请求失败: ${response.status}`);
  }
  return data?.data || {};
}
