// 文件作用说明：实现封装好的鼠标移动能力，只接收已找到元素并调用鼠标移动原子能力。

import type { ActionContext } from "../../contracts/common.js";
import type { ElementView } from "../../contracts/selector.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
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
      const paddingX = Math.min(box.width * 0.2, 16);
      const paddingY = Math.min(box.height * 0.2, 12);
      const safeWidth = Math.max(1, box.width - paddingX * 2);
      const safeHeight = Math.max(1, box.height - paddingY * 2);
      const minX = Math.max(1, box.x + paddingX);
      const maxX = Math.min(
        found.view.viewport.width - 1,
        box.x + box.width - paddingX,
      );
      const minY = Math.max(1, box.y + paddingY);
      const maxY = Math.min(
        found.view.viewport.height - 1,
        box.y + box.height - paddingY,
      );
      const x =
        maxX > minX
          ? minX + Math.random() * Math.min(safeWidth, maxX - minX)
          : Math.max(1, Math.min(found.view.viewport.width - 1, box.x + box.width / 2));
      const y =
        maxY > minY
          ? minY + Math.random() * Math.min(safeHeight, maxY - minY)
          : Math.max(1, Math.min(found.view.viewport.height - 1, box.y + box.height / 2));
      const distance = Math.hypot(x, y);
      const steps = Math.max(6, Math.min(30, Math.round(distance / 45)));
      await this.mouse.move(found.page, x, y, steps);
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
      this.logger.error(actionContext, step, "failed", normalized.details);
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
    this.logger.info(actionContext, "move", "success", {
      source: "viewport_center",
      x: Math.round(x),
      y: Math.round(y),
    });
  }
}
