// 文件作用说明：统一检查浏览器请求取消和截止时间，阻止旧请求继续发送读取或输入命令。
import type { ActionContext } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";
import { actionContextStorage } from "../../logging/operation.js";

/** assertRequestActive 同时检查显式请求与队列执行上下文，不允许旧请求绕过取消信号。 */
export function assertRequestActive(context: ActionContext | undefined = actionContextStorage.getStore(), step = "read_cancelled"): void {
  const execution = actionContextStorage.getStore();
  for (const current of execution === context ? [context] : [execution, context]) {
    if (!current || (!current.signal?.aborted && Date.now() < (current.deadline_at ?? Infinity))) continue;
    if (current.signal?.reason instanceof WorkerError) throw current.signal.reason;
    throw new WorkerError({ code: "ACTION_CANCELLED", message: "这次操作已停止，浏览器窗口保留着，请检查页面后重新开始",
      action: current.action, step, trace_id: current.trace_id, retryable: false,
      details: { request_id: current.request_id ?? "", reason: current.signal?.aborted ? "request_cancelled" : "deadline_exceeded" } });
  }
}

/** readWhileActive 只执行仍有效的标准读取；已发出的协议命令等待结束后，不再继续旧请求。 */
export async function readWhileActive<T>(operation: () => Promise<T>): Promise<T> {
  assertRequestActive();
  const value = await operation();
  assertRequestActive();
  return value;
}
