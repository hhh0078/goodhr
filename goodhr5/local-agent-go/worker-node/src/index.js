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
const elementRefs = new Map();
let elementRefSeq = 0;

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
  const userDataDir = String(payload.user_data_dir || "").trim();
  if (browser || context || page) {
    if (!(await hasLiveBrowserSession())) {
      resetBrowserState();
    }
  }
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
    const viewport = { width: Number(payload.viewport_width), height: Number(payload.viewport_height) };
    options.viewport = viewport;
    options.args = [`--window-size=${viewport.width},${viewport.height}`];
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
    return { running: true, persistent: true, user_data_dir: userDataDir, downloads_path: options.downloadsPath, viewport: options.viewport };
  }
  if (!launch) throw new Error("CloakBrowser Node SDK 缺少启动方法");
  browser = await launch(options);
  context = await browser.newContext?.({ acceptDownloads: true }) || null;
  currentUserDataDir = "";
  currentDownloadsPath = options.downloadsPath;
  page = context ? await context.newPage() : await browser.newPage();
  registerPage(page);
  return { running: true, persistent: false, downloads_path: options.downloadsPath, viewport: options.viewport };
}

/**
 * 停止 CloakBrowser。
 * @returns {Promise<Record<string, any>>} 停止结果。
 */
async function stopBrowser() {
  if (context) await context.close().catch(() => {});
  if (browser) await browser.close().catch(() => {});
  resetBrowserState();
  return { running: false };
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
      if (!browser) return true;
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
      page = await context.newPage();
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
  const target = String(payload.url || "").trim();
  if (!target) throw new Error("页面地址不能为空");
  if (!browser && !context && (payload.user_data_dir || payload.persistent)) {
    await startBrowser(payload);
  }
  const currentPage = await ensurePage();
  clearElementRefs();
  await currentPage.goto(target, { waitUntil: "domcontentloaded", timeout: Number(payload.timeout || 60000) });
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
  const locators = await allLocators(currentPage, element, visibleOnly);
  const maxItems = Math.max(1, Math.min(200, Number(payload.max_items || 100)));
  const fields = Array.isArray(payload.fields) ? payload.fields : [];
  const items = [];
  for (let index = 0; index < Math.min(locators.length, maxItems); index += 1) {
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
  const platformConfig = payload.platform_config || payload.config || {};
  const rules = bossRules(platformConfig);
  const maxItems = Math.max(1, Math.min(100, Number(payload.max_items || 15)));
  const findResp = await findElements({
    element: rules.candidate_card,
    visible_only: true,
    fields: rules.field_requests,
    max_items: maxItems,
  });
  const candidates = [];
  for (const item of findResp.items || []) {
    try {
      const fields = item.fields || {};
      if (!fields.basic_info && item.text) fields.basic_info = item.text;
      const rawText = candidateRawText(fields);
      candidates.push({
        id: candidateID(fields, rawText, item.index),
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
  return { candidates, count: candidates.length };
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
  return { detail_text: detailText, text: detailText, screenshot, scroll_attempts: cardInfo.attempts };
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
  let cards = await allLocators(currentPage, rules.candidate_card, true, 200);
  let count = cards.length;
  const maxAttempts = Math.max(1, Math.min(12, Number(payload.card_scroll_attempts || 8)));
  const distance = Math.max(120, Number(payload.card_scroll_distance || payload.distance || 720));
  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    if (cardIndex >= count) {
      await scrollBossListByRules(currentPage, rules, distance);
      await currentPage.waitForTimeout(250);
      cards = await allLocators(currentPage, rules.candidate_card, true, 200);
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
    cards = await allLocators(currentPage, rules.candidate_card, true, 200);
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
  for (const selector of selectors) {
    try {
      const locator = currentPage.locator(selector).first();
      if ((await locator.count()) <= 0) continue;
      if (!(await locator.isVisible().catch(() => false))) continue;
      return screenshotLocatorWithParts(currentPage, locator, payload);
    } catch {
      continue;
    }
  }
  return screenshotPage({ ...payload, full_page: true, filename: payload.filename || "candidate-detail.png" });
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
  await locator.scrollIntoViewIfNeeded({ timeout: 1200 }).catch(() => {});
  const box = await locator.boundingBox().catch(() => null);
  const viewport = currentPage.viewportSize?.() || { width: 1280, height: 900 };
  if (!box || box.width < 20 || box.height < 20) return screenshotPage({ ...payload, filename });
  const scrollInfo = await detailScrollInfo(locator);
  if (scrollInfo.scrollable) {
    const parts = await screenshotScrollableLocatorParts(currentPage, locator, scrollInfo, directory, filename, payload);
    if (parts.length > 0) {
      return {
        ...parts[0],
        path: parts[0].path,
        file_path: parts[0].file_path,
        screenshot_parts: parts,
        parts_count: parts.length,
        overlap: parts[0].overlap || 0,
        scrollable_container: true,
      };
    }
  }
  const needsScroll = box.y < 0 || box.y + box.height > viewport.height;
  if (!needsScroll) return saveLocatorScreenshot(locator, directory, filename);
  const parts = await screenshotLocatorParts(currentPage, box, viewport, directory, filename, payload);
  if (parts.length <= 0) return saveLocatorScreenshot(locator, directory, filename);
  return {
    ...parts[0],
    path: parts[0].path,
    file_path: parts[0].file_path,
    screenshot_parts: parts,
    parts_count: parts.length,
    overlap: parts[0].overlap || 0,
  };
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
  const targetPath = await uniquePath(directory, filename);
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
  await currentPage.mouse.move(box.x + box.width / 2, box.y + Math.min(box.height / 2, 120)).catch(() => {});
  await currentPage.waitForTimeout(200);
  const clientHeight = Math.max(1, Number(scrollInfo.clientHeight || box.height || 1));
  const scrollHeight = Math.max(clientHeight, Number(scrollInfo.scrollHeight || clientHeight));
  const scrollDelta = Math.max(1, Math.round(clientHeight * 0.72));
  const overlap = Math.max(clientHeight - scrollDelta, 0);
  const configuredMax = Math.max(1, Math.min(16, Number(payload.max_scrolls || payload.screenshot_max_scrolls || 12)));
  const estimated = Math.max(1, Math.ceil(Math.max(scrollHeight - clientHeight, 0) / scrollDelta) + 1);
  const maxScrolls = Math.min(configuredMax, estimated);
  const parsed = path.parse(filename);
  const parts = [];
  const originalTop = Number(scrollInfo.scrollTop || 0);
  try {
    for (let index = 0; index < maxScrolls; index += 1) {
      const top = Math.min(index * scrollDelta, Math.max(scrollHeight - clientHeight, 0));
      await locator.evaluate((el, y) => {
        el.scrollTop = y;
      }, top).catch(() => {});
      await currentPage.waitForTimeout(350);
      const partName = `${parsed.name || "candidate-detail"}-part-${index + 1}${parsed.ext || ".png"}`;
      const targetPath = await uniquePath(directory, partName);
      const sizeInfo = await locator.boundingBox().catch(() => null) || box;
      await locator.screenshot({ path: targetPath, type: "png" });
      const stat = await fs.stat(targetPath);
      parts.push({
        path: targetPath,
        file_path: targetPath,
        size: stat.size,
        width: Math.round(sizeInfo.width || box.width || 0),
        height: Math.round(sizeInfo.height || box.height || 0),
        overlap,
        index,
        scroll_top: Math.round(top),
      });
      if (top >= scrollHeight - clientHeight - 2) break;
    }
  } finally {
    await locator.evaluate((el, y) => {
      el.scrollTop = y;
    }, originalTop).catch(() => {});
  }
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
  await currentPage.mouse.move(clipX + clipWidth / 2, clipY + clipHeight / 2).catch(() => {});
  await currentPage.waitForTimeout(300);
  const scrollDelta = Math.max(Math.round(clipHeight * 0.7), 1);
  const overlap = Math.max(clipHeight - scrollDelta, 0);
  const configuredMax = Math.max(1, Math.min(12, Number(payload.max_scrolls || payload.screenshot_max_scrolls || 10)));
  const estimated = Math.max(1, Math.ceil(Math.max(box.height - clipHeight, 0) / scrollDelta) + 1);
  const maxScrolls = Math.min(configuredMax, estimated);
  const parsed = path.parse(filename);
  const parts = [];
  for (let index = 0; index < maxScrolls; index += 1) {
    const partName = `${parsed.name || "candidate-detail"}-part-${index + 1}${parsed.ext || ".png"}`;
    const targetPath = await uniquePath(directory, partName);
    await currentPage.screenshot({ path: targetPath, clip, type: "png" });
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
    await currentPage.mouse.wheel(0, scrollDelta);
    await currentPage.waitForTimeout(500);
  }
  return parts;
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
async function screenshotPage(payload) {
  const currentPage = await ensurePage();
  const filename = safeFilename(String(payload.filename || "page-screenshot.png"));
  const directory = String(payload.dir || payload.directory || path.join(os.tmpdir(), "goodhr-screenshots"));
  await fs.mkdir(directory, { recursive: true });
  const targetPath = path.join(directory, filename);
  const selector = firstSelector(payload);
  let sizeInfo = { width: 0, height: 0 };
  if (selector) {
    const locator = currentPage.locator(selector).first();
    if (await locator.count() <= 0) throw new Error("截图元素不存在");
    sizeInfo = await locator.boundingBox().catch(() => null) || sizeInfo;
    await locator.screenshot({ path: targetPath, type: "png" });
  } else {
    sizeInfo = await pageSize(currentPage, Boolean(payload.full_page));
    await currentPage.screenshot({ path: targetPath, fullPage: Boolean(payload.full_page), type: "png" });
  }
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
  if (action === "hide" || action === "close" || action === "remove") {
    await currentPage.evaluate(() => {
      document.getElementById("goodhr-ai-thinking-overlay")?.remove();
    }).catch(() => {});
    return { visible: false };
  }
  const title = String(payload.title || "AI 正在思考").trim();
  const target = String(payload.target || "").trim();
  const message = String(payload.message || "正在分析候选人，请稍候").trim();
  await currentPage.evaluate(({ title, target, message }) => {
    let box = document.getElementById("goodhr-ai-thinking-overlay");
    if (!box) {
      box = document.createElement("div");
      box.id = "goodhr-ai-thinking-overlay";
      box.style.cssText = [
        "position:fixed",
        "right:18px",
        "top:18px",
        "z-index:2147483647",
        "width:min(340px,calc(100vw - 36px))",
        "box-sizing:border-box",
        "padding:14px 16px",
        "border-radius:10px",
        "background:#111814",
        "color:#f7f4ec",
        "box-shadow:0 14px 36px rgba(0,0,0,.28)",
        "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif",
        "font-size:13px",
        "line-height:1.45",
        "pointer-events:none",
        "border:1px solid rgba(255,255,255,.12)",
      ].join(";");
      const style = document.createElement("style");
      style.id = "goodhr-ai-thinking-style";
      style.textContent = `
        @keyframes goodhrAiPulse {
          0% { transform: scale(.7); opacity:.45; }
          50% { transform: scale(1); opacity:1; }
          100% { transform: scale(.7); opacity:.45; }
        }
        #goodhr-ai-thinking-overlay .goodhr-ai-dot {
          width:7px;height:7px;border-radius:50%;background:#e7c86f;display:inline-block;
          animation:goodhrAiPulse 1s infinite ease-in-out;
        }
        #goodhr-ai-thinking-overlay .goodhr-ai-dot:nth-child(2){animation-delay:.16s}
        #goodhr-ai-thinking-overlay .goodhr-ai-dot:nth-child(3){animation-delay:.32s}
      `;
      document.head.appendChild(style);
      box.innerHTML = `
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px;">
          <span class="goodhr-ai-dot"></span><span class="goodhr-ai-dot"></span><span class="goodhr-ai-dot"></span>
          <strong data-goodhr-ai-title style="font-size:14px;font-weight:700;color:#fff8dd;"></strong>
        </div>
        <div data-goodhr-ai-target style="font-weight:600;margin-bottom:5px;color:#f0df9b;"></div>
        <div data-goodhr-ai-message style="white-space:pre-wrap;color:#e7e0d1;"></div>
      `;
      document.body.appendChild(box);
    }
    box.querySelector("[data-goodhr-ai-title]").textContent = title;
    box.querySelector("[data-goodhr-ai-target]").textContent = target;
    box.querySelector("[data-goodhr-ai-message]").textContent = message;
  }, { title, target, message });
  return { visible: true, title, target, message };
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
 * @param {number} limit - 最大数量。
 * @returns {Promise<any[]>} locator 数组。
 */
async function allLocators(scope, element, visibleOnly = true, limit = 200) {
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
        for (let index = 0; index < count && scopes.length < limit; index += 1) {
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
          if (result.length >= limit) return result;
          continue;
        }
      }
      const locator = currentScope.locator(selector);
      const count = await locator.count().catch(() => 0);
      for (let index = 0; index < count && result.length < limit; index += 1) {
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
  "/api/v1/page/cookies": importCookies,
  "/api/v1/boss/candidates/extract": extractBossCandidates,
  "/api/v1/boss/candidates/scroll": scrollBossCandidates,
  "/api/v1/boss/candidates/greet": greetBossCandidate,
  "/api/v1/boss/candidates/detail": extractBossCandidateDetail,
  "/api/v1/boss/candidates/detail/close": closeBossCandidateDetail,
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
