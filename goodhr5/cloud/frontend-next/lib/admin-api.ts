/** 本文件负责新版后台统一访问云端和本地程序接口。 */
"use client";

import { TOKEN_KEY } from "./api";

const DEFAULT_CLOUD_API_BASE =
  process.env.NODE_ENV === "production"
    ? "https://goodhr5.58it.cn"
    : "http://127.0.0.1:8084";

export const CLOUD_API_BASE = (
  process.env.NEXT_PUBLIC_CLOUD_API_BASE || DEFAULT_CLOUD_API_BASE
).replace(/\/$/, "");
export const LOCAL_AGENT_PORTS = [43129];
const LOCAL_AGENT_DETECT_CACHE_MS = 2000;
const LOCAL_AGENT_DETECT_CACHE_KEY = "goodhr5_local_agent_detect_cache";
const LOCAL_AGENT_MACHINE_ID_KEY = "goodhr5_local_agent_machine_id";
const LOCAL_AGENT_PORT_QUERY_KEY = "local_port";
const LOCAL_AGENT_PORT_CACHE_KEY = "goodhr5_local_agent_port";

type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
  auth?: boolean;
  timeoutMS?: number;
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
  const { body, headers, timeoutMS = 45000, ...rest } = options;
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), timeoutMS);
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

/** captureLocalAgentPortFromURL 读取本地程序写入页面链接的端口，并持久化供登录页和后台共用。 */
export function captureLocalAgentPortFromURL(search?: string) {
  if (typeof window === "undefined") return 0;
  const port = normalizeLocalAgentPort(
    new URLSearchParams(search ?? window.location.search).get(
      LOCAL_AGENT_PORT_QUERY_KEY,
    ),
  );
  if (!port) return 0;
  localStorage.setItem(LOCAL_AGENT_PORT_CACHE_KEY, String(port));
  clearLocalAgentDetectCache();
  return port;
}

/** bindDetectedLocalAgent 把浏览器已探测到的本地程序版本和匿名机器码同步到云端。 */
export async function bindDetectedLocalAgent(baseURL: string) {
  const health = await localRequest(baseURL, "/health");
  const localPort = Number(new URL(baseURL).port || 0);
  const machineID = await buildLocalAgentMachineID(health);
  return cloudRequest("/api/agents/bind", {
    method: "POST",
    body: {
      machine_id: machineID,
      agent_version: String(health?.version || health?.agent_version || ""),
      local_port: localPort,
    },
  });
}

/** buildLocalAgentMachineID 根据本地数据目录生成不可逆机器码，缺少目录时使用浏览器持久化随机值。 */
async function buildLocalAgentMachineID(health: any) {
  const dataDir = String(health?.dataDir || health?.data_dir || "")
    .trim()
    .toLowerCase();
  if (dataDir && globalThis.crypto?.subtle) {
    const digest = await globalThis.crypto.subtle.digest(
      "SHA-256",
      new TextEncoder().encode(`goodhr-local-agent:${dataDir}`),
    );
    const hash = Array.from(new Uint8Array(digest), (value) =>
      value.toString(16).padStart(2, "0"),
    ).join("");
    return `sha256-${hash}`;
  }
  const saved = localStorage.getItem(LOCAL_AGENT_MACHINE_ID_KEY);
  if (saved) return saved;
  const generated = `browser-${globalThis.crypto.randomUUID()}`;
  localStorage.setItem(LOCAL_AGENT_MACHINE_ID_KEY, generated);
  return generated;
}

/** detectLocalAgentOnce 执行一次真实端口探测。 */
async function detectLocalAgentOnce(
  state: LocalAgentDetectState,
  preferredBaseURL = "",
) {
  const preferredPort = normalizeLocalAgentPort(
    preferredBaseURL.match(/:(\d+)$/)?.[1],
  );
  const ports = Array.from(
    new Set(
      [preferredPort, cachedLocalAgentPort(), ...LOCAL_AGENT_PORTS].filter(
        (port): port is number => Boolean(port),
      ),
    ),
  );
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
  const cachedPort = normalizeLocalAgentPort(
    state.cache.baseURL.match(/:(\d+)$/)?.[1],
  );
  if (state.cache.baseURL && !cachedPort) return false;
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

/** cachedLocalAgentPort 返回页面最近缓存的合法本地程序端口。 */
function cachedLocalAgentPort() {
  if (typeof window === "undefined") return 0;
  const port = normalizeLocalAgentPort(
    localStorage.getItem(LOCAL_AGENT_PORT_CACHE_KEY),
  );
  if (!port) localStorage.removeItem(LOCAL_AGENT_PORT_CACHE_KEY);
  return port;
}

/** normalizeLocalAgentPort 把外部端口值限制为有效的 TCP 端口整数。 */
function normalizeLocalAgentPort(value: unknown) {
  const raw = String(value ?? "").trim();
  if (!/^\d+$/.test(raw)) return 0;
  const port = Number(raw);
  return Number.isInteger(port) && port >= 1 && port <= 65535 ? port : 0;
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
		if (response.status === 401 && clearInvalidToken) {
			localStorage.removeItem(TOKEN_KEY);
			if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
				const next = encodeURIComponent(window.location.pathname + window.location.search);
				window.location.replace(`/login?next=${next}`);
			}
		}
    throw new Error(responseErrorMessage(data, fallback));
  }
  return data;
}

/** responseErrorMessage 从字符串或错误对象中读取用户可见信息，避免出现 object Object。 */
function responseErrorMessage(data: unknown, fallback: string) {
  if (!isRecord(data)) return fallback;
  for (const key of ["message", "msg", "error", "detail"]) {
    const value = data[key];
    if (typeof value === "string" && value.trim()) return value.trim();
    if (isRecord(value) && typeof value.message === "string" && value.message.trim()) {
      return value.message.trim();
    }
  }
  return fallback;
}

/** isRecord 判断未知接口值是否为可安全读取字段的普通对象。 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** formatDate 将接口日期转换为当前电脑的本地时间。 */
export function formatDate(value: unknown) {
  if (!value) return "--";
  const date = new Date(String(value));
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleDateString("zh-CN");
}
