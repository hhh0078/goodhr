// 文件作用说明：管理 Camoufox、持久化 Profile、页面、Cookie、下载和浏览器生命周期。

import fs from "node:fs/promises";
import { launchOptions as camoufoxLaunchOptions } from "camoufox-js";
import type {
  Browser,
  BrowserContext,
  Cookie,
  LaunchOptions,
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
import { pageURLContainsTarget, safeURL } from "./navigation.js";

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

  readonly elements = new ElementRegistry();

  /** 创建浏览器会话管理器。 */
  constructor(private readonly logger: WorkerLogger) {
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
    });
    try {
      if (await this.isRunning()) {
        if (
          request.user_data_dir === this.userDataDir &&
          samePaths(request.extension_paths, this.extensionPaths)
        ) {
          if (pageRequest) {
            await this.open(
              pageRequest,
              { ...actionContext, action: "page.open" },
            );
          }
          if (request.downloads_path) {
            await this.downloadManager.prepare(request.downloads_path);
          }
          const status = await this.status(true);
          this.logger.info(actionContext, step, "success", {
            reused: true,
          });
          return status;
        }
        await this.stop(actionContext);
      } else if (this.context || this.browser) {
        await this.dispose();
      }

      await this.downloadManager.prepare(request.downloads_path);
      this.extensionPaths = [...(request.extension_paths ?? [])];
      if (request.user_data_dir) {
        this.userDataDir = request.user_data_dir;
        await fs.mkdir(this.userDataDir, { recursive: true });
        const fingerprint = await loadStableProfileFingerprint(
          this.userDataDir,
        );
        const options = await this.buildLaunchOptions(
          request,
          actionContext,
          fingerprint,
        );
        this.context = await firefox.launchPersistentContext(
          this.userDataDir,
          {
            ...options,
            viewport:
              request.viewport_width && request.viewport_height
                ? {
                    width: request.viewport_width,
                    height: request.viewport_height,
                  }
                : null,
            acceptDownloads: true,
            downloadsPath: this.downloadManager.directory(),
          },
        );
        this.browser = this.context.browser();
      } else {
        this.userDataDir = "";
        const options = await this.buildLaunchOptions(request, actionContext);
        this.browser = await firefox.launch(options);
        this.context = await this.browser.newContext({
          acceptDownloads: true,
          viewport:
            request.viewport_width && request.viewport_height
              ? {
                  width: request.viewport_width,
                  height: request.viewport_height,
                }
              : null,
          ...(request.user_agent ? { userAgent: request.user_agent } : {}),
        });
      }
      this.registerContext(this.context);
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
      this.logger.info(actionContext, step, "success", {
        reused: false,
        current_url: status.current_url,
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
      await this.dispose();
      throw normalized;
    }
  }

  /** stop 关闭当前浏览器并清理全部页面引用。 */
  async stop(actionContext: ActionContext): Promise<BrowserStatusResult> {
    const step = "stop_browser";
    this.logger.info(actionContext, step, "start");
    try {
      await this.dispose();
      const result = await this.status(false);
      this.logger.info(actionContext, step, "success");
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
    this.logger.info(actionContext, step, "start", {
      target_url: safeURL(request.url),
      new_tab: request.new_tab ?? false,
    });
    try {
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
      await page.goto(request.url, {
        waitUntil: request.wait_until ?? "domcontentloaded",
        timeout: request.timeout_ms ?? 30_000,
      });
      this.elements.clearPage(page);
      const result = await this.pageInfo(page);
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

  /** closeCurrentPage 关闭当前标签页并切换到剩余页面。 */
  async closeCurrentPage(actionContext: ActionContext): Promise<JsonObject> {
    const page = await this.requirePage(actionContext, "close_page");
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
  listDownloads(): DownloadListResult {
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
  clearDownloads(): JsonObject {
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
    return this.context.pages().some((item) => !item.isClosed());
  }

  /** buildLaunchOptions 把启动请求转换为 Camoufox + Playwright 启动参数，GeoIP 异常时自动降级。 */
  private async buildLaunchOptions(
    request: BrowserStartRequest,
    actionContext: ActionContext,
    fingerprint?: Awaited<ReturnType<typeof loadStableProfileFingerprint>>,
  ): Promise<LaunchOptions> {
    const wantsGeoIP = request.geoip ?? Boolean(request.proxy);
    const camoufoxInput = {
      headless: request.headless ?? false,
      // 决策点 D3：关闭 Camoufox 自带鼠标人类化，统一沿用 Worker 自研类人操作原语，行为更可控。
      humanize: false,
      locale: request.locale,
      proxy: request.proxy,
      // Firefox 插件目录（沿用原扩展目录约定，需包含 manifest.json）。
      addons: request.extension_paths,
      // 不传 executable_path：macOS 下 camoufox-js 会到错误目录找 properties.json；
      // 通过 CAMOUFOX_INSTALL_DIR 环境变量（Go 注入）让 camoufox-js 按官方逻辑解析启动文件。
      ...(request.timezone ? { config: { timezone: request.timezone } } : {}),
      ...(fingerprint ? { fingerprint } : {}),
    };
    let prepared: Record<string, unknown>;
    try {
      prepared = await camoufoxLaunchOptions({
        ...camoufoxInput,
        ...(wantsGeoIP ? { geoip: true } : {}),
      });
    } catch (error) {
      if (!wantsGeoIP) {
        throw error;
      }
      // GeoIP 依赖在线 IP 定位和 MaxMind 数据库，网络不佳时降级为不启用，保证浏览器能启动。
      this.logger.warn(actionContext, "build_launch_options", "geoip_fallback", {
        message: error instanceof Error ? error.message : String(error),
      });
      prepared = await camoufoxLaunchOptions({ ...camoufoxInput });
    }
    return toPlaywrightLaunchOptions(prepared);
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

  /** registerContext 为已有页面和后续新页面统一注册生命周期与下载监听。 */
  private registerContext(context: BrowserContext): void {
    for (const page of context.pages()) {
      this.registerPage(page);
    }
    context.on("page", (page) => {
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
    page.on("download", (download) => {
      this.downloadManager.capture(download, page);
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
    await this.downloadManager.waitForPending();
    this.context = null;
    this.browser = null;
    this.currentPage = null;
    this.userDataDir = "";
    this.extensionPaths = [];
    this.elements.clear();
    if (context) {
      await context.close().catch(() => undefined);
    }
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    this.downloadManager.reset();
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

/** toPlaywrightLaunchOptions 把 Camoufox 返回的启动配置收敛为 Playwright 强类型启动参数。 */
function toPlaywrightLaunchOptions(
  prepared: Record<string, unknown>,
): LaunchOptions {
  const options: LaunchOptions = {};
  const executablePath = prepared["executablePath"];
  if (typeof executablePath === "string" && executablePath) {
    options.executablePath = executablePath;
  }
  const headless = prepared["headless"];
  if (typeof headless === "boolean") {
    options.headless = headless;
  }
  const args = prepared["args"];
  if (Array.isArray(args)) {
    options.args = args.filter((item): item is string =>
      typeof item === "string",
    );
  }
  const env = prepared["env"];
  if (env && typeof env === "object" && !Array.isArray(env)) {
    const stringEnv: Record<string, string> = {};
    for (const [key, value] of Object.entries(env)) {
      if (["string", "number", "boolean"].includes(typeof value)) {
        stringEnv[key] = String(value);
      }
    }
    options.env = stringEnv;
  }
  const userPrefs = prepared["firefoxUserPrefs"];
  if (isPrimitiveRecord(userPrefs)) {
    options.firefoxUserPrefs = userPrefs;
  }
  const proxy = prepared["proxy"];
  if (isProxySettings(proxy)) {
    options.proxy = proxy;
  }
  return options;
}

/** CamoufoxProxySettings 与 Playwright 代理配置保持结构一致。 */
interface CamoufoxProxySettings {
  server: string;
  bypass?: string;
  username?: string;
  password?: string;
}

/** isProxySettings 校验并收敛外部返回的代理配置。 */
function isProxySettings(value: unknown): value is CamoufoxProxySettings {
  if (!value || typeof value !== "object") {
    return false;
  }
  const candidate = value as { server?: unknown };
  return typeof candidate.server === "string" && candidate.server.length > 0;
}

/** isPrimitiveRecord 判断值是否为基础类型组成的记录，用于收敛外部依赖返回的弱类型数据。 */
function isPrimitiveRecord(
  value: unknown,
): value is Record<string, string | number | boolean> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return false;
  }
  return Object.values(value).every(
    (item) =>
      typeof item === "string" ||
      typeof item === "number" ||
      typeof item === "boolean",
  );
}
