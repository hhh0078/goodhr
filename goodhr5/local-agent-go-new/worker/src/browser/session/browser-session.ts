// 文件作用说明：管理 Camoufox、持久化 Profile、页面、Cookie、下载和浏览器生命周期。

import fs from "node:fs/promises";
import type {
  Browser,
  BrowserContext,
  Cookie,
  Page,
} from "playwright-core";
import { firefox } from "playwright-core";
import type {
  BrowserStartRequest,
  BrowserStatusResult,
  DownloadListResult,
  PageInfo,
  PageListResult,
  PageOpenRequest,
} from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError, normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { DownloadManager } from "./download-manager.js";
import { ElementRegistry } from "./element-registry.js";
import { loadStableProfileFingerprint } from "./fingerprint.js";
import { initializeChineseLanguagePack } from "./language-initializer.js";
import { pageURLContainsTarget, safeURL } from "./navigation.js";
import { trackPageDiagnostics } from "./page-diagnostics.js";
import { BrowserLifecycle } from "./browser-lifecycle.js";
import { buildLaunchOptions } from "./launch-options.js";
import { nativeInputSafetyDiagnostic } from "./native-input-safety.js";
import { focusCurrentBrowserWindow, type WindowFocusResult } from "./window-focus.js";
import { assertInputAllowed } from "../primitives/input-lifecycle.js";

export { pageURLContainsTarget } from "./navigation.js";

/** BrowserSession 保存当前唯一浏览器会话和页面状态。 */
export class BrowserSession {
  private browser: Browser | null = null;
  private context: BrowserContext | null = null;
  private currentPage: Page | null = null;
  private userDataDir = "";
  private extensionPaths: string[] = [];
  private readonly registeredPages = new WeakSet<Page>();
  private readonly downloadManager: DownloadManager;
  private readonly lifecycle: BrowserLifecycle;

  readonly elements = new ElementRegistry();

  /** 创建浏览器会话管理器。 */
  constructor(
    private readonly logger: WorkerLogger,
    private readonly focusBrowserWindow: () => Promise<WindowFocusResult> = focusCurrentBrowserWindow,
  ) {
    this.lifecycle = new BrowserLifecycle(logger);
    this.downloadManager = new DownloadManager(logger);
  }

  /** start 启动或复用 Camoufox 会话。 */
  async start(
    request: BrowserStartRequest,
    actionContext: ActionContext,
  ): Promise<BrowserStatusResult> {
    const step = "start_browser";
    const pageRequest: PageOpenRequest | null = request.url
      ? {
          url: request.url,
          ...(request.wait_until ? { wait_until: request.wait_until } : {}),
          ...(request.timeout_ms ? { timeout_ms: request.timeout_ms } : {}),
          ...(request.new_tab !== undefined
            ? { new_tab: request.new_tab }
            : {}),
        }
      : null;
    this.logger.info(actionContext, step, "start", {
      persistent: Boolean(request.user_data_dir),
      headless: request.headless ?? false,
      diagnostic: true,
      input: { headless: request.headless ?? false, persistent: Boolean(request.user_data_dir), native_input_patch: nativeInputSafetyDiagnostic() },
    });
    try {
      assertInputAllowed(actionContext);
      const running = await this.isRunning();
      assertInputAllowed(actionContext);
      if (running) {
        if (
          (request.user_data_dir ?? this.userDataDir) === this.userDataDir &&
          samePaths(request.extension_paths ?? this.extensionPaths, this.extensionPaths)
        ) {
          if (pageRequest) {
            await this.open(
              pageRequest,
              { ...actionContext, action: "page.open" },
            );
          }
          if (request.downloads_path) {
            await this.downloadManager.configure(request.downloads_path, actionContext);
          }
          const status = await this.status(true);
          this.logger.info(actionContext, step, "success", {
            reused: true,
            diagnostic: true,
            output: { reused: true, current_url: safeURL(status.current_url), native_input_patch: nativeInputSafetyDiagnostic() },
          });
          return status;
        }
        throw new WorkerError({ code: "INVALID_REQUEST", message: "旧浏览器窗口还留着，请先手动关闭，再切换账号配置",
          action: actionContext.action, step: "browser_config_change", trace_id: actionContext.trace_id,
          details: { browser_retained: true, manual_close_required: true } });
      } else if (this.context || this.browser) {
        assertInputAllowed(actionContext);
        // 已断开的旧会话只清理引用；不向浏览器发关闭指令，保留失联时可能仍存在的窗口。
        if (this.browser?.isConnected()) throw new WorkerError({ code: "BROWSER_NOT_RUNNING",
          message: "浏览器窗口还留着，但页面连接没准备好，请先手动关闭后再开始", action: actionContext.action,
          step: "existing_session_unavailable", trace_id: actionContext.trace_id,
          details: { browser_retained: true, manual_close_required: true } });
        this.clearSessionReferences();
      }

      await this.downloadManager.prepare(request.downloads_path);
      assertInputAllowed(actionContext);
      this.lifecycle.begin();
      const nativeDownloadPrefs = await this.downloadManager.prepareNativeSession();
      this.extensionPaths = [...(request.extension_paths ?? [])];
      if (request.user_data_dir) {
        this.userDataDir = request.user_data_dir;
        await fs.mkdir(this.userDataDir, { recursive: true });
        const fingerprint = await loadStableProfileFingerprint(
          this.userDataDir,
        );
        const options = await buildLaunchOptions(request, actionContext, fingerprint);
        assertInputAllowed(actionContext);
        await initializeChineseLanguagePack(this.userDataDir, options);
        options.firefoxUserPrefs = { ...options.firefoxUserPrefs, ...nativeDownloadPrefs };
        await this.downloadManager.resetSnapshot();
        assertInputAllowed(actionContext);
        this.context = await firefox.launchPersistentContext(
          this.userDataDir,
          {
            ...options,
            viewport: null,
          },
        );
        this.browser = this.context.browser();
      } else {
        this.userDataDir = "";
        const options = await buildLaunchOptions(request, actionContext);
        options.firefoxUserPrefs = { ...options.firefoxUserPrefs, ...nativeDownloadPrefs };
        assertInputAllowed(actionContext);
        this.browser = await firefox.launch(options);
        this.context = await this.browser.newContext({
          viewport: null,
          ...(request.user_agent ? { userAgent: request.user_agent } : {}),
        });
      }
      this.registerContext(this.context);
      await this.downloadManager.waitForReady();
      assertInputAllowed(actionContext);
      this.currentPage =
        this.context.pages().find((item) => !item.isClosed()) ??
        (await this.context.newPage());
      this.registerPage(this.currentPage);
      if (pageRequest) {
        await this.open(
          pageRequest,
          { ...actionContext, action: "page.open" },
        );
      }
      const status = await this.status(false);
      this.lifecycle.lastAction = "浏览器已打开";
      this.logger.info(actionContext, step, "success", {
        reused: false,
        current_url: status.current_url,
        diagnostic: true,
        output: { reused: false, current_url: safeURL(status.current_url), native_input_patch: nativeInputSafetyDiagnostic() },
      });
      return status;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "INTERNAL_ERROR",
        message: "浏览器没启动成功，我先把原因记下来了",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
      });
      this.logger.failure(actionContext, normalized);
      // 启动或复用中的错误只记日志；已有窗口留给用户检查，不自动关闭。
      throw normalized;
    }
  }

  /** stop 关闭当前浏览器并清理全部页面引用。 */
  async stop(actionContext: ActionContext): Promise<BrowserStatusResult> {
    const step = "stop_browser";
    this.logger.info(actionContext, step, "start", { diagnostic: true });
    try {
      await this.dispose();
      const result = await this.status(false);
      this.logger.info(actionContext, step, "success", { diagnostic: true });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        message: "浏览器关闭时有点磨蹭，我已经继续清理了",
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** status 返回当前浏览器状态。 */
  async status(reused = false): Promise<BrowserStatusResult> {
    const running = await this.isRunning();
    const current =
      running && this.currentPage && !this.currentPage.isClosed()
        ? this.currentPage.url()
        : "";
    return {
      running,
      persistent: Boolean(this.userDataDir),
      reused,
      user_data_dir: this.userDataDir,
      downloads_path: this.downloadManager.directory(),
      extension_paths: [...this.extensionPaths],
      current_url: current,
    };
  }

  /** open 打开 URL，并按请求决定复用页面或新建标签页。 */
  async open(
    request: PageOpenRequest,
    actionContext: ActionContext,
  ): Promise<PageInfo> {
    const step = "open_page";
    this.lifecycle.lastAction = request.new_tab ? "新建标签页" : "打开页面";
    this.logger.info(actionContext, step, "start", {
      target_url: safeURL(request.url),
      new_tab: request.new_tab ?? false,
    });
    try {
      assertInputAllowed(actionContext);
      let page: Page;
      if (request.new_tab) {
        const context = this.requireContext(actionContext, step);
        page = await context.newPage();
        this.currentPage = page;
        this.registerPage(page);
      } else {
        const context = this.requireContext(actionContext, step);
        const reusable = context
          .pages()
          .find(
            (item) =>
              !item.isClosed() &&
              pageURLContainsTarget(item.url(), request.url),
          );
        if (reusable) {
          this.currentPage = reusable;
          this.registerPage(reusable);
          this.elements.clear();
          assertInputAllowed(actionContext);
          await reusable.bringToFront();
          const reusedResult = await this.pageInfo(reusable);
          this.logger.info(actionContext, step, "success", {
            page_id: reusedResult.page_id,
            page_url: safeURL(reusedResult.url),
            reused_page: true,
            navigated: false,
          });
          return reusedResult;
        }
        page = await this.requirePage(actionContext, step);
      }
      assertInputAllowed(actionContext);
      await page.goto(request.url, {
        waitUntil: request.wait_until ?? "domcontentloaded",
        timeout: request.timeout_ms ?? 30_000,
      });
      assertInputAllowed(actionContext);
      await page.bringToFront();
      this.elements.clearPage(page);
      const result = await this.pageInfo(page);
      this.lifecycle.lastPageURL = safeURL(result.url);
      this.logger.info(actionContext, step, "success", {
        page_id: result.page_id,
        page_url: safeURL(result.url),
        reused_page: false,
        navigated: true,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        message: "页面没打开成功，我已经把地址和原因记下来了",
        details: { target_url: safeURL(request.url) },
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** listPages 返回当前全部标签页。 */
  async listPages(): Promise<PageListResult> {
    const context = this.context;
    if (!context) {
      return { pages: [], count: 0 };
    }
    const pages: PageInfo[] = [];
    for (const page of context.pages()) {
      if (!page.isClosed()) {
        pages.push(await this.pageInfo(page));
      }
    }
    return { pages, count: pages.length };
  }

  /** usePage 按页面编号切换当前标签页。 */
  async usePage(
    pageID: string,
    actionContext: ActionContext,
  ): Promise<PageInfo> {
    assertInputAllowed(actionContext);
    const context = this.requireContext(actionContext, "use_page");
    const index = Number.parseInt(pageID, 10);
    const page = context.pages()[index];
    if (!page || page.isClosed()) {
      throw new WorkerError({
        code: "PAGE_NOT_AVAILABLE",
        message: "这个标签页已经不在了，可以重新打开",
        action: actionContext.action,
        step: "use_page",
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { page_id: pageID },
      });
    }
    this.currentPage = page;
    this.registerPage(page);
    await page.bringToFront();
    return this.pageInfo(page);
  }

  /** reactivatePage 先激活当前 Worker 的桌面窗口，再尝试标签切换；不触碰招聘页面脚本或重放输入。 */
  async reactivatePage(page: Page, actionContext: ActionContext): Promise<void> {
    assertInputAllowed(actionContext);
    const context = this.requireContext(actionContext, "window_tab_cycle");
    if (page.isClosed() || page.context() !== context) {
      throw new Error("待恢复的浏览器标签已关闭或不属于当前会话");
    }
    const focus = await this.focusBrowserWindow();
    this.logger.info(actionContext, "window_os_focus", focus.status, { ...focus });
    assertInputAllowed(actionContext);
    if (focus.status === "focused" || focus.status === "already_active") {
      await page.bringToFront();
      this.currentPage = page;
      return;
    }
    let temporary: Page | undefined;
    this.logger.info(actionContext, "window_tab_cycle", "start", { temporary_tab: true });
    try {
      temporary = await context.newPage();
      assertInputAllowed(actionContext);
      await temporary.bringToFront();
      await page.bringToFront();
      await temporary.close();
      this.currentPage = page;
      this.logger.info(actionContext, "window_tab_cycle", "success", { temporary_tab_closed: true });
    } catch (error) {
      this.logger.error(actionContext, "window_tab_cycle", "failed", {
        temporary_tab_created: Boolean(temporary), error: String(error),
      });
      throw error;
    } finally {
      if (temporary && !temporary.isClosed()) await temporary.close().catch(() => undefined);
      if (!page.isClosed()) this.currentPage = page;
    }
  }

  /** closeCurrentPage 关闭当前标签页并切换到剩余页面。 */
  async closeCurrentPage(actionContext: ActionContext): Promise<JsonObject> {
    assertInputAllowed(actionContext);
    const page = await this.requirePage(actionContext, "close_page");
    assertInputAllowed(actionContext);
    this.elements.clearPage(page);
    await page.close();
    const pages = this.context?.pages().filter((item) => !item.isClosed()) ?? [];
    this.currentPage = pages.at(-1) ?? null;
    if (this.currentPage) {
      this.registerPage(this.currentPage);
    }
    return {
      closed: true,
      current_url: this.currentPage?.url() ?? "",
    };
  }

  /** requirePage 返回可用的当前页面，不存在时统一抛错。 */
  async requirePage(
    actionContext: ActionContext,
    step: string,
  ): Promise<Page> {
    if (this.currentPage && !this.currentPage.isClosed()) {
      return this.currentPage;
    }
    const candidate = this.context
      ?.pages()
      .find((item) => !item.isClosed());
    if (candidate) {
      this.currentPage = candidate;
      this.registerPage(candidate);
      return candidate;
    }
    throw new WorkerError({
      code: "PAGE_NOT_AVAILABLE",
      message: "浏览器页面暂时不在，我需要先重新打开页面",
      action: actionContext.action,
      step,
      trace_id: actionContext.trace_id,
      retryable: true,
    });
  }

  /** cookies 读取当前上下文 Cookie。 */
  async cookies(actionContext: ActionContext): Promise<Cookie[]> {
    return this.requireContext(actionContext, "list_cookies").cookies();
  }

  /** setCookies 导入 Cookie。 */
  async setCookies(
    cookies: Cookie[],
    actionContext: ActionContext,
  ): Promise<void> {
    if (cookies.length === 0) {
      return;
    }
    await this.requireContext(actionContext, "set_cookies").addCookies(cookies);
  }

  /** downloadDirectory 返回当前下载目录。 */
  downloadDirectory(): string {
    return this.downloadManager.directory();
  }

  /** listDownloads 返回当前会话已经保存的下载记录。 */
  listDownloads(): Promise<DownloadListResult> {
    return this.downloadManager.list();
  }

  /** configureDownloads 切换后续下载保存目录，不删除已有文件和记录。 */
  async configureDownloads(
    directory: string,
    actionContext: ActionContext,
  ): Promise<JsonObject> {
    return this.downloadManager.configure(directory, actionContext);
  }

  /** clearDownloads 清空内存下载记录，不删除用户已经下载的文件。 */
  clearDownloads(): Promise<JsonObject> {
    return this.downloadManager.clear();
  }

  /** isRunning 判断浏览器会话是否仍可用。 */
  private async isRunning(): Promise<boolean> {
    if (!this.context) {
      return false;
    }
    if (this.browser && !this.browser.isConnected()) {
      return false;
    }
    return Boolean(this.browser?.isConnected()) || this.context.pages().some((item) => !item.isClosed());
  }

  /** requireContext 返回浏览器上下文，不存在时统一抛错。 */
  private requireContext(
    actionContext: ActionContext,
    step: string,
  ): BrowserContext {
    if (this.context) {
      return this.context;
    }
    throw new WorkerError({
      code: "BROWSER_NOT_RUNNING",
      message: "浏览器还没启动，我先小声提醒一下",
      action: actionContext.action,
      step,
      trace_id: actionContext.trace_id,
      retryable: true,
    });
  }

  /** registerContext 为已有页面和后续新页面统一注册生命周期，下载由浏览器全局原生观察器覆盖。 */
  private registerContext(context: BrowserContext): void {
    this.lifecycle.register(context, this.browser);
    for (const page of context.pages()) {
      this.registerPage(page);
    }
    context.on("page", (page) => {
      if (!this.lifecycle.isCurrent(context)) return;
      this.currentPage = page;
      this.registerPage(page);
    });
  }

  /** registerPage 注册页面关闭和导航后的引用清理。 */
  private registerPage(page: Page): void {
    if (this.registeredPages.has(page)) {
      return;
    }
    this.registeredPages.add(page);
    trackPageDiagnostics(page);
    page.on("close", () => {
      this.elements.clearPage(page);
      if (this.currentPage === page) {
        this.currentPage = null;
      }
    });
    page.on("framenavigated", (frame) => {
      if (frame === page.mainFrame()) {
        this.elements.clearPage(page);
      }
    });
  }

  /** pageInfo 返回标签页基础信息。 */
  private async pageInfo(page: Page): Promise<PageInfo> {
    const pages = this.context?.pages() ?? [];
    return {
      page_id: String(Math.max(0, pages.indexOf(page))),
      url: page.url(),
      title: await page.title().catch(() => ""),
      current: page === this.currentPage,
    };
  }

  /** dispose 关闭浏览器并清理本地引用。 */
  private async dispose(): Promise<void> {
    const context = this.context;
    const browser = this.browser;
    await this.downloadManager.list().catch(() => undefined);
    this.clearSessionReferences();
    if (context) {
      await context.close().catch(() => undefined);
    }
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    await this.downloadManager.list().catch(() => undefined);
    await this.downloadManager.reset();
  }

  /** clearSessionReferences 只释放本地引用，不关闭浏览器，用于已断开连接后的明确重新启动。 */
  private clearSessionReferences(): void {
    this.lifecycle.dispose();
    this.context = null;
    this.browser = null;
    this.currentPage = null;
    this.userDataDir = "";
    this.extensionPaths = [];
    this.elements.clear();
  }

}

/** samePaths 判断本次扩展列表是否与当前浏览器会话完全一致。 */
function samePaths(left: string[] | undefined, right: string[]): boolean {
  const current = left ?? [];
  return (
    current.length === right.length &&
    current.every((value, index) => value === right[index])
  );
}
