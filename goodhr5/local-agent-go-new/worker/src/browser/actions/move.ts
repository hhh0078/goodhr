// 文件作用说明：实现封装好的鼠标移动能力，只接收已找到元素并调用鼠标移动原子能力。

import type { ActionContext } from "../../contracts/common.js";
import type { ElementView } from "../../contracts/selector.js";
import type { Page } from "playwright-core";
import {
  normalizeWorkerError,
  WorkerError,
} from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { LocatorPrimitive, type ResolvedElement } from "../primitives/locator.js";
import type { VisibleArea } from "./scroll-geometry.js";
import { nativeMouseDiagnostics } from "../../errors/native-mouse-diagnostics.js";

/** MoveResult 表示鼠标移动位置和元素视口状态。 */
export interface MoveResult {
  x: number;
  y: number;
  view: ElementView;
}

/** MoveAction 实现对已找到元素的通用安全移动。 */
export class MoveAction {
  /** 创建鼠标移动封装能力。 */
  constructor(
    private readonly mouse: MousePrimitive,
    private readonly logger: WorkerLogger,
    private readonly locator: Pick<LocatorPrimitive, "view"> = new LocatorPrimitive(),
    private readonly reactivatePage: (page: Page, context: ActionContext) => Promise<void> = page => page.bringToFront(),
  ) {}

  /** toElement 移动到元素、窗口和可选容器交集内的安全随机点，不重新查找。 */
  async toElement(
    found: ResolvedElement,
    actionContext: ActionContext,
    options: { allowAckRetry?: boolean; allowWindowRetry?: boolean; centered?: boolean; visibleArea?: VisibleArea } = {},
  ): Promise<MoveResult> {
    const step = "move";
    this.logger.info(actionContext, step, "start");
    try {
      const { box } = found.view;
      if (box.width <= 0 || box.height <= 0) {
        throw new Error("元素没有有效位置");
      }
      const viewport = found.view.viewport;
      const visibleLeft = Math.max(1, box.x, options.visibleArea?.left ?? 0);
      const visibleTop = Math.max(1, box.y, options.visibleArea?.top ?? 0);
      const visibleRight = Math.min(viewport.width - 1, box.x + box.width, options.visibleArea?.right ?? viewport.width);
      const visibleBottom = Math.min(viewport.height - 1, box.y + box.height, options.visibleArea?.bottom ?? viewport.height);
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
      const paddingX = options.centered ? visibleWidth * 0.3 : Math.min(16, box.width * 0.2, visibleWidth * 0.25);
      const paddingY = options.centered ? visibleHeight * 0.3 : Math.min(12, box.height * 0.2, visibleHeight * 0.25);
      const minX = visibleLeft + paddingX;
      const maxX = visibleRight - paddingX;
      const minY = visibleTop + paddingY;
      const maxY = visibleBottom - paddingY;
      const x = randomBetween(minX, maxX);
      const y = randomBetween(minY, maxY);
      await this.mouse.move(found.page, x, y);
      const result = { x, y, view: found.view };
      this.logger.info(actionContext, step, "success", {
        x: Math.round(x),
        y: Math.round(y),
        movement: "native_humanize",
      });
      return result;
    } catch (error) {
      if (options.allowWindowRetry !== false && await this.reactivateWindowBeforeMove(found.page, actionContext, error)) {
        try {
          // 标签重新激活后重新读取坐标，避免把旧位置用于后续候选人。
          found.view = await this.locator.view(found.page, found.locator);
        } catch (refreshError) {
          this.logger.error(actionContext, "window_recovery", "failed", {
            attempt: 1, reason: "target_refresh_failed", error: String(refreshError),
          });
          // 目标坐标没有重新确认，保留最初的窗口错误以回收失活会话。
          throw normalizeWorkerError(error, {
            code: "MOVE_FAILED", message: "鼠标没顺利移动到目标位置",
            action: actionContext.action, step, trace_id: actionContext.trace_id,
          });
        }
        try {
          const result = await this.toElement(found, actionContext, { ...options, allowAckRetry: false, allowWindowRetry: false });
          this.logger.info(actionContext, "window_recovery", "success", { attempt: 1, action: "window_reactivation" });
          return result;
        } catch (retryError) {
          this.logger.error(actionContext, "window_recovery", "failed", {
            attempt: 1, reason: "move_retry_failed",
            native_mouse: retryError instanceof WorkerError ? retryError.details.native_mouse ?? {} : nativeMouseDiagnostics(retryError) ?? {},
          });
          throw retryError;
        }
      }
      if (options.allowAckRetry !== false && nativeMouseDiagnostics(error)?.native_error_code === "GOODHR_MOUSE_ACK_TIMEOUT" &&
          !actionContext.signal?.aborted &&
          (actionContext.deadline_at ?? Infinity) - Date.now() > 7_000) {
        this.logger.warn(actionContext, step, "retry", { reason: "native_mouse_ack_timeout", attempt: 2 });
        // 原协议调用已经明确结束；只重新定位和移动，不重放任何按键、点击或发送。
        found.view = await this.locator.view(found.page, found.locator);
        return this.toElement(found, actionContext, { ...options, allowAckRetry: false });
      }
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
    try {
      await this.mouse.move(page, x, y);
    } catch (error) {
      if (!await this.reactivateWindowBeforeMove(page, actionContext, error)) throw error;
      try {
        await this.mouse.move(page, x, y);
        this.logger.info(actionContext, "window_recovery", "success", { attempt: 1, action: "window_reactivation" });
      } catch (retryError) {
        this.logger.error(actionContext, "window_recovery", "failed", {
          attempt: 1, reason: "move_retry_failed",
          native_mouse: retryError instanceof WorkerError ? retryError.details.native_mouse ?? {} : nativeMouseDiagnostics(retryError) ?? {},
        });
        throw retryError;
      }
    }
    this.logger.info(actionContext, "move", "success", {
      source: "viewport_center",
      movement: "native_humanize",
      x: Math.round(x),
      y: Math.round(y),
    });
  }

  /** reactivateWindowBeforeMove 只在原生鼠标尚未发出时恢复窗口，并记录一次恢复结果。 */
  private async reactivateWindowBeforeMove(
    page: ResolvedElement["page"],
    actionContext: ActionContext,
    error: unknown,
  ): Promise<boolean> {
    const diagnostic = nativeMouseDiagnostics(error);
    if (diagnostic?.native_error_code !== "GOODHR_MOUSE_WINDOW_NOT_READY" ||
        diagnostic.event_types !== "mousemove" || diagnostic.events_sent !== 0 ||
        diagnostic.events_acknowledged !== 0 || actionContext.signal?.aborted ||
        (actionContext.deadline_at ?? Infinity) - Date.now() < 7_000 ||
        (typeof page.isClosed === "function" && page.isClosed())) {
      return false;
    }
    this.logger.warn(actionContext, "window_recovery", "start", {
      attempt: 1, action: "window_reactivation", native_mouse: diagnostic,
    });
    try {
      await this.reactivatePage(page, actionContext);
      this.logger.info(actionContext, "window_recovery", "retry", {
        attempt: 1, action: "window_reactivation", input_sent_before_retry: false,
      });
      return true;
    } catch (recoveryError) {
      this.logger.error(actionContext, "window_recovery", "failed", {
        attempt: 1, action: "window_reactivation", reason: "window_activation_failed",
        error: String(recoveryError),
      });
      return false;
    }
  }
}

/** randomBetween 返回两个边界之间的随机坐标，边界相同时直接返回该位置。 */
function randomBetween(minimum: number, maximum: number): number {
  if (maximum <= minimum) {
    return minimum;
  }
  return minimum + Math.random() * (maximum - minimum);
}
