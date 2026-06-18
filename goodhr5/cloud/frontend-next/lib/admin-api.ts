/** 本文件负责新版后台统一访问云端和本地程序接口。 */
"use client";

import { TOKEN_KEY } from "./api";

export const CLOUD_API_BASE = (process.env.NEXT_PUBLIC_CLOUD_API_BASE || "https://goodhr5.58it.cn").replace(/\/$/, "");
export const LOCAL_AGENT_PORTS = Array.from({ length: 9 }, (_, index) => 55271 + index);

type RequestOptions = Omit<RequestInit, "body"> & { body?: unknown; auth?: boolean };

/** getToken 返回浏览器缓存的登录凭证。 */
export function getToken() {
  return typeof window === "undefined" ? "" : localStorage.getItem(TOKEN_KEY) || "";
}

/** cloudRequest 统一请求云端接口并处理鉴权与错误。 */
export async function cloudRequest(path: string, options: RequestOptions = {}) {
  const { body, auth = true, headers, ...rest } = options;
  let response: Response;
  try {
    response = await fetch(`${CLOUD_API_BASE}${path}`, {
      ...rest,
      cache: "no-store",
      headers: { "Content-Type": "application/json", ...(auth && getToken() ? { Authorization: `Bearer ${getToken()}` } : {}), ...(headers || {}) },
      body: body == null ? undefined : typeof body === "string" ? body : JSON.stringify(body),
    });
  } catch {
    throw new Error("无法连接云端服务，请检查网络后重试");
  }
  return parseResponse(response, "云端请求失败");
}

/** localRequest 统一请求本地程序接口。 */
export async function localRequest(baseURL: string, path: string, options: RequestOptions = {}) {
  const { body, headers, ...rest } = options;
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 45000);
  try {
    const response = await fetch(`${baseURL.replace(/\/$/, "")}${path}`, {
      ...rest,
      signal: controller.signal,
      cache: "no-store",
      headers: { "Content-Type": "application/json", ...(headers || {}) },
      body: body == null ? undefined : typeof body === "string" ? body : JSON.stringify(body),
    });
    const data = await parseResponse(response, "本地程序请求失败");
    return data?.data ?? data;
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") throw new Error("本地程序请求超时，请稍后重试");
    throw error instanceof Error ? error : new Error("无法连接本地程序");
  } finally {
    window.clearTimeout(timeout);
  }
}

/** detectLocalAgent 探测 55271 至 55279 的本地程序端口。 */
export async function detectLocalAgent() {
  for (const port of LOCAL_AGENT_PORTS) {
    const baseURL = `http://127.0.0.1:${port}`;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 450);
    try {
      const response = await fetch(`${baseURL}/health`, { cache: "no-store", signal: controller.signal });
      if (response.ok) return baseURL;
    } catch {
      // 当前端口不可用时继续检查下一个端口。
    } finally {
      window.clearTimeout(timeout);
    }
  }
  return "";
}

/** parseResponse 解析统一 JSON 响应并输出中文错误。 */
async function parseResponse(response: Response, fallback: string) {
  const text = await response.text();
  let data: any = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    throw new Error("接口返回的数据格式不正确");
  }
  const code = Number(data.code || (response.ok && data.ok !== false ? 200 : response.status));
  if (!response.ok || data.ok === false || (data.code != null && code !== 200)) {
    if (response.status === 401) localStorage.removeItem(TOKEN_KEY);
    throw new Error(String(data.msg || data.error || data.detail || fallback));
  }
  return data;
}

/** formatDate 将接口日期转换为当前电脑的本地时间。 */
export function formatDate(value: unknown) {
  if (!value) return "--";
  const date = new Date(String(value));
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN");
}
