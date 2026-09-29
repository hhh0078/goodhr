// 文件作用说明：沿异步请求保留追踪编号，记录可能卡住的原子操作及其独立耗时，不记录输入内容。
import { AsyncLocalStorage } from "node:async_hooks";
import type { ActionContext, JsonObject } from "../contracts/common.js";
import { nativeMouseDiagnostics, nativeMouseErrorText } from "../errors/native-mouse-diagnostics.js";
import { WorkerLogger } from "./logger.js";

export const actionContextStorage = new AsyncLocalStorage<ActionContext>();
const logger = new WorkerLogger();

/** traceOperation 记录操作前后状态，崩溃或卡住时最后一条 start 即为待查步骤。 */
export async function traceOperation<T>(step: string, operation: () => Promise<T>, fields: JsonObject = {}): Promise<T> {
  const context = actionContextStorage.getStore();
  if (!context) return operation();
  const started = Date.now();
  logger.info(context, step, "start", fields);
  try {
    const value = await operation();
    logger.info(context, step, "success", { ...fields, step_duration_ms: Date.now() - started });
    return value;
  } catch (error) {
    const nativeMouse = nativeMouseDiagnostics(error);
    logger.error(context, step, "failed", {
      ...fields, step_duration_ms: Date.now() - started, error: nativeMouseErrorText(error),
      ...(nativeMouse ? { native_mouse: nativeMouse } : {}),
    });
    throw error;
  }
}
