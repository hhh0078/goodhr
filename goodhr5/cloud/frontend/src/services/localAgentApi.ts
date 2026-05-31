// GoodHR 5 本地 Agent API 封装
export function agentURL(base: string, path: string): string {
  if (base.endsWith("/")) base = base.slice(0, -1);
  return `${base}${path}`;
}

type AgentRequestOptions = Omit<RequestInit, "body"> & {
  body?: BodyInit | Record<string, any> | null;
};

type BrowserRuntimeOptions = {
  persistent?: boolean;
  user_data_dir?: string;
  headless?: boolean;
  humanize?: boolean;
  proxy?: string;
  cookies?: any[];
};

export type StartBrowserPayload = BrowserRuntimeOptions & {
  viewport_width?: number;
  viewport_height?: number;
};

export type OpenPagePayload = BrowserRuntimeOptions & {
  url: string;
  timeout?: number;
};

async function req(base: string, path: string, opts: AgentRequestOptions = {}) {
  const { body, ...rest } = opts;
  // console.info("[goodhr5][local-agent][request]", {
  //   base,
  //   path,
  //   method: rest.method || "GET",
  //   body,
  // });
  const res = await fetch(agentURL(base, path), {
    headers: {
      "Content-Type": "application/json",
      ...(opts.headers as Record<string, string> | undefined),
    },
    ...rest,
    body: serializeBody(body),
  });
  const data = await res.json();
  // console.info('[goodhr5][local-agent][response]', { base, path, status: res.status, data })
  if (!res.ok || !data.ok)
    throw new Error(data.error || data.detail || "Local Agent 请求失败");
  return data;
}

function serializeBody(body: AgentRequestOptions["body"]) {
  if (body == null) return undefined;
  if (
    typeof body === "string" ||
    body instanceof FormData ||
    body instanceof Blob
  )
    return body;
  return JSON.stringify(body);
}

export async function getLocalHealth(base: string) {
  const res = await fetch(agentURL(base, "/health"), { cache: "no-store" });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "Local Agent 不可用");
  return data;
}

export async function bindCloudUser(base: string, payload: any) {
  return req(base, "/api/v1/session/bind-cloud-user", {
    method: "POST",
    body: payload,
  });
}

/**
 * 通知 Local Agent 主动连接云端 WebSocket。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 包含 cloud_ws_url 和 token 的参数。
 * @returns {Promise<any>} 返回 Local Agent 的 WS 状态。
 */
export async function connectCloudWS(base: string, payload: any) {
  return req(base, "/api/v1/ws/connect", { method: "POST", body: payload });
}

/**
 * 查询 Local Agent 到云端 WebSocket 的连接状态。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回 WS 连接状态。
 */
export async function getCloudWSStatus(base: string) {
  return req(base, "/api/v1/ws/status");
}

/**
 * 通过 Local Agent 建立任务级 WebSocket 并启动云端任务。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 云端任务 ID。
 * @param {any} payload - 包含 cloud_api_base、cloud_ws_url 和 token 的参数。
 * @returns {Promise<any>} 返回启动结果。
 */
export async function startTaskWS(base: string, taskID: string, payload: any) {
  return req(base, `/api/v1/tasks/${encodeURIComponent(taskID)}/start-ws`, {
    method: "POST",
    body: payload,
  });
}

/**
 * 通过 Local Agent 停止云端任务并按需断开 WebSocket。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 云端任务 ID。
 * @param {any} payload - 包含 cloud_api_base 和 token 的参数。
 * @returns {Promise<any>} 返回停止结果。
 */
export async function stopTaskWS(base: string, taskID: string, payload: any) {
  return req(base, `/api/v1/tasks/${encodeURIComponent(taskID)}/stop-ws`, {
    method: "POST",
    body: payload,
  });
}

export async function initLocalTask(base: string, payload: any) {
  return req(base, "/api/v1/tasks/init", { method: "POST", body: payload });
}

export async function listLocalCandidates(base: string, taskID: string) {
  const data = await req(
    base,
    `/api/v1/tasks/${encodeURIComponent(taskID)}/candidates`,
  );
  return data.data || data;
}

export async function deleteLocalCandidate(
  base: string,
  taskID: string,
  candidateID: string,
) {
  return req(
    base,
    `/api/v1/tasks/${encodeURIComponent(taskID)}/candidates/${encodeURIComponent(candidateID)}`,
    { method: "DELETE" },
  );
}

export async function listLocalScreenshots(base: string, taskID: string) {
  const data = await req(
    base,
    `/api/v1/tasks/${encodeURIComponent(taskID)}/screenshots`,
  );
  return data.screenshots;
}

export async function listLocalProfiles(base: string) {
  const data = await req(base, "/api/v1/profiles");
  return data.profiles;
}

/**
 * 启动本地浏览器。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {StartBrowserPayload} payload - 浏览器启动参数，可选传 user_data_dir 指定账号目录。
 * @returns {Promise<any>} 返回启动结果。
 */
export async function startBrowser(base: string, payload: StartBrowserPayload) {
  return req(base, "/api/v1/browser/start", { method: "POST", body: payload });
}

/**
 * 打开当前浏览器页面。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {OpenPagePayload} payload - 页面打开参数，可选传 user_data_dir 指定账号目录。
 * @returns {Promise<any>} 返回打开结果。
 */
export async function openPage(base: string, payload: OpenPagePayload) {
  return req(base, "/api/v1/page/open", { method: "POST", body: payload });
}

/**
 * 读取当前浏览器页面 URL。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<string>} 返回当前页面 URL。
 */
export async function currentPageURL(base: string) {
  const data = await req(base, "/api/v1/page/url");
  return data.url || "";
}

/**
 * 导出当前浏览器上下文 cookies。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any[]>} 返回 cookies 数组。
 */
export async function exportPageCookies(base: string) {
  const data = await req(base, "/api/v1/page/cookies");
  return data.cookies || [];
}
