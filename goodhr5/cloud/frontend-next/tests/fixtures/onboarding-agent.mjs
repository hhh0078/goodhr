/** 文件作用：提供新手教程 Agent 协议模拟与仅本机 Go 云端透传代理；补测试平台和版本元数据，不启动招聘浏览器、不下载更新、不发送招呼。 */
import http from "node:http";

const port = Number(process.env.ONBOARDING_AGENT_PORT || 43130);
// 容器预览只通过回环端口发布；普通本机测试继续仅监听回环地址。
const host = process.env.ONBOARDING_AGENT_HOST || "127.0.0.1";
const cloudBase = process.env.ONBOARDING_CLOUD_BASE || "http://127.0.0.1:8084";
const machineID = `goodhr-device-v1-${"e".repeat(64)}`;
if (new URL(cloudBase).hostname !== "127.0.0.1" || new URL(cloudBase).port !== "8084") throw new Error("教程模拟只允许使用本机 8084 后端");
let state = initialState();

/** initialState 创建完全独立的模拟状态，默认组件已安装且平台登录成功。 */
function initialState() {
  return { available: true, runtimeReady: true, systemReady: true, loggedIn: true, opened: false, autoResult: true, version: "6.2.10.8", targetVersion: "6.2.10.9", pageURL: "", installProgress: { running: false, stage: "idle", percent: 0 }, updateProgress: { running: false, stage: "idle", percent: 0 }, tasks: {}, token: "", calls: [], errors: [] };
}

/** json 输出兼容本地程序的 JSON，同时允许本机开发前端跨域。 */
function json(response, status, data, wrapped = true) {
  response.writeHead(status, { "content-type": "application/json; charset=utf-8", "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "GET, POST, PUT, DELETE, OPTIONS", "cache-control": "no-store" });
  response.end(JSON.stringify(wrapped && status < 400 ? { success: true, data } : data));
}

/** readBody 读取有限大小的请求内容，空内容按空对象处理。 */
async function readBody(request) {
  let value = "";
  for await (const chunk of request) { value += chunk; if (value.length > 1024 * 1024) throw new Error("测试请求过大"); }
  return value ? JSON.parse(value) : {};
}

/** cloudRequest 只把身份和岗位状态写入实际本机后端，禁止访问外部服务。 */
async function cloudRequest(path, body, token = state.token, method = "POST") {
  const response = await fetch(`${cloudBase}${path}`, { method, headers: { "content-type": "application/json", authorization: `Bearer ${token}` }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  const result = await response.json();
  if (!response.ok) { const error = new Error(result.message || result.error?.message || JSON.stringify(result)); error.status = response.status; error.payload = result; throw error; }
  return result;
}

/** runtimeStatus 返回组件安装、Windows 运行库和安装进度，不触碰实际运行组件。 */
function runtimeStatus() {
  return { version: state.version, platform: "win-x64", node_installed: state.runtimeReady, camoufox_installed: state.runtimeReady, ocr_installed: true, system_runtime: { required: true, ready: state.systemReady, name: "Microsoft Visual C++ x64 运行库", download_url: "https://example.invalid/test-only-runtime.exe" }, install_progress: state.installProgress };
}

/** applyControl 支持测试主动模拟连接、组件、登录、扫描与终态结果。 */
async function applyControl(payload) {
  if (payload.reset) state = initialState();
  for (const key of ["available", "runtimeReady", "systemReady", "loggedIn", "opened", "autoResult", "version", "targetVersion", "pageURL"]) if (Object.hasOwn(payload, key)) state[key] = payload[key];
  if (payload.positionID) {
    const task = state.tasks[payload.positionID] || createTask(payload.positionID);
    if (payload.scannedCount != null) task.scanned_count = Number(payload.scannedCount);
    if (payload.analysis === null) task.analysis = null;
    else if (payload.analysis) task.analysis = { ...payload.analysis, updated_at: new Date().toISOString() };
    if (payload.status) task.status = payload.status;
    state.tasks[payload.positionID] = task;
    if (payload.reportResult) await cloudRequest("/api/user-flow", { step: "first_screening_result", source: "local_agent", position_id: payload.positionID, metadata: { kind: task.analysis?.kind, phase: task.analysis?.phase, terminal: task.analysis?.terminal, accepted: task.analysis?.accepted } });
  }
  return publicState();
}

/** createTask 创建未产生筛选结果的任务；扫描数量不会制造完成事实。 */
function createTask(positionID) {
  return { task_id: `onboarding-${positionID}`, position_id: positionID, status: "running", current_step: "正在查看测试候选人", scanned_count: 0, greeted_count: 0, skipped_count: 0, analysis: null, started_at: new Date().toISOString() };
}

/** publicState 返回诊断状态，隐藏登录凭证。 */
function publicState() {
  const { token, ...publicValue } = state;
  return { ...publicValue, tokenBound: Boolean(token) };
}

/** proxyCloud 把业务接口透明转发到实际本机 Go，仅补齐空内存库缺少的平台和 Windows 测试下载元数据。 */
async function proxyCloud(request, response, url, payload) {
  if (url.pathname === "/api/platforms/config/") {
    return json(response, 200, { ok: true, configs: [{ config_key: "platform.boss", enabled: true, config_value: JSON.stringify({ id: "boss", open: true, auth: { pages: [{ url: "https://www.zhipin.com/web/chat/recommend", match: "prefix", entry: true }] }, public: { pages: [{ url: "https://login.zhipin.com", match: "prefix", code: "login" }] } }) }] }, false);
  }
  const upstream = await fetch(`${cloudBase}${url.pathname}${url.search}`, { method: request.method, headers: { "content-type": request.headers["content-type"] || "application/json", ...(request.headers.authorization ? { authorization: request.headers.authorization } : {}) }, ...(["GET", "HEAD"].includes(request.method) ? {} : { body: JSON.stringify(payload) }) });
  const text = await upstream.text();
  if (url.pathname === "/api/runtime/config" && upstream.ok) {
    const result = JSON.parse(text);
    result.config.local_agent = [{ version: "6.2.10.8", url_win: "https://example.invalid/test-only-agent.exe", url_mac: "https://example.invalid/test-only-agent.pkg", sha256: "a".repeat(64), note: "仅本机协议模拟的测试版本" }];
    return json(response, upstream.status, result, false);
  }
  response.writeHead(upstream.status, { "content-type": upstream.headers.get("content-type") || "application/json; charset=utf-8", "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "GET, POST, PUT, DELETE, OPTIONS", "cache-control": "no-store" });
  response.end(text);
}

/** handleRequest 实现教程使用的现有 Agent 路由和测试控制路由。 */
async function handleRequest(request, response) {
  const url = new URL(request.url || "/", `http://127.0.0.1:${port}`);
  if (request.method === "OPTIONS") return json(response, 204, {}, false);
  const payload = ["POST", "PUT"].includes(request.method) ? await readBody(request) : {};
  if (url.pathname === "/__onboarding/control") return json(response, 200, await applyControl(payload), false);
  if (url.pathname === "/__onboarding/state") return json(response, 200, publicState(), false);
  // 云端代理独立于模拟 Agent 是否在线，也不把验证码、密码或凭证写入接口调用记录。
  if (url.pathname.startsWith("/api/") && !url.pathname.startsWith("/api/v1/")) return proxyCloud(request, response, url, payload);
  if (!state.available) return json(response, 503, { error: { code: "TEST_AGENT_OFFLINE", message: "教程测试：本地程序尚未连接" } }, false);
  state.calls.push({ path: url.pathname, method: request.method, at: new Date().toISOString(), body: Object.fromEntries(Object.entries(payload).filter(([key]) => key !== "token")) });
  if (url.pathname === "/health") return json(response, 200, { version: state.version, agent_version: state.version, machine_id: machineID, local_port: port });
  if (url.pathname === "/api/v1/session/bind") {
    state.token = String(payload.token || "");
    await cloudRequest("/api/agents/bind", { machine_id: machineID, agent_version: state.version, local_port: port });
    return json(response, 200, { bound: true, machine_id: machineID });
  }
  if (url.pathname === "/api/v1/runtime/status") return json(response, 200, runtimeStatus());
  if (url.pathname === "/api/v1/runtime/install") {
    state.installProgress = { running: true, stage: "downloading", percent: 25, message: "教程测试：模拟组件下载", received: 25, total: 100 };
    setTimeout(() => { state.installProgress = { running: true, stage: "installing", percent: 75, message: "教程测试：模拟组件安装" }; }, 600);
    setTimeout(() => { state.runtimeReady = true; state.installProgress = { running: false, stage: "installed", percent: 100, message: "教程测试：组件已准备好" }; }, 1300);
    return json(response, 202, { queued: true });
  }
  if (url.pathname === "/api/v1/runtime/system-check") { state.systemReady = true; return json(response, 200, runtimeStatus()); }
  if (url.pathname === "/api/v1/app-update/status") return json(response, 200, { current_version: state.version, target_version: state.targetVersion, ...state.updateProgress });
  if (url.pathname === "/api/v1/app-update/start") {
    state.updateProgress = { running: true, stage: "downloading", percent: 30, message: "教程测试：模拟更新" };
    setTimeout(() => { state.version = state.targetVersion; state.updateProgress = { running: false, stage: "idle", percent: 100 }; }, 1200);
    return json(response, 202, { queued: true });
  }
  if (url.pathname === "/api/v1/page/open") {
    if (!state.runtimeReady || !state.systemReady) return json(response, 409, { error: { code: "RUNTIME_NOT_READY", message: "教程测试：请先安装组件" } }, false);
    state.opened = true; state.pageURL = String(payload.url || "https://www.zhipin.com/web/frame/recommend");
    return json(response, 200, { opened: true, url: state.pageURL });
  }
  if (url.pathname === "/api/v1/page/url") return json(response, 200, { url: !state.opened ? "" : state.loggedIn ? state.pageURL : "https://www.zhipin.com/web/user/" });
  const positionMatch = url.pathname.match(/^\/api\/v1\/local\/positions\/([^/]+)\/(run|stop|status|logs|logs\/upload)$/);
  if (positionMatch) {
    const positionID = decodeURIComponent(positionMatch[1]);
    const action = positionMatch[2];
    if (action === "run") {
      state.token = String(payload.token || state.token);
      await cloudRequest(`/api/positions/${encodeURIComponent(positionID)}/start`, { task_type: "greeting", machine_id: machineID });
      const task = createTask(positionID);
      state.tasks[positionID] = task;
      if (state.autoResult) {
        const runToken = state.token;
        setTimeout(() => {
          // 只在本次隔离预览任务仍有效时产生测试结果，换号或重置后不得写旧账号。
          if (state.tasks[positionID] !== task || state.token !== runToken || task.status !== "running") return;
          void cloudRequest(`/api/positions/${encodeURIComponent(positionID)}`, undefined, runToken, "GET").then((value) => {
            if (state.tasks[positionID] !== task || state.token !== runToken || task.status !== "running") return;
            const kind = value.position?.common_config?.mode_default === "keyword" ? "keyword" : "ai";
            task.scanned_count = 1;
            task.skipped_count = 1;
            task.analysis = { kind, phase: "result", terminal: true, accepted: false, score: 40, threshold: 70, candidate_name: "本地联调测试候选人", reason: "本地协议模拟：测试候选人未达到岗位要求，已跳过。", updated_at: new Date().toISOString() };
            return cloudRequest("/api/user-flow", { step: "first_screening_result", source: "local_agent_test_fixture", position_id: positionID, metadata: { kind, phase: "result", terminal: true } }, runToken);
          }).catch((error) => state.errors.push(error.message));
        }, 5000);
      }
      return json(response, 202, state.tasks[positionID]);
    }
    if (action === "stop") {
      await cloudRequest(`/api/positions/${encodeURIComponent(positionID)}/stop`, { run_greeted_count: 0 });
      if (state.tasks[positionID]) state.tasks[positionID].status = "stopped";
      return json(response, 200, { status: "stopped" });
    }
    const task = state.tasks[positionID] || { position_id: positionID, status: "stopped", analysis: null, scanned_count: 0, greeted_count: 0, skipped_count: 0 };
    if (action === "status") return json(response, 200, task);
    return json(response, 200, { task, logs: task.analysis ? [{ id: `analysis-${positionID}`, level: "info", message: "教程测试：已完成一条筛选结果", created_at: new Date().toISOString() }] : [] });
  }
  if (url.pathname.includes("/extension/")) return json(response, 200, { requests: [], pairs: [], connections: [] });
  return json(response, 200, { ok: true, tasks: [], profiles: [], accounts: [], logs: [] });
}

const server = http.createServer((request, response) => { handleRequest(request, response).catch((error) => { state.errors.push(error.message); json(response, error.status || 500, error.payload || { error: { code: "TEST_PROTOCOL_ERROR", message: error.message } }, false); }); });
server.listen(port, host, () => process.stdout.write(`教程测试协议模拟 Agent http://${host}:${port}；仅访问云端 ${cloudBase}\n`));
