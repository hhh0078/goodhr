// 文件作用说明：整理 GoodHR Browser Worker 的浏览器基础操作、高级组合操作、浮层操作和下载操作。
//
// 功能清单：
// 1. BrowserBaseActions：浏览器和页面的最原始能力，包括启动/关闭浏览器、打开 URL、管理标签页、鼠标、键盘、截图、Cookie、元素引用。
// 2. BrowserAdvancedActions：基于基础能力组合出来的通用动作，包括移动到元素、拟人化点击、滚轮滚动、查找元素、提取文本、点击列表项、等待元素进入视口。
// 3. BrowserOverlayActions：页面内浮层能力，包括显示/关闭 AI 提示浮层、关键词匹配浮层和通用右上角提示卡片。
// 4. BrowserDownloadActions：下载能力，包括注册下载监听、保存下载文件、补全后缀、生成唯一文件名、维护下载记录。
// 5. createBrowserActionKit：快速创建上述四类能力，方便后续从 index.js 逐步迁移。
//
// 边界说明：
// 这个文件只放浏览器通用能力，不放 Boss、猎聘、智联等招聘平台的个性化流程。
// 平台动作应该单独建平台 action 文件，然后调用这里的基础类和高级类。

import crypto from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

/**
 * BrowserBaseActions 提供最基础的浏览器原子操作。
 * 这些方法尽量只做一件事，给高级类和平台类组合使用。
 */
export class BrowserBaseActions {
  /**
   * 创建基础浏览器操作实例。
   * @param {Record<string, any>} options - 启动器、日志器和默认下载目录。
   */
  constructor(options = {}) {
    this.browser = null;
    this.context = null;
    this.page = null;
    this.currentUserDataDir = "";
    this.currentDownloadsPath = "";
    this.elementRefs = new Map();
    this.elementRefSeq = 0;
    this.launcher = options.launcher || null;
    this.logger = options.logger || console;
    this.defaultDownloadsPath =
      options.downloadsPath || path.join(os.homedir(), "Downloads");
  }

  /**
   * 设置日志器。
   * @param {{log?:Function,error?:Function,warn?:Function}} logger - 外部日志器。
   * @returns {void} 无返回值。
   */
  setLogger(logger) {
    this.logger = logger || console;
  }

  /**
   * 写入普通日志。
   * @param {string} message - 日志内容。
   * @param {Record<string, any>} data - 附加字段。
   * @returns {void} 无返回值。
   */
  log(message, data = {}) {
    const fields = Object.entries(data)
      .filter(([, value]) => value !== undefined && value !== null && value !== "")
      .map(([key, value]) => `${key}=${String(value).slice(0, 240)}`);
    this.logger?.log?.(
      `[${new Date().toISOString()}] ${message}${fields.length ? ` ${fields.join(" ")}` : ""}`,
    );
  }

  /**
   * 解析浏览器启动器。
   * @returns {Promise<Record<string, any>>} CloakBrowser 或外部注入的启动器。
   */
  async resolveLauncher() {
    if (this.launcher) return this.launcher;
    const module = await import("cloakbrowser");
    this.launcher = module.default || module;
    return this.launcher;
  }

  /**
   * 启动浏览器。
   * @param {Record<string, any>} payload - 启动参数，支持 user_data_dir、downloads_path、viewport_width、viewport_height、headless、url。
   * @returns {Promise<Record<string, any>>} 浏览器启动结果。
   */
  async startBrowser(payload = {}) {
    const startedAt = Date.now();
    const userDataDir = stringValue(payload.user_data_dir);
    if (await this.hasLiveBrowserSession()) {
      if (!userDataDir || userDataDir === this.currentUserDataDir) {
        this.registerContext(this.context);
        this.registerPage(this.page);
        return {
          running: true,
          persistent: Boolean(this.currentUserDataDir),
          user_data_dir: this.currentUserDataDir,
          reused: true,
        };
      }
      await this.stopBrowser();
    }

    const launcher = await this.resolveLauncher();
    const launchOptions = this.buildLaunchOptions(payload);
    if (userDataDir) {
      await fs.mkdir(userDataDir, { recursive: true });
      if (typeof launcher.launchPersistentContext !== "function") {
        throw new Error("当前浏览器启动器不支持 launchPersistentContext");
      }
      this.context = await launcher.launchPersistentContext(
        userDataDir,
        launchOptions,
      );
      this.browser =
        typeof this.context.browser === "function" ? this.context.browser() : null;
      this.currentUserDataDir = userDataDir;
      this.currentDownloadsPath = launchOptions.downloadsPath || "";
      this.registerContext(this.context);
      this.page = this.pickOpenPage() || (await this.context.newPage());
      this.registerPage(this.page);
    } else {
      if (typeof launcher.launch !== "function") {
        throw new Error("当前浏览器启动器不支持 launch");
      }
      this.browser = await launcher.launch(launchOptions);
      this.context =
        typeof this.browser.newContext === "function"
          ? await this.browser.newContext({
              acceptDownloads: true,
              viewport: launchOptions.viewport,
            })
          : null;
      this.currentUserDataDir = "";
      this.currentDownloadsPath = launchOptions.downloadsPath || "";
      this.registerContext(this.context);
      this.page = this.context
        ? await this.context.newPage()
        : await this.browser.newPage();
      this.registerPage(this.page);
    }

    if (payload.url) {
      await this.openURL(payload.url, payload);
    }
    return {
      running: true,
      persistent: Boolean(this.currentUserDataDir),
      user_data_dir: this.currentUserDataDir,
      downloads_path: this.currentDownloadsPath,
      elapsed_ms: Date.now() - startedAt,
    };
  }

  /**
   * 生成浏览器启动参数。
   * @param {Record<string, any>} payload - 原始启动参数。
   * @returns {Record<string, any>} 传给启动器的参数。
   */
  buildLaunchOptions(payload = {}) {
    const width = positiveNumber(payload.viewport_width) || 1280;
    const height = positiveNumber(payload.viewport_height) || 900;
    return {
      headless: Boolean(payload.headless),
      acceptDownloads: true,
      downloadsPath:
        stringValue(payload.downloads_path) || this.defaultDownloadsPath,
      viewport: { width, height },
      args: Array.isArray(payload.args) ? payload.args : [],
    };
  }

  /**
   * 停止浏览器。
   * @returns {Promise<Record<string, any>>} 停止结果。
   */
  async stopBrowser() {
    await this.disposeBrowserState();
    return { running: false };
  }

  /**
   * 关闭浏览器并清空本地状态。
   * @returns {Promise<void>} 无返回值。
   */
  async disposeBrowserState() {
    const oldContext = this.context;
    const oldBrowser = this.browser;
    this.resetBrowserState();
    if (oldContext) await oldContext.close().catch(() => {});
    if (oldBrowser) await oldBrowser.close().catch(() => {});
  }

  /**
   * 清空浏览器对象和元素引用。
   * @returns {void} 无返回值。
   */
  resetBrowserState() {
    this.browser = null;
    this.context = null;
    this.page = null;
    this.currentUserDataDir = "";
    this.currentDownloadsPath = "";
    this.clearElementRefs();
  }

  /**
   * 判断浏览器会话是否可用。
   * @returns {Promise<boolean>} 可用返回 true。
   */
  async hasLiveBrowserSession() {
    if (this.page && !this.page.isClosed?.()) return true;
    if (this.context) {
      const page = this.pickOpenPage();
      if (page) {
        this.page = page;
        this.registerPage(this.page);
        return true;
      }
    }
    if (this.browser && typeof this.browser.isConnected === "function") {
      return this.browser.isConnected();
    }
    return Boolean(this.browser || this.context);
  }

  /**
   * 确保当前页面存在。
   * @returns {Promise<any>} Playwright 页面对象。
   */
  async ensurePage() {
    if (this.page && !this.page.isClosed?.()) return this.page;
    this.page = this.pickOpenPage();
    if (this.page) {
      this.registerPage(this.page);
      return this.page;
    }
    if (this.context) {
      this.page = await this.context.newPage();
      this.registerPage(this.page);
      return this.page;
    }
    throw new Error("浏览器未启动");
  }

  /**
   * 从上下文中挑选一个可用页面。
   * @returns {any|null} 页面对象或空。
   */
  pickOpenPage() {
    const pages = this.context?.pages?.() || [];
    return pages.find((item) => !item.isClosed?.()) || null;
  }

  /**
   * 注册浏览器上下文事件。
   * @param {any} targetContext - Playwright context。
   * @returns {void} 无返回值。
   */
  registerContext(targetContext) {
    if (!targetContext || targetContext.__goodhrBaseRegistered) return;
    targetContext.__goodhrBaseRegistered = true;
    targetContext.on?.("page", (newPage) => this.registerPage(newPage));
  }

  /**
   * 注册页面基础事件。
   * @param {any} targetPage - Playwright page。
   * @returns {void} 无返回值。
   */
  registerPage(targetPage) {
    if (!targetPage || targetPage.__goodhrBaseRegistered) return;
    targetPage.__goodhrBaseRegistered = true;
    targetPage.on?.("close", () => {
      if (this.page === targetPage) this.page = null;
      this.clearElementRefs();
    });
  }

  /**
   * 打开 URL。
   * @param {string} url - 目标地址。
   * @param {Record<string, any>} options - 导航选项。
   * @returns {Promise<Record<string, any>>} 页面地址结果。
   */
  async openURL(url, options = {}) {
    const currentPage = await this.ensurePage();
    const waitUntil = options.wait_until || options.waitUntil || "domcontentloaded";
    const timeout = positiveNumber(options.timeout) || 30000;
    await currentPage.goto(String(url), { waitUntil, timeout });
    return { url: currentPage.url() };
  }

  /**
   * 新建标签页。
   * @param {Record<string, any>} options - 可选 URL 和导航参数。
   * @returns {Promise<Record<string, any>>} 新标签页信息。
   */
  async openTab(options = {}) {
    if (!this.context) throw new Error("浏览器未启动，无法新建标签页");
    const nextPage = await this.context.newPage();
    this.page = nextPage;
    this.registerPage(nextPage);
    if (options.url) await this.openURL(options.url, options);
    return { page_id: String(this.pageIndex(nextPage)), url: nextPage.url() };
  }

  /**
   * 列出所有标签页。
   * @returns {Promise<Record<string, any>>} 标签页列表。
   */
  async listPages() {
    if (!this.context) throw new Error("浏览器未启动，无法读取页面列表");
    const pages = this.context.pages?.() || [];
    const items = pages.map((item, index) => ({
      page_id: String(index),
      url: pageURL(item),
      is_default: item === this.page,
    }));
    return { pages: items, count: items.length };
  }

  /**
   * 切换当前标签页。
   * @param {Record<string, any>} payload - page_id 或 index。
   * @returns {Promise<Record<string, any>>} 切换后的页面信息。
   */
  async usePage(payload = {}) {
    if (!this.context) throw new Error("浏览器未启动，无法切换页面");
    const pages = this.context.pages?.() || [];
    const index = Number(payload.page_id ?? payload.index ?? 0);
    const nextPage = pages[index];
    if (!nextPage || nextPage.isClosed?.()) throw new Error("指定页面不存在");
    this.page = nextPage;
    this.registerPage(nextPage);
    return { page_id: String(index), url: pageURL(nextPage) };
  }

  /**
   * 返回页面序号。
   * @param {any} targetPage - 页面对象。
   * @returns {number} 页面序号。
   */
  pageIndex(targetPage) {
    const pages = this.context?.pages?.() || [];
    return Math.max(0, pages.indexOf(targetPage));
  }

  /**
   * 读取当前 URL。
   * @returns {Promise<Record<string, any>>} URL 结果。
   */
  async currentURL() {
    const currentPage = await this.ensurePage();
    return { url: pageURL(currentPage) };
  }

  /**
   * 移动鼠标。
   * @param {number} x - 视口 X 坐标。
   * @param {number} y - 视口 Y 坐标。
   * @param {Record<string, any>} options - 鼠标移动选项。
   * @returns {Promise<Record<string, any>>} 鼠标位置。
   */
  async moveMouse(x, y, options = {}) {
    const currentPage = await this.ensurePage();
    const steps = positiveNumber(options.steps) || 1;
    await currentPage.mouse.move(Number(x), Number(y), { steps });
    return { moved: true, x: Number(x), y: Number(y), steps };
  }

  /**
   * 按下鼠标。
   * @param {Record<string, any>} options - 鼠标按键选项。
   * @returns {Promise<Record<string, any>>} 按下结果。
   */
  async mouseDown(options = {}) {
    const currentPage = await this.ensurePage();
    const button = options.button || "left";
    await currentPage.mouse.down({ button });
    return { down: true, button };
  }

  /**
   * 松开鼠标。
   * @param {Record<string, any>} options - 鼠标按键选项。
   * @returns {Promise<Record<string, any>>} 松开结果。
   */
  async mouseUp(options = {}) {
    const currentPage = await this.ensurePage();
    const button = options.button || "left";
    await currentPage.mouse.up({ button });
    return { up: true, button };
  }

  /**
   * 点击鼠标。
   * @param {number} x - 视口 X 坐标。
   * @param {number} y - 视口 Y 坐标。
   * @param {Record<string, any>} options - 点击选项。
   * @returns {Promise<Record<string, any>>} 点击结果。
   */
  async clickMouse(x, y, options = {}) {
    const currentPage = await this.ensurePage();
    await currentPage.mouse.click(Number(x), Number(y), {
      button: options.button || "left",
      clickCount: positiveNumber(options.click_count) || 1,
      delay: positiveNumber(options.delay_ms) || 0,
    });
    return { clicked: true, x: Number(x), y: Number(y) };
  }

  /**
   * 滚动鼠标滚轮。
   * @param {number} deltaX - 横向滚动距离。
   * @param {number} deltaY - 纵向滚动距离。
   * @returns {Promise<Record<string, any>>} 滚动结果。
   */
  async wheel(deltaX, deltaY) {
    const currentPage = await this.ensurePage();
    await currentPage.mouse.wheel(Number(deltaX || 0), Number(deltaY || 0));
    return { scrolled: true, delta_x: Number(deltaX || 0), delta_y: Number(deltaY || 0) };
  }

  /**
   * 按键盘。
   * @param {string} key - 按键名称。
   * @param {Record<string, any>} options - 按键选项。
   * @returns {Promise<Record<string, any>>} 按键结果。
   */
  async pressKey(key, options = {}) {
    const currentPage = await this.ensurePage();
    await currentPage.keyboard.press(String(key || "Enter"), {
      delay: positiveNumber(options.delay_ms) || 0,
    });
    return { pressed: true, key: String(key || "Enter") };
  }

  /**
   * 输入文本。
   * @param {string} text - 文本内容。
   * @param {Record<string, any>} options - 输入选项。
   * @returns {Promise<Record<string, any>>} 输入结果。
   */
  async typeText(text, options = {}) {
    const currentPage = await this.ensurePage();
    await currentPage.keyboard.type(String(text || ""), {
      delay: positiveNumber(options.delay_ms) || 0,
    });
    return { typed: true, length: String(text || "").length };
  }

  /**
   * 页面截图。
   * @param {string} filePath - 保存路径。
   * @param {Record<string, any>} options - 截图选项。
   * @returns {Promise<Record<string, any>>} 截图文件信息。
   */
  async screenshot(filePath, options = {}) {
    const currentPage = await this.ensurePage();
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await currentPage.screenshot({
      path: filePath,
      fullPage: Boolean(options.full_page || options.fullPage),
      type: "png",
    });
    const stat = await fs.stat(filePath);
    return { path: filePath, file_path: filePath, size: stat.size };
  }

  /**
   * 导入 Cookie。
   * @param {Record<string, any>} payload - Cookie 配置。
   * @returns {Promise<Record<string, any>>} 导入结果。
   */
  async importCookies(payload = {}) {
    if (!this.context) throw new Error("浏览器未启动，无法导入 Cookie");
    const cookies = Array.isArray(payload.cookies) ? payload.cookies : [];
    if (cookies.length > 0) await this.context.addCookies(cookies);
    return { imported: cookies.length };
  }

  /**
   * 导出 Cookie。
   * @returns {Promise<Record<string, any>>} Cookie 列表。
   */
  async exportCookies() {
    if (!this.context) throw new Error("浏览器未启动，无法导出 Cookie");
    const cookies = await this.context.cookies();
    return { cookies, count: cookies.length };
  }

  /**
   * 记住元素引用。
   * @param {any} locator - Playwright locator。
   * @returns {string} 元素引用编号。
   */
  rememberElement(locator) {
    const ref = `el_${Date.now()}_${this.elementRefSeq++}`;
    this.elementRefs.set(ref, locator);
    return ref;
  }

  /**
   * 根据引用读取元素。
   * @param {string} ref - 元素引用编号。
   * @returns {any|null} Playwright locator 或空。
   */
  locatorByRef(ref) {
    const key = stringValue(ref);
    return key ? this.elementRefs.get(key) || null : null;
  }

  /**
   * 清空元素引用。
   * @returns {void} 无返回值。
   */
  clearElementRefs() {
    this.elementRefs.clear();
  }
}

/**
 * BrowserAdvancedActions 提供由基础能力组合出来的通用高级操作。
 */
export class BrowserAdvancedActions {
  /**
   * 创建高级操作实例。
   * @param {BrowserBaseActions} base - 基础操作实例。
   */
  constructor(base) {
    this.base = base;
  }

  /**
   * 获取当前页面。
   * @returns {Promise<any>} Playwright page。
   */
  async page() {
    return this.base.ensurePage();
  }

  /**
   * 移动鼠标到矩形范围内的随机点。
   * @param {Record<string, any>} box - 目标矩形。
   * @param {Record<string, any>} options - 移动选项。
   * @returns {Promise<Record<string, any>>} 鼠标移动结果。
   */
  async moveMouseToBox(box, options = {}) {
    const safeBox = normalizeMouseTargetBox(box);
    const paddingRatio = Number(options.padding_ratio ?? options.paddingRatio ?? 0.2);
    const point = randomPointInBox(safeBox, paddingRatio);
    const minSteps = Math.max(1, Number(options.min_steps || 8));
    const maxSteps = Math.max(minSteps, Number(options.max_steps || 18));
    const steps = Math.round(minSteps + Math.random() * (maxSteps - minSteps));
    const moved = await this.base.moveMouse(point.x, point.y, { steps });
    return {
      ...moved,
      box: safeBox,
      padding_ratio: Math.max(0, Math.min(0.45, paddingRatio || 0)),
      padding_x: point.paddingX,
      padding_y: point.paddingY,
    };
  }

  /**
   * 移动鼠标到元素上。
   * @param {any} element - locator 或元素配置。
   * @param {Record<string, any>} options - 移动选项。
   * @returns {Promise<Record<string, any>>} 鼠标移动结果。
   */
  async moveMouseToElement(element, options = {}) {
    const currentPage = await this.page();
    const locator = isLocatorLike(element)
      ? element
      : await this.firstLocator(currentPage, element, true);
    if (!locator) throw new Error("鼠标移动目标选择器不能为空或未找到元素");
    const view = await this.isElementInViewport(locator, {
      margin: options.viewport_margin || 0,
      full: options.require_full,
    });
    if (!view.visible) throw new Error("鼠标移动目标元素不可见");
    const box = await locator.boundingBox().catch(() => null);
    if (!box || box.width <= 0 || box.height <= 0) {
      throw new Error("鼠标移动目标元素没有有效位置");
    }
    const move = await this.moveMouseToBox(
      {
        x1: box.x,
        y1: box.y,
        x2: box.x + box.width,
        y2: box.y + box.height,
      },
      options,
    );
    return { ...move, locator_visible: view };
  }

  /**
   * 执行拟人化鼠标点击。
   * @param {Record<string, any>} options - 点击选项。
   * @returns {Promise<Record<string, any>>} 点击结果。
   */
  async humanMouseClick(options = {}) {
    const currentPage = await this.page();
    const minDown = Math.max(20, Number(options.down_min_ms || 80));
    const maxDown = Math.max(minDown, Number(options.down_max_ms || 220));
    const holdMs = Math.round(minDown + Math.random() * (maxDown - minDown));
    const button = String(options.button || "left");
    await currentPage.mouse.down({ button });
    await currentPage.waitForTimeout(holdMs);
    await currentPage.mouse.up({ button });
    return { clicked: true, button, hold_ms: holdMs };
  }

  /**
   * 移动到元素并点击。
   * @param {any} element - locator 或元素配置。
   * @param {Record<string, any>} options - 点击选项。
   * @returns {Promise<Record<string, any>>} 点击结果。
   */
  async clickElement(element, options = {}) {
    const mouse = await this.moveMouseToElement(element, options);
    const click = await this.humanMouseClick(options);
    return { clicked: true, mouse, click };
  }

  /**
   * 点击列表中的第 N 项。
   * @param {Record<string, any>} payload - 列表元素、序号和子级点击目标。
   * @returns {Promise<Record<string, any>>} 点击结果。
   */
  async clickListItem(payload = {}) {
    const currentPage = await this.page();
    const element = payload.element || payload.item || payload;
    const index = Math.max(0, Number(payload.index || 0));
    const locators = await this.allLocators(currentPage, element, true);
    const target = locators[index]?.locator || locators[index];
    if (!target) throw new Error("指定列表项不存在");
    const clickTarget = payload.click_target || payload.clickTarget;
    const nested = clickTarget ? await this.firstLocator(target, clickTarget, true) : null;
    const result = await this.clickElement(nested || target, payload);
    return { ...result, index };
  }

  /**
   * 滚动页面或元素。
   * @param {Record<string, any>} payload - 滚动参数。
   * @returns {Promise<Record<string, any>>} 滚动结果。
   */
  async scroll(payload = {}) {
    const currentPage = await this.page();
    const distance = randomDistance(payload);
    const locator = await this.firstLocator(
      currentPage,
      payload.element || payload,
      true,
    );
    if (locator) {
      const mouse = await this.moveMouseToElement(locator, payload);
      await currentPage.mouse.wheel(0, distance);
      return { scrolled: true, distance, target: "element", mouse };
    }
    await currentPage.mouse.wheel(0, distance);
    return { scrolled: true, distance, target: "page" };
  }

  /**
   * 持续滚轮滚动，直到元素进入视口。
   * @param {any} targetLocator - 目标元素 locator。
   * @param {any} wheelTarget - 滚轮停留目标。
   * @param {Record<string, any>} options - 滚动选项。
   * @returns {Promise<Record<string, any>>} 滚动检测结果。
   */
  async wheelUntilElementVisible(targetLocator, wheelTarget, options = {}) {
    const currentPage = await this.page();
    const maxAttempts = Math.max(1, Number(options.max_attempts || 6));
    const distance = Number(options.distance || options.y || 720);
    let lastView = null;
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      lastView = await this.isElementInViewport(targetLocator, options);
      if (lastView.in_viewport) {
        return { visible: true, attempts: attempt, view: lastView };
      }
      const target = isLocatorLike(wheelTarget)
        ? wheelTarget
        : await this.firstLocator(currentPage, wheelTarget, true);
      if (target) await this.moveMouseToElement(target, options).catch(() => {});
      await currentPage.mouse.wheel(0, distance);
      await currentPage.waitForTimeout(Number(options.wait_ms || 450));
    }
    return { visible: false, attempts: maxAttempts, view: lastView };
  }

  /**
   * 判断元素是否在浏览器可视范围内。
   * @param {any} locator - Playwright locator。
   * @param {Record<string, any>} options - 检测选项。
   * @returns {Promise<Record<string, any>>} 可视检测结果。
   */
  async isElementInViewport(locator, options = {}) {
    const visible = await locator.isVisible().catch(() => false);
    if (!visible) return { visible: false, in_viewport: false, reason: "not-visible" };
    const box = await locator.boundingBox().catch(() => null);
    if (!box || box.width <= 0 || box.height <= 0) {
      return { visible: true, in_viewport: false, reason: "no-box", box };
    }
    const pageForViewport =
      typeof locator.page === "function" ? locator.page() : await this.page();
    const viewport = pageForViewport?.viewportSize?.() || { width: 1280, height: 900 };
    const margin = Math.max(0, Number(options.margin || 0));
    const requireFull = Boolean(options.full || options.require_full);
    const left = box.x;
    const right = box.x + box.width;
    const top = box.y;
    const bottom = box.y + box.height;
    const partiallyVisible =
      right > margin &&
      bottom > margin &&
      left < viewport.width - margin &&
      top < viewport.height - margin;
    const fullyVisible =
      left >= margin &&
      top >= margin &&
      right <= viewport.width - margin &&
      bottom <= viewport.height - margin;
    return {
      visible: true,
      in_viewport: requireFull ? fullyVisible : partiallyVisible,
      partially_visible: partiallyVisible,
      fully_visible: fullyVisible,
      box,
      viewport,
    };
  }

  /**
   * 提取页面或元素文本。
   * @param {Record<string, any>} payload - 文本提取参数。
   * @returns {Promise<Record<string, any>>} 文本结果。
   */
  async extractText(payload = {}) {
    const currentPage = await this.page();
    const element = payload.element || payload;
    const locators = await this.allLocators(currentPage, element, false, 1);
    const locator = locators[0]?.locator || locators[0];
    if (!locator) {
      const text = await currentPage.locator("body").innerText({
        timeout: positiveNumber(payload.timeout) || 10000,
      });
      return { text, texts: text ? [text] : [], found: true, count: 1, selector: "body" };
    }
    const text = await locator.innerText({
      timeout: positiveNumber(payload.timeout) || 10000,
    });
    return { text, texts: text ? [text] : [], found: true, count: locators.length };
  }

  /**
   * 查找元素并提取字段。
   * @param {Record<string, any>} payload - 查找参数。
   * @returns {Promise<Record<string, any>>} 元素列表。
   */
  async findElements(payload = {}) {
    const currentPage = await this.page();
    const element = payload.element || payload.item || payload;
    const visibleOnly = payload.visible_only !== false;
    const maxItems = Math.max(0, Number(payload.max_items || 0));
    const locators = await this.allLocators(currentPage, element, visibleOnly, maxItems || 200);
    const fields = Array.isArray(payload.fields) ? payload.fields : [];
    const total = maxItems > 0 ? Math.min(locators.length, maxItems) : locators.length;
    const items = [];
    for (let index = 0; index < total; index += 1) {
      const meta = locators[index];
      const locator = meta.locator || meta;
      const extracted = {};
      for (const field of fields) {
        if (!field || typeof field !== "object") continue;
        for (const [name, config] of Object.entries(field)) {
          extracted[name] = await this.locatorText(locator, config);
        }
      }
      const ref = this.base.rememberElement(locator);
      items.push({
        index,
        ref,
        element_ref: ref,
        text: await locator.innerText({ timeout: 1000 }).catch(() => ""),
        fields: extracted,
        selector: meta.targetSelector || "",
        parent_selector: meta.parentSelector || "",
        frame_url: meta.frameURL || "",
      });
    }
    return { items, count: locators.length };
  }

  /**
   * 元素截图。
   * @param {any} element - locator 或元素配置。
   * @param {string} filePath - 保存路径。
   * @returns {Promise<Record<string, any>>} 截图文件信息。
   */
  async screenshotElement(element, filePath) {
    const currentPage = await this.page();
    const locator = isLocatorLike(element)
      ? element
      : await this.firstLocator(currentPage, element, true);
    if (!locator) throw new Error("截图元素不存在");
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await locator.screenshot({ path: filePath, type: "png" });
    const box = await locator.boundingBox().catch(() => null);
    const stat = await fs.stat(filePath);
    return {
      path: filePath,
      file_path: filePath,
      size: stat.size,
      width: Math.round(box?.width || 0),
      height: Math.round(box?.height || 0),
    };
  }

  /**
   * 返回第一个匹配元素。
   * @param {any} scope - 页面、frame 或 locator。
   * @param {any} element - 元素配置。
   * @param {boolean} visibleOnly - 是否只返回可见元素。
   * @returns {Promise<any|null>} locator 或空。
   */
  async firstLocator(scope, element, visibleOnly = true) {
    const locators = await this.allLocators(scope, element, visibleOnly, 1);
    return locators[0]?.locator || locators[0] || null;
  }

  /**
   * 返回全部匹配元素。
   * @param {any} scope - 页面、frame 或 locator。
   * @param {any} element - 元素配置。
   * @param {boolean} visibleOnly - 是否只返回可见元素。
   * @param {number} limit - 最大数量。
   * @returns {Promise<Array<Record<string, any>>>} locator 列表。
   */
  async allLocators(scope, element, visibleOnly = true, limit = 200) {
    if (isLocatorLike(element)) return [{ locator: element }];
    const selectors = selectorList(element);
    if (selectors.length === 0) return [];
    const result = [];
    const unlimited = Number(limit || 0) <= 0;
    const containers = searchContainerList(scope);
    for (const container of containers) {
      const parents = parentSelectorList(element);
      const parentScopes = parents.length
        ? await this.parentScopes(container.scope, parents, visibleOnly)
        : [{ scope: container.scope, parentSelector: "" }];
      for (const parent of parentScopes) {
        for (const selector of selectors) {
          const locator = parent.scope.locator(selector);
          const count = await locator.count().catch(() => 0);
          for (
            let index = 0;
            index < count && (unlimited || result.length < limit);
            index += 1
          ) {
            const item = locator.nth(index);
            if (visibleOnly && !(await item.isVisible().catch(() => false))) continue;
            result.push({
              locator: item,
              parentSelector: parent.parentSelector,
              targetSelector: selector,
              frameURL: container.frameURL,
            });
          }
        }
      }
    }
    return result;
  }

  /**
   * 返回父级元素 scope 列表。
   * @param {any} scope - 页面、frame 或 locator。
   * @param {string[]} parents - 父级选择器列表。
   * @param {boolean} visibleOnly - 是否只返回可见元素。
   * @returns {Promise<Array<Record<string, any>>>} 父级 scope 列表。
   */
  async parentScopes(scope, parents, visibleOnly) {
    const result = [];
    for (const selector of parents) {
      const locator = scope.locator(selector);
      const count = await locator.count().catch(() => 0);
      for (let index = 0; index < count; index += 1) {
        const item = locator.nth(index);
        if (visibleOnly && !(await item.isVisible().catch(() => false))) continue;
        result.push({ scope: item, parentSelector: selector });
      }
    }
    return result;
  }

  /**
   * 在元素内读取文本。
   * @param {any} scope - 页面或 locator。
   * @param {any} config - 元素配置。
   * @returns {Promise<string>} 文本内容。
   */
  async locatorText(scope, config) {
    const locator = await this.firstLocator(scope, config, true);
    if (!locator) return "";
    return (await locator.innerText({ timeout: 1000 }).catch(() => "")).trim();
  }
}

/**
 * BrowserOverlayActions 提供页面右上角提示浮层能力。
 */
export class BrowserOverlayActions {
  /**
   * 创建浮层操作实例。
   * @param {BrowserBaseActions} base - 基础操作实例。
   */
  constructor(base) {
    this.base = base;
  }

  /**
   * 显示通用右上角提示卡片。
   * @param {Record<string, any>} payload - 浮层内容。
   * @returns {Promise<Record<string, any>>} 浮层结果。
   */
  async showCard(payload = {}) {
    const currentPage = await this.base.ensurePage();
    const id = stringValue(payload.id) || "__goodhr_overlay_card";
    const title = stringValue(payload.title) || "GoodHR";
    const subtitle = stringValue(payload.subtitle);
    const message = stringValue(payload.message || payload.text);
    const maxAgeMS = Math.max(3000, Math.min(60000, Number(payload.max_age_ms || 15000)));
    await currentPage.evaluate(
      ({ id, title, subtitle, message, maxAgeMS }) => {
        const old = document.getElementById(id);
        if (old) old.remove();
        const box = document.createElement("div");
        box.id = id;
        box.style.cssText = [
          "position:fixed",
          "right:16px",
          "top:16px",
          "z-index:2147483647",
          "max-width:360px",
          "width:calc(100vw - 32px)",
          "box-sizing:border-box",
          "padding:14px",
          "border-radius:12px",
          "background:rgba(252,250,244,.96)",
          "color:#18221d",
          "box-shadow:0 18px 48px rgba(18,28,22,.22),0 2px 8px rgba(18,28,22,.10)",
          "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif",
          "font-size:13px",
          "line-height:1.45",
          "pointer-events:none",
          "border:1px solid rgba(48,79,63,.18)",
        ].join(";");
        box.innerHTML = [
          `<div style="font-size:14px;font-weight:750;">${escapeHTML(title)}</div>`,
          subtitle
            ? `<div style="font-size:12px;color:#6d7a72;margin-top:2px;">${escapeHTML(subtitle)}</div>`
            : "",
          `<div style="margin-top:10px;white-space:pre-wrap;">${escapeHTML(message)}</div>`,
        ].join("");
        document.body.appendChild(box);
        setTimeout(() => box.remove(), maxAgeMS);
        function escapeHTML(value) {
          return String(value || "").replace(/[&<>"']/g, (char) => ({
            "&": "&amp;",
            "<": "&lt;",
            ">": "&gt;",
            '"': "&quot;",
            "'": "&#39;",
          })[char]);
        }
      },
      { id, title, subtitle, message, maxAgeMS },
    );
    return { visible: true, id, title, subtitle, message };
  }

  /**
   * 关闭通用提示卡片。
   * @param {Record<string, any>} payload - 浮层 ID。
   * @returns {Promise<Record<string, any>>} 关闭结果。
   */
  async hideCard(payload = {}) {
    const currentPage = await this.base.ensurePage();
    const id = stringValue(payload.id) || "__goodhr_overlay_card";
    await currentPage.evaluate((targetID) => document.getElementById(targetID)?.remove(), id);
    return { visible: false, id };
  }

  /**
   * 显示 AI 调用提示浮层。
   * @param {Record<string, any>} payload - AI 浮层内容。
   * @returns {Promise<Record<string, any>>} 浮层结果。
   */
  async showAIOverlay(payload = {}) {
    return this.showCard({
      ...payload,
      id: "__goodhr_ai_overlay",
      title: payload.title || "AI 正在干活",
      subtitle: payload.subtitle || "我先小声处理一下",
      message: payload.message || payload.text || "正在分析候选人信息...",
    });
  }

  /**
   * 关闭 AI 调用提示浮层。
   * @returns {Promise<Record<string, any>>} 关闭结果。
   */
  async hideAIOverlay() {
    return this.hideCard({ id: "__goodhr_ai_overlay" });
  }

  /**
   * 显示关键词匹配提示浮层。
   * @param {Record<string, any>} payload - 关键词匹配内容。
   * @returns {Promise<Record<string, any>>} 浮层结果。
   */
  async showKeywordOverlay(payload = {}) {
    const keywords = cleanWords(payload.keywords).join("、") || "无";
    const matched = cleanWords(payload.matched_keywords).join("、") || "无";
    const excludes = cleanWords(payload.exclude_keywords || payload.excludes).join("、") || "无";
    return this.showCard({
      ...payload,
      id: "__goodhr_keyword_overlay",
      title: payload.title || "关键词匹配",
      subtitle: payload.subtitle || "我先把重点圈出来",
      message:
        payload.message ||
        `关键词：${keywords}\n已匹配：${matched}\n排除词：${excludes}`,
    });
  }

  /**
   * 关闭关键词匹配提示浮层。
   * @returns {Promise<Record<string, any>>} 关闭结果。
   */
  async hideKeywordOverlay() {
    return this.hideCard({ id: "__goodhr_keyword_overlay" });
  }
}

/**
 * BrowserDownloadActions 提供下载监听和文件保存能力。
 */
export class BrowserDownloadActions {
  /**
   * 创建下载操作实例。
   * @param {BrowserBaseActions} base - 基础操作实例。
   * @param {Record<string, any>} options - 下载目录和回调。
   */
  constructor(base, options = {}) {
    this.base = base;
    this.downloads = [];
    this.downloadsPath =
      options.downloadsPath || base.defaultDownloadsPath || path.join(os.homedir(), "Downloads");
    this.onDownload = options.onDownload || null;
  }

  /**
   * 给页面注册下载监听。
   * @param {any} targetPage - Playwright page。
   * @returns {void} 无返回值。
   */
  registerPage(targetPage) {
    if (!targetPage || targetPage.__goodhrDownloadRegistered) return;
    targetPage.__goodhrDownloadRegistered = true;
    targetPage.on?.("download", (download) => {
      this.handleDownload(download, targetPage).catch((error) => {
        this.base.log("下载处理失败", { error: error?.message || error });
      });
    });
  }

  /**
   * 保存单个下载文件。
   * @param {any} download - Playwright download。
   * @param {any} targetPage - 触发下载的页面。
   * @returns {Promise<Record<string, any>>} 下载记录。
   */
  async handleDownload(download, targetPage) {
    const directory = this.downloadsPath;
    await fs.mkdir(directory, { recursive: true });
    const downloadURL = download.url?.() || "";
    const rawSuggested = download.suggestedFilename?.() || "download";
    const suggested = await this.filenameWithExtension(rawSuggested, downloadURL);
    const targetPath = await this.uniquePath(directory, suggested);
    await download.saveAs(targetPath);
    const failure = await download.failure?.();
    if (failure) throw new Error(`下载失败：${failure}`);
    const savedPath = await this.ensureDownloadExtension(targetPath);
    const stat = await fs.stat(savedPath).catch(() => null);
    const record = {
      id: downloadID(savedPath, downloadURL),
      path: savedPath,
      file_path: savedPath,
      file_name: path.basename(savedPath),
      size: stat?.size || 0,
      url: downloadURL,
      page_url: pageURL(targetPage),
      created_at: new Date().toISOString(),
    };
    this.downloads.unshift(record);
    this.downloads = this.downloads.slice(0, 50);
    if (this.onDownload) await this.onDownload(record);
    return record;
  }

  /**
   * 列出下载记录。
   * @returns {Record<string, any>} 下载记录结果。
   */
  listDownloads() {
    return { downloads: this.downloads, count: this.downloads.length };
  }

  /**
   * 清空下载记录。
   * @returns {Record<string, any>} 清空结果。
   */
  clearDownloads() {
    this.downloads = [];
    return { cleared: true };
  }

  /**
   * 补全下载文件扩展名。
   * @param {string} filename - 原文件名。
   * @param {string} downloadURL - 下载地址。
   * @returns {Promise<string>} 补全后的文件名。
   */
  async filenameWithExtension(filename, downloadURL = "") {
    const safe = safeFilename(filename);
    if (path.extname(safe)) return safe;
    const urlExt = safeURLPathExt(downloadURL);
    return safe + (urlExt || "");
  }

  /**
   * 如果文件没有扩展名，则根据文件头补一个。
   * @param {string} filePath - 文件路径。
   * @returns {Promise<string>} 最终文件路径。
   */
  async ensureDownloadExtension(filePath) {
    if (path.extname(filePath)) return filePath;
    const ext = await this.extensionFromFile(filePath);
    if (!ext) return filePath;
    const nextPath = `${filePath}${ext}`;
    await fs.rename(filePath, nextPath);
    return nextPath;
  }

  /**
   * 根据文件头识别扩展名。
   * @param {string} filePath - 文件路径。
   * @returns {Promise<string>} 扩展名。
   */
  async extensionFromFile(filePath) {
    const handle = await fs.open(filePath, "r").catch(() => null);
    if (!handle) return "";
    try {
      const buffer = Buffer.alloc(65536);
      const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
      return extensionFromBuffer(buffer.subarray(0, bytesRead));
    } finally {
      await handle.close().catch(() => {});
    }
  }

  /**
   * 返回不重复文件路径。
   * @param {string} directory - 保存目录。
   * @param {string} filename - 文件名。
   * @returns {Promise<string>} 唯一文件路径。
   */
  async uniquePath(directory, filename) {
    const safe = safeFilename(filename);
    const parsed = path.parse(safe);
    for (let index = 0; index < 1000; index += 1) {
      const candidate =
        index === 0
          ? path.join(directory, safe)
          : path.join(directory, `${parsed.name}-${index}${parsed.ext}`);
      try {
        await fs.access(candidate);
      } catch {
        return candidate;
      }
    }
    return path.join(directory, `${Date.now()}-${safe}`);
  }
}

/**
 * 创建完整浏览器动作工具箱。
 * @param {Record<string, any>} options - 基础配置。
 * @returns {{base:BrowserBaseActions,advanced:BrowserAdvancedActions,overlay:BrowserOverlayActions,downloads:BrowserDownloadActions}} 动作工具箱。
 */
export function createBrowserActionKit(options = {}) {
  const base = new BrowserBaseActions(options);
  const advanced = new BrowserAdvancedActions(base);
  const overlay = new BrowserOverlayActions(base);
  const downloads = new BrowserDownloadActions(base, options);
  return { base, advanced, overlay, downloads };
}

/**
 * 返回随机滚动距离。
 * @param {Record<string, any>} payload - 滚动参数。
 * @returns {number} 滚动距离。
 */
export function randomDistance(payload = {}) {
  const min = Number(payload.distance_min || 0);
  const max = Number(payload.distance_max || 0);
  if (min > 0 && max >= min) return Math.round(min + Math.random() * (max - min));
  return Number(payload.distance || payload.y || 720);
}

/**
 * 整理鼠标目标矩形。
 * @param {Record<string, any>} box - 原始矩形。
 * @returns {{x1:number,x2:number,y1:number,y2:number,width:number,height:number}} 安全矩形。
 */
export function normalizeMouseTargetBox(box) {
  const x1 = Number(box?.x1 ?? box?.left ?? box?.x ?? 0);
  const y1 = Number(box?.y1 ?? box?.top ?? box?.y ?? 0);
  const rawX2 = Number(box?.x2 ?? box?.right ?? x1 + Number(box?.width || 0));
  const rawY2 = Number(box?.y2 ?? box?.bottom ?? y1 + Number(box?.height || 0));
  const safeX1 = Math.min(x1, rawX2);
  const safeX2 = Math.max(x1, rawX2);
  const safeY1 = Math.min(y1, rawY2);
  const safeY2 = Math.max(y1, rawY2);
  const width = safeX2 - safeX1;
  const height = safeY2 - safeY1;
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    throw new Error("鼠标目标范围无效");
  }
  return { x1: safeX1, x2: safeX2, y1: safeY1, y2: safeY2, width, height };
}

/**
 * 在矩形内生成随机点。
 * @param {{x1:number,x2:number,y1:number,y2:number,width:number,height:number}} box - 安全矩形。
 * @param {number} paddingRatio - 内边距比例。
 * @returns {{x:number,y:number,paddingX:number,paddingY:number}} 随机点。
 */
export function randomPointInBox(box, paddingRatio = 0.2) {
  const ratio = Math.max(0, Math.min(0.45, Number(paddingRatio || 0)));
  const paddingX = Math.min(box.width / 2, Math.max(0, box.width * ratio));
  const paddingY = Math.min(box.height / 2, Math.max(0, box.height * ratio));
  const minX = box.x1 + paddingX;
  const maxX = box.x2 - paddingX;
  const minY = box.y1 + paddingY;
  const maxY = box.y2 - paddingY;
  return {
    x: Math.round(minX + Math.random() * Math.max(1, maxX - minX)),
    y: Math.round(minY + Math.random() * Math.max(1, maxY - minY)),
    paddingX: Math.round(paddingX),
    paddingY: Math.round(paddingY),
  };
}

/**
 * 读取选择器列表。
 * @param {any} value - 字符串、数组或元素配置。
 * @returns {string[]} 选择器列表。
 */
export function selectorList(value) {
  if (!value) return [];
  if (typeof value === "string") return [value].filter(Boolean);
  if (Array.isArray(value)) return value.flatMap(selectorList);
  if (typeof value !== "object") return [];
  return [
    ...selectorList(value.selector || value.css || value.xpath),
    ...selectorList(value.selectors),
    ...classGroupSelectors(value.class || value.classes || value.class_names),
  ].filter(Boolean);
}

/**
 * 读取 class 选择器组。
 * @param {any} value - class 名称或数组。
 * @returns {string[]} CSS 选择器列表。
 */
export function classGroupSelectors(value) {
  if (!value) return [];
  const groups = Array.isArray(value) ? value : [value];
  return groups.flatMap((group) => {
    const items = Array.isArray(group) ? group : [group];
    return items.map(normalizeClassSelector).filter(Boolean);
  });
}

/**
 * 将 class 名或完整选择器转成 CSS 选择器。
 * @param {any} value - class 名称或 CSS 选择器。
 * @returns {string} CSS 选择器。
 */
export function normalizeClassSelector(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  if (/^[.#[:>~+]/.test(text)) return text;
  if (/[ >~+:[\]()=]/.test(text)) return text;
  return `.${cssEscape(text)}`;
}

/**
 * 转义 CSS class 名称。
 * @param {string} value - 原始 class 名。
 * @returns {string} 转义后的 class 名。
 */
export function cssEscape(value) {
  return String(value).replace(/[^a-zA-Z0-9_-]/g, (char) => `\\${char}`);
}

/**
 * 返回搜索容器列表。
 * @param {any} scope - 页面、frame 或 locator。
 * @returns {Array<{scope:any,frameURL:string}>} 搜索容器。
 */
export function searchContainerList(scope) {
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
 * @returns {string[]} 父级选择器。
 */
export function parentSelectorList(element) {
  if (!element || typeof element !== "object") return [];
  return [
    ...selectorList(element.parent || element.parent_selector),
    ...classGroupSelectors(element.parent_classes),
  ].filter(Boolean);
}

/**
 * 清理文件名。
 * @param {string} name - 原始文件名。
 * @returns {string} 安全文件名。
 */
export function safeFilename(name) {
  const cleaned = path
    .basename(name || "download")
    .replace(/[<>:"/\\|?*\x00-\x1f]/g, "_")
    .trim();
  return cleaned || "download";
}

/**
 * 根据文件内容识别扩展名。
 * @param {Buffer} buffer - 文件头内容。
 * @returns {string} 扩展名。
 */
export function extensionFromBuffer(buffer) {
  if (buffer.length >= 4 && buffer.subarray(0, 4).toString("latin1") === "%PDF") return ".pdf";
  if (buffer.length >= 8 && buffer.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) return ".png";
  if (buffer.length >= 3 && buffer[0] === 0xff && buffer[1] === 0xd8 && buffer[2] === 0xff) return ".jpg";
  if (buffer.length >= 6 && /^GIF8[79]a$/.test(buffer.subarray(0, 6).toString("latin1"))) return ".gif";
  if (buffer.length >= 4 && buffer.subarray(0, 4).toString("latin1") === "PK\x03\x04") return ".zip";
  if (buffer.length >= 2 && buffer[0] === 0x1f && buffer[1] === 0x8b) return ".gz";
  return "";
}

/**
 * 判断对象是否像 Playwright locator。
 * @param {any} value - 原始对象。
 * @returns {boolean} 是 locator 返回 true。
 */
function isLocatorLike(value) {
  return Boolean(value && typeof value === "object" && typeof value.boundingBox === "function");
}

/**
 * 读取页面 URL。
 * @param {any} targetPage - 页面对象。
 * @returns {string} 页面 URL。
 */
function pageURL(targetPage) {
  try {
    return targetPage?.url?.() || "";
  } catch {
    return "";
  }
}

/**
 * 将任意值转成去空格字符串。
 * @param {any} value - 原始值。
 * @returns {string} 字符串。
 */
function stringValue(value) {
  return String(value ?? "").trim();
}

/**
 * 将任意值转成正数。
 * @param {any} value - 原始值。
 * @returns {number} 正数或 0。
 */
function positiveNumber(value) {
  const number = Number(value || 0);
  return Number.isFinite(number) && number > 0 ? number : 0;
}

/**
 * 清理关键词数组。
 * @param {any} value - 原始关键词。
 * @returns {string[]} 关键词列表。
 */
function cleanWords(value) {
  const items = Array.isArray(value) ? value : String(value || "").split(/[,\n，、]/);
  return items.map((item) => String(item || "").trim()).filter(Boolean);
}

/**
 * 安全读取 URL 里的文件扩展名。
 * @param {string} rawURL - 原始下载地址。
 * @returns {string} 扩展名。
 */
function safeURLPathExt(rawURL) {
  try {
    if (!rawURL) return "";
    return path.extname(new URL(rawURL).pathname || "");
  } catch {
    return "";
  }
}

/**
 * 生成下载记录 ID。
 * @param {string} filePath - 文件路径。
 * @param {string} downloadURL - 下载地址。
 * @returns {string} 下载 ID。
 */
function downloadID(filePath, downloadURL) {
  return crypto
    .createHash("sha1")
    .update(`${filePath}|${downloadURL}|${Date.now()}`)
    .digest("hex")
    .slice(0, 16);
}
