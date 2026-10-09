// 文件作用说明：串行执行页面操作，取消过期请求，并在操作卡住时隔离队列，禁止旧动作与新动作重叠。
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { exitDiagnostics } from "../../logging/exit-diagnostics.js";
import { actionContextStorage } from "../../logging/operation.js";
import { waitForNativeInputs } from "../primitives/input-lifecycle.js";

/** ActionQueue 保证鼠标、键盘及页面切换一次只执行一个动作。 */
export class ActionQueue {
  private tail: Promise<void> = Promise.resolve();
  private fault: WorkerError | undefined;
  private faultSignal = new AbortController();
  private active: ActionContext | undefined;
  private pending = 0;
  private readonly logger = new WorkerLogger();

  /** 创建队列；故障回调只记录隔离信息，不能关闭浏览器或直接放行新动作。 */
  constructor(private readonly onFault: (context: ActionContext) => void = () => {}) {}

  /** status 区分 Worker 存活、输入隔离及旧操作是否已结束。 */
  status(): JsonObject {
    return { blocked: Boolean(this.fault), recoverable: Boolean(this.fault) && !this.active && this.pending === 0, pending: this.pending,
      active_action: this.active?.action ?? "", active_request_id: this.active?.request_id ?? "",
      active_ms: this.active ? Date.now() - this.active.started_at : 0 };
  }

  /** runAfterRecovery 仅在明确重新启动且旧动作已结束时恢复队列，不自动重放历史请求。 */
  runAfterRecovery<T>(operation: () => Promise<T>, context: ActionContext): Promise<T> {
    if (context.signal?.aborted || Date.now() >= (context.deadline_at ?? Infinity)) {
      return Promise.reject(this.cancelError(context, false));
    }
    if (this.fault) {
      if (this.active || this.pending > 0) return Promise.reject(new WorkerError({
        code: "WORKER_UNRESPONSIVE", message: "上一次浏览器操作还没结束，窗口已经保留，请稍后再开始",
        action: context.action, step: "queue_recovery_wait", trace_id: context.trace_id, retryable: false,
        details: { browser_retained: true, output: { queue: this.status() } },
      }));
      this.fault = undefined;
      this.faultSignal = new AbortController();
      this.logger.info(context, "queue_recovery", "success", { browser_retained: true, input_replayed: false });
    }
    return this.run(operation, context);
  }

  /** run 排队执行；超时后保留浏览器并隔离输入，等待底层旧操作结束后才允许明确恢复。 */
  run<T>(operation: () => Promise<T>, context: ActionContext = {
    trace_id: "", action: "interaction", started_at: Date.now(),
  }): Promise<T> {
    if (this.fault) return Promise.reject(this.fault);
    const requestSignal = context.signal;
    const executionCancel = new AbortController();
    const executionContext: ActionContext = { ...context,
      signal: requestSignal ? AbortSignal.any([requestSignal, executionCancel.signal]) : executionCancel.signal };
    const faultSignal = this.faultSignal.signal;
    this.pending++;
    this.logger.info(context, "queue", "queued", this.status());
    let started = false;
    let finished = false;
    let rejectResult!: (error: Error) => void;
    const result = new Promise<T>((resolve, reject) => {
      rejectResult = reject;
      const queued = this.tail.then(async () => {
        if (finished) return;
        if (this.fault) throw this.fault;
        if (requestSignal?.aborted || Date.now() >= (context.deadline_at ?? Infinity)) throw this.cancelError(context, false);
        started = true;
        this.active = context;
        this.logger.info(context, "queue", "start", this.status());
        try {
          const value = await actionContextStorage.run(executionContext, operation);
          if (!finished) resolve(value);
        } catch (error) {
          if (error instanceof WorkerError && error.code === "WORKER_UNRESPONSIVE") this.isolate(context, error, executionCancel);
          reject(error);
        } finally {
          // 鼠标独立限时可能先返回，但原生 Promise 仍在执行，不能在这里提前放行下一次启动。
          await waitForNativeInputs(executionContext);
        }
      });
      this.tail = queued.then(() => undefined, (error: unknown) => {
        reject(error);
      }).finally(() => {
        if (this.active === context) this.active = undefined;
      });
    });
    /** cancel 取消未执行请求；已开始的动作无法安全撤销，因此隔离整个队列。 */
    const cancel = (): void => {
      if (finished) return;
      const error = this.cancelError(context, started);
      if (started) this.isolate(context, error, executionCancel);
      rejectResult(error);
    };
    /** rejectFault 让等待中的请求立即收到队列故障，不再各等一次超时。 */
    const rejectFault = (): void => { if (this.fault) rejectResult(this.fault); };
    faultSignal.addEventListener("abort", rejectFault, { once: true });
    const timer = setTimeout(cancel, Math.max(1, (context.deadline_at ?? Date.now() + 40_000) - Date.now()));
    requestSignal?.addEventListener("abort", cancel, { once: true });
    if (requestSignal?.aborted) cancel();
    return result.finally(() => {
      finished = true;
      clearTimeout(timer);
      faultSignal.removeEventListener("abort", rejectFault);
      requestSignal?.removeEventListener("abort", cancel);
      this.pending--;
      this.logger.info(context, "queue", this.fault ? "blocked" : "finished", this.status());
    });
  }

  /** isolate 取消当前请求的后续输入并拒绝排队请求，保留浏览器供人工检查。 */
  private isolate(context: ActionContext, error: WorkerError, executionCancel: AbortController): void {
    if (this.fault) { executionCancel.abort(error); return; }
    this.fault = error;
    this.logger.failure(context, error);
    this.faultSignal.abort();
    executionCancel.abort(error);
    this.onFault(context);
  }

  /** cancelError 区分尚未执行的过期请求与需要保留窗口、隔离输入的进行中请求。 */
  private cancelError(context: ActionContext, started: boolean): WorkerError {
    return new WorkerError({
      code: started ? "WORKER_UNRESPONSIVE" : "ACTION_CANCELLED",
      message: started ? "浏览器操作没按时结束，岗位先停一下；窗口保留着，请检查页面后重新开始" : "这次操作已取消，还没有执行页面动作",
      action: context.action, step: "queue", trace_id: context.trace_id, retryable: false,
      details: { request_id: context.request_id ?? "", elapsed_ms: Date.now() - context.started_at,
        timeout_ms: context.deadline_at ? context.deadline_at - context.started_at : 40_000,
        reason: context.signal?.aborted ? "client_disconnected" : "deadline_exceeded", recovery_required: started, browser_retained: true,
        output: { queue: this.status(), diagnostics: exitDiagnostics(context.trace_id) } },
    });
  }
}
