// 本文件负责提供 GoodHR 5 Node Browser Worker HTTP 服务。
import http from "node:http";

const addr = process.env.GOODHR_WORKER_ADDR || "127.0.0.1:9101";
const [host, rawPort] = addr.split(":");
const port = Number(rawPort || 9101);

let browser = null;
let context = null;
let page = null;

/**
 * 读取 HTTP 请求 JSON。
 * @param {import("node:http").IncomingMessage} req - HTTP 请求对象。
 * @returns {Promise<Record<string, any>>} 请求 JSON。
 */
async function readJSON(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const text = Buffer.concat(chunks).toString("utf8").trim();
  if (!text) return {};
  return JSON.parse(text);
}

/**
 * 写入统一 JSON 响应。
 * @param {import("node:http").ServerResponse} res - HTTP 响应对象。
 * @param {number} status - HTTP 状态码。
 * @param {Record<string, any>} body - 响应内容。
 * @returns {void} 无返回值。
 */
function writeJSON(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json; charset=utf-8" });
  res.end(JSON.stringify(body));
}

/**
 * 写入成功响应。
 * @param {import("node:http").ServerResponse} res - HTTP 响应对象。
 * @param {Record<string, any>} data - 业务数据。
 * @returns {void} 无返回值。
 */
function success(res, data = {}) {
  writeJSON(res, 200, { ok: true, code: 200, msg: "成功", data });
}

/**
 * 写入失败响应。
 * @param {import("node:http").ServerResponse} res - HTTP 响应对象。
 * @param {number} status - HTTP 状态码。
 * @param {string} msg - 中文错误信息。
 * @returns {void} 无返回值。
 */
function failure(res, status, msg) {
  writeJSON(res, status, { ok: false, code: status, msg: msg || "请求失败" });
}

/**
 * 启动 CloakBrowser。
 * @param {Record<string, any>} payload - 浏览器启动参数。
 * @returns {Promise<Record<string, any>>} 启动结果。
 */
async function startBrowser(payload) {
  if (browser || context) return { running: true };
  const cloak = await import("cloakbrowser");
  const launchPersistent = cloak.launchPersistentContext;
  const launch = cloak.launch;
  const userDataDir = String(payload.user_data_dir || "").trim();
  const options = {
    headless: Boolean(payload.headless),
    humanize: payload.humanize !== false,
    acceptDownloads: true,
    downloadsPath: payload.downloads_path,
  };
  if (payload.proxy) options.proxy = payload.proxy;
  if (payload.viewport_width && payload.viewport_height) {
    options.viewport = { width: Number(payload.viewport_width), height: Number(payload.viewport_height) };
  }
  if (payload.timezone) options.timezone = String(payload.timezone);
  if (payload.locale) options.locale = String(payload.locale);
  if (payload.user_agent) options.userAgent = String(payload.user_agent);
  if (userDataDir && launchPersistent) {
    context = await launchPersistent({ ...options, userDataDir });
    page = context.pages?.()[0] || await context.newPage();
    return { running: true, persistent: true };
  }
  if (!launch) throw new Error("CloakBrowser Node SDK 缺少启动方法");
  browser = await launch(options);
  context = await browser.newContext?.({ acceptDownloads: true }) || null;
  page = context ? await context.newPage() : await browser.newPage();
  return { running: true, persistent: false };
}

/**
 * 停止 CloakBrowser。
 * @returns {Promise<Record<string, any>>} 停止结果。
 */
async function stopBrowser() {
  if (context) await context.close().catch(() => {});
  if (browser) await browser.close().catch(() => {});
  context = null;
  browser = null;
  page = null;
  return { running: false };
}

/**
 * 确保当前页面存在。
 * @returns {Promise<any>} Playwright 页面对象。
 */
async function ensurePage() {
  if (page) return page;
  if (context) {
    page = await context.newPage();
    return page;
  }
  if (browser) {
    page = await browser.newPage();
    return page;
  }
  throw new Error("浏览器未启动，请先启动浏览器");
}

/**
 * 打开页面地址。
 * @param {Record<string, any>} payload - 打开页面参数。
 * @returns {Promise<Record<string, any>>} 页面结果。
 */
async function openPage(payload) {
  const target = String(payload.url || "").trim();
  if (!target) throw new Error("页面地址不能为空");
  const currentPage = await ensurePage();
  await currentPage.goto(target, { waitUntil: "domcontentloaded", timeout: Number(payload.timeout || 60000) });
  return { url: currentPage.url() };
}

/**
 * 点击页面元素。
 * @param {Record<string, any>} payload - 点击参数。
 * @returns {Promise<Record<string, any>>} 点击结果。
 */
async function clickPage(payload) {
  const selector = String(payload.selector || payload.css || "").trim();
  if (!selector) throw new Error("点击选择器不能为空");
  const currentPage = await ensurePage();
  await currentPage.locator(selector).first().click({ timeout: Number(payload.timeout || 10000) });
  return { clicked: true };
}

/**
 * 输入页面文本。
 * @param {Record<string, any>} payload - 输入参数。
 * @returns {Promise<Record<string, any>>} 输入结果。
 */
async function typePage(payload) {
  const selector = String(payload.selector || payload.css || "").trim();
  const text = String(payload.text || "");
  if (!selector) throw new Error("输入选择器不能为空");
  const currentPage = await ensurePage();
  await currentPage.locator(selector).first().fill(text, { timeout: Number(payload.timeout || 10000) });
  return { typed: true };
}

const routes = {
  "/api/v1/browser/start": startBrowser,
  "/api/v1/browser/stop": stopBrowser,
  "/api/v1/page/open": openPage,
  "/api/v1/page/click": clickPage,
  "/api/v1/page/type": typePage,
};

const server = http.createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/health") {
    success(res, { status: "ok", worker: "node" });
    return;
  }
  if (req.method !== "POST") {
    failure(res, 405, "请求方法不支持");
    return;
  }
  const handler = routes[req.url || ""];
  if (!handler) {
    failure(res, 404, "接口不存在");
    return;
  }
  try {
    const payload = await readJSON(req);
    const data = await handler(payload);
    success(res, data);
  } catch (error) {
    failure(res, 500, error?.message || "浏览器操作失败");
  }
});

server.listen(port, host, () => {
  console.log(`GoodHR Browser Worker started on http://${host}:${port}`);
});

process.on("SIGINT", async () => {
  await stopBrowser();
  process.exit(0);
});
