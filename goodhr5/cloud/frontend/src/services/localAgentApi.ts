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
    showLocalAgentMessage(msg);
    throw new Error(msg);
  }
  const data = await parseLocalAgentJSON(res);
  // console.info('[goodhr5][local-agent][response]', { base, path, status: res.status, data })
  const code = Number(data.code || (res.ok && data.ok !== false ? 200 : res.status || 500));
  if (!res.ok || data.ok === false || code !== 200) {
    const msg = String(data.msg || data.error || data.detail || "本地程序请求失败");
    showLocalAgentMessage(msg);
    throw new Error(msg);
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
    showLocalAgentMessage(msg);
    throw new Error(msg);
  }
}

/**
 * 弹框展示 Local Agent 返回的消息。
 * @param {string} msg - 本地程序返回的中文提示。
 * @returns {void} 无返回值。
 */
function showLocalAgentMessage(msg: string) {
  if (typeof window === "undefined") return;
  window.alert(msg || "本地程序请求失败");
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
export async function stopLocalTask(base: string, taskID: string) {
  return req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/stop`, { method: "POST" });
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
 * 读取 Local Agent 本地明文 AI 配置。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回 AI 配置。
 */
export async function getLocalAIConfig(base: string) {
  const data = await req(base, "/api/v1/local/ai/config");
  return data.config || {};
}

/**
 * 保存 Local Agent 本地明文 AI 配置。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - AI 配置参数。
 * @returns {Promise<any>} 返回保存后的 AI 配置。
 */
export async function saveLocalAIConfig(base: string, payload: any) {
  const data = await req(base, "/api/v1/local/ai/config", { method: "POST", body: payload });
  return data.config || {};
}

/**
 * 通过 Local Agent 统一调用本地 AI 聊天接口。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - OpenAI 兼容聊天参数。
 * @returns {Promise<any>} 返回 AI 调用结果。
 */
export async function chatWithLocalAI(base: string, payload: any) {
  return req(base, "/api/v1/local/ai/chat", { method: "POST", body: payload });
}

/**
 * 通过 Local Agent 本地 AI 识别图片内容。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 图片识别参数，支持 file_path 或 image_base64。
 * @returns {Promise<any>} 返回识别文本和用量。
 */
export async function analyzeImageWithLocalAI(base: string, payload: any) {
  return req(base, "/api/v1/local/ai/vision", {
    method: "POST",
    body: payload,
  });
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
 * 读取 Local Agent 本地岗位模板列表。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any[]>} 返回本地岗位模板数组。
 */
export async function listLocalPositions(base: string) {
  const data = await req(base, "/api/v1/local/positions");
  return data.positions || [];
}

/**
 * 保存 Local Agent 本地岗位模板。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 岗位模板参数。
 * @returns {Promise<any>} 返回保存后的岗位模板。
 */
export async function saveLocalPosition(base: string, payload: any) {
  const data = await req(base, "/api/v1/local/positions", { method: "POST", body: payload });
  return data.position || {};
}

/**
 * 删除 Local Agent 本地岗位模板。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} positionID - 岗位模板 ID。
 * @returns {Promise<any>} 返回删除结果。
 */
export async function deleteLocalPosition(base: string, positionID: string) {
  return req(base, `/api/v1/local/positions/${encodeURIComponent(positionID)}`, { method: "DELETE" });
}

/**
 * 读取 Local Agent 本地岗位默认提示词。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回默认提示词。
 */
export async function getLocalPositionDefaultPrompts(base: string) {
  const data = await req(base, "/api/v1/local/positions/default-prompts");
  return data.prompts || {};
}

/**
 * 使用 Local Agent 本地 AI 优化岗位要求。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} text - 原始岗位要求。
 * @returns {Promise<string>} 返回优化后的岗位要求。
 */
export async function optimizeLocalPositionRequirement(base: string, text: string) {
  const data = await req(base, "/api/v1/local/positions/optimize-requirement", {
    method: "POST",
    body: { text },
  });
  return String(data.optimized || "");
}

/**
 * 读取 Local Agent 本地设置。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<any>} 返回本地设置。
 */
export async function getLocalSettings(base: string) {
  const data = await req(base, "/api/v1/local/settings");
  return data.settings || {};
}

/**
 * 保存 Local Agent 本地设置。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - 设置参数。
 * @returns {Promise<any>} 返回保存后的本地设置。
 */
export async function saveLocalSettings(base: string, payload: any) {
  const data = await req(base, "/api/v1/local/settings", { method: "POST", body: payload });
  return data.settings || {};
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

/**
 * 读取 SQLite 本地任务候选人。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<any>} 返回候选人列表包装对象。
 */
export async function listLocalTaskCandidates(base: string, taskID: string) {
  const data = await req(base, `/api/v1/local/tasks/${encodeURIComponent(taskID)}/candidates`);
  return { items: data.candidates || [] };
}

/**
 * 读取 SQLite 本地候选人分页列表。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {{ taskId?: string; positionId?: string; keyword?: string; page?: number; pageSize?: number }} params - 查询和分页参数。
 * @returns {Promise<any>} 返回候选人分页对象。
 */
export async function listLocalCandidatesPaged(
  base: string,
  params: { taskId?: string; positionId?: string; keyword?: string; page?: number; pageSize?: number } = {},
) {
  const query = new URLSearchParams();
  if (params.taskId) query.set("task_id", params.taskId);
  if (params.positionId) query.set("position_id", params.positionId);
  if (params.keyword) query.set("keyword", params.keyword);
  if (params.page) query.set("page", String(params.page));
  if (params.pageSize) query.set("page_size", String(params.pageSize));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  const data = await req(base, `/api/v1/local/candidates${suffix}`);
  return {
    items: data.candidates || [],
    total: Number(data.total || 0),
    page: Number(data.page || params.page || 1),
    pageSize: Number(data.page_size || params.pageSize || 20),
  };
}

/**
 * 读取 SQLite 本地候选人详情。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} candidateID - 候选人 ID。
 * @param {string} taskID - 可选任务 ID，用于定位同名候选人。
 * @returns {Promise<any>} 返回候选人详情。
 */
export async function getLocalCandidate(base: string, candidateID: string, taskID = "") {
  const query = taskID ? `?task_id=${encodeURIComponent(taskID)}` : "";
  const data = await req(base, `/api/v1/local/candidates/${encodeURIComponent(candidateID)}${query}`);
  return data.candidate || null;
}

/**
 * 清空 SQLite 本地候选人数据。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @returns {Promise<number>} 返回删除数量。
 */
export async function clearLocalCandidates(base: string) {
  const data = await req(base, "/api/v1/local/candidates", { method: "DELETE" });
  return Number(data.deleted || 0);
}

/**
 * 删除 SQLite 本地任务候选人。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} taskID - 任务 ID。
 * @param {string} candidateID - 候选人 ID。
 * @returns {Promise<any>} 返回删除结果。
 */
export async function deleteLocalTaskCandidate(base: string, taskID: string, candidateID: string) {
  return req(
    base,
    `/api/v1/local/tasks/${encodeURIComponent(taskID)}/candidates/${encodeURIComponent(candidateID)}`,
    { method: "DELETE" },
  );
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

/**
 * 读取本地浏览器账号 profile 列表。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} platformID - 可选平台 ID。
 * @returns {Promise<any[]>} 返回本地 profile 列表。
 */
export async function listLocalProfiles(base: string, platformID = "") {
  const query = platformID
    ? `?platform_id=${encodeURIComponent(platformID)}`
    : "";
  const data = await req(base, `/api/v1/profiles${query}`);
  return data.profiles;
}

/**
 * 创建本地浏览器账号 profile。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {any} payload - profile 创建参数。
 * @returns {Promise<any>} 返回新建 profile。
 */
export async function createLocalProfile(base: string, payload: any) {
  const data = await req(base, "/api/v1/profiles", {
    method: "POST",
    body: payload,
  });
  return data.profile;
}

/**
 * 更新本地浏览器账号 profile。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} profileID - profile ID。
 * @param {any} payload - profile 更新参数。
 * @returns {Promise<any>} 返回更新后的 profile。
 */
export async function updateLocalProfile(
  base: string,
  profileID: string,
  payload: any,
) {
  const data = await req(base, `/api/v1/profiles/${encodeURIComponent(profileID)}`, {
    method: "PUT",
    body: payload,
  });
  return data.profile;
}

/**
 * 删除本地浏览器账号 profile。
 * @param {string} base - Local Agent HTTP 基础地址。
 * @param {string} profileID - profile ID。
 * @returns {Promise<void>} 无返回值。
 */
export async function deleteLocalProfile(base: string, profileID: string) {
  await req(base, `/api/v1/profiles/${encodeURIComponent(profileID)}`, {
    method: "DELETE",
  });
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
