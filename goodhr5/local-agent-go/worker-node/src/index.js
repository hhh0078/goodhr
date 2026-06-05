// 本文件负责提供 GoodHR 5 Node Browser Worker HTTP 服务。
import fs from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";

const addr = process.env.GOODHR_WORKER_ADDR || "127.0.0.1:9101";
const [host, rawPort] = addr.split(":");
const port = Number(rawPort || 9101);

let browser = null;
let context = null;
let page = null;
const downloads = [];

/**
 * 返回浏览器下载目录。
 * @returns {string} 下载目录。
 */
function downloadDir() {
  return process.env.GOODHR_DOWNLOAD_DIR || path.join(os.homedir(), "Downloads");
}

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
    downloadsPath: payload.downloads_path || downloadDir(),
  };
  await fs.mkdir(options.downloadsPath, { recursive: true });
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
    registerPage(page);
    return { running: true, persistent: true };
  }
  if (!launch) throw new Error("CloakBrowser Node SDK 缺少启动方法");
  browser = await launch(options);
  context = await browser.newContext?.({ acceptDownloads: true }) || null;
  page = context ? await context.newPage() : await browser.newPage();
  registerPage(page);
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
    registerPage(page);
    return page;
  }
  if (browser) {
    page = await browser.newPage();
    registerPage(page);
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
  const selector = firstSelector(payload);
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
  const selector = firstSelector(payload);
  const text = String(payload.text || "");
  if (!selector) throw new Error("输入选择器不能为空");
  const currentPage = await ensurePage();
  await currentPage.locator(selector).first().fill(text, { timeout: Number(payload.timeout || 10000) });
  return { typed: true };
}

/**
 * 滚动当前页面或指定元素。
 * @param {Record<string, any>} payload - 滚动参数。
 * @returns {Promise<Record<string, any>>} 滚动结果。
 */
async function scrollPage(payload) {
  const currentPage = await ensurePage();
  const distance = Number(payload.distance || payload.y || 720);
  const selector = firstSelector(payload);
  if (selector) {
    const locator = currentPage.locator(selector).first();
    if (await locator.count() > 0) {
      await locator.evaluate((el, y) => el.scrollBy(0, y), distance);
      return { scrolled: true, selector, distance };
    }
  }
  await currentPage.mouse.wheel(0, distance);
  return { scrolled: true, distance };
}

/**
 * 提取当前页面或指定元素文本。
 * @param {Record<string, any>} payload - 文本提取参数。
 * @returns {Promise<Record<string, any>>} 文本结果。
 */
async function extractText(payload) {
  const currentPage = await ensurePage();
  const selector = firstSelector(payload);
  if (!selector) {
    const text = await currentPage.locator("body").innerText({ timeout: Number(payload.timeout || 10000) });
    return { text };
  }
  const locator = currentPage.locator(selector).first();
  if (await locator.count() <= 0) return { text: "" };
  const text = await locator.innerText({ timeout: Number(payload.timeout || 10000) });
  return { text, selector };
}

/**
 * 截取当前页面或指定元素。
 * @param {Record<string, any>} payload - 截图参数。
 * @returns {Promise<Record<string, any>>} 截图结果。
 */
async function screenshotPage(payload) {
  const currentPage = await ensurePage();
  const filename = safeFilename(String(payload.filename || "page-screenshot.png"));
  const directory = String(payload.dir || payload.directory || path.join(os.tmpdir(), "goodhr-screenshots"));
  await fs.mkdir(directory, { recursive: true });
  const targetPath = path.join(directory, filename);
  const selector = firstSelector(payload);
  if (selector) {
    const locator = currentPage.locator(selector).first();
    if (await locator.count() <= 0) throw new Error("截图元素不存在");
    await locator.screenshot({ path: targetPath, type: "png" });
  } else {
    await currentPage.screenshot({ path: targetPath, fullPage: Boolean(payload.full_page), type: "png" });
  }
  const stat = await fs.stat(targetPath);
  return { path: targetPath, size: stat.size };
}

/**
 * 导出当前浏览器 Cookie。
 * @returns {Promise<Record<string, any>>} Cookie 结果。
 */
async function exportCookies() {
  if (!context) throw new Error("浏览器未启动，无法导出 Cookie");
  const cookies = await context.cookies();
  return { cookies, count: cookies.length };
}

/**
 * 导入 Cookie 到当前浏览器上下文。
 * @param {Record<string, any>} payload - Cookie 参数。
 * @returns {Promise<Record<string, any>>} 导入结果。
 */
async function importCookies(payload) {
  if (!context) throw new Error("浏览器未启动，无法导入 Cookie");
  const cookies = Array.isArray(payload.cookies) ? payload.cookies : [];
  if (cookies.length > 0) await context.addCookies(cookies);
  return { count: cookies.length };
}

/**
 * 注册页面下载事件。
 * @param {any} targetPage - Playwright 页面对象。
 * @returns {void} 无返回值。
 */
function registerPage(targetPage) {
  if (!targetPage || targetPage.__goodhrDownloadRegistered) return;
  targetPage.__goodhrDownloadRegistered = true;
  targetPage.on("download", async (download) => {
    try {
      const directory = downloadDir();
      await fs.mkdir(directory, { recursive: true });
      const suggested = safeFilename(download.suggestedFilename?.() || "download");
      const targetPath = await uniquePath(directory, suggested);
      await download.saveAs(targetPath);
      downloads.unshift({
        path: targetPath,
        filename: path.basename(targetPath),
        suggested_filename: suggested,
        url: download.url?.() || "",
        created_at: new Date().toISOString(),
      });
      if (downloads.length > 100) downloads.length = 100;
    } catch (error) {
      console.error("保存下载文件失败", error);
    }
  });
}

/**
 * 返回下载记录。
 * @returns {Record<string, any>} 下载记录。
 */
function listDownloads() {
  return { downloads, count: downloads.length, directory: downloadDir() };
}

/**
 * 从请求参数中读取第一个 CSS 选择器。
 * @param {Record<string, any>} payload - 请求参数。
 * @returns {string} CSS 选择器。
 */
function firstSelector(payload) {
  const selectors = selectorList(payload.selector || payload.css || payload.element || payload.elements);
  return selectors[0] || "";
}

/**
 * 将选择器配置转换为列表。
 * @param {any} value - 选择器配置。
 * @returns {string[]} CSS 选择器列表。
 */
function selectorList(value) {
  if (!value) return [];
  if (typeof value === "string") return value.trim() ? [value.trim()] : [];
  if (Array.isArray(value)) return value.flatMap(selectorList);
  if (typeof value === "object") {
    return ["target_classes", "selectors", "selector", "css"].flatMap((key) => selectorList(value[key]));
  }
  return [];
}

/**
 * 清理文件名中的危险字符。
 * @param {string} name - 原始文件名。
 * @returns {string} 安全文件名。
 */
function safeFilename(name) {
  const cleaned = path.basename(name || "download").replace(/[<>:"/\\|?*\x00-\x1F]/g, "_").trim();
  return cleaned || "download";
}

/**
 * 返回不重复的文件路径。
 * @param {string} directory - 目录。
 * @param {string} filename - 文件名。
 * @returns {Promise<string>} 文件路径。
 */
async function uniquePath(directory, filename) {
  const parsed = path.parse(filename);
  for (let index = 0; index < 1000; index += 1) {
    const suffix = index === 0 ? "" : `-${index}`;
    const candidate = path.join(directory, `${parsed.name || "download"}${suffix}${parsed.ext}`);
    try {
      await fs.access(candidate);
    } catch {
      return candidate;
    }
  }
  return path.join(directory, `${Date.now()}-${filename}`);
}

const routes = {
  "/api/v1/browser/start": startBrowser,
  "/api/v1/browser/stop": stopBrowser,
  "/api/v1/page/open": openPage,
  "/api/v1/page/click": clickPage,
  "/api/v1/page/type": typePage,
  "/api/v1/page/scroll": scrollPage,
  "/api/v1/page/extract-text": extractText,
  "/api/v1/page/screenshot": screenshotPage,
  "/api/v1/page/cookies": importCookies,
};

const server = http.createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/health") {
    success(res, { status: "ok", worker: "node" });
    return;
  }
  if (req.method === "GET" && req.url === "/api/v1/page/cookies") {
    try {
      success(res, await exportCookies());
    } catch (error) {
      failure(res, 500, error?.message || "导出 Cookie 失败");
    }
    return;
  }
  if (req.method === "GET" && req.url === "/api/v1/downloads") {
    success(res, listDownloads());
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
