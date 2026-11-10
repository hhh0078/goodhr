// 文件作用说明：阻止已取消请求继续输入，并跟踪已经发出的原生输入，恢复队列前必须等待其结束。
import type { ActionContext } from "../../contracts/common.js";
import { assertRequestActive } from "./request-lifecycle.js";
import { actionContextStorage } from "../../logging/operation.js";

const pendingInputs = new WeakMap<ActionContext, Set<Promise<void>>>();

/** assertInputAllowed 在每次原生输入前后检查取消和限时，防止旧操作继续点击或逐字发送。 */
export function assertInputAllowed(context = actionContextStorage.getStore()): void {
  assertRequestActive(context, "input_cancelled");
}

/** trackNativeInput 跟踪底层协议 Promise；上层超时也不能把尚未结束的原生输入当成已结束。 */
export async function trackNativeInput<T>(operation: () => Promise<T>, cleanup = false): Promise<T> {
  const context = actionContextStorage.getStore();
  if (!cleanup) assertInputAllowed(context);
  const input = operation();
  if (!context) return input;
  let pending = pendingInputs.get(context);
  if (!pending) { pending = new Set(); pendingInputs.set(context, pending); }
  const settled = input.then(() => undefined, () => undefined);
  pending.add(settled);
  void settled.then(() => pending.delete(settled));
  const result = await input;
  if (!cleanup) assertInputAllowed(context);
  return result;
}

/** waitForNativeInputs 等待旧请求所有已经发出的输入结束，不重放操作，也不关闭浏览器。 */
export async function waitForNativeInputs(context: ActionContext): Promise<void> {
  const pending = pendingInputs.get(context);
  while (pending?.size) await Promise.all([...pending]);
  pendingInputs.delete(context);
}
