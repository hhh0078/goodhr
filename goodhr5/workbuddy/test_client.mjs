#!/usr/bin/env node
/**
 * 本文件是 WorkBuddy 连接器的本地自测客户端：
 * 模拟 WorkBuddy 完成 OAuth 动态注册、PKCE 授权、令牌交换，再依次调用 5 个 MCP 工具验证链路。
 * 用法：node test_client.mjs [云端地址]   （默认 http://127.0.0.1:8084，需先在浏览器登录云端账号）
 */
import http from "node:http";
import crypto from "node:crypto";

const BASE = (process.argv[2] || "http://127.0.0.1:8084").replace(/\/$/, "");
const CALLBACK_PORT = 18765;
const CALLBACK_URI = `http://127.0.0.1:${CALLBACK_PORT}/callback`;

async function main() {
  console.log(`[1/5] 动态注册客户端 → ${BASE}/oauth/register`);
  const reg = await post("/oauth/register", {
    client_name: "workbuddy-local-test",
    redirect_uris: [CALLBACK_URI],
    grant_types: ["authorization_code", "refresh_token"],
    response_types: ["code"],
    token_endpoint_auth_method: "none",
  });
  console.log("  client_id =", reg.client_id);

  console.log("[2/5] 生成 PKCE 并等待你在浏览器完成授权");
  const verifier = crypto.randomBytes(48).toString("base64url");
  const challenge = crypto.createHash("sha256").update(verifier).digest("base64url");
  const state = crypto.randomBytes(8).toString("hex");
  const authorizeURL = `${BASE}/oauth/authorize?response_type=code&client_id=${encodeURIComponent(reg.client_id)}&redirect_uri=${encodeURIComponent(CALLBACK_URI)}&scope=mcp&state=${state}&code_challenge=${challenge}&code_challenge_method=S256`;
  console.log("\n  请用浏览器打开下面的链接（需已登录 GoodHR）并点击“同意授权”：\n");
  console.log("  " + authorizeURL + "\n");

  const code = await waitForCallback(state);
  console.log("  收到授权码:", code.slice(0, 12) + "...");

  console.log("[3/5] 用授权码换令牌");
  const token = await postForm("/oauth/token", {
    grant_type: "authorization_code",
    code,
    client_id: reg.client_id,
    redirect_uri: CALLBACK_URI,
    code_verifier: verifier,
  });
  if (!token.access_token) throw new Error("令牌交换失败: " + JSON.stringify(token));
  console.log("  access_token =", token.access_token.slice(0, 12) + "...", "expires_in =", token.expires_in);

  console.log("[4/5] 验证刷新令牌轮换");
  const refreshed = await postForm("/oauth/token", {
    grant_type: "refresh_token",
    refresh_token: token.refresh_token,
    client_id: reg.client_id,
  });
  if (!refreshed.access_token) throw new Error("刷新令牌失败: " + JSON.stringify(refreshed));
  console.log("  新 access_token =", refreshed.access_token.slice(0, 12) + "...");
  const bearer = refreshed.access_token;

  console.log("[5/5] 依次调用 MCP 工具");
  const init = await mcp(bearer, "initialize", {
    protocolVersion: "2025-03-26",
    capabilities: {},
    clientInfo: { name: "goodhr-test-client", version: "1.0.0" },
  });
  console.log("  initialize → server:", init.result?.serverInfo?.name, "protocol:", init.result?.protocolVersion);

  const tools = await mcp(bearer, "tools/list");
  console.log("  tools/list →", tools.result?.tools?.map((t) => t.name).join(", "));

  const setup = await callTool(bearer, "check_setup");
  console.log("  check_setup →", JSON.stringify(setup));

  const quota = await callTool(bearer, "get_quota");
  console.log("  get_quota → 订阅激活:", quota.subscription?.active, "岗位数:", quota.position_count);
  const positions = quota.positions || [];
  if (positions.length === 0) {
    console.log("\n账号还没有岗位，5 个工具里 create_task/get_task_status/get_results 需要真实岗位才能完整验证。");
    console.log("自测通过（OAuth + initialize + tools/list + check_setup + get_quota）。");
    return;
  }
  const positionID = positions[0].position_id;
  console.log("  选择岗位:", positions[0].name, positionID);

  const status = await callTool(bearer, "get_task_status", { position_id: positionID });
  console.log("  get_task_status →", status.status, JSON.stringify(status.counts || {}));

  const results = await callTool(bearer, "get_results", { position_id: positionID, page: 1, page_size: 5 });
  console.log("  get_results → 总数:", results.total, "本页:", (results.candidates || []).length);

  console.log("\n全部工具自测通过。create_task 会真的启动岗位任务，如需验证请手动执行：");
  console.log(`  node -e '/* 或直接在 WorkBuddy 里说“帮我跑一遍 ${positions[0].name} */'`);
}

/** post 发送 JSON 请求。 */
async function post(path, body) {
  const response = await fetch(BASE + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(`${path} 失败 ${response.status}: ` + JSON.stringify(data));
  return data;
}

/** postForm 按 OAuth 规范发送表单请求。 */
async function postForm(path, body) {
  const response = await fetch(BASE + path, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(body).toString(),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(`${path} 失败 ${response.status}: ` + JSON.stringify(data));
  return data;
}

/** waitForCallback 启动本地回调服务器，等待浏览器带授权码跳回。 */
function waitForCallback(state) {
  return new Promise((resolve, reject) => {
    const server = http.createServer((req, res) => {
      const query = new URL(req.url, "http://localhost").searchParams;
      if (query.get("error")) {
        res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        res.end("<h3>授权被拒绝了，回到终端看结果。</h3>");
        server.close();
        reject(new Error("用户拒绝授权: " + query.get("error")));
        return;
      }
      const code = query.get("code") || "";
      res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      res.end("<h3>授权成功！回到终端继续，这个页面可以关掉了。</h3>");
      server.close();
      if (query.get("state") !== state) reject(new Error("state 不一致，可能被劫持"));
      else resolve(code);
    });
    server.on("error", reject);
    server.listen(CALLBACK_PORT, () => {
      console.log(`  正在等待回调 http://127.0.0.1:${CALLBACK_PORT}/callback ...`);
    });
  });
}

/** mcp 发送一条 JSON-RPC 请求到 /mcp。 */
async function mcp(bearer, method, params) {
  const response = await fetch(BASE + "/mcp", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${bearer}` },
    body: JSON.stringify({ jsonrpc: "2.0", id: Date.now(), method, params }),
  });
  const data = await response.json().catch(() => ({}));
  if (data.error) throw new Error(`MCP ${method} 失败: ` + JSON.stringify(data.error));
  return data;
}

/** callTool 调用 MCP 工具并解出文本结果。 */
async function callTool(bearer, name, args = {}) {
  const data = await mcp(bearer, "tools/call", { name, arguments: args });
  const result = data.result || {};
  if (result.isError) throw new Error(`工具 ${name} 返回错误: ${result.content?.[0]?.text}`);
  const text = result.content?.[0]?.text || "{}";
  return JSON.parse(text);
}

main().catch((error) => {
  console.error("\n自测失败:", error.message);
  process.exitCode = 1;
});
