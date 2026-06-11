// GoodHR 5 本地 Agent API 封装
import { alertError } from "./notify";
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
  let res: Response;
  try {
    res = await fetch(agentURL(base, path), {
      headers: {
        "Content-Type": "application/json",
        ...(opts.headers as Record<string, string> | undefined),
      },
      ...rest,
      body: serializeBody(body),
    });
  } catch {
    const msg = "无法连接本地程序，请确认本地程序已经启动";
    throw showLocalAgentMessage(msg);
  }
  const data = await parseLocalAgentJSON(res);
  // console.info('[goodhr5][local-agent][response]', { base, path, status: res.status, data })
  const code = Number(data.code || (res.ok && data.ok !== false ? 200 : res.status || 500));
  if (!res.ok || data.ok === false || code !== 200) {
    const msg = String(data.msg || data.error || data.detail || "本地程序请求失败");
    throw showLocalAgentMessage(msg);
  }
  if (data && typeof data === "object" && "data" in data) {
    return data.data || {};
  }
  return data;
}

/**
 * 解析 Local Agent JSON 响应。
 * @param {Response} res - fetch 响应对象。
 * @returns {Promise<any>} JSON 数据。
 */
async function parseLocalAgentJSON(res: Response) {
  const text = await res.text();
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    const msg = "本地程序返回的数据格式不正确";
    throw showLocalAgentMessage(msg);
  }
}

/**
 * 弹框展示 Local Agent 返回的消息。
 * @param {string} msg - 本地程序返回的中文提示。
 * @returns {Error} 返回已标记提醒状态的错误对象。
 */
function showLocalAgentMessage(msg: string) {
  const error = new Error(msg || "本地程序请求失败") as Error & {
    notified?: boolean;
  };
  void alertError(msg || "本地程序请求失败");
  error.notified = true;
  return error;
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
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), 1000);
  try {
    const res = await fetch(agentURL(base, "/health"), {
      cache: "no-store",
      signal: controller.signal,
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "Local Agent 不可用");
    return data?.data || data;
  } finally {
    window.clearTimeout(timer);
  }
}

/**
 * 读取 Local Agent 本地诊断信息。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回诊断信息。
 */
export async function getLocalDiagnostics(base: string) {
  return req(base, "/api/v1/diagnostics");
}

/**
 * 读取 Local Agent 控制台前端包状态。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回控制台前端包状态。
 */
export async function getLocalConsoleStatus(base: string) {
  const data = await req(base, "/api/v1/console/status");
  return data.console || {};
}

/**
 * 读取 Local Agent 运行组件安装状态。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回运行组件状态。
 */
export async function getLocalRuntimeStatus(base: string) {
  return req(base, "/api/v1/runtime/status");
}

/**
 * 触发 Local Agent 更新运行组件。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 可选 manifest_url。
 * @returns {Promise<any>} 返回更新结果。
 */
export async function installLocalRuntime(base: string, payload: any = {}) {
  return req(base, "/api/v1/runtime/install", { method: "POST", body: payload });
}

/**
 * 触发 Local Agent 更新控制台前端包。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 可选 manifest_url。
 * @returns {Promise<any>} 返回更新结果。
 */
export async function updateLocalConsolePackage(base: string, payload: any = {}) {
  return req(base, "/api/v1/console/update", { method: "POST", body: payload });
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

/**
 * 读取本地 SQLite 任务列表。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any[]>} 返回任务数组。
 */
export async function listLocalTasks(base: string) {
  const data = await req(base, "/api/v1/local/tasks");
  return data.tasks || [];
}

/**
 * 创建本地 SQLite 任务。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 任务创建参数。
 * @returns {Promise<any>} 返回新建任务。
 */
export async function createLocalTask(base: string, payload: any) {
  const data = await req(base, "/api/v1/local/tasks", { method: "POST", body: payload });
  return data.task;
}

/**
 * 更新本地 SQLite 任务。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @param {any} payload - 任务更新参数。
 * @returns {Promise<any>} 返回更新后的任务。
 */
export async function updateLocalTask(base: string, taskID: string, payload: any) {
  const data = await req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}`, { method: "PUT", body: payload });
  return data.task;
}

/**
 * 删除本地 SQLite 任务。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回删除结果。
 */
export async function deleteLocalTask(base: string, taskID: string) {
  return req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}`, { method: "DELETE" });
}

/**
 * 更新本地任务状态。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @param {string} status - 新状态。
 * @returns {Promise<any>} 返回更新后的任务。
 */
export async function updateLocalTaskStatus(base: string, taskID: string, status: string) {
  const data = await req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/status`, {
    method: "POST",
    body: { status },
  });
  return data.task;
}

/**
 * 查询本地任务运行状态和进度。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回任务、running、progress 和最近日志。
 */
export async function getLocalTaskStatus(base: string, taskID: string) {
  return req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/status`);
}

/**
 * 启动本地 SQLite 任务运行器。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @param {any} payload - 本地任务启动参数。
 * @returns {Promise<any>} 返回启动结果。
 */
export async function runLocalTask(base: string, taskID: string, payload: any = {}) {
  return req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/run`, {
    method: "POST",
    body: payload,
  });
}

/**
 * 停止本地 SQLite 任务运行器。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回停止结果。
 */
export async function stopLocalTask(base: string, taskID: string, payload: Record<string, any> = {}) {
  return req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/stop`, { method: "POST", body: payload });
}

/**
 * 读取本地任务日志。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @param {{ limit?: number }} params - 日志参数。
 * @returns {Promise<any>} 返回日志列表和分页状态。
 */
export async function listLocalTaskLogs(base: string, taskID: string, params: { limit?: number } = {}) {
  const query = params.limit ? `?limit=${encodeURIComponent(String(params.limit))}` : "";
  const data = await req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/logs${query}`);
  return { logs: data.logs || [], has_more: false };
}

/**
 * 清空本地任务日志。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回清空结果。
 */
export async function clearLocalTaskLogs(base: string, taskID: string) {
  return req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/logs`, { method: "DELETE" });
}

/**
 * 写入本地任务日志。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @param {any} payload - 日志参数。
 * @returns {Promise<any>} 返回日志记录。
 */
export async function addLocalTaskLog(base: string, taskID: string, payload: any) {
  const data = await req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/logs`, {
    method: "POST",
    body: payload,
  });
  return data.log;
}

/**
 * 读取 Local Agent 本地 OCR 组件状态。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回 OCR 状态。
 */
export async function getLocalOCRStatus(base: string) {
  const data = await req(base, "/api/v1/local/ocr/status");
  return data.ocr || {};
}

/**
 * 通过 Local Agent 本地 OCR 识别图片文字。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 图片识别参数，支持 file_path、path 或 screenshot_path。
 * @returns {Promise<any>} 返回 OCR 文本和原始结果。
 */
export async function recognizeImageWithLocalOCR(base: string, payload: any) {
  return req(base, "/api/v1/local/ocr/recognize", {
    method: "POST",
    body: payload,
  });
}

/**
 * 读取 Local Agent 规则包状态。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回规则包状态。
 */
export async function getLocalRulesStatus(base: string) {
  return req(base, "/api/v1/local/rules/status");
}

/**
 * 触发 Local Agent 更新规则包。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 可选 manifest_url。
 * @returns {Promise<any>} 返回更新结果。
 */
export async function updateLocalRules(base: string, payload: any = {}) {
  return req(base, "/api/v1/local/rules/update", { method: "POST", body: payload });
}

/**
 * 读取 Local Agent 本地下载记录。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 可选任务 ID。
 * @returns {Promise<any[]>} 返回下载记录列表。
 */
export async function listLocalDownloads(base: string, taskID = "") {
  const query = taskID ? `?task_id=${encodeURIComponent(taskID)}` : "";
  const data = await req(base, `/api/v1/local/downloads${query}`);
  return data.downloads || [];
}

/**
 * 保存 Local Agent 本地下载记录。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 下载记录参数。
 * @returns {Promise<any>} 返回保存后的下载记录。
 */
export async function saveLocalDownload(base: string, payload: any) {
  const data = await req(base, "/api/v1/local/downloads", { method: "POST", body: payload });
  return data.download || {};
}

/**
 * 读取 Local Agent 本地截图记录。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 可选任务 ID。
 * @returns {Promise<any[]>} 返回截图记录列表。
 */
export async function listLocalScreenshotRecords(base: string, taskID = "") {
  const query = taskID ? `?task_id=${encodeURIComponent(taskID)}` : "";
  const data = await req(base, `/api/v1/local/screenshots${query}`);
  return data.screenshots || [];
}

/**
 * 保存 Local Agent 本地截图记录。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 截图记录参数。
 * @returns {Promise<any>} 返回保存后的截图记录。
 */
export async function saveLocalScreenshotRecord(base: string, payload: any) {
  const data = await req(base, "/api/v1/local/screenshots", { method: "POST", body: payload });
  return data.screenshot || {};
}

export async function listLocalScreenshots(base: string, taskID: string) {
  const data = await req(
    base,
    `/api/v1/tasks/${encodeURIComponent(taskID)}/screenshots`,
  );
  return data.screenshots;
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

/**
 * 提取 Boss 候选人详情文本。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 详情提取参数，包含 card_index 和 platform_config。
 * @returns {Promise<any>} 返回详情文本和可选截图。
 */
export async function extractBossCandidateDetail(base: string, payload: any) {
  return req(base, "/api/v1/boss/candidates/detail", {
    method: "POST",
    body: payload,
  });
}
