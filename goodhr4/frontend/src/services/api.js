const DEFAULT_BASE = "http://127.0.0.1:8787";
const AUTH_BASE = "https://ai.58it.cn";

export function getApiBase() {
  return globalThis.GOODHR_CONFIG?.API_BASE || DEFAULT_BASE;
}

async function request(path, options = {}) {
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

export function bindIdentity(identifier) {
  return request("/api/v1/account/bind", {
    method: "POST",
    body: JSON.stringify({ identifier }),
  });
}

export function fetchSettings(identifier) {
  return request(`/api/v1/account/${encodeURIComponent(identifier)}/settings`);
}

export function saveSettings(identifier, settings) {
  return request(`/api/v1/account/${encodeURIComponent(identifier)}/settings`, {
    method: "POST",
    body: JSON.stringify({ settings }),
  });
}

export function fetchSystemConfig(key = "frontend") {
  return request(`/api/v1/system/config?key=${encodeURIComponent(key)}`);
}

export async function registerAuthUser(email) {
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
