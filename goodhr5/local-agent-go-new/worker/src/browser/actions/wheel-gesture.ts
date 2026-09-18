// 文件作用：为滚动和长截图共用真实滚轮节奏，保持总距离和方向可控，不操作页面脚本。
import { setTimeout as pause } from "node:timers/promises";
import type { Page } from "playwright-core";
import type { ActionContext } from "../../contracts/common.js";
import type { MousePrimitive } from "../primitives/mouse.js";
import type { WorkerLogger } from "../../logging/logger.js";

/** variedDistance 在给定上限内改变单次滚动距离，保留方向，避免截图出现缺口。 */
export function variedDistance(maximum: number, random = Math.random): number {
  return Math.sign(maximum) * Math.max(1, Math.round(Math.abs(maximum) * (0.68 + random() * 0.32)));
}

/** wheelPlan 生成先快后缓的分段滚轮，所有分段之和严格等于目标距离。 */
export function wheelPlan(x: number, y: number, random = Math.random): Array<{ x: number; y: number; wait: number }> {
  const count = Math.min(Math.max(1, Math.ceil(Math.max(Math.abs(x), Math.abs(y)) / 40)), 3 + Math.floor(random() * 4));
  const weights = Array.from({ length: count }, (_, index) => (count - index) * (0.7 + random() * 0.6));
  const total = weights.reduce((sum, value) => sum + value, 0);
  let sentX = 0, sentY = 0, cumulative = 0;
  return weights.map((weight, index) => {
    cumulative += weight;
    const nextX = index === count - 1 ? x : Math.round(x * cumulative / total);
    const nextY = index === count - 1 ? y : Math.round(y * cumulative / total);
    const part = { x: nextX - sentX, y: nextY - sentY, wait: 18 + Math.floor(random() * 35) };
    sentX = nextX; sentY = nextY;
    return part;
  });
}

/** wheelGesture 串行执行真实滚轮并记录节奏，取消后不发送后续分段。 */
export async function wheelGesture(mouse: MousePrimitive, page: Page, x: number, y: number, context: ActionContext, logger: WorkerLogger): Promise<void> {
  const plan = wheelPlan(x, y);
  logger.info(context, "wheel_gesture", "start", { delta_x: x, delta_y: y, strokes: plan.length, intervals_ms: plan.map(part => part.wait) });
  for (const part of plan) {
    context.signal?.throwIfAborted();
    await mouse.wheel(page, part.x, part.y);
    await pause(part.wait, undefined, { signal: context.signal });
  }
}

/** readingPause 改变滚动后的阅读停留时间，并响应请求取消。 */
export async function readingPause(base: number, context: ActionContext): Promise<void> {
  await pause(Math.max(80, Math.round(base * (0.75 + Math.random() * 1.1))), undefined, { signal: context.signal });
}
