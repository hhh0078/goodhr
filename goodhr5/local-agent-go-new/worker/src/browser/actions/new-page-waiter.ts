// 文件作用说明：用标准浏览器事件等待新标签页，提前处理超时并在点击结束时清理监听。

import type { BrowserContext, Page } from "playwright-core";
import type { ActionContext } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";

type PageOutcome = { page: Page } | { error: WorkerError };

/** NewPageWaiter 保存点击期间的新标签页结果，后台等待本身不会产生未处理的拒绝。 */
export class NewPageWaiter {
  private readonly outcome: Promise<PageOutcome>;
  private complete!: (value: PageOutcome) => void;
  private timer?: ReturnType<typeof setTimeout>;
  private settled = false;

  /** 创建可取消的标签页等待，在鼠标动作开始前安装标准事件监听。 */
  constructor(
    private readonly browserContext: BrowserContext,
    private readonly actionContext: ActionContext,
    private readonly timeoutMs: number,
  ) {
    this.outcome = new Promise((resolve) => { this.complete = resolve; });
    browserContext.on("page", this.onPage);
    browserContext.on("close", this.onClose);
    actionContext.signal?.addEventListener("abort", this.onAbort, { once: true });
    this.timer = setTimeout(() => this.finish({ error: this.error("ACTION_TIMEOUT", "等待新标签页超时") }), timeoutMs);
    if (actionContext.signal?.aborted) this.onAbort();
  }

  /** wait 仅在调用者等待结果时抛出超时或取消错误，交给点击流程统一处理。 */
  async wait(): Promise<Page> {
    const result = await this.outcome;
    if ("error" in result) throw result.error;
    return result.page;
  }

  /** cancel 在点击失败或结束后移除监听和计时器，防止遗留后台等待。 */
  cancel(): void {
    this.finish({ error: this.error("ACTION_CANCELLED", "点击已经结束，停止等待新标签页") });
  }

  /** onPage 保存新标签页并立即释放监听。 */
  private readonly onPage = (page: Page): void => { this.finish({ page }); };

  /** onClose 保存浏览器关闭原因，由点击调用者接收错误。 */
  private readonly onClose = (): void => {
    this.finish({ error: this.error("PAGE_CLOSED", "等待新标签页时浏览器已关闭") });
  };

  /** onAbort 在请求取消时结束标签页等待。 */
  private readonly onAbort = (): void => {
    this.finish({ error: this.error("ACTION_CANCELLED", "请求已取消，停止等待新标签页") });
  };

  /** finish 只接收第一个结果，并清理本次等待创建的全部资源。 */
  private finish(result: PageOutcome): void {
    if (this.settled) return;
    this.settled = true;
    clearTimeout(this.timer);
    this.browserContext.off("page", this.onPage);
    this.browserContext.off("close", this.onClose);
    this.actionContext.signal?.removeEventListener("abort", this.onAbort);
    this.complete(result);
  }

  /** error 构造带原始步骤、超时和调用标识的等待错误。 */
  private error(code: "ACTION_TIMEOUT" | "ACTION_CANCELLED" | "PAGE_CLOSED", message: string): WorkerError {
    return new WorkerError({
      code, message, action: this.actionContext.action, step: "wait_new_page",
      trace_id: this.actionContext.trace_id, retryable: false,
      details: { timeout_ms: this.timeoutMs, clicked: true },
    });
  }
}
