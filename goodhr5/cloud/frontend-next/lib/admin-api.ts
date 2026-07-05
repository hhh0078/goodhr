/** 本文件负责新版后台统一访问云端和本地程序接口。 */
"use client";

import { TOKEN_KEY } from "./api";

export const CLOUD_API_BASE = (
  process.env.NEXT_PUBLIC_CLOUD_API_BASE || "https://goodhr5.58it.cn"
).replace(/\/$/, "");
export const LOCAL_AGENT_PORTS = [55271];
const LOCAL_AGENT_DETECT_CACHE_MS = 2000;
const LOCAL_AGENT_DETECT_CACHE_KEY = "goodhr5_local_agent_detect_cache";
const LOCAL_AGENT_BIND_CACHE_MS = 60_000;
const LOCAL_AGENT_BIND_CACHE_KEY = "goodhr5_local_agent_bind_cache";

type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
  auth?: boolean;
};

type LocalAgentDetectState = {
  detecting: Promise<string> | null;
  cache: { baseURL: string; checkedAt: number };
};

declare global {
  interface Window {
    __goodhrLocalAgentDetectState?: LocalAgentDetectState;
  }
}

const localAgentDetectFallbackState: LocalAgentDetectState = {
  detecting: null,
  cache: { baseURL: "", checkedAt: 0 },
};

/** getToken 返回浏览器缓存的登录凭证。 */
export function getToken() {
  return typeof window === "undefined"
    ? ""
    : localStorage.getItem(TOKEN_KEY) || "";
}

/** cloudRequest 统一请求云端接口并处理鉴权与错误。 */
export async function cloudRequest(path: string, options: RequestOptions = {}) {
  const { body, auth = true, headers, ...rest } = options;
  const token = auth ? getToken() : "";
  let response: Response;
  try {
    response = await fetch(`${CLOUD_API_BASE}${path}`, {
      ...rest,
      cache: "no-store",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(headers || {}),
      },
      body:
        body == null
          ? undefined
          : typeof body === "string"
            ? body
            : JSON.stringify(body),
    });
  } catch {
    throw new Error("无法连接云端服务，请检查网络后重试");
  }
  return parseResponse(response, "云端请求失败", Boolean(token));
}

/** localRequest 统一请求本地程序接口。 */
export async function localRequest(
  baseURL: string,
  path: string,
  options: RequestOptions = {},
) {
  const { body, headers, ...rest } = options;
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 45000);
  try {
    const response = await fetch(`${baseURL.replace(/\/$/, "")}${path}`, {
      ...rest,
      signal: controller.signal,
      cache: "no-store",
      headers: { "Content-Type": "application/json", ...(headers || {}) },
      body:
        body == null
          ? undefined
          : typeof body === "string"
            ? body
            : JSON.stringify(body),
    });
    const data = await parseResponse(response, "本地程序请求失败", false);
    return data?.data ?? data;
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError")
      throw new Error("本地程序请求超时，请稍后重试");
    throw error instanceof Error ? error : new Error("无法连接本地程序");
  } finally {
    window.clearTimeout(timeout);
  }
}

/** openLocalPage 通过本地程序打开当前浏览器页面。 */
export async function openLocalPage(baseURL: string, payload: unknown) {
  return localRequest(baseURL, "/api/v1/page/open", {
    method: "POST",
    body: payload,
  });
}

/** currentLocalPageURL 读取本地浏览器当前页面地址。 */
export async function currentLocalPageURL(baseURL: string) {
  const data = await localRequest(baseURL, "/api/v1/page/url");
  return String(data?.url || "");
}

/** bindLocalAgent 将当前本地程序绑定信息上报云端。 */
export async function bindLocalAgent(baseURL: string) {
  if (!baseURL || !getToken()) return;
  const cacheKey = `${LOCAL_AGENT_BIND_CACHE_KEY}:${baseURL}`;
  const lastBoundAt = Number(localStorage.getItem(cacheKey) || 0);
  if (Date.now() - lastBoundAt < LOCAL_AGENT_BIND_CACHE_MS) return;
  const health = await localRequest(baseURL, "/health");
  const machineID = await localAgentMachineID(baseURL, health);
  await cloudRequest("/api/agents/bind", {
    method: "POST",
    body: {
      machine_id: machineID,
      agent_version: String(health?.version || health?.agent_version || ""),
      local_port: Number(health?.port || baseURL.match(/:(\d+)$/)?.[1] || 0),
    },
  });
  localStorage.setItem(cacheKey, String(Date.now()));
}

/** localAgentMachineID 生成当前本地程序稳定机器码。 */
async function localAgentMachineID(baseURL: string, health: any) {
  const stableParts = [
    health?.machine_id,
    health?.machineId,
    health?.dataDir,
    health?.data_dir,
    health?.dbPath,
    health?.db_path,
  ]
    .filter(Boolean)
    .join("|");
  return `local-${await sha256(stableParts || baseURL)}`;
}

/** sha256 计算短哈希字符串。 */
async function sha256(value: string) {
  const bytes = new TextEncoder().encode(value);
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(hash))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

/** detectLocalAgent 探测本地程序端口，并合并短时间内的重复探测。 */
export async function detectLocalAgent(preferredBaseURL = "") {
  const state = localAgentDetectState();
  syncLocalAgentDetectCacheFromStorage(state);
  if (isLocalAgentDetectCacheValid(state, preferredBaseURL))
    return state.cache.baseURL;
  if (state.detecting) return state.detecting;
  state.detecting = detectLocalAgentOnce(state, preferredBaseURL).finally(
    () => {
      state.detecting = null;
    },
  );
  return state.detecting;
}

/** detectLocalAgentOnce 执行一次真实端口探测。 */
async function detectLocalAgentOnce(
  state: LocalAgentDetectState,
  preferredBaseURL = "",
) {
  const preferredPort = Number(preferredBaseURL.match(/:(\d+)$/)?.[1] || 0);
  const ports =
    preferredPort && LOCAL_AGENT_PORTS.includes(preferredPort)
      ? [
          preferredPort,
          ...LOCAL_AGENT_PORTS.filter((port) => port !== preferredPort),
        ]
      : LOCAL_AGENT_PORTS;
  for (const port of ports) {
    const baseURL = `http://127.0.0.1:${port}`;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 450);
    try {
      const response = await fetch(`${baseURL}/health`, {
        cache: "no-store",
        signal: controller.signal,
      });
      if (response.ok) {
        state.cache = { baseURL, checkedAt: Date.now() };
        saveLocalAgentDetectCache(state.cache);
        return baseURL;
      }
    } catch {
      // 当前端口不可用时继续检查下一个端口。
    } finally {
      window.clearTimeout(timeout);
    }
  }
  state.cache = { baseURL: "", checkedAt: Date.now() };
  saveLocalAgentDetectCache(state.cache);
  return "";
}

/** isLocalAgentDetectCacheValid 判断上次本地程序探测结果是否还能复用。 */
function isLocalAgentDetectCacheValid(
  state: LocalAgentDetectState,
  preferredBaseURL: string,
) {
  if (!state.cache.checkedAt) return false;
  const cachedPort = Number(state.cache.baseURL.match(/:(\d+)$/)?.[1] || 0);
  if (state.cache.baseURL && !LOCAL_AGENT_PORTS.includes(cachedPort))
    return false;
  if (Date.now() - state.cache.checkedAt > LOCAL_AGENT_DETECT_CACHE_MS)
    return false;
  if (
    preferredBaseURL &&
    state.cache.baseURL &&
    preferredBaseURL !== state.cache.baseURL
  )
    return false;
  return true;
}

/** localAgentDetectState 返回浏览器全局共享的本地程序探测状态。 */
function localAgentDetectState() {
  if (typeof window === "undefined") return localAgentDetectFallbackState;
  window.__goodhrLocalAgentDetectState ||= {
    detecting: null,
    cache: { baseURL: "", checkedAt: 0 },
  };
  return window.__goodhrLocalAgentDetectState;
}

/** clearLocalAgentDetectCache 清空本地程序探测缓存。 */
export function clearLocalAgentDetectCache() {
  const state = localAgentDetectState();
  state.cache = { baseURL: "", checkedAt: 0 };
  state.detecting = null;
  if (typeof window === "undefined") return;
  try {
    localStorage.removeItem(LOCAL_AGENT_DETECT_CACHE_KEY);
  } catch {
    // 浏览器缓存不可写时忽略，页面状态已经清空。
  }
}

/** syncLocalAgentDetectCacheFromStorage 从浏览器缓存同步最近一次本地程序探测结果。 */
function syncLocalAgentDetectCacheFromStorage(state: LocalAgentDetectState) {
  if (typeof window === "undefined" || state.cache.checkedAt) return;
  try {
    const raw = localStorage.getItem(LOCAL_AGENT_DETECT_CACHE_KEY);
    const cache = raw ? JSON.parse(raw) : null;
    if (
      cache &&
      typeof cache.baseURL === "string" &&
      typeof cache.checkedAt === "number"
    ) {
      state.cache = cache;
    }
  } catch {
    // 浏览器缓存不可读时忽略，继续走实时探测。
  }
}

/** saveLocalAgentDetectCache 保存最近一次本地程序探测结果，减少页面切换后的重复 health 请求。 */
function saveLocalAgentDetectCache(cache: {
  baseURL: string;
  checkedAt: number;
}) {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(LOCAL_AGENT_DETECT_CACHE_KEY, JSON.stringify(cache));
  } catch {
    // 浏览器缓存不可写时忽略，不影响本次探测结果。
  }
}

/** parseResponse 解析统一 JSON 响应并输出中文错误。 */
async function parseResponse(
  response: Response,
  fallback: string,
  clearInvalidToken: boolean,
) {
  const text = await response.text();
  let data: any = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    throw new Error("接口返回的数据格式不正确");
  }
  const code = Number(
    data.code || (response.ok && data.ok !== false ? 200 : response.status),
  );
  if (
    !response.ok ||
    data.ok === false ||
    (data.code != null && code !== 200)
  ) {
    if (response.status === 401 && clearInvalidToken)
      localStorage.removeItem(TOKEN_KEY);
    throw new Error(String(data.msg || data.error || data.detail || fallback));
  }
  return data;
}

/** formatDate 将接口日期转换为当前电脑的本地时间。 */
export function formatDate(value: unknown) {
  if (!value) return "--";
  const date = new Date(String(value));
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleDateString("zh-CN");
}
