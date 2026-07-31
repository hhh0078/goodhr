// 文件作用说明：实现封装好的鼠标移动能力，只接收已找到元素并调用鼠标移动原子能力。

import type { ActionContext } from "../../contracts/common.js";
import type { ElementView } from "../../contracts/selector.js";
import {
  normalizeWorkerError,
  WorkerError,
} from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { MousePrimitive } from "../primitives/mouse.js";
import type { ResolvedElement } from "../primitives/locator.js";

/** MoveResult 表示鼠标移动位置和元素视口状态。 */
export interface MoveResult {
  x: number;
  y: number;
  steps: number;
  view: ElementView;
}

/** MoveAction 实现对已找到元素的通用安全移动。 */
export class MoveAction {
  private readonly lastPositions = new WeakMap<
    ResolvedElement["page"],
    { x: number; y: number }
  >();

  /** 创建鼠标移动封装能力。 */
  constructor(
    private readonly mouse: MousePrimitive,
    private readonly logger: WorkerLogger,
  ) {}

  /** toElement 移动到已找到元素内部的安全随机点，不重新查找。 */
  async toElement(
    found: ResolvedElement,
    actionContext: ActionContext,
  ): Promise<MoveResult> {
    const step = "move";
    this.logger.info(actionContext, step, "start");
    try {
      const { box } = found.view;
      if (box.width <= 0 || box.height <= 0) {
        throw new Error("元素没有有效位置");
      }
      const viewport = found.view.viewport;
      const visibleLeft = Math.max(1, box.x);
      const visibleTop = Math.max(1, box.y);
      const visibleRight = Math.min(viewport.width - 1, box.x + box.width);
      const visibleBottom = Math.min(viewport.height - 1, box.y + box.height);
      if (
        viewport.width <= 2 ||
        viewport.height <= 2 ||
        visibleRight <= visibleLeft ||
        visibleBottom <= visibleTop
      ) {
        throw new WorkerError({
          code: "MOUSE_TARGET_OUTSIDE_VIEWPORT",
          message: "目标没有位于浏览器窗口内，鼠标没有安全落点",
          action: actionContext.action,
          step,
          trace_id: actionContext.trace_id,
          retryable: true,
          details: {
            box: {
              x: box.x,
              y: box.y,
              width: box.width,
              height: box.height,
            },
            viewport: {
              width: viewport.width,
              height: viewport.height,
            },
          },
        });
      }
      const visibleWidth = visibleRight - visibleLeft;
      const visibleHeight = visibleBottom - visibleTop;
      const paddingX = Math.min(16, box.width * 0.2, visibleWidth * 0.25);
      const paddingY = Math.min(12, box.height * 0.2, visibleHeight * 0.25);
      const minX = visibleLeft + paddingX;
      const maxX = visibleRight - paddingX;
      const minY = visibleTop + paddingY;
      const maxY = visibleBottom - paddingY;
      const x = randomBetween(minX, maxX);
      const y = randomBetween(minY, maxY);
      const previous = this.lastPositions.get(found.page);
      const distance = previous
        ? Math.hypot(x - previous.x, y - previous.y)
        : Math.hypot(x, y);
      const steps = Math.max(4, Math.min(12, Math.round(distance / 90)));
      await this.mouse.move(found.page, x, y, steps);
      this.lastPositions.set(found.page, { x, y });
      const result = { x, y, steps, view: found.view };
      this.logger.info(actionContext, step, "success", {
        x: Math.round(x),
        y: Math.round(y),
        steps,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "MOVE_FAILED",
        message: "鼠标没顺利移动到目标位置",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** toViewportCenter 把鼠标移动到页面中央安全区域。 */
  async toViewportCenter(
    page: ResolvedElement["page"],
    width: number,
    height: number,
    actionContext: ActionContext,
  ): Promise<void> {
    const x = Math.max(1, width / 2);
    const y = Math.max(1, height / 2);
    await this.mouse.move(page, x, y, 12);
    this.lastPositions.set(page, { x, y });
    this.logger.info(actionContext, "move", "success", {
      source: "viewport_center",
      x: Math.round(x),
      y: Math.round(y),
    });
  }
}

/** randomBetween 返回两个边界之间的随机坐标，边界相同时直接返回该位置。 */
function randomBetween(minimum: number, maximum: number): number {
  if (maximum <= minimum) {
    return minimum;
  }
  return minimum + Math.random() * (maximum - minimum);
}
