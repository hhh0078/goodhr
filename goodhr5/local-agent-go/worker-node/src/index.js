// 本文件负责提供 GoodHR 5 Node Browser Worker HTTP 服务。
import fs from "node:fs/promises";
import crypto from "node:crypto";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import zlib from "node:zlib";

const addr = process.env.GOODHR_WORKER_ADDR || "127.0.0.1:9101";
const [host, rawPort] = addr.split(":");
const port = Number(rawPort || 9101);
const rawMaxPort = Number(process.env.GOODHR_WORKER_PORT_END || 9109);
const maxPort = Number.isFinite(rawMaxPort) ? rawMaxPort : 9109;

let browser = null;
let context = null;
let page = null;
let currentUserDataDir = "";
let currentDownloadsPath = "";
const downloads = [];
const elementRefs = new Map();
let elementRefSeq = 0;

/**
 * 写入 Worker 诊断日志。
 * @param {string} message - 日志内容。
 * @param {Record<string, any>} data - 附加字段。
 * @returns {void} 无返回值。
 */
function logWorker(message, data = {}) {
  const fields = Object.entries(data)
    .filter(([, value]) => value !== undefined && value !== null && value !== "")
    .map(([key, value]) => `${key}=${String(value).slice(0, 240)}`);
  console.log(`[${new Date().toISOString()}] ${message}${fields.length ? ` ${fields.join(" ")}` : ""}`);
}

process.on("uncaughtException", (error) => {
  console.error("Node Worker 未捕获异常", error);
});

process.on("unhandledRejection", (error) => {
  console.error("Node Worker 未处理异步异常", error);
});

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
  const startedAt = Date.now();
  const userDataDir = String(payload.user_data_dir || "").trim();
  logWorker("收到浏览器启动请求", {
    user_data_dir: userDataDir,
    headless: Boolean(payload.headless),
    persistent: Boolean(payload.persistent || userDataDir),
    downloads_path: payload.downloads_path || downloadDir(),
  });
  if (browser || context || page) {
    logWorker("检测已有浏览器状态");
    if (!(await hasLiveBrowserSession())) {
      logWorker("已有浏览器状态不可用，准备清理");
      await disposeBrowserState();
    }
  }
  if (browser || context) {
    if (!userDataDir || userDataDir === currentUserDataDir) {
      logWorker("复用已有浏览器", { user_data_dir: currentUserDataDir });
      return { running: true, persistent: Boolean(currentUserDataDir), user_data_dir: currentUserDataDir };
    }
    logWorker("账号目录不同，准备关闭旧浏览器", { old_user_data_dir: currentUserDataDir, new_user_data_dir: userDataDir });
    await stopBrowser();
  }
  logWorker("准备加载 CloakBrowser Node SDK");
  const cloak = await import("cloakbrowser");
  logWorker("CloakBrowser Node SDK 加载完成");
  const launchPersistent = cloak.launchPersistentContext;
  const launch = cloak.launch;
  const options = {
    headless: Boolean(payload.headless),
    humanize: payload.humanize !== false,
    acceptDownloads: true,
    downloadsPath: payload.downloads_path || downloadDir(),
    windowsHide: true,
    // 隐藏 Chromium 对 --no-sandbox 等启动参数的顶部提示条。
    args: ["--test-type"],
  };
  await fs.mkdir(options.downloadsPath, { recursive: true });
  if (payload.proxy) options.proxy = payload.proxy;
  if (payload.viewport_width && payload.viewport_height) {
    const viewport = { width: Number(payload.viewport_width), height: Number(payload.viewport_height) };
    options.viewport = viewport;
    options.args.push(`--window-size=${viewport.width},${viewport.height}`);
  }
  if (payload.timezone) options.timezone = String(payload.timezone);
  if (payload.locale) options.locale = String(payload.locale);
  if (payload.user_agent) options.userAgent = String(payload.user_agent);
  logWorker("浏览器启动参数已准备", {
    downloads_path: options.downloadsPath,
    viewport: options.viewport ? `${options.viewport.width}x${options.viewport.height}` : "",
  });
  if (userDataDir && launchPersistent) {
    logWorker("准备清理账号目录锁文件", { user_data_dir: userDataDir });
    await cleanupProfileLocks(userDataDir);
    logWorker("准备启动持久化浏览器", { user_data_dir: userDataDir });
    context = await launchPersistent({ ...options, userDataDir });
    logWorker("持久化浏览器启动完成", { elapsed_ms: Date.now() - startedAt });
    currentUserDataDir = userDataDir;
    currentDownloadsPath = options.downloadsPath;
    page = context.pages?.()[0] || await context.newPage();
    registerPage(page);
    logWorker("浏览器页面已就绪", { elapsed_ms: Date.now() - startedAt });
    return { running: true, persistent: true, user_data_dir: userDataDir, downloads_path: options.downloadsPath, viewport: options.viewport };
  }
  if (!launch) throw new Error("CloakBrowser Node SDK 缺少启动方法");
  logWorker("准备启动普通浏览器");
  browser = await launch(options);
  logWorker("普通浏览器启动完成", { elapsed_ms: Date.now() - startedAt });
  context = await browser.newContext?.({ acceptDownloads: true }) || null;
  currentUserDataDir = "";
  currentDownloadsPath = options.downloadsPath;
  page = context ? await context.newPage() : await browser.newPage();
  registerPage(page);
  logWorker("浏览器页面已就绪", { elapsed_ms: Date.now() - startedAt });
  return { running: true, persistent: false, downloads_path: options.downloadsPath, viewport: options.viewport };
}

/**
 * 停止 CloakBrowser。
 * @returns {Promise<Record<string, any>>} 停止结果。
 */
async function stopBrowser() {
  await disposeBrowserState();
  return { running: false };
}

/**
 * 关闭并清空 Worker 内保存的浏览器对象。
 * @returns {Promise<void>} 无返回值。
 */
async function disposeBrowserState() {
  const oldContext = context;
  const oldBrowser = browser;
  resetBrowserState();
  if (oldContext) await oldContext.close().catch(() => {});
  if (oldBrowser) await oldBrowser.close().catch(() => {});
}

/**
 * 清空 Worker 内保存的浏览器对象。
 * @returns {void} 无返回值。
 */
function resetBrowserState() {
  context = null;
  browser = null;
  page = null;
  currentUserDataDir = "";
  currentDownloadsPath = "";
  clearElementRefs();
}

/**
 * 判断浏览器会话是否仍然可用。
 * @returns {Promise<boolean>} 可用返回 true。
 */
async function hasLiveBrowserSession() {
  if (page && !page.isClosed?.()) return true;
  if (context) {
    try {
      const pages = context.pages?.() || [];
      page = pages.find((item) => !item.isClosed?.()) || null;
      if (page) {
        registerPage(page);
        return true;
      }
      return false;
    } catch (error) {
      if (isClosedTargetError(error)) return false;
      throw error;
    }
  }
  if (browser) {
    try {
      if (typeof browser.isConnected === "function") return browser.isConnected();
      return true;
    } catch (error) {
      if (isClosedTargetError(error)) return false;
      throw error;
    }
  }
  return false;
}

/**
 * 返回 Worker 和浏览器当前状态。
 * @returns {Promise<Record<string, any>>} 状态信息。
 */
async function workerHealth() {
  const browserRunning = await hasLiveBrowserSession().catch(() => false);
  if (!browserRunning && (browser || context || page)) {
    await disposeBrowserState();
  }
  return {
    status: "ok",
    worker: "node",
    pid: process.pid,
    browser_running: browserRunning,
    persistent: Boolean(currentUserDataDir),
    user_data_dir: currentUserDataDir,
    downloads_path: currentDownloadsPath || downloadDir(),
  };
}

/**
 * 判断错误是否表示浏览器、上下文或页面已经关闭。
 * @param {unknown} error - 原始错误。
 * @returns {boolean} 关闭类错误返回 true。
 */
function isClosedTargetError(error) {
  const message = String(error?.message || error || "");
  return /Target page, context or browser has been closed|Browser has been closed|Context closed|Target closed/i.test(message);
}

/**
 * 确保当前页面存在。
 * @returns {Promise<any>} Playwright 页面对象。
 */
async function ensurePage() {
  if (page && !page.isClosed?.()) return page;
  page = null;
  if (context) {
    try {
      const pages = context.pages?.() || [];
      page = pages.find((item) => !item.isClosed?.()) || null;
      if (!page) {
        page = await context.newPage();
      }
      registerPage(page);
      return page;
    } catch (error) {
      if (isClosedTargetError(error)) {
        resetBrowserState();
        throw new Error("浏览器已关闭，请重新启动浏览器");
      }
      throw error;
    }
  }
  if (browser) {
    try {
      page = await browser.newPage();
      registerPage(page);
      return page;
    } catch (error) {
      if (isClosedTargetError(error)) {
        resetBrowserState();
        throw new Error("浏览器已关闭，请重新启动浏览器");
      }
      throw error;
    }
  }
  throw new Error("浏览器未启动，请先启动浏览器");
}

/**
 * 打开页面地址。
 * @param {Record<string, any>} payload - 打开页面参数。
 * @returns {Promise<Record<string, any>>} 页面结果。
 */
async function openPage(payload) {
  const startedAt = Date.now();
  const target = String(payload.url || "").trim();
  if (!target) throw new Error("页面地址不能为空");
  logWorker("收到页面打开请求", { url: target, user_data_dir: payload.user_data_dir || "" });
  if (!browser && !context && (payload.user_data_dir || payload.persistent)) {
    logWorker("页面打开前浏览器未启动，准备自动启动");
    await startBrowser(payload);
  }
  const currentPage = await ensurePage();
  clearElementRefs();
  logWorker("准备跳转页面", { url: target });
  await currentPage.goto(target, { waitUntil: "domcontentloaded", timeout: Number(payload.timeout || 60000) });
  logWorker("页面跳转完成", { url: currentPage.url(), elapsed_ms: Date.now() - startedAt });
  return { url: currentPage.url() };
}

/**
 * 列出当前浏览器上下文中的页面。
 * @returns {Promise<Record<string, any>>} 页面列表。
 */
async function listPages() {
  if (!context) throw new Error("浏览器未启动，无法读取页面列表");
  const pages = context.pages?.() || [];
  const items = pages.map((item, index) => ({
    page_id: String(index),
    url: item.url?.() || "",
    title: "",
    is_default: item === page,
  }));
  return { pages: items, count: items.length };
}

/**
 * 切换当前默认页面。
 * @param {Record<string, any>} payload - 页面切换参数。
 * @returns {Promise<Record<string, any>>} 页面结果。
 */
async function usePage(payload) {
  if (!context) throw new Error("浏览器未启动，无法切换页面");
  const pages = context.pages?.() || [];
  const index = Number(payload.page_id || payload.index || 0);
  const nextPage = pages[index];
  if (!nextPage || nextPage.isClosed?.()) throw new Error("指定页面不存在");
  page = nextPage;
  registerPage(page);
  return { page_id: String(index), url: page.url?.() || "" };
}

/**
 * 读取当前页面地址。
 * @returns {Promise<Record<string, any>>} 页面地址结果。
 */
async function currentPageURL() {
  const currentPage = await ensurePage();
  return { url: currentPage.url() };
}

/**
 * 点击页面元素。
 * @param {Record<string, any>} payload - 点击参数。
 * @returns {Promise<Record<string, any>>} 点击结果。
 */
async function clickPage(payload) {
  const currentPage = await ensurePage();
  const base = payload.element_ref ? locatorByRef(currentPage, payload.element_ref) || currentPage : currentPage;
  const locator = await firstLocator(base, payload.element || payload, true);
  if (!locator) throw new Error("点击选择器不能为空或未找到元素");
  if (payload.delay_before) await currentPage.waitForTimeout(Math.max(0, Number(payload.delay_before) * 1000));
  await locator.click({ timeout: Number(payload.timeout || 10000) });
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
 * 按下页面键盘按键。
 * @param {Record<string, any>} payload - 按键参数。
 * @returns {Promise<Record<string, any>>} 按键结果。
 */
async function pressKey(payload) {
  const currentPage = await ensurePage();
  const key = String(payload.key || "").trim();
  if (!key) throw new Error("按键不能为空");
  await currentPage.keyboard.press(key);
  return { pressed: true, key };
}

/**
 * 滚动当前页面或指定元素。
 * @param {Record<string, any>} payload - 滚动参数。
 * @returns {Promise<Record<string, any>>} 滚动结果。
 */
async function scrollPage(payload) {
  const currentPage = await ensurePage();
  const distance = randomDistance(payload);
  const locator = await firstLocator(currentPage, payload.element || payload, true);
  if (locator) {
    await locator.evaluate((el, y) => el.scrollBy(0, y), distance);
    return { scrolled: true, distance };
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
  const element = payload.element || payload;
  const selectors = selectorList(element);
  const locators = await allLocators(currentPage, element, false);
  const item = locators[0];
  const locator = item?.locator;
  if (!locator) {
    if (payload.element || selectors.length > 0) return { text: "", texts: [], found: false, count: 0, selector: selectors[0] || "", selectors };
    const text = await currentPage.locator("body").innerText({ timeout: Number(payload.timeout || 10000) });
    return { text, texts: text ? [text] : [], found: true, count: 1, selector: "body" };
  }
  const text = await locator.innerText({ timeout: Number(payload.timeout || 10000) });
  return {
    text,
    texts: text ? [text] : [],
    found: true,
    count: locators.length,
    selector: item.targetSelector || selectors[0] || "",
    parent_selector: item.parentSelector || "",
    frame_url: item.frameURL || "",
  };
}

/**
 * 按通用元素定位协议查找元素并提取字段。
 * @param {Record<string, any>} payload - 查找参数。
 * @returns {Promise<Record<string, any>>} 查找结果。
 */
async function findElements(payload) {
  const currentPage = await ensurePage();
  const element = payload.element || payload.item || payload;
  const visibleOnly = payload.visible_only !== false;
  const rawMaxItems = Number(payload.max_items || 0);
  const maxItems = rawMaxItems > 0 ? rawMaxItems : 0;
  const locators = await allLocators(currentPage, element, visibleOnly, maxItems);
  const fields = Array.isArray(payload.fields) ? payload.fields : [];
  const items = [];
  const total = maxItems > 0 ? Math.min(locators.length, maxItems) : locators.length;
  for (let index = 0; index < total; index += 1) {
    const locator = locators[index].locator || locators[index];
    const extracted = {};
    for (const field of fields) {
      if (!field || typeof field !== "object") continue;
      for (const [name, config] of Object.entries(field)) {
        extracted[name] = await locatorText(locator, config);
      }
    }
    const ref = rememberElement(locator);
    items.push({
      index,
      ref,
      element_ref: ref,
      text: await locator.innerText({ timeout: 800 }).catch(() => ""),
      fields: extracted,
    });
  }
  return { items, count: items.length };
}

/**
 * 按列表索引点击元素。
 * @param {Record<string, any>} payload - 点击参数。
 * @returns {Promise<Record<string, any>>} 点击结果。
 */
async function listClickByIndex(payload) {
  const currentPage = await ensurePage();
  const index = Math.max(0, Number(payload.index || 0));
  const element = payload.item || payload.element || payload;
  const locators = await allLocators(currentPage, element, true);
  const target = locators[index]?.locator || locators[index];
  if (!target) throw new Error("指定列表项不存在");
  await target.scrollIntoViewIfNeeded({ timeout: Number(payload.timeout || 3000) }).catch(() => {});
  const clickTarget = payload.click_target || payload.clickTarget;
  const nested = clickTarget ? await firstLocator(target, clickTarget, true) : null;
  await (nested || target).click({ timeout: Number(payload.timeout || 10000) });
  return { clicked: true, index };
}

/**
 * 提取当前页面可见 Boss 候选人卡片。
 * @param {Record<string, any>} payload - 提取参数。
 * @returns {Promise<Record<string, any>>} 候选人列表。
 */
async function extractBossCandidates(payload) {
  const startedAt = Date.now();
  const platformConfig = payload.platform_config || payload.config || {};
  const rules = bossRules(platformConfig);
  const rawMaxItems = Number(payload.max_items || 0);
  const maxItems = rawMaxItems > 0 ? rawMaxItems : 0;
  const findResp = await findElements({
    element: rules.candidate_card,
    visible_only: true,
    fields: rules.field_requests,
    max_items: maxItems,
  });
  const foundAt = Date.now();
  const candidates = [];
  for (const item of findResp.items || []) {
    try {
      const fields = item.fields || {};
      if (!fields.basic_info && item.text) fields.basic_info = item.text;
      const rawText = candidateRawText(fields);
      candidates.push({
        name: fields.name || `候选人${item.index + 1}`,
        candidate_name: fields.name || `候选人${item.index + 1}`,
        status: "scanned",
        raw_text: rawText,
        filter_text: rawText,
        platform_id: "boss",
        card_index: item.index,
        element_ref: item.ref || item.element_ref,
        fields,
      });
    } catch {
      continue;
    }
  }
  return {
    candidates,
    count: candidates.length,
    found_count: Number(findResp.count || (findResp.items || []).length || 0),
    find_elapsed_ms: foundAt - startedAt,
    convert_elapsed_ms: Date.now() - foundAt,
    elapsed_ms: Date.now() - startedAt,
  };
}

/**
 * 按 Boss 平台配置滚动候选人列表。
 * @param {Record<string, any>} payload - 滚动参数。
 * @returns {Promise<Record<string, any>>} 滚动结果。
 */
async function scrollBossCandidates(payload) {
  const currentPage = await ensurePage();
  const platformConfig = payload.platform_config || payload.config || {};
  const rules = bossRules(platformConfig);
  const distance = Number(payload.distance || payload.y || 720);
  const selectors = selectorList(rules.scroll_containers);
  for (const selector of selectors) {
    try {
      const locator = currentPage.locator(selector).first();
      if ((await locator.count()) <= 0) continue;
      if (!(await locator.isVisible().catch(() => false))) continue;
      await locator.evaluate((el, y) => el.scrollBy(0, y), distance);
      return { scrolled: true, selector, distance };
    } catch {
      continue;
    }
  }
  await currentPage.mouse.wheel(0, distance);
  return { scrolled: true, distance, fallback: true };
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
  const cardIndex = Math.max(0, Number(payload.card_index || 0));
  const cardInfo = await bossCardByIndex(currentPage, rules, cardIndex, payload);
  const card = cardInfo.card;
  const clicked = await clickFirstVisible(card, selectorList(rules.greet_buttons), 1500);
  if (!clicked) throw new Error("未找到可点击的打招呼按钮");
  await clickFirstVisible(currentPage, selectorList(rules.continue_buttons), 800);
  await clickFirstVisible(currentPage, selectorList(rules.confirm_buttons), 800);
  return { greeted: true, card_index: cardIndex, scroll_attempts: cardInfo.attempts };
}

/**
 * 打开并提取指定 Boss 候选人的详情文本。
 * @param {Record<string, any>} payload - 详情提取参数。
 * @returns {Promise<Record<string, any>>} 详情文本结果。
 */
async function extractBossCandidateDetail(payload) {
  const currentPage = await ensurePage();
  const platformConfig = payload.platform_config || payload.config || {};
  const rules = bossRules(platformConfig);
  const cardIndex = Math.max(0, Number(payload.card_index || 0));
  const cardInfo = await bossCardByIndex(currentPage, rules, cardIndex, payload);
  const card = cardInfo.card;
  const opened = await clickFirstVisible(card, selectorList(rules.detail_buttons), 1500);
  if (!opened) await card.click({ timeout: 1500 });
  await currentPage.waitForTimeout(Number(payload.wait_ms || 800));
  const detailText = await firstDetailText(currentPage, selectorList(rules.detail_containers));
  const screenshot = payload.screenshot
    ? await screenshotDetailContainer(currentPage, selectorList(rules.detail_containers), payload)
    : null;
  const debugInfo = JSON.stringify({ selectors: selectorList(rules.detail_containers), hadContainer: !!detailText, cardFound: !!card, opened });
  return { detail_text: detailText, text: detailText, screenshot, scroll_attempts: cardInfo.attempts, _screenshot_debug: debugInfo };
}

/**
 * 关闭 Boss 候选人详情页或详情弹层。
 * @param {Record<string, any>} payload - 关闭参数。
 * @returns {Promise<Record<string, any>>} 关闭结果。
 */
async function closeBossCandidateDetail(payload) {
  const currentPage = await ensurePage();
  const key = String(payload.key || "Escape").trim() || "Escape";
  await currentPage.keyboard.press(key);
  await currentPage.waitForTimeout(Number(payload.wait_ms || 200));
  return { closed: true, key };
}

/**
 * 按序号找到候选人卡片，并主动滚动到可见范围。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {Record<string, any>} rules - Boss 平台规则。
 * @param {number} cardIndex - 候选人卡片序号。
 * @param {Record<string, any>} payload - 请求参数。
 * @returns {Promise<{card:any, attempts:number}>} 候选人卡片和滚动次数。
 */
async function bossCardByIndex(currentPage, rules, cardIndex, payload) {
  const refLocator = locatorByRef(currentPage, payload.element_ref || payload.ref);
  if (refLocator) {
    await refLocator.evaluate((el) => {
      if (el && el.scrollIntoView) el.scrollIntoView({ block: "center", inline: "nearest" });
    }).catch(() => {});
    await refLocator.scrollIntoViewIfNeeded({ timeout: 1200 }).catch(() => {});
    if (await refLocator.isVisible().catch(() => false)) return { card: refLocator, attempts: 1, by_ref: true };
  }
  const cardSelectors = selectorList(rules.candidate_card);
  if (cardSelectors.length <= 0) throw new Error("云端平台配置缺少候选人卡片选择器");
  let cards = await allLocators(currentPage, rules.candidate_card, true, 0);
  let count = cards.length;
  const maxAttempts = Math.max(1, Math.min(12, Number(payload.card_scroll_attempts || 8)));
  const distance = Math.max(120, Number(payload.card_scroll_distance || payload.distance || 720));
  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    if (cardIndex >= count) {
      await scrollBossListByRules(currentPage, rules, distance);
      await currentPage.waitForTimeout(250);
      cards = await allLocators(currentPage, rules.candidate_card, true, 0);
      count = cards.length;
      continue;
    }
    let card = cards[cardIndex]?.locator || cards[cardIndex];
    await card.evaluate((el) => {
      if (el && el.scrollIntoView) el.scrollIntoView({ block: "center", inline: "nearest" });
    }).catch(() => {});
    await card.scrollIntoViewIfNeeded({ timeout: 1200 }).catch(() => {});
    if (await card.isVisible().catch(() => false)) {
      return { card, attempts: attempt };
    }
    await scrollBossListByRules(currentPage, rules, distance);
    await currentPage.waitForTimeout(250);
    cards = await allLocators(currentPage, rules.candidate_card, true, 0);
    count = cards.length;
  }
  throw new Error("候选人卡片已不在当前页面");
}

/**
 * 按平台规则滚动候选人列表。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {Record<string, any>} rules - Boss 平台规则。
 * @param {number} distance - 滚动距离。
 * @returns {Promise<boolean>} 是否命中列表容器。
 */
async function scrollBossListByRules(currentPage, rules, distance) {
  const selectors = selectorList(rules.scroll_containers);
  for (const selector of selectors) {
    try {
      const locator = currentPage.locator(selector).first();
      if ((await locator.count()) <= 0) continue;
      if (!(await locator.isVisible().catch(() => false))) continue;
      await locator.evaluate((el, y) => el.scrollBy(0, y), distance);
      return true;
    } catch {
      continue;
    }
  }
  await currentPage.mouse.wheel(0, distance);
  return false;
}

/**
 * 读取第一个可见详情容器文本。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {string[]} selectors - 详情容器选择器。
 * @returns {Promise<string>} 详情文本。
 */
async function firstDetailText(currentPage, selectors) {
  for (const selector of selectors) {
    try {
      const locator = currentPage.locator(selector).first();
      if ((await locator.count()) <= 0) continue;
      if (!(await locator.isVisible().catch(() => false))) continue;
      const text = (await locator.innerText({ timeout: 1500 })).trim();
      if (text) return text;
    } catch {
      continue;
    }
  }
  return (await currentPage.locator("body").innerText({ timeout: 1500 }).catch(() => "")).trim();
}

/**
 * 截取第一个可见详情容器。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {string[]} selectors - 详情容器选择器。
 * @param {Record<string, any>} payload - 截图参数。
 * @returns {Promise<Record<string, any>|null>} 截图结果。
 */
async function screenshotDetailContainer(currentPage, selectors, payload) {
  const steps = [];
  for (const selector of selectors) {
    try {
      // 使用 allLocators 统一查找（支持 iframe、多个匹配）
      const elementConfig = typeof selector === "string" ? selector : { selector };
      const locators = await allLocators(currentPage, elementConfig, false);
      if (locators.length === 0) {
        steps.push("选择器["+selector+"]未找到元素");
        continue;
      }
      steps.push("选择器["+selector+"]找到"+locators.length+"个元素");
      for (const locInfo of locators) {
        const loc = locInfo.locator;
        const frameInfo = locInfo.frameURL ? " (iframe:"+locInfo.frameURL.substring(0,40)+")" : "";
        try {
          const visible = await loc.isVisible().catch(() => false);
          if (!visible) { steps.push("  元素不可见"+frameInfo); continue; }
          const box = await loc.boundingBox().catch(() => null);
          if (!box || box.width < 20 || box.height < 20) { steps.push("  元素太小"+frameInfo+" box="+JSON.stringify({w:Math.round(box?.width||0),h:Math.round(box?.height||0)})); continue; }
          const boxInfo = { x:Math.round(box.x), y:Math.round(box.y), w:Math.round(box.width), h:Math.round(box.height) };
          const scrollInfo = await detailScrollInfo(loc);
          steps.push("  可见 框="+JSON.stringify(boxInfo)+" 滚动="+JSON.stringify(scrollInfo)+frameInfo);
          const vp = currentPage.viewportSize?.() || {};
          const dbg = JSON.stringify({ match:{selector,box:boxInfo,scrollInfo,viewport:{w:vp.width,h:vp.height}} });
          const result = await screenshotLocatorWithParts(currentPage, loc, { ...payload, _detail_debug: dbg });
          result._detail_debug = dbg;
          return result;
        } catch(e) {
          steps.push("  处理失败:"+e.message);
        }
      }
    } catch(e) {
      steps.push("选择器["+selector+"]错误:"+e.message);
    }
  }
  steps.push("fallback:全页截图");
  const result = await screenshotPage({ ...payload, full_page: true, filename: payload.filename || "candidate-detail.png" });
  result._detail_debug = steps.join(" | ");
  return result;
}

/**
 * 对详情元素执行分段截图。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {any} locator - 详情容器定位器。
 * @param {Record<string, any>} payload - 截图参数。
 * @returns {Promise<Record<string, any>>} 主截图和分段截图。
 */
async function screenshotLocatorWithParts(currentPage, locator, payload) {
  const filename = safeFilename(String(payload.filename || "candidate-detail.png"));
  const directory = String(payload.dir || payload.directory || path.join(os.tmpdir(), "goodhr-screenshots"));
  await fs.mkdir(directory, { recursive: true });
  await cleanupScreenshotSeries(directory, filename);
  await locator.scrollIntoViewIfNeeded({ timeout: 1200 }).catch(() => {});
  const box = await locator.boundingBox().catch(() => null);
  const viewport = currentPage.viewportSize?.() || { width: 1280, height: 900 };
  if (!box || box.width < 20 || box.height < 20) {
    console.log("[截图Debug] 详情容器太小或不存在", JSON.stringify(box));
    return screenshotPage({ ...payload, filename });
  }
  console.log("[截图Debug] 详情容器 box:", JSON.stringify({ x: box.x, y: box.y, width: box.width, height: box.height }), "viewport:", JSON.stringify(viewport));
  
  // 第一步：检查容器自身是否可滚动（内部 overflow）
  const scrollInfo = await detailScrollInfo(locator);
  console.log("[截图Debug] 容器滚动信息:", JSON.stringify(scrollInfo));
  if (scrollInfo.scrollable) {
    console.log("[截图Debug] 容器可内部滚动，使用 srollable 分段截图");
    const parts = await screenshotScrollableLocatorParts(currentPage, locator, scrollInfo, directory, filename, payload);
    if (parts.length > 0) {
      const result = {
        ...parts[0],
        path: parts[0].path,
        file_path: parts[0].file_path,
        screenshot_parts: parts,
        parts_count: parts.length,
        overlap: parts[0].overlap || 0,
        scrollable_container: true,
        _scroll_debug: JSON.stringify({ scrollable: true, parts_count: parts.length, scrollHeight: scrollInfo.scrollHeight, clientHeight: scrollInfo.clientHeight, overflowY: scrollInfo.overflowY }),
      };
      return result;
    }
  }
  
  // 第二步：容器不可滚动，检查是否超出视口或被业务强制要求滚动截图（需整体滚动页面）
  const needsScroll = box.y < 0 || box.y + box.height > viewport.height;
  const forceScroll = Boolean(payload.force_scroll || payload.scroll_full || payload.forceScroll);
  console.log("[截图Debug] needsScroll:", needsScroll, "forceScroll:", forceScroll, "box.y:", box.y, "box.bottom:", box.y + box.height, "viewport.height:", viewport.height);
  if (needsScroll || forceScroll) {
    console.log("[截图Debug] 详情使用鼠标滚轮分段截图");
    const parts = await screenshotLocatorParts(currentPage, box, viewport, directory, filename, payload);
    if (parts.length > 0) {
      const result = {
        ...parts[0],
        path: parts[0].path,
        file_path: parts[0].file_path,
        screenshot_parts: parts,
        parts_count: parts.length,
        overlap: parts[0].overlap || 0,
        wheel_scroll: true,
        _scroll_debug: JSON.stringify({ wheel_scroll: true, force_scroll: forceScroll, needsScroll, parts_count: parts.length, boxH: Math.round(box.height), boxY: Math.round(box.y), boxBottom: Math.round(box.y + box.height), vpH: viewport.height, initial: parts[0]._debug_initial || null, rounds: parts[0]._debug_rounds || [] }),
      };
      return result;
    }
  }
  
  // 第三步：不需要滚动，单次截图
  const singleResult = await saveLocatorScreenshot(locator, directory, filename);
  singleResult._scroll_debug = JSON.stringify({ single: true, scrollable: false, needsScroll: false, boxY: Math.round(box.y), boxBottom: Math.round(box.y + box.height), vpHeight: viewport.height });
  return singleResult;
}

/**
 * 读取详情容器滚动信息。
 * @param {any} locator - 详情容器定位器。
 * @returns {Promise<Record<string, any>>} 滚动信息。
 */
async function detailScrollInfo(locator) {
  return locator.evaluate((el) => {
    const style = window.getComputedStyle(el);
    const overflowY = style.overflowY || "";
    const scrollHeight = Math.ceil(el.scrollHeight || 0);
    const clientHeight = Math.ceil(el.clientHeight || 0);
    const scrollable = scrollHeight > clientHeight + 8 && !["hidden", "clip"].includes(overflowY);
    return {
      scrollable,
      scrollTop: Math.round(el.scrollTop || 0),
      scrollHeight,
      clientHeight,
      overflowY,
    };
  }).catch(() => ({ scrollable: false, scrollTop: 0, scrollHeight: 0, clientHeight: 0 }));
}

/**
 * 保存指定元素的截图。
 * @param {any} locator - 元素定位器。
 * @param {string} directory - 保存目录。
 * @param {string} filename - 文件名。
 * @returns {Promise<Record<string, any>>} 截图信息。
 */
async function saveLocatorScreenshot(locator, directory, filename) {
  const targetPath = path.join(directory, filename);
  const sizeInfo = await locator.boundingBox().catch(() => null) || { width: 0, height: 0 };
  await locator.screenshot({ path: targetPath, type: "png" });
  const stat = await fs.stat(targetPath);
  return {
    path: targetPath,
    file_path: targetPath,
    size: stat.size,
    width: Math.round(sizeInfo.width || 0),
    height: Math.round(sizeInfo.height || 0),
  };
}

/**
 * 滚动详情容器自身并保存多张截图。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {any} locator - 详情容器定位器。
 * @param {Record<string, any>} scrollInfo - 容器滚动信息。
 * @param {string} directory - 保存目录。
 * @param {string} filename - 基础文件名。
 * @param {Record<string, any>} payload - 截图参数。
 * @returns {Promise<Record<string, any>[]>} 分段截图列表。
 */
async function screenshotScrollableLocatorParts(currentPage, locator, scrollInfo, directory, filename, payload) {
  const box = await locator.boundingBox().catch(() => null);
  if (!box || box.width < 20 || box.height < 20) return [];
  const mouseX = box.x + box.width / 2;
  const mouseY = box.y + Math.min(box.height / 2, 120);
  await currentPage.mouse.move(mouseX, mouseY).catch(() => {});
  await currentPage.waitForTimeout(200);
  const clientHeight = Math.max(1, Number(scrollInfo.clientHeight || box.height || 1));
  const scrollHeight = Math.max(clientHeight, Number(scrollInfo.scrollHeight || clientHeight));
  const scrollDelta = Math.max(1, Math.round(clientHeight * 0.8));
  const overlap = Math.max(clientHeight - scrollDelta, 0);
  const configuredMax = Math.max(1, Math.min(16, Number(payload.max_scrolls || payload.screenshot_max_scrolls || 12)));
  const conservativeDelta = Math.max(1, Math.round(clientHeight * 0.45));
  const estimated = Math.max(1, Math.ceil(Math.max(scrollHeight - clientHeight, 0) / conservativeDelta) + 1);
  const maxScrolls = Math.min(configuredMax, estimated + 2);
  const parsed = path.parse(filename);
  const parts = [];
  let previousBuffer = null;
  logWorker("详情截图开始：滚轮滚动容器", {
    filename,
    clientHeight,
    scrollHeight,
    scrollDelta,
    overlap,
    estimated,
    maxScrolls,
    mouseX: Math.round(mouseX),
    mouseY: Math.round(mouseY),
  });
  for (let index = 0; index < maxScrolls; index += 1) {
    await currentPage.mouse.move(mouseX, mouseY).catch(() => {});
    await currentPage.waitForTimeout(index === 0 ? 600 : 1200);
    const partName = `${parsed.name || "candidate-detail"}-part-${index + 1}${parsed.ext || ".png"}`;
    const targetPath = path.join(directory, partName);
    const sizeInfo = await locator.boundingBox().catch(() => null) || box;
    const currentBuffer = await locator.screenshot({ type: "png" });
    if (previousBuffer && screenshotsAreDuplicate(previousBuffer, currentBuffer)) {
      logWorker("详情截图停止：检测到重复容器截图", { filename, index: index + 1, parts: parts.length });
      break;
    }
    await fs.writeFile(targetPath, currentBuffer);
    const stat = await fs.stat(targetPath);
    const beforeScroll = await pageScrollState(currentPage);
    parts.push({
      path: targetPath,
      file_path: targetPath,
      size: stat.size,
      width: Math.round(sizeInfo.width || box.width || 0),
      height: Math.round(sizeInfo.height || box.height || 0),
      overlap,
      index,
      scroll_top: Math.round(Number(beforeScroll.top || 0)),
    });
    previousBuffer = currentBuffer;
    logWorker("详情截图保存：容器分段", {
      filename,
      part: index + 1,
      size: stat.size,
      scrollTop: beforeScroll.top,
      maxed: beforeScroll.maxed,
    });
    await currentPage.mouse.wheel(0, scrollDelta);
    await currentPage.waitForTimeout(1600);
    const afterScroll = await pageScrollState(currentPage);
    const moved = scrollStateDistance(beforeScroll, afterScroll);
    logWorker("详情截图滚轮：容器滚动后状态", {
      filename,
      part: index + 1,
      before: beforeScroll.top,
      after: afterScroll.top,
      moved,
      maxed: afterScroll.maxed,
    });
    if (afterScroll.maxed || moved < 3) {
      logWorker("详情截图停止：容器滚动已到底或未移动", { filename, part: index + 1, moved, maxed: afterScroll.maxed });
      break;
    }
  }
  logWorker("详情截图完成：滚轮滚动容器", { filename, parts: parts.length });
  return parts;
}

/**
 * 滚动详情元素并保存多张分段截图。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {Record<string, number>} box - 元素边界。
 * @param {Record<string, number>} viewport - 视口尺寸。
 * @param {string} directory - 保存目录。
 * @param {string} filename - 基础文件名。
 * @param {Record<string, any>} payload - 截图参数。
 * @returns {Promise<Record<string, any>[]>} 分段截图列表。
 */
async function screenshotLocatorParts(currentPage, box, viewport, directory, filename, payload) {
  const clipX = Math.max(Math.round(box.x), 0);
  const clipY = Math.max(Math.round(box.y), 0);
  const clipWidth = Math.max(Math.round(box.width), 1);
  const clipBottom = Math.min(Math.round(box.y + box.height), Math.round(viewport.height || 900));
  const clipHeight = Math.max(clipBottom - clipY, 1);
  const clip = { x: clipX, y: clipY, width: clipWidth, height: clipHeight };
  const mouseX = clipX + clipWidth / 2;
  const mouseY = clipY + clipHeight / 2;
  await currentPage.mouse.move(mouseX, mouseY).catch(() => {});
  await currentPage.waitForTimeout(1500);
  const forceScroll = Boolean(payload.force_scroll || payload.scroll_full || payload.forceScroll);
  const scrollPoint = { x: mouseX, y: mouseY };
  const scrollState = await pageScrollState(currentPage, scrollPoint);
  const scrollDelta = Math.max(Math.round(clipHeight * 0.72), 1);
  const overlap = Math.max(clipHeight - scrollDelta, 0);
  const configuredMax = Math.max(1, Math.min(12, Number(payload.max_scrolls || payload.screenshot_max_scrolls || 10)));
  const conservativeDelta = Math.max(1, Math.round(clipHeight * 0.45));
  const remainingPageScroll = Math.max(0, Number(scrollState.scrollHeight || 0) - Number(scrollState.clientHeight || 0) - Number(scrollState.top || 0));
  const estimatedByBox = Math.ceil(Math.max(box.height - clipHeight, 0) / conservativeDelta) + 1;
  const estimatedByPage = forceScroll ? Math.ceil(remainingPageScroll / conservativeDelta) + 1 : 1;
  const estimated = Math.max(1, estimatedByBox, estimatedByPage);
  const maxScrolls = Math.min(configuredMax, estimated + 2);
  const parsed = path.parse(filename);
  const parts = [];
  let previousBuffer = null;
  const debugRounds = [];
  logWorker("详情截图开始：滚轮滚动页面", {
    filename,
    clipHeight,
    boxHeight: Math.round(box.height || 0),
    forceScroll,
    scrollDelta,
    overlap,
    estimated,
    maxScrolls,
    pageScrollTop: scrollState.top,
    pageScrollHeight: scrollState.scrollHeight,
    pageClientHeight: scrollState.clientHeight,
    mouseX: Math.round(mouseX),
    mouseY: Math.round(mouseY),
  });
  for (let index = 0; index < maxScrolls; index += 1) {
    await currentPage.mouse.move(mouseX, mouseY).catch(() => {});
    const partName = `${parsed.name || "candidate-detail"}-part-${index + 1}${parsed.ext || ".png"}`;
    const targetPath = path.join(directory, partName);
    const beforeShot = await pageScrollState(currentPage, scrollPoint);
    logWorker("详情截图准备保存：页面分段", {
      filename,
      part: index + 1,
      maxScrolls,
      beforeShot,
      clip,
    });
    const currentBuffer = await currentPage.screenshot({ clip, type: "png" });
    if (previousBuffer && screenshotsAreDuplicate(previousBuffer, currentBuffer)) {
      debugRounds.push({ part: index + 1, beforeShot, duplicate: true, stop_reason: "duplicate_before_save" });
      logWorker("详情截图停止：检测到重复页面截图", { filename, index: index + 1, parts: parts.length, beforeShot });
      break;
    }
    await fs.writeFile(targetPath, currentBuffer);
    const stat = await fs.stat(targetPath);
    parts.push({
      path: targetPath,
      file_path: targetPath,
      size: stat.size,
      width: clip.width,
      height: clip.height,
      overlap,
      index,
    });
    logWorker("详情截图保存：页面分段", { filename, part: index + 1, size: stat.size, beforeShot });
    previousBuffer = currentBuffer;
    const beforeScroll = await pageScrollState(currentPage, scrollPoint);
    await currentPage.mouse.wheel(0, scrollDelta);
    await currentPage.waitForTimeout(2000);
    const afterScroll = await pageScrollState(currentPage, scrollPoint);
    const moved = scrollStateDistance(beforeScroll, afterScroll);
    const hasExpectedMoreParts = parts.length < Math.min(maxScrolls, Math.max(2, estimatedByBox));
    logWorker("详情截图滚轮：页面滚动后状态", {
      filename,
      part: index + 1,
      before: beforeScroll.top,
      after: afterScroll.top,
      moved,
      maxed: afterScroll.maxed,
      hasExpectedMoreParts,
      targetBefore: beforeScroll.target,
      targetAfter: afterScroll.target,
    });
    const round = {
      part: index + 1,
      saved: true,
      size: stat.size,
      beforeShot,
      beforeScroll,
      afterScroll,
      moved,
      maxed: afterScroll.maxed,
    };
    if ((afterScroll.maxed || moved < 3) && !hasExpectedMoreParts) {
      round.stop_reason = afterScroll.maxed ? "maxed_after_scroll" : "moved_lt_3";
      debugRounds.push(round);
      logWorker("详情截图停止：页面滚动已到底或未移动", { filename, part: index + 1, moved, maxed: afterScroll.maxed, beforeScroll, afterScroll, stopReason: round.stop_reason });
      break;
    }
    round.stop_reason = afterScroll.maxed || moved < 3 ? "continue_for_expected_parts" : "";
    debugRounds.push(round);
  }
  logWorker("详情截图完成：滚轮滚动页面", { filename, parts: parts.length, debugRounds });
  if (parts.length > 0) {
    parts[0]._debug_rounds = debugRounds;
    parts[0]._debug_initial = {
      forceScroll,
      clip,
      clipHeight,
      boxHeight: Math.round(box.height || 0),
      scrollDelta,
      overlap,
      configuredMax,
      conservativeDelta,
      remainingPageScroll,
      estimatedByBox,
      estimatedByPage,
      estimated,
      maxScrolls,
      scrollState,
    };
  }
  return parts;
}

/**
 * 读取页面当前滚动状态。
 * @param {any} currentPage - Playwright 页面对象。
 * @param {{x:number,y:number}=} point - 鼠标所在视口位置，用于定位实际滚动容器。
 * @returns {Promise<Record<string, any>>} 页面滚动状态。
 */
async function pageScrollState(currentPage, point) {
  return currentPage.evaluate((mousePoint) => {
    const doc = document.scrollingElement || document.documentElement;
    const docTop = Math.round(doc?.scrollTop || window.scrollY || 0);
    const docHeight = Math.round(doc?.scrollHeight || document.documentElement.scrollHeight || 0);
    const docClientHeight = Math.round(doc?.clientHeight || window.innerHeight || 0);
    let top = docTop;
    let canScrollMore = docTop < Math.max(docHeight - docClientHeight - 2, 0);
    let target = null;
    if (mousePoint && Number.isFinite(mousePoint.x) && Number.isFinite(mousePoint.y)) {
      const start = document.elementFromPoint(mousePoint.x, mousePoint.y);
      let node = start instanceof HTMLElement ? start : null;
      let depth = 0;
      while (node && depth < 12) {
        const scrollHeight = Math.round(node.scrollHeight || 0);
        const clientHeight = Math.round(node.clientHeight || 0);
        const scrollTop = Math.round(node.scrollTop || 0);
        const style = window.getComputedStyle(node);
        const overflowY = style.overflowY || "";
        if (scrollHeight > clientHeight + 8 && !["hidden", "clip"].includes(overflowY)) {
          const maxTop = Math.max(scrollHeight - clientHeight - 2, 0);
          target = {
            tag: node.tagName,
            className: String(node.className || "").slice(0, 120),
            scrollTop,
            scrollHeight,
            clientHeight,
            overflowY,
            maxed: scrollTop >= maxTop,
          };
          top += scrollTop * 3;
          if (!target.maxed) canScrollMore = true;
          break;
        }
        node = node.parentElement;
        depth += 1;
      }
    }
    for (const el of Array.from(document.querySelectorAll("*"))) {
      const node = /** @type {HTMLElement} */ (el);
      const scrollHeight = Math.round(node.scrollHeight || 0);
      const clientHeight = Math.round(node.clientHeight || 0);
      if (scrollHeight <= clientHeight + 8) continue;
      const style = window.getComputedStyle(node);
      if (["hidden", "clip"].includes(style.overflowY || "")) continue;
      const scrollTop = Math.round(node.scrollTop || 0);
      top += scrollTop;
      if (scrollTop < Math.max(scrollHeight - clientHeight - 2, 0)) {
        canScrollMore = true;
      }
    }
    return {
      top,
      height: docHeight,
      scrollHeight: docHeight,
      clientHeight: docClientHeight,
      maxed: !canScrollMore,
      doc: {
        scrollTop: docTop,
        scrollHeight: docHeight,
        clientHeight: docClientHeight,
      },
      target,
    };
  }, point || null).catch(() => ({ top: 0, height: 0, scrollHeight: 0, clientHeight: 0, maxed: false, target: null }));
}

/**
 * 计算两次滚动状态之间的移动距离。
 * @param {Record<string, any>} before - 滚动前状态。
 * @param {Record<string, any>} after - 滚动后状态。
 * @returns {number} 滚动距离。
 */
function scrollStateDistance(before, after) {
  return Math.abs(Number(after?.top || 0) - Number(before?.top || 0));
}

/**
 * 清理同一任务下上一次详情截图及分段图，确保只保留最新一份。
 * @param {string} directory - 截图目录。
 * @param {string} filename - 主截图文件名。
 * @returns {Promise<void>} 无返回值。
 */
async function cleanupScreenshotSeries(directory, filename) {
  const parsed = path.parse(filename);
  const base = parsed.name || "candidate-detail";
  const ext = parsed.ext || ".png";
  await fs.rm(path.join(directory, filename), { force: true }).catch(() => {});
  const files = await fs.readdir(directory).catch(() => []);
  await Promise.all(files
    .filter((name) => name.startsWith(`${base}-part-`) && name.endsWith(ext))
    .map((name) => fs.rm(path.join(directory, name), { force: true }).catch(() => {})));
}

/**
 * 判断两张滚动截图是否重复。
 * @param {Buffer} previous - 上一张截图。
 * @param {Buffer} current - 当前截图。
 * @returns {boolean} 重复返回 true。
 */
function screenshotsAreDuplicate(previous, current) {
  if (!previous || !current) return false;
  const previousImage = decodePNG(previous);
  const currentImage = decodePNG(current);
  if (!previousImage || !currentImage) {
    return compressedScreenshotsAreDuplicate(previous, current);
  }
  if (previousImage.width !== currentImage.width || previousImage.height !== currentImage.height) {
    return false;
  }
  const width = previousImage.width;
  const height = previousImage.height;
  const startX = Math.floor(width * 0.1);
  const endX = Math.max(startX + 1, Math.floor(width * 0.9));
  const startY = Math.floor(height * 0.05);
  const endY = Math.max(startY + 1, Math.floor(height * 0.95));
  const stepX = Math.max(1, Math.floor((endX - startX) / 90));
  const stepY = Math.max(1, Math.floor((endY - startY) / 90));
  let same = 0;
  let total = 0;
  for (let y = startY; y < endY; y += stepY) {
    for (let x = startX; x < endX; x += stepX) {
      const offset = (y * width + x) * 4;
      const diff = Math.abs(previousImage.data[offset] - currentImage.data[offset])
        + Math.abs(previousImage.data[offset + 1] - currentImage.data[offset + 1])
        + Math.abs(previousImage.data[offset + 2] - currentImage.data[offset + 2]);
      total += 1;
      if (diff <= 24) same += 1;
    }
  }
  return total > 0 && same / total >= 0.98;
}

/**
 * 使用压缩后的 PNG 字节粗略判断重复，作为 PNG 解析失败时的兜底。
 * @param {Buffer} previous - 上一张截图。
 * @param {Buffer} current - 当前截图。
 * @returns {boolean} 重复返回 true。
 */
function compressedScreenshotsAreDuplicate(previous, current) {
  if (!previous || !current) return false;
  if (previous.length !== current.length) return false;
  const startOffset = Math.floor(previous.length * 0.12);
  const endOffset = Math.floor(previous.length * 0.88);
  if (endOffset <= startOffset) return false;
  const step = Math.max(1, Math.floor((endOffset - startOffset) / 4000));
  let same = 0;
  let total = 0;
  for (let index = startOffset; index < endOffset && index < previous.length && index < current.length; index += step) {
    total += 1;
    if (Math.abs(previous[index] - current[index]) <= 8) same += 1;
  }
  return total > 0 && same / total >= 0.985;
}

/**
 * 解码 Playwright PNG 截图为 RGBA 像素。
 * @param {Buffer} buffer - PNG 图片内容。
 * @returns {{width:number,height:number,data:Buffer}|null} 解码后的图片。
 */
function decodePNG(buffer) {
  try {
    if (!Buffer.isBuffer(buffer) || buffer.length < 33) return null;
    const signature = buffer.subarray(0, 8).toString("hex");
    if (signature !== "89504e470d0a1a0a") return null;
    let offset = 8;
    let width = 0;
    let height = 0;
    let colorType = 0;
    const idat = [];
    while (offset + 8 <= buffer.length) {
      const length = buffer.readUInt32BE(offset);
      const type = buffer.subarray(offset + 4, offset + 8).toString("ascii");
      const dataStart = offset + 8;
      const dataEnd = dataStart + length;
      if (dataEnd > buffer.length) return null;
      if (type === "IHDR") {
        width = buffer.readUInt32BE(dataStart);
        height = buffer.readUInt32BE(dataStart + 4);
        const bitDepth = buffer[dataStart + 8];
        colorType = buffer[dataStart + 9];
        const interlace = buffer[dataStart + 12];
        if (bitDepth !== 8 || interlace !== 0 || ![2, 6].includes(colorType)) return null;
      } else if (type === "IDAT") {
        idat.push(buffer.subarray(dataStart, dataEnd));
      } else if (type === "IEND") {
        break;
      }
      offset = dataEnd + 4;
    }
    if (width <= 0 || height <= 0 || idat.length === 0) return null;
    const channels = colorType === 6 ? 4 : 3;
    const stride = width * channels;
    const inflated = zlib.inflateSync(Buffer.concat(idat));
    const raw = Buffer.alloc(height * stride);
    let inputOffset = 0;
    for (let y = 0; y < height; y += 1) {
      const filter = inflated[inputOffset];
      inputOffset += 1;
      const row = inflated.subarray(inputOffset, inputOffset + stride);
      inputOffset += stride;
      unfilterPNGRow(row, raw, y, stride, channels, filter);
    }
    const rgba = Buffer.alloc(width * height * 4);
    for (let i = 0, j = 0; i < raw.length; i += channels, j += 4) {
      rgba[j] = raw[i];
      rgba[j + 1] = raw[i + 1];
      rgba[j + 2] = raw[i + 2];
      rgba[j + 3] = channels === 4 ? raw[i + 3] : 255;
    }
    return { width, height, data: rgba };
  } catch {
    return null;
  }
}

/**
 * 还原 PNG 单行滤镜。
 * @param {Buffer} row - 当前压缩行。
 * @param {Buffer} output - 输出像素缓存。
 * @param {number} y - 当前行号。
 * @param {number} stride - 每行字节数。
 * @param {number} channels - 每个像素通道数。
 * @param {number} filter - PNG 滤镜类型。
 * @returns {void} 无返回值。
 */
function unfilterPNGRow(row, output, y, stride, channels, filter) {
  const rowStart = y * stride;
  const prevStart = rowStart - stride;
  for (let x = 0; x < stride; x += 1) {
    const left = x >= channels ? output[rowStart + x - channels] : 0;
    const up = y > 0 ? output[prevStart + x] : 0;
    const upLeft = y > 0 && x >= channels ? output[prevStart + x - channels] : 0;
    let value = row[x];
    if (filter === 1) value += left;
    if (filter === 2) value += up;
    if (filter === 3) value += Math.floor((left + up) / 2);
    if (filter === 4) value += paethPredictor(left, up, upLeft);
    output[rowStart + x] = value & 0xff;
  }
}

/**
 * PNG Paeth 滤镜预测。
 * @param {number} left - 左侧像素值。
 * @param {number} up - 上方像素值。
 * @param {number} upLeft - 左上像素值。
 * @returns {number} 预测值。
 */
function paethPredictor(left, up, upLeft) {
  const p = left + up - upLeft;
  const pa = Math.abs(p - left);
  const pb = Math.abs(p - up);
  const pc = Math.abs(p - upLeft);
  if (pa <= pb && pa <= pc) return left;
  if (pb <= pc) return up;
  return upLeft;
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
    field_requests: fieldRequestsFromCard(card),
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
 * 从 card 配置中读取通用字段请求。
 * @param {Record<string, any>} card - card 配置。
 * @returns {Record<string, any>[]} 字段请求。
 */
function fieldRequestsFromCard(card) {
  if (Array.isArray(card.fields)) return card.fields;
  if (card.fields && typeof card.fields === "object") {
    return Object.entries(card.fields).map(([key, value]) => ({ [key]: value }));
  }
  return Object.entries(fieldRulesFromCard(card)).map(([key, value]) => ({ [key]: value }));
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

/**
 * 在元素内部查找第一个可滚动的子元素（递归，最多3层）。
 * @param {any} locator - 父元素定位器。
 * @returns {Promise<Record<string, any>|null>} 滚动信息和子元素 locator。
 */
async function findScrollableChild(locator) {
  const maxDepth = 3;
  async function search(el, depth) {
    if (depth > maxDepth) return null;
    const info = await el.evaluate((node) => {
      const style = window.getComputedStyle(node);
      const overflowY = style.overflowY || "";
      const scrollHeight = Math.ceil(node.scrollHeight || 0);
      const clientHeight = Math.ceil(node.clientHeight || 0);
      const scrollable = scrollHeight > clientHeight + 8 && !["hidden", "clip"].includes(overflowY);
      if (scrollable) {
        return { scrollable: true, scrollTop: Math.round(node.scrollTop || 0), scrollHeight, clientHeight, overflowY, tag: node.tagName, depth };
      }
      return null;
    }).catch(() => null);
    if (info) return { ...info, locator: el };
    // 检查子元素
    const childCount = await el.locator("> *").count().catch(() => 0);
    for (let i = 0; i < childCount; i++) {
      const child = el.locator("> *").nth(i);
      const result = await search(child, depth + 1);
      if (result) return result;
    }
    return null;
  }
  return search(locator, 1);
}

async function screenshotPage(payload) {
  const currentPage = await ensurePage();
  const filename = safeFilename(String(payload.filename || "page-screenshot.png"));
  const directory = String(payload.dir || payload.directory || path.join(os.tmpdir(), "goodhr-screenshots"));
  await fs.mkdir(directory, { recursive: true });
  const selector = firstSelector(payload);
  if (selector) {
    const locator = currentPage.locator(selector).first();
    if (await locator.count() <= 0) throw new Error("截图元素不存在");
    // 支持 force_scroll / scroll_full 触发分段滚动截图
    if (payload.force_scroll || payload.scroll_full) {
      const partsResult = await screenshotLocatorWithParts(currentPage, locator, payload);
      return partsResult;
    }
    // 否则检查容器自身是否可滚动，是的话也用分段截图
    const scrollInfo = await detailScrollInfo(locator);
    if (scrollInfo.scrollable) {
      const partsResult = await screenshotLocatorWithParts(currentPage, locator, payload);
      return partsResult;
    }
    const sizeInfo = await locator.boundingBox().catch(() => null) || { width: 0, height: 0 };
    await locator.screenshot({ path: path.join(directory, filename), type: "png" });
    const stat = await fs.stat(path.join(directory, filename));
    return { path: path.join(directory, filename), file_path: path.join(directory, filename), size: stat.size, width: Math.round(sizeInfo.width || 0), height: Math.round(sizeInfo.height || 0) };
  }
  const sizeInfo = await pageSize(currentPage, Boolean(payload.full_page));
  const targetPath = path.join(directory, filename);
  await currentPage.screenshot({ path: targetPath, fullPage: Boolean(payload.full_page), type: "png" });
  const stat = await fs.stat(targetPath);
  return { path: targetPath, file_path: targetPath, size: stat.size, width: Math.round(sizeInfo.width || 0), height: Math.round(sizeInfo.height || 0) };
}

/**
 * 在招聘平台页面右上角显示或关闭 AI 状态浮层。
 * @param {Record<string, any>} payload - 浮层参数。
 * @returns {Promise<Record<string, any>>} 浮层结果。
 */
async function aiOverlay(payload) {
  const currentPage = await ensurePage();
  const action = String(payload.action || "show").trim().toLowerCase();
  // hide 不再主动移除卡片，由 show 管理卡片生命周期
  if (action === "hide" || action === "close" || action === "remove") {
    await currentPage.evaluate(() => {
      const refs = window.__gohOvl = window.__gohOvl || [];
      // 清理已移除卡片的引用
      window.__gohOvl = refs.filter(r => r.card && r.card.parentNode);
    }).catch(() => {});
    return { visible: false };
  }
  const title = String(payload.title || "AI 正在思考").trim();
  const subtitle = String(payload.subtitle || payload.target || "").trim();
  const message = String(payload.message || "正在分析候选人，请稍候").trim();
  await currentPage.evaluate(({ title, subtitle, message }) => {
    const randStr = (len) => Math.random().toString(36).substring(2, 2 + len);
    const ctx = window.__gohCtx = window.__gohCtx || {};
    const mk = title + "|" + subtitle;

    // 同标题+副标题的流式更新 → 复用当前卡片，只更新消息内容
    if (ctx.matchKey === mk && ctx.card && ctx.card.parentNode && !ctx.removing) {
      var msgDiv = ctx.card.children[0] && ctx.card.children[0].children[1];
      if (msgDiv && msgDiv.children[3] && msgDiv.children[3].children[0]) {
        // 也更新标题和副标题（可能因 showAIReply 改变）
        if (msgDiv.children[0]) msgDiv.children[0].textContent = title;
        if (msgDiv.children[1]) msgDiv.children[1].textContent = subtitle;
        var mEl = msgDiv.children[3].children[0];
        mEl.textContent = message;
        mEl.scrollTop = mEl.scrollHeight;
      }
      return; // 不复用旧卡片，不走删除流程
    }

    // 新 AI 调用（不同标题）→ 旧卡片 5 秒淡出移除
    if (ctx.card && ctx.card.parentNode && !ctx.removing) {
      ctx.removing = true;
      ctx.card.style.transition = "opacity 0.3s ease";
      ctx.card.style.opacity = "0.3";
      var s = ctx.style;
      var c = ctx.card;
      setTimeout(function() {
        if (s && s.parentNode) s.remove();
        if (c && c.parentNode) c.remove();
      }, 5000);
    }

    const msgCls = randStr(6);
    const ringCls = randStr(6);
    const cursorCls = randStr(6);
    const animSpin = "a" + randStr(8);
    const animBreathe = "b" + randStr(8);
    // 此卡片序号，用于标识生成顺序
    const seq = (window.__gohSeq || 0) + 1;
    window.__gohSeq = seq;

    const vw = Math.max(document.documentElement.clientWidth || 0, window.innerWidth || 0);
    const pw = Math.min(360, Math.max(260, vw - 32));

    const box = document.createElement("div");
    box.style.cssText = [
      "position:fixed", "right:16px", "top:16px", "z-index:2147483647",
      "width:" + pw + "px", "box-sizing:border-box", "padding:14px",
      "border-radius:14px", "background:rgba(252,250,244,.96)", "color:#18221d",
      "box-shadow:0 18px 48px rgba(18,28,22,.22),0 2px 8px rgba(18,28,22,.10)",
      "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif",
      "font-size:13px", "line-height:1.45", "pointer-events:none",
      "border:1px solid rgba(48,79,63,.18)",
      "backdrop-filter:saturate(1.1) blur(10px)",
    ].join(";");

    const style = document.createElement("style");
    style.textContent = [
      "@keyframes " + animSpin + " { to { transform: rotate(360deg); } }",
      "@keyframes " + animBreathe + " { 0%,100% { opacity:.58; transform: translateY(0); } 50% { opacity:1; transform: translateY(-1px); } }",
      "." + ringCls + " { width:28px;height:28px;border-radius:50%;border:2px solid rgba(69,104,83,.18);border-top-color:#4f7f64;animation:" + animSpin + " .9s linear infinite;flex:0 0 auto; }",
      "." + msgCls + " { display:block;max-height:200px;overflow-y:auto;color:#405249;white-space:pre-wrap;word-break:break-word; }",
      "." + cursorCls + " { display:inline-block;width:6px;height:14px;margin-left:2px;border-radius:3px;background:#4f7f64;vertical-align:-2px;animation:" + animBreathe + " 1s infinite ease-in-out; }",
    ].join("\n");
    document.head.appendChild(style);

    // 构建卡片 DOM 结构 — 全内联样式，无任何可检测属性
    box.innerHTML = [
      '<div style="display:flex;gap:12px;align-items:flex-start;">',
      '<div class="' + ringCls + '"></div>',
      '<div style="min-width:0;flex:1;">',
      '<div style="font-size:14px;font-weight:750;color:#18221d;margin-top:1px;"></div>',
      '<div style="font-size:12px;color:#6d7a72;margin-top:2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;"></div>',
      '<div style="height:1px;background:rgba(48,79,63,.12);margin:10px 0 9px;"></div>',
      '<div><span class="' + msgCls + '"></span><span class="' + cursorCls + '"></span></div>',
      '</div></div>',
    ].join("");
    document.body.appendChild(box);

    // 通过 DOM 结构索引设置文本内容，不依赖任何自定义属性
    var contentCol = box.children[0] && box.children[0].children[1];
    if (contentCol) {
      if (contentCol.children[0]) contentCol.children[0].textContent = title;
      if (contentCol.children[1]) contentCol.children[1].textContent = subtitle;
      if (contentCol.children[3] && contentCol.children[3].children[0]) {
        var msgEl = contentCol.children[3].children[0];
        msgEl.textContent = message;
        msgEl.scrollTop = msgEl.scrollHeight;
      }
    }

    // 15秒自动移除兜底
    setTimeout(function() {
      if (box && box.parentNode) {
        box.style.transition = "opacity 0.3s ease";
        box.style.opacity = "0.3";
        setTimeout(function() {
          if (style && style.parentNode) style.remove();
          if (box && box.parentNode) box.remove();
        }, 500);
      }
    }, 15000);

    // 更新上下文为当前卡片，供下次流式更新时复用
    ctx.matchKey = mk;
    ctx.card = box;
    ctx.style = style;
    ctx.removing = false;
  }, { title, subtitle, message });
  return { visible: true, title, subtitle, message };
}

/**
 * 在招聘平台页面右上角显示 OCR 关键词匹配浮层。
 * @param {Record<string, any>} payload - 关键词匹配展示参数。
 * @returns {Promise<Record<string, any>>} 浮层结果。
 */
async function keywordOverlay(payload) {
  const currentPage = await ensurePage();
  const action = String(payload.action || "show").trim().toLowerCase();
 if (action === "hide" || action === "close" || action === "remove") {
 await currentPage.evaluate(() => {
 if (window.__goodhrKeywordOverlayTimer) clearTimeout(window.__goodhrKeywordOverlayTimer);
 const box = document.getElementById("__goodhr_keyword_overlay");
 if (box) box.remove();
 }).catch(() => {});
    return { visible: false };
  }
  const title = String(payload.title || "关键词匹配").trim();
  const subtitle = String(payload.subtitle || "").trim();
  const keywords = cleanOverlayWords(payload.keywords);
  const excludes = cleanOverlayWords(payload.exclude_keywords || payload.excludes);
  const matchedKeywords = cleanOverlayWords(payload.matched_keywords);
  const matchedExcludes = cleanOverlayWords(payload.matched_excludes || payload.matched_exclude_keywords);
 const loading = Boolean(payload.loading);
 const text = String(payload.text || "").trim() || (loading ? "OCR图文识别中..." : "OCR 未识别到文字");
 const maxAgeMS = Math.max(3000, Math.min(60000, Number(payload.max_age_ms || payload.maxAgeMS || 20000)));
 await currentPage.evaluate(({ title, subtitle, keywords, excludes, matchedKeywords, matchedExcludes, text, maxAgeMS }) => {
    const chip = (word, color, bg) => {
      const item = document.createElement("span");
      item.textContent = word;
      item.style.cssText = "display:inline-flex;align-items:center;max-width:100%;padding:2px 7px;border-radius:999px;font-size:12px;font-weight:650;color:" + color + ";background:" + bg + ";overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
      return item;
    };
    const renderWords = (wrap, words, matched, color, bg) => {
      wrap.textContent = "";
      if (!words.length) {
        const empty = document.createElement("span");
        empty.textContent = "无";
        empty.style.cssText = "font-size:12px;color:#7b867f;";
        wrap.appendChild(empty);
        return;
      }
      const matchedSet = new Set(matched.map((word) => String(word).toLowerCase()));
      words.forEach((word) => {
        wrap.appendChild(chip(word, matchedSet.has(String(word).toLowerCase()) ? color : "#56635c", matchedSet.has(String(word).toLowerCase()) ? bg : "rgba(86,99,92,.10)"));
      });
    };
    const highlightText = (wrap, source, greenWords, redWords) => {
      wrap.textContent = "";
      const words = [...redWords.map((word) => ({ word, color: "#b4232c", bg: "rgba(180,35,44,.14)" })), ...greenWords.map((word) => ({ word, color: "#157347", bg: "rgba(21,115,71,.14)" }))].filter((item) => item.word);
      if (!words.length) {
        wrap.textContent = source;
        return;
      }
      const escaped = words.map((item) => item.word.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).sort((a, b) => b.length - a.length);
      const pattern = new RegExp("(" + escaped.join("|") + ")", "gi");
      let last = 0;
      source.replace(pattern, (match, _value, offset) => {
        if (offset > last) wrap.appendChild(document.createTextNode(source.slice(last, offset)));
        const found = words.find((item) => item.word.toLowerCase() === match.toLowerCase()) || words[0];
        const mark = document.createElement("span");
        mark.textContent = match;
        mark.style.cssText = "color:" + found.color + ";background:" + found.bg + ";border-radius:4px;padding:0 2px;font-weight:750;";
        wrap.appendChild(mark);
        last = offset + match.length;
        return match;
      });
      if (last < source.length) wrap.appendChild(document.createTextNode(source.slice(last)));
    };
 const removeOverlay = () => {
 const oldBox = document.getElementById("__goodhr_keyword_overlay");
 if (oldBox) oldBox.remove();
 };
 let box = document.getElementById("__goodhr_keyword_overlay");
    if (!box) {
      box = document.createElement("div");
      box.id = "__goodhr_keyword_overlay";
      document.body.appendChild(box);
    }
    const vw = Math.max(document.documentElement.clientWidth || 0, window.innerWidth || 0);
    const width = Math.min(360, Math.max(260, vw - 32));
 box.style.cssText = "position:fixed;right:16px;top:16px;z-index:2147483647;width:" + width + "px;box-sizing:border-box;padding:14px;border-radius:14px;background:rgba(252,250,244,.96);color:#18221d;box-shadow:0 18px 48px rgba(18,28,22,.22),0 2px 8px rgba(18,28,22,.10);font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;font-size:13px;line-height:1.45;pointer-events:none;border:1px solid rgba(48,79,63,.18);backdrop-filter:saturate(1.1) blur(10px);";
 if (window.__goodhrKeywordOverlayTimer) clearTimeout(window.__goodhrKeywordOverlayTimer);
 window.__goodhrKeywordOverlayTimer = setTimeout(removeOverlay, maxAgeMS);
 box.innerHTML = [
      '<div style="font-size:14px;font-weight:750;color:#18221d;"></div>',
      '<div style="font-size:12px;color:#6d7a72;margin-top:2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;"></div>',
      '<div style="height:1px;background:rgba(48,79,63,.12);margin:10px 0 9px;"></div>',
      '<div style="display:grid;grid-template-columns:42px 1fr;gap:7px 8px;align-items:start;"></div>',
      '<div style="height:1px;background:rgba(48,79,63,.12);margin:10px 0 9px;"></div>',
      '<div style="max-height:230px;overflow-y:auto;color:#405249;white-space:pre-wrap;word-break:break-word;"></div>',
    ].join("");
    box.children[0].textContent = title;
    box.children[1].textContent = subtitle;
    const grid = box.children[3];
    grid.innerHTML = '<div style="font-size:12px;color:#6d7a72;">关键词</div><div style="display:flex;gap:5px;flex-wrap:wrap;min-width:0;"></div><div style="font-size:12px;color:#6d7a72;">排除词</div><div style="display:flex;gap:5px;flex-wrap:wrap;min-width:0;"></div>';
    renderWords(grid.children[1], keywords, matchedKeywords, "#157347", "rgba(21,115,71,.14)");
    renderWords(grid.children[3], excludes, matchedExcludes, "#b4232c", "rgba(180,35,44,.14)");
    highlightText(box.children[5], text, matchedKeywords, matchedExcludes);
    box.children[5].scrollTop = 0;
 }, { title, subtitle, keywords, excludes, matchedKeywords, matchedExcludes, text, maxAgeMS });
  return { visible: true, title, subtitle };
}

/**
 * 读取页面截图尺寸。
 * @param {any} targetPage - Playwright 页面对象。
 * @param {boolean} fullPage - 是否整页截图。
 * @returns {Promise<{width:number,height:number}>} 截图尺寸。
 */
async function pageSize(targetPage, fullPage) {
  if (fullPage) {
    return targetPage.evaluate(() => ({
      width: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth || 0, window.innerWidth),
      height: Math.max(document.documentElement.scrollHeight, document.body?.scrollHeight || 0, window.innerHeight),
    })).catch(() => ({ width: 0, height: 0 }));
  }
  const viewport = targetPage.viewportSize?.();
  return viewport || { width: 0, height: 0 };
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
  targetPage.on("close", () => {
    if (page === targetPage) page = null;
    clearElementRefs();
  });
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
 * 清理浮层展示用关键词数组。
 * @param {any} value - 原始关键词数组。
 * @returns {string[]} 清理后的关键词。
 */
function cleanOverlayWords(value) {
  const items = Array.isArray(value) ? value : [];
  const seen = new Set();
  const result = [];
  for (const item of items) {
    const word = String(item || "").trim();
    const key = word.toLowerCase();
    if (!word || seen.has(key)) continue;
    seen.add(key);
    result.push(word);
  }
  return result;
}

/**
 * 返回随机滚动距离。
 * @param {Record<string, any>} payload - 滚动参数。
 * @returns {number} 滚动距离。
 */
function randomDistance(payload) {
  const min = Number(payload.distance_min || 0);
  const max = Number(payload.distance_max || 0);
  if (min > 0 && max >= min) return Math.round(min + Math.random() * (max - min));
  return Number(payload.distance || payload.y || 720);
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
    const classSelectors = classGroupSelectors(value.target_classes);
    return [...classSelectors, ...["selectors", "selector", "css"].flatMap((key) => selectorList(value[key]))];
  }
  return [];
}

/**
 * 将 class 组转换为 CSS 选择器。
 * @param {any} value - 二维 class 数组。
 * @returns {string[]} CSS 选择器数组。
 */
function classGroupSelectors(value) {
  if (!Array.isArray(value)) return [];
  const groups = Array.isArray(value[0]) ? value : [value];
  return groups.flatMap((group) => {
    const items = Array.isArray(group) ? group : [group];
    return items.map(normalizeClassSelector).filter(Boolean);
  });
}

/**
 * 规范化 class 或完整 CSS 选择器。
 * @param {any} value - class 名称或完整 CSS 选择器。
 * @returns {string} 可直接传给 Playwright locator 的选择器。
 */
function normalizeClassSelector(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  if (/^[.#[:>~+]/.test(text)) return text;
  if (/[ >~+:[\]()=]/.test(text)) return text;
  return `.${cssEscape(text)}`;
}

/**
 * 转义纯 CSS class 名称。
 * @param {string} value - 纯 class 名称。
 * @returns {string} 转义后的 class 名称。
 */
function cssEscape(value) {
  return String(value).replace(/[^a-zA-Z0-9_-]/g, (char) => `\\${char}`);
}

/**
 * 返回第一个匹配定位器。
 * @param {any} scope - 页面或 locator。
 * @param {any} element - 元素配置。
 * @param {boolean} visibleOnly - 是否只要可见元素。
 * @returns {Promise<any|null>} Playwright locator。
 */
async function firstLocator(scope, element, visibleOnly) {
  const locators = await allLocators(scope, element, visibleOnly, 1);
  return locators[0]?.locator || locators[0] || null;
}

/**
 * 返回全部匹配定位器。
 * @param {any} scope - 页面或 locator。
 * @param {any} element - 元素配置。
 * @param {boolean} visibleOnly - 是否只返回可见元素。
 * @param {number} limit - 最大数量，0 表示不限量。
 * @returns {Promise<any[]>} locator 数组。
 */
async function allLocators(scope, element, visibleOnly = true, limit = 200) {
  const unlimited = Number(limit || 0) <= 0;
  const selectors = selectorList(element);
  if (selectors.length <= 0) return [];
  const parentSelectors = parentSelectorList(element);
  const scopes = [];
  const searchContainers = searchContainerList(scope);
  if (parentSelectors.length > 0) {
    for (const container of searchContainers) {
      for (const parentSelector of parentSelectors) {
        const parents = container.scope.locator(parentSelector);
        const count = await parents.count().catch(() => 0);
        for (let index = 0; index < count && (unlimited || scopes.length < limit); index += 1) {
          scopes.push({ locator: parents.nth(index), includeSelf: true, parentSelector, frameURL: container.frameURL });
        }
      }
    }
  } else {
    for (const container of searchContainers) {
      scopes.push({ locator: container.scope, includeSelf: false, parentSelector: "", frameURL: container.frameURL });
    }
  }
  const result = [];
  for (const current of scopes) {
    const currentScope = current.locator;
    for (const selector of selectors) {
      if (current.includeSelf) {
        const selfMatches = await currentScope.evaluate((el, rawSelector) => {
          return Boolean(el && el.matches && el.matches(rawSelector));
        }, selector).catch(() => false);
        if (selfMatches && (!visibleOnly || await currentScope.isVisible().catch(() => false))) {
          result.push({ locator: currentScope, parentSelector: current.parentSelector || "", targetSelector: selector, frameURL: current.frameURL || "" });
          if (!unlimited && result.length >= limit) return result;
          continue;
        }
      }
      const locator = currentScope.locator(selector);
      const count = await locator.count().catch(() => 0);
      for (let index = 0; index < count && (unlimited || result.length < limit); index += 1) {
        const item = locator.nth(index);
        if (visibleOnly && !(await item.isVisible().catch(() => false))) continue;
        result.push({ locator: item, parentSelector: current.parentSelector || "", targetSelector: selector, frameURL: current.frameURL || "" });
      }
    }
  }
  return result;
}

/**
 * 返回用于查找元素的页面容器列表。
 * @param {any} scope - 页面、Frame 或 locator。
 * @returns {{scope:any, frameURL:string}[]} 查找容器列表。
 */
function searchContainerList(scope) {
  if (scope?.frames && typeof scope.frames === "function") {
    const frames = scope.frames() || [];
    const items = [{ scope, frameURL: scope.url?.() || "" }];
    for (const frame of frames) {
      if (frame === scope.mainFrame?.()) continue;
      items.push({ scope: frame, frameURL: frame.url?.() || "" });
    }
    return items;
  }
  return [{ scope, frameURL: "" }];
}

/**
 * 读取父级选择器列表。
 * @param {any} element - 元素配置。
 * @returns {string[]} 父级选择器列表。
 */
function parentSelectorList(element) {
  if (!element || typeof element !== "object") return [];
  return classGroupSelectors(element.parent_classes);
}

/**
 * 在 locator 内读取元素文本。
 * @param {any} scope - 页面或 locator。
 * @param {any} config - 元素配置。
 * @returns {Promise<string>} 文本。
 */
async function locatorText(scope, config) {
  const locator = await firstLocator(scope, config, true);
  if (!locator) return "";
  return (await locator.innerText({ timeout: 1000 }).catch(() => "")).trim();
}

/**
 * 按元素引用返回 locator。
 * @param {any} currentPage - 页面对象。
 * @param {string} ref - 元素引用。
 * @returns {any} locator。
 */
function locatorByRef(currentPage, ref) {
  const key = String(ref || "").trim();
  if (!key) return null;
  return elementRefs.get(key) || null;
}

/**
 * 记住本次扫描到的元素定位器，供后续详情和打招呼复用。
 * @param {any} locator - Playwright 元素定位器。
 * @returns {string} 元素引用编号。
 */
function rememberElement(locator) {
  const ref = `el_${Date.now()}_${elementRefSeq++}`;
  elementRefs.set(ref, locator);
  return ref;
}

/**
 * 清空页面元素引用缓存，避免跨页面复用旧定位器。
 * @returns {void} 无返回值。
 */
function clearElementRefs() {
  elementRefs.clear();
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
  "/api/v1/page/list": listPages,
  "/api/v1/page/use": usePage,
  "/api/v1/page/open": openPage,
  "/api/v1/page/click": clickPage,
  "/api/v1/page/type": typePage,
  "/api/v1/page/press-key": pressKey,
  "/api/v1/page/scroll": scrollPage,
  "/api/v1/page/extract-text": extractText,
  "/api/v1/page/find-elements": findElements,
  "/api/v1/page/list-click-by-index": listClickByIndex,
  "/api/v1/page/screenshot": screenshotPage,
  "/api/v1/page/ai-overlay": aiOverlay,
  "/api/v1/page/keyword-overlay": keywordOverlay,
  "/api/v1/page/cookies": importCookies,
  "/api/v1/boss/candidates/extract": extractBossCandidates,
  "/api/v1/boss/candidates/scroll": scrollBossCandidates,
  "/api/v1/boss/candidates/greet": greetBossCandidate,
  "/api/v1/boss/candidates/detail": extractBossCandidateDetail,
  "/api/v1/boss/candidates/detail/close": closeBossCandidateDetail,
};

const server = http.createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/health") {
    success(res, await workerHealth());
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
  if (req.method === "GET" && req.url === "/api/v1/page/url") {
    try {
      success(res, await currentPageURL());
    } catch (error) {
      failure(res, 500, error?.message || "读取当前页面地址失败");
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
    logWorker("收到 Worker API 请求", { path: req.url || "" });
    const data = await handler(payload);
    logWorker("Worker API 请求完成", { path: req.url || "" });
    success(res, data);
  } catch (error) {
    logWorker("Worker API 请求失败", { path: req.url || "", error: error?.message || error });
    failure(res, 500, error?.message || "浏览器操作失败");
  }
});

/**
 * 依次尝试监听 Worker 端口，避免旧 Worker 残留时直接启动失败。
 * @param {number} startPort - 首次尝试监听的端口。
 * @returns {void} 无返回值。
 */
function listenWithFallback(startPort) {
  const targetPort = Number(startPort || 9101);
  server.once("error", (error) => {
    if (error?.code !== "EADDRINUSE" || targetPort >= maxPort) {
      console.error("Node Worker 监听端口失败", error);
      process.exit(1);
      return;
    }
    console.error(`Node Worker 端口 ${targetPort} 已占用，尝试 ${targetPort + 1}`);
    listenWithFallback(targetPort + 1);
  });
  server.listen(targetPort, host, () => {
    console.log(`GoodHR Browser Worker started on http://${host}:${targetPort}`);
  });
}

listenWithFallback(port);

process.on("SIGINT", async () => {
  await stopBrowser();
  process.exit(0);
});
