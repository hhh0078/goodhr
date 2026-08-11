// 文件作用说明：管理 CloakBrowser、持久化 Profile、页面、Cookie、下载和浏览器生命周期。

import fs from "node:fs/promises";
import {
  launch,
  launchPersistentContext,
  type LaunchContextOptions,
} from "cloakbrowser";
import type {
  APIResponse,
  Browser,
  BrowserContext,
  Cookie,
  Page,
} from "playwright-core";
import type {
  BrowserStartRequest,
  BrowserStatusResult,
  DownloadListResult,
  DownloadRecord,
  PageInfo,
  PageListResult,
  PageOpenRequest,
  SaveCurrentDocumentRequest,
} from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError, normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { DownloadManager } from "./download-manager.js";
import { ElementRegistry } from "./element-registry.js";
import { withStableProfileFingerprint } from "./fingerprint.js";
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

  /** start 启动或复用 CloakBrowser 会话。 */
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
      const options = this.launchOptions(request);
      this.extensionPaths = [...(request.extension_paths ?? [])];
      if (request.user_data_dir) {
        this.userDataDir = request.user_data_dir;
        await fs.mkdir(this.userDataDir, { recursive: true });
        this.context = await launchPersistentContext({
          ...options,
          userDataDir: this.userDataDir,
          contextOptions: {
            acceptDownloads: true,
          },
        });
        this.browser = this.context.browser();
      } else {
        this.userDataDir = "";
        this.browser = await launch(options);
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

  /** saveCurrentDocument 使用当前 BrowserContext 的 Cookie 读取并保存当前文档页。 */
  async saveCurrentDocument(
    request: SaveCurrentDocumentRequest,
    actionContext: ActionContext,
  ): Promise<DownloadRecord> {
    const step = "save_current_document";
    const maximumBytes = request.max_bytes ?? 26_214_400;
    const allowedContentTypes = request.allowed_content_types ?? ["application/pdf"];
    const page = await this.requirePage(actionContext, step);
    const context = this.requireContext(actionContext, step);
    const pageURL = page.url();
    assertHTTPDocumentURL(pageURL, actionContext);
    this.logger.info(actionContext, step, "start", {
      page_url: safeURL(pageURL),
      max_bytes: maximumBytes,
      allowed_content_types: allowedContentTypes,
    });
    try {
      const saved = await streamCurrentDocument(
        context,
        this.downloadManager,
        pageURL,
        maximumBytes,
        allowedContentTypes,
        request.suggested_filename ?? "document",
        request.timeout_ms ?? 30_000,
        actionContext,
      );
      this.logger.info(actionContext, step, "success", {
        page_url: safeURL(pageURL),
        filename: saved.filename,
        size: saved.size,
      });
      return saved;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "DOWNLOAD_FAILED",
        message: "当前文档页没保存成功，我已经把原因记下来了",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { page_url: safeURL(pageURL) },
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
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

  /** launchOptions 生成 CloakBrowser 官方启动参数。 */
  private launchOptions(request: BrowserStartRequest): LaunchContextOptions {
    const options: LaunchContextOptions = {
      headless: request.headless ?? false,
      humanize: request.humanize ?? true,
      geoip: request.geoip ?? Boolean(request.proxy),
      ...(request.extension_paths
        ? { extensionPaths: request.extension_paths }
        : {}),
      args: withStableProfileFingerprint(
        request.args,
        request.user_data_dir,
      ),
      launchOptions: {
        downloadsPath: this.downloadManager.directory(),
      },
    };
    if (request.locale) {
      options.locale = request.locale;
    }
    if (request.timezone) {
      options.timezone = request.timezone;
    }
    if (request.user_agent) {
      options.userAgent = request.user_agent;
    }
    if (request.viewport_width && request.viewport_height) {
      options.viewport = {
        width: request.viewport_width,
        height: request.viewport_height,
      };
    }
    if (request.proxy) {
      options.proxy = request.proxy;
    }
    return options;
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

/** assertHTTPDocumentURL 拒绝使用浏览器会话读取非 HTTP 文档地址。 */
function assertHTTPDocumentURL(
  rawURL: string,
  actionContext: ActionContext,
): void {
  try {
    const parsed = new URL(rawURL);
    if (parsed.protocol === "http:" || parsed.protocol === "https:") {
      return;
    }
  } catch {
    // 统一在下方返回结构化错误。
  }
  throw documentError(actionContext, "当前页面不是可以保存的网页文档", {
    page_url: safeURL(rawURL),
  });
}

/** assertContentLength 在读取正文前拦截响应头中已经超限的文件。 */
function assertContentLength(
  rawLength: string | undefined,
  maximumBytes: number,
  actionContext: ActionContext,
): void {
  if (!rawLength || !/^\d+$/.test(rawLength.trim())) {
    return;
  }
  const contentLength = Number.parseInt(rawLength, 10);
  if (contentLength > maximumBytes) {
    throw documentError(actionContext, "文档文件太大了，已经停止保存", {
      max_bytes: maximumBytes,
      content_length: contentLength,
    });
  }
}

/** assertContentTypeBeforeRead 在读取正文前拒绝明确不允许的响应类型。 */
function assertContentTypeBeforeRead(
  contentType: string,
  allowedContentTypes: string[],
  actionContext: ActionContext,
): void {
  const allowed = normalizedAllowedContentTypes(allowedContentTypes);
  if (allowed.includes(contentType)) {
    return;
  }
  const genericDocumentType =
    contentType === "" ||
    contentType === "application/octet-stream" ||
    contentType === "binary/octet-stream" ||
    contentType === "application/download";
  if (genericDocumentType && allowed.includes("application/pdf")) {
    return;
  }
  throw documentError(actionContext, "文档类型不在允许范围内，已经停止保存", {
    content_type: contentType || "unknown",
    allowed_content_types: allowed,
  });
}

/** streamCurrentDocument 通过当前浏览器上下文保存标签页文档，不允许调用方传入其他地址。 */
async function streamCurrentDocument(
  context: BrowserContext,
  downloadManager: DownloadManager,
  currentPageURL: string,
  maximumBytes: number,
  allowedContentTypes: string[],
  suggestedFilename: string,
  timeoutMS: number,
  actionContext: ActionContext,
): Promise<DownloadRecord> {
  assertHTTPDocumentURL(currentPageURL, actionContext);
  const deadlineAt = Date.now() + timeoutMS;
  let response: APIResponse | undefined;
  try {
    response = await context.request.get(currentPageURL, {
      headers: {
        accept: allowedContentTypes.join(", "),
        "accept-encoding": "identity",
      },
      failOnStatusCode: false,
      maxRedirects: 5,
      timeout: timeoutMS,
    });
    const responseURL = response.url();
    assertHTTPDocumentURL(responseURL, actionContext);
    const status = response.status();
    if (status < 200 || status >= 300) {
      throw new WorkerError({
        code: "DOWNLOAD_FAILED",
        message: `文档页面返回了 HTTP ${status}，暂时没保存下来`,
        action: actionContext.action,
        step: "save_current_document",
        trace_id: actionContext.trace_id,
        retryable: status >= 500,
        details: { status, page_url: safeURL(responseURL) },
      });
    }
    const headers = response.headers();
    const contentType = normalizeContentType(headers["content-type"] ?? "");
    assertContentLength(
      headers["content-length"],
      maximumBytes,
      actionContext,
    );
    assertContentTypeBeforeRead(
      contentType,
      allowedContentTypes,
      actionContext,
    );
    const body = await response.body();
    if (Date.now() >= deadlineAt) {
      throw documentError(actionContext, "保存文档页面超过总时限，已经停止读取", {
        page_url: safeURL(responseURL),
      });
    }
    if (body.length > maximumBytes) {
      throw documentError(actionContext, "文档实际大小超过限制，已经停止保存", {
        max_bytes: maximumBytes,
        actual_bytes: body.length,
      });
    }
    const requiredPrefix = requiresPDFSignature(
      contentType,
      allowedContentTypes,
    )
      ? Buffer.from("%PDF-", "latin1")
      : undefined;
    return await downloadManager.saveFetchedDocument(
      singleDocumentChunk(body),
      maximumBytes,
      requiredPrefix,
      responseURL,
      currentPageURL,
      suggestedFilename,
      actionContext,
      deadlineAt,
    );
  } finally {
    await response?.dispose().catch(() => undefined);
  }
}

/** singleDocumentChunk 把 BrowserContext 请求返回的单个 Buffer 交给统一文件保存流程。 */
async function* singleDocumentChunk(body: Buffer): AsyncGenerator<Buffer> {
  yield body;
}

/** requiresPDFSignature 判断当前响应必须以 PDF 文件签名开头。 */
function requiresPDFSignature(
  contentType: string,
  allowedContentTypes: string[],
): boolean {
  const allowed = normalizedAllowedContentTypes(allowedContentTypes);
  const declaresPDF = contentType === "application/pdf";
  const genericType =
    contentType === "" ||
    contentType === "application/octet-stream" ||
    contentType === "binary/octet-stream" ||
    contentType === "application/download";
  return allowed.includes("application/pdf") && (declaresPDF || genericType);
}

/** normalizedAllowedContentTypes 统一调用方传入的内容类型格式。 */
function normalizedAllowedContentTypes(values: string[]): string[] {
  return values.map(normalizeContentType).filter(Boolean);
}

/** normalizeContentType 去掉响应类型参数并统一小写。 */
function normalizeContentType(value: string): string {
  return value.toLowerCase().split(";", 1)[0]?.trim() ?? "";
}

/** documentError 创建保存当前文档页使用的统一下载错误。 */
function documentError(
  actionContext: ActionContext,
  message: string,
  details: JsonObject,
): WorkerError {
  return new WorkerError({
    code: "DOWNLOAD_FAILED",
    message,
    action: actionContext.action,
    step: "save_current_document",
    trace_id: actionContext.trace_id,
    retryable: false,
    details,
  });
}
