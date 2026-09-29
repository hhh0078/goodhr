// 文件作用说明：集中记录浏览器和标签页生命周期，隔离旧会话事件并附加退出前诊断。

import type { Browser, BrowserContext, Page } from "playwright-core";
import { WorkerLogger } from "../../logging/logger.js";
import { safeURL } from "./navigation.js";

/** BrowserLifecycle 只观察标准事件和运行信息，不读取或修改页面 DOM。 */
export class BrowserLifecycle {
  lastAction = "启动浏览器";
  lastPageURL = "";
  private activeContext: BrowserContext | null = null;
  private intentionalClose = false;
  private exitRecorded = false;
  private startedAt = 0;
  private lastPageClosedAt = "";
  private readonly registeredPages = new WeakSet<Page>();

  /** 创建浏览器生命周期观察器。 */
  constructor(private readonly logger: WorkerLogger) {}

  /** begin 清理上一会话信息，并记录本次浏览器启动时间。 */
  begin(): void {
    this.activeContext = null;
    this.intentionalClose = false;
    this.exitRecorded = false;
    this.startedAt = Date.now();
    this.lastAction = "启动浏览器";
    this.lastPageURL = "";
    this.lastPageClosedAt = "";
  }

  /** isCurrent 判断事件是否属于仍在使用的浏览器会话。 */
  isCurrent(context: BrowserContext): boolean { return this.activeContext === context; }

  /** dispose 提前记录主动关闭意图，忽略随后到达的旧会话事件。 */
  dispose(): void { this.intentionalClose = true; this.activeContext = null; }

  /** register 观察上下文、浏览器连接和新页面，全部使用 Playwright 标准事件。 */
  register(context: BrowserContext, browser: Browser | null): void {
    this.activeContext = context;
    context.on("close", () => {
      if (this.isCurrent(context)) this.recordExit("浏览器上下文关闭，可能是浏览器窗口被关闭或进程退出", "context.close");
    });
    browser?.on("disconnected", () => {
      if (this.isCurrent(context)) this.recordExit("浏览器进程连接断开，具体原因需结合原生进程退出日志", "browser.disconnected");
    });
    for (const page of context.pages()) this.registerPage(page, context);
    context.on("page", page => {
      if (!this.isCurrent(context)) return;
      this.lastAction = "新建标签页";
      this.registerPage(page, context);
    });
  }

  /** registerPage 保存页面崩溃、关闭时间和最后导航地址，不保留页面正文。 */
  private registerPage(page: Page, context: BrowserContext): void {
    if (this.registeredPages.has(page)) return;
    this.registeredPages.add(page);
    this.lastPageURL = safeURL(page.url());
    page.on("crash", () => {
      if (!this.isCurrent(context)) return;
      this.lastAction = "页面崩溃";
      this.lastPageURL = safeURL(page.url());
      this.logger.error({ trace_id: "", action: "browser.crash", started_at: Date.now() }, "page_crash", "failed", {
        category: "异常退出", error_message: "浏览器报告标签页崩溃", reason_source: "page.crash",
        page_url: this.lastPageURL, browser_uptime_ms: this.startedAt ? Date.now() - this.startedAt : 0,
      });
    });
    page.on("close", () => { if (this.isCurrent(context)) this.lastPageClosedAt = new Date().toISOString(); });
    page.on("framenavigated", frame => {
      if (this.isCurrent(context) && frame === page.mainFrame()) this.lastPageURL = safeURL(frame.url());
    });
  }

  /** recordExit 非主动关闭时只记录一次断开事件，并明确原生退出码需要另外观察。 */
  private recordExit(reason: string, source: string): void {
    if (this.intentionalClose || this.exitRecorded) return;
    this.exitRecorded = true;
    this.logger.error({ trace_id: "", action: "browser.exit", started_at: Date.now() }, "unexpected_exit", "failed", {
      category: "异常退出", error_message: reason, exit_reason: reason, reason_source: source,
      last_page_closed_at: this.lastPageClosedAt, process_exit_observed: false,
      last_action: this.lastAction, page_url: this.lastPageURL,
      browser_uptime_ms: this.startedAt ? Date.now() - this.startedAt : 0,
      input: { last_action: this.lastAction, page_url: this.lastPageURL },
      output: { exit_reason: reason, exit_code: null },
    });
  }
}
