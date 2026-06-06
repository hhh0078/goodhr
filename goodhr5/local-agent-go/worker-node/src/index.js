// 本文件负责提供 GoodHR 5 Node Browser Worker HTTP 服务。
import fs from "node:fs/promises";
import crypto from "node:crypto";
import http from "node:http";
import os from "node:os";
import path from "node:path";

const addr = process.env.GOODHR_WORKER_ADDR || "127.0.0.1:9101";
const [host, rawPort] = addr.split(":");
const port = Number(rawPort || 9101);

let browser = null;
let context = null;
let page = null;
let currentUserDataDir = "";
let currentDownloadsPath = "";
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
  const userDataDir = String(payload.user_data_dir || "").trim();
  if (browser || context) {
    if (!userDataDir || userDataDir === currentUserDataDir) {
      return { running: true, persistent: Boolean(currentUserDataDir), user_data_dir: currentUserDataDir };
    }
    await stopBrowser();
  }
  const cloak = await import("cloakbrowser");
  const launchPersistent = cloak.launchPersistentContext;
  const launch = cloak.launch;
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
    await cleanupProfileLocks(userDataDir);
    context = await launchPersistent({ ...options, userDataDir });
    currentUserDataDir = userDataDir;
    currentDownloadsPath = options.downloadsPath;
    page = context.pages?.()[0] || await context.newPage();
    registerPage(page);
    return { running: true, persistent: true, user_data_dir: userDataDir, downloads_path: options.downloadsPath };
  }
  if (!launch) throw new Error("CloakBrowser Node SDK 缺少启动方法");
  browser = await launch(options);
  context = await browser.newContext?.({ acceptDownloads: true }) || null;
  currentUserDataDir = "";
  currentDownloadsPath = options.downloadsPath;
  page = context ? await context.newPage() : await browser.newPage();
  registerPage(page);
  return { running: true, persistent: false, downloads_path: options.downloadsPath };
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
  currentUserDataDir = "";
  currentDownloadsPath = "";
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
  if (!browser && !context && (payload.user_data_dir || payload.persistent)) {
    await startBrowser(payload);
  }
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
 * 提取当前页面可见 Boss 候选人卡片。
 * @param {Record<string, any>} payload - 提取参数。
 * @returns {Promise<Record<string, any>>} 候选人列表。
 */
async function extractBossCandidates(payload) {
  const currentPage = await ensurePage();
  const platformConfig = payload.platform_config || payload.config || {};
  const rules = bossRules(platformConfig);
  const cardSelectors = selectorList(rules.candidate_card);
  if (cardSelectors.length <= 0) throw new Error("云端平台配置缺少候选人卡片选择器");
  const maxItems = Math.max(1, Math.min(100, Number(payload.max_items || 30)));
  const locator = currentPage.locator(cardSelectors.join(", "));
  const count = await locator.count();
  const candidates = [];
  for (let index = 0; index < Math.min(count, maxItems); index += 1) {
    const card = locator.nth(index);
    try {
      if (await card.isVisible().catch(() => false)) {
        const fields = await extractCardFields(card, rules);
        const rawText = candidateRawText(fields);
        candidates.push({
          id: candidateID(fields, rawText, index),
          name: fields.name || `候选人${index + 1}`,
          candidate_name: fields.name || `候选人${index + 1}`,
          status: "scanned",
          raw_text: rawText,
          filter_text: rawText,
          platform_id: "boss",
          card_index: index,
          fields,
        });
      }
    } catch {
      continue;
    }
  }
  return { candidates, count: candidates.length };
}

/**
 * 点击指定 Boss 候选人的打招呼按钮。
 * @param {Record<string, any>} payload - 打招呼参数。
 * @returns {Promise<Record<string, any>>} 点击结果。
 */
async function greetBossCandidate(payload) {
  const currentPage = await ensurePage();
  const platformConfig = payload.platform_config || payload.config || {};
  const rules = bossRules(platformConfig);
  const cardSelectors = selectorList(rules.candidate_card);
  if (cardSelectors.length <= 0) throw new Error("云端平台配置缺少候选人卡片选择器");
  const cardIndex = Math.max(0, Number(payload.card_index || 0));
  const cards = currentPage.locator(cardSelectors.join(", "));
  const count = await cards.count();
  if (cardIndex >= count) throw new Error("候选人卡片已不在当前页面");
  const card = cards.nth(cardIndex);
  if (!(await card.isVisible().catch(() => false))) throw new Error("候选人卡片当前不可见");
  await card.scrollIntoViewIfNeeded({ timeout: 1500 }).catch(() => {});
  const clicked = await clickFirstVisible(card, selectorList(rules.greet_buttons), 1500);
  if (!clicked) throw new Error("未找到可点击的打招呼按钮");
  await clickFirstVisible(currentPage, selectorList(rules.continue_buttons), 800);
  await clickFirstVisible(currentPage, selectorList(rules.confirm_buttons), 800);
  return { greeted: true, card_index: cardIndex };
}

/**
 * 点击选择器列表中第一个可见元素。
 * @param {any} scope - 页面或卡片 locator。
 * @param {string[]} selectors - CSS 选择器列表。
 * @param {number} timeout - 点击超时时间。
 * @returns {Promise<boolean>} 是否点击成功。
 */
async function clickFirstVisible(scope, selectors, timeout = 1000) {
  for (const selector of selectors) {
    try {
      const locator = scope.locator(selector).first();
      if ((await locator.count()) <= 0) continue;
      if (!(await locator.isVisible().catch(() => false))) continue;
      await locator.click({ timeout });
      return true;
    } catch {
      continue;
    }
  }
  return false;
}

/**
 * 提取单张候选人卡片字段。
 * @param {any} card - Playwright locator。
 * @param {Record<string, any>} rules - 运行选择器规则。
 * @returns {Promise<Record<string, string>>} 字段字典。
 */
async function extractCardFields(card, rules) {
  const fields = {};
  const configured = rules.fields && typeof rules.fields === "object" ? rules.fields : {};
  for (const [field, value] of Object.entries(configured)) {
    fields[field] = await firstCardText(card, selectorList(value));
  }
  if (!fields.basic_info) {
    fields.basic_info = await card.innerText({ timeout: 800 }).catch(() => "");
  }
  return fields;
}

/**
 * 返回卡片中第一个非空文本。
 * @param {any} card - Playwright locator。
 * @param {string[]} selectors - 选择器列表。
 * @returns {Promise<string>} 文本内容。
 */
async function firstCardText(card, selectors) {
  for (const selector of selectors) {
    try {
      const item = card.locator(selector).first();
      if ((await item.count()) <= 0) continue;
      const text = (await item.innerText({ timeout: 800 })).trim();
      if (text) return text;
    } catch {
      continue;
    }
  }
  return "";
}

/**
 * 拼接候选人筛选文本。
 * @param {Record<string, string>} fields - 候选人字段。
 * @returns {string} 拼接文本。
 */
function candidateRawText(fields) {
  return ["name", "basic_info", "education", "university", "description"]
    .map((key) => String(fields[key] || "").trim())
    .filter(Boolean)
    .join(" ");
}

/**
 * 生成候选人本地 ID。
 * @param {Record<string, string>} fields - 候选人字段。
 * @param {string} rawText - 候选人文本。
 * @param {number} index - 页面序号。
 * @returns {string} 候选人 ID。
 */
function candidateID(fields, rawText, index) {
  const base = [fields.name || "", rawText || "", String(index)].join("|");
  return `boss_${crypto.createHash("sha1").update(base).digest("hex").slice(0, 16)}`;
}

/**
 * 将云端平台配置转换为 Boss 运行规则。
 * @param {Record<string, any>} platformConfig - 云端平台配置。
 * @returns {Record<string, any>} 运行规则。
 */
function bossRules(platformConfig) {
  if (platformConfig?.selectors && typeof platformConfig.selectors === "object") return platformConfig.selectors;
  const card = platformConfig?.card && typeof platformConfig.card === "object" ? platformConfig.card : {};
  const actions = platformConfig?.actions && typeof platformConfig.actions === "object" ? platformConfig.actions : {};
  const detail = platformConfig?.detail && typeof platformConfig.detail === "object" ? platformConfig.detail : {};
  return {
    candidate_card: card.item || card.card,
    scroll_containers: card.scroll || card.container,
    fields: fieldRulesFromCard(card),
    greet_buttons: actions.greetBtn || actions.greet_buttons,
    continue_buttons: actions.continueBtn || actions.continue_buttons,
    confirm_buttons: actions.confirmBtn || actions.confirm_buttons,
    detail_buttons: detail.openTarget || detail.open_target,
    detail_containers: detail.content || detail.container,
    detail_close_buttons: detail.closeBtn || detail.close_buttons,
  };
}

/**
 * 从 card 配置中读取字段选择器。
 * @param {Record<string, any>} card - card 配置。
 * @returns {Record<string, any>} 字段选择器。
 */
function fieldRulesFromCard(card) {
  const result = {};
  if (Array.isArray(card.fields)) {
    for (const item of card.fields) {
      if (item && typeof item === "object") Object.assign(result, item);
    }
  } else if (card.fields && typeof card.fields === "object") {
    Object.assign(result, card.fields);
  }
  for (const [cloudKey, runtimeKey] of Object.entries({
    name: "name",
    basicInfo: "basic_info",
    basic_info: "basic_info",
    education: "education",
    university: "university",
    description: "description",
  })) {
    if (card[cloudKey] && !result[runtimeKey]) result[runtimeKey] = card[cloudKey];
  }
  return result;
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
      const directory = currentDownloadsPath || downloadDir();
      await fs.mkdir(directory, { recursive: true });
      const url = download.url?.() || "";
      const suggested = filenameWithExtension(download.suggestedFilename?.() || "download", url);
      const targetPath = await uniquePath(directory, suggested);
      await download.saveAs(targetPath);
      const stat = await fs.stat(targetPath).catch(() => null);
      downloads.unshift({
        id: downloadID(targetPath, url),
        path: targetPath,
        file_path: targetPath,
        file_name: path.basename(targetPath),
        filename: path.basename(targetPath),
        suggested_filename: suggested,
        url,
        size: stat?.size || 0,
        status: "saved",
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
  return { downloads, count: downloads.length, directory: downloadDir(), downloads_path: currentDownloadsPath || downloadDir() };
}

/**
 * 清理浏览器 Profile 残留锁文件。
 * @param {string} userDataDir - 浏览器用户目录。
 * @returns {Promise<void>} 无返回值。
 */
async function cleanupProfileLocks(userDataDir) {
  if (!userDataDir) return;
  await fs.mkdir(userDataDir, { recursive: true });
  for (const name of ["SingletonLock", "SingletonCookie", "SingletonSocket", "lockfile"]) {
    await fs.rm(path.join(userDataDir, name), { force: true, recursive: true }).catch(() => {});
  }
}

/**
 * 给下载文件名补充 URL 中可识别的后缀。
 * @param {string} suggested - 浏览器建议文件名。
 * @param {string} url - 原始下载地址。
 * @returns {string} 修复后的安全文件名。
 */
function filenameWithExtension(suggested, url) {
  const safe = safeFilename(suggested || "download");
  if (path.extname(safe)) return safe;
  const ext = extensionFromURL(url);
  return ext ? `${safe}${ext}` : safe;
}

/**
 * 从 URL 中提取常见文件后缀。
 * @param {string} url - 原始地址。
 * @returns {string} 文件后缀。
 */
function extensionFromURL(url) {
  try {
    const parsed = new URL(url);
    const ext = path.extname(parsed.pathname || "").toLowerCase();
    if (/^\.[a-z0-9]{1,8}$/.test(ext)) return ext;
  } catch {
    return "";
  }
  return "";
}

/**
 * 生成下载记录稳定 ID。
 * @param {string} filePath - 文件路径。
 * @param {string} url - 原始下载地址。
 * @returns {string} 下载记录 ID。
 */
function downloadID(filePath, url) {
  return `download_${crypto.createHash("sha1").update(`${filePath}|${url}`).digest("hex").slice(0, 16)}`;
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
  "/api/v1/boss/candidates/extract": extractBossCandidates,
  "/api/v1/boss/candidates/greet": greetBossCandidate,
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
