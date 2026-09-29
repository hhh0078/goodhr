// 文件作用说明：串行执行页面操作，取消过期请求，并在操作卡住时隔离队列，禁止旧动作与新动作重叠。
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { exitDiagnostics } from "../../logging/exit-diagnostics.js";

/** ActionQueue 保证鼠标、键盘及页面切换一次只执行一个动作。 */
export class ActionQueue {
  private tail: Promise<void> = Promise.resolve();
  private fault: WorkerError | undefined;
  private readonly faultSignal = new AbortController();
  private active: ActionContext | undefined;
  private pending = 0;
  private readonly logger = new WorkerLogger();

  /** 创建队列；故障回调只用于关闭旧浏览器，不允许直接放行新动作。 */
  constructor(private readonly onFault: (context: ActionContext) => void = () => {}) {}

  /** status 返回排队数量、当前操作及是否必须重启 Worker。 */
  status(): JsonObject {
    return { blocked: Boolean(this.fault), pending: this.pending,
      active_action: this.active?.action ?? "", active_request_id: this.active?.request_id ?? "",
      active_ms: this.active ? Date.now() - this.active.started_at : 0 };
  }

  /** run 排队执行；超时后隔离旧会话，由 Go 回收进程树，不能仅放行后续动作。 */
  run<T>(operation: () => Promise<T>, context: ActionContext = {
    trace_id: "", action: "interaction", started_at: Date.now(),
  }): Promise<T> {
    if (this.fault) return Promise.reject(this.fault);
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
        if (context.signal?.aborted) throw this.cancelError(context, false);
        started = true;
        this.active = context;
        this.logger.info(context, "queue", "start", this.status());
        const value = await operation();
        if (!finished) resolve(value);
      });
      this.tail = queued.then(() => undefined, (error: unknown) => {
        if (error instanceof WorkerError && error.code === "WORKER_UNRESPONSIVE" && !this.fault) {
          this.fault = error;
          this.logger.failure(context, error);
          this.faultSignal.abort();
          this.onFault(context);
        }
        reject(error);
      });
    });
    /** cancel 取消未执行请求；已开始的动作无法安全撤销，因此隔离整个队列。 */
    const cancel = (): void => {
      if (finished) return;
      const error = this.cancelError(context, started);
      if (started && !this.fault) {
        this.fault = error;
        this.logger.failure(context, error);
        this.faultSignal.abort();
        this.onFault(context);
      }
      rejectResult(error);
    };
    /** rejectFault 让等待中的请求立即收到队列故障，不再各等一次超时。 */
    const rejectFault = (): void => { if (this.fault) rejectResult(this.fault); };
    this.faultSignal.signal.addEventListener("abort", rejectFault, { once: true });
    const timer = setTimeout(cancel, Math.max(1, (context.deadline_at ?? Date.now() + 40_000) - Date.now()));
    context.signal?.addEventListener("abort", cancel, { once: true });
    if (context.signal?.aborted) cancel();
    return result.finally(() => {
      finished = true;
      clearTimeout(timer);
      this.faultSignal.signal.removeEventListener("abort", rejectFault);
      context.signal?.removeEventListener("abort", cancel);
      this.pending--;
      if (this.active === context && !this.fault) this.active = undefined;
      this.logger.info(context, "queue", this.fault ? "blocked" : "finished", this.status());
    });
  }

  /** cancelError 区分尚未执行的过期请求与必须回收浏览器的进行中请求。 */
  private cancelError(context: ActionContext, started: boolean): WorkerError {
    return new WorkerError({
      code: started ? "WORKER_UNRESPONSIVE" : "ACTION_CANCELLED",
      message: started ? "浏览器操作未在限定时间内完成，当前任务已停止；重新开始时会恢复浏览器" : "这次操作已取消，还没有执行页面动作",
      action: context.action, step: "queue", trace_id: context.trace_id, retryable: false,
      details: { request_id: context.request_id ?? "", elapsed_ms: Date.now() - context.started_at,
        timeout_ms: context.deadline_at ? context.deadline_at - context.started_at : 40_000,
        reason: context.signal?.aborted ? "client_disconnected" : "deadline_exceeded", recovery_required: started,
        output: { queue: this.status(), diagnostics: exitDiagnostics(context.trace_id) } },
    });
  }
}
