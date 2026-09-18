// 文件作用说明：实现封装好的真实滚轮滚动，平铺执行查找、移动、滚轮和结果验证。

import { createHash } from "node:crypto";
import type { ScrollRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import type { ElementView } from "../../contracts/selector.js";
import {
  normalizeWorkerError,
  WorkerError,
} from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { LocatorPrimitive, type ResolvedElement } from "../primitives/locator.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { ScreenshotPrimitive } from "../primitives/screenshot.js";
import { ViewportPrimitive } from "../primitives/viewport.js";
import { BrowserSession } from "../session/browser-session.js";
import type { FindAction, FoundElement } from "./find.js";
import { MoveAction } from "./move.js";
import { assertTargetCanFit, directedWheel, intersectsViewport, targetGap, viewAccepted, visibleArea as calculateVisibleArea } from "./scroll-geometry.js";

/** ScrollResult 表示真实滚轮滚动次数和前后状态。 */
export interface ScrollResult extends JsonObject {
  scrolled: boolean;
  attempts: number;
  distance: number;
  before: JsonObject;
  after: JsonObject;
}

/** ScrollAction 实现通用页面或目标元素滚动能力。 */
export class ScrollAction {
  private readonly screenshot = new ScreenshotPrimitive();
  private readonly viewport = new ViewportPrimitive();

  /** 创建真实滚轮封装能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly find: FindAction,
    private readonly move: MoveAction,
    private readonly locator: LocatorPrimitive,
    private readonly mouse: MousePrimitive,
    private readonly logger: WorkerLogger,
  ) {}

  /** execute 平铺执行查找、移动、真实滚轮和状态验证。 */
  async execute(
    request: ScrollRequest,
    actionContext: ActionContext,
  ): Promise<ScrollResult> {
    const step = "scroll";
    this.logger.info(actionContext, step, "start", {
      distance: request.distance,
      max_attempts: request.max_attempts ?? 1,
      target_description: request.target?.description ?? "页面",
    });
    try {
      const page = await this.session.requirePage(actionContext, step);
      const target = request.target
        ? await this.find.one(request.target, actionContext, true)
        : null;
      const anchor = request.wheel_anchor
        ? await this.find.one(request.wheel_anchor, actionContext, true)
        : target;
      if (anchor && intersectsViewport(anchor.resolved.view)) {
        await this.move.toElement(anchor.resolved, actionContext, { centered: true });
      } else if (target && intersectsViewport(target.resolved.view)) {
        await this.move.toElement(target.resolved, actionContext, { centered: true });
      } else {
        const viewport = await this.viewport.size(page);
        await this.move.toViewportCenter(
          page,
          viewport.width,
          viewport.height,
          actionContext,
        );
      }
      const before = target
        ? target.resolved.view
        : await this.screenshotState(page);
      let visibleArea = target
        ? calculateVisibleArea(
            target.resolved.view,
            request.wheel_anchor ? anchor?.resolved.view : undefined,
            request.viewport_margin ?? 0,
          )
        : undefined;
      if (target && visibleArea) {
        assertTargetCanFit(
          target.resolved.view,
          visibleArea,
          request.require_full ?? false,
          actionContext,
        );
      }
      const maxAttempts = Math.max(1, request.max_attempts ?? 1);
      let attempts = 0;
      let after: ElementView | JsonObject = before;
      let previousGap =
        target && visibleArea
          ? targetGap(target.resolved.view, visibleArea, request.require_full ?? false)
          : 0;
      let noProgressCount = 0;
      for (let index = 0; index < maxAttempts; index += 1) {
        if (
          target &&
          visibleArea &&
          viewAccepted(
            target.resolved.view,
            request.require_full ?? false,
            visibleArea,
          )
        ) {
          break;
        }
        const distance = target
          ? directedWheel(
              target.resolved.view,
              request.distance,
              visibleArea ??
                calculateVisibleArea(target.resolved.view, undefined, 0),
              request.require_full ?? false,
            )
          : { x: 0, y: request.distance };
        this.logger.info(actionContext, "wheel", "start", {
          attempt: index + 1,
          delta_x: distance.x, delta_y: distance.y,
        });
        await this.mouse.wheel(page, distance.x, distance.y);
        attempts += 1;
        await delay(Math.max(50, request.wait_ms ?? 250));
        after = target
          ? await this.refreshTarget(target.resolved)
          : await this.screenshotState(page);
        if (target) {
          target.resolved.view = after as ElementView;
          const anchorView =
            request.wheel_anchor && anchor
              ? await this.refreshTarget(anchor.resolved)
              : undefined;
          visibleArea = calculateVisibleArea(
            after as ElementView,
            anchorView,
            request.viewport_margin ?? 0,
          );
          assertTargetCanFit(
            after as ElementView,
            visibleArea,
            request.require_full ?? false,
            actionContext,
          );
          const nextGap = targetGap(after as ElementView, visibleArea, request.require_full ?? false);
          if (nextGap >= previousGap - 0.5) {
            noProgressCount += 1;
          } else {
            noProgressCount = 0;
          }
          previousGap = nextGap;
          if (noProgressCount >= 3 && nextGap > 0) {
            throw new WorkerError({
              code: "SCROLL_NO_PROGRESS",
              message: "真实滚轮已经执行，但目标没有继续靠近可操作区域",
              action: actionContext.action,
              step: "wheel",
              trace_id: actionContext.trace_id,
              retryable: true,
              details: {
                attempt: index + 1,
                remaining_distance: Math.round(nextGap),
              },
            });
          }
        }
        this.logger.info(actionContext, "wheel", "success", {
          attempt: index + 1,
          delta_x: distance.x, delta_y: distance.y,
          target_in_viewport:
            "in_viewport" in after ? Boolean(after.in_viewport) : false,
        });
        if (
          target &&
          visibleArea &&
          viewAccepted(
            after as ElementView,
            request.require_full ?? false,
            visibleArea,
          )
        ) {
          target.resolved.view = after as ElementView;
          break;
        }
      }
      if (target && visibleArea && !viewAccepted(target.resolved.view, request.require_full ?? false, visibleArea)) {
        throw new WorkerError({
          code: "SCROLL_FAILED", message: "滚动次数已到上限，目标仍未进入可操作区域",
          action: actionContext.action, step, trace_id: actionContext.trace_id, retryable: true,
          details: { attempts, remaining_distance: Math.round(targetGap(target.resolved.view, visibleArea, request.require_full ?? false)) },
        });
      }
      const result: ScrollResult = {
        scrolled:
          attempts > 0 &&
          JSON.stringify(viewToJson(before)) !==
            JSON.stringify(viewToJson(after)),
        attempts,
        distance: request.distance,
        before: viewToJson(before),
        after: viewToJson(after),
      };
      if (target && attempts > 0 && !result.scrolled) {
        throw new Error("真实滚轮已执行，但页面状态没有变化");
      }
      this.logger.info(actionContext, step, "success", {
        attempts,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "SCROLL_FAILED",
        message: "页面没滚到合适位置，我已经记录卡住的步骤",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** ensureVisible 让已经找到的目标进入视口，不重新查找目标。 */
  async ensureVisible(
    found: FoundElement,
    request: {
      wheel_anchor?: ScrollRequest["wheel_anchor"];
      distance?: number;
      max_attempts?: number;
      viewport_margin?: number;
      require_full?: boolean;
    },
    actionContext: ActionContext,
  ): Promise<void> {
    let anchor: FoundElement | undefined;
    if (request.wheel_anchor) {
      anchor = await this.find.one(
        request.wheel_anchor,
        actionContext,
        true,
      );
    }
    let visibleArea = calculateVisibleArea(
      found.resolved.view,
      anchor?.resolved.view,
      request.viewport_margin ?? 0,
    );
    assertTargetCanFit(
      found.resolved.view,
      visibleArea,
      request.require_full ?? false,
      actionContext,
    );
    if (
      viewAccepted(
        found.resolved.view,
        request.require_full ?? false,
        visibleArea,
      )
    ) {
      return;
    }
    if (anchor && intersectsViewport(anchor.resolved.view)) {
      await this.move.toElement(anchor.resolved, actionContext, { centered: true });
    } else if (intersectsViewport(found.resolved.view)) {
      await this.move.toElement(found.resolved, actionContext, { centered: true });
    } else {
      const viewport = found.resolved.view.viewport;
      await this.move.toViewportCenter(
        found.resolved.page,
        viewport.width,
        viewport.height,
        actionContext,
      );
    }
    const maxAttempts = Math.max(1, request.max_attempts ?? 8);
    let previousGap = targetGap(found.resolved.view, visibleArea, request.require_full ?? false);
    let noProgressCount = 0;
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      const distance = directedWheel(
        found.resolved.view,
        request.distance ?? 160,
        visibleArea,
        request.require_full ?? false,
      );
      await this.mouse.wheel(found.resolved.page, distance.x, distance.y);
      await delay(220);
      found.resolved.view = await this.refreshTarget(found.resolved);
      const anchorView = anchor
        ? await this.refreshTarget(anchor.resolved)
        : undefined;
      visibleArea = calculateVisibleArea(
        found.resolved.view,
        anchorView,
        request.viewport_margin ?? 0,
      );
      assertTargetCanFit(
        found.resolved.view,
        visibleArea,
        request.require_full ?? false,
        actionContext,
      );
      const nextGap = targetGap(found.resolved.view, visibleArea, request.require_full ?? false);
      if (nextGap >= previousGap - 0.5) {
        noProgressCount += 1;
      } else {
        noProgressCount = 0;
      }
      previousGap = nextGap;
      this.logger.info(actionContext, "ensure_visible", "progress", {
        attempt,
        delta_x: distance.x, delta_y: distance.y,
        in_viewport: found.resolved.view.in_viewport,
        remaining_distance: Math.round(nextGap),
      });
      if (noProgressCount >= 3 && nextGap > 0) {
        throw new WorkerError({
          code: "SCROLL_NO_PROGRESS",
          message: "真实滚轮已经执行，但目标没有继续靠近可操作区域",
          action: actionContext.action,
          step: "ensure_visible",
          trace_id: actionContext.trace_id,
          retryable: true,
          details: {
            attempt,
            remaining_distance: Math.round(nextGap),
          },
        });
      }
      if (
        viewAccepted(
          found.resolved.view,
          request.require_full ?? false,
          visibleArea,
        )
      ) {
        return;
      }
    }
    throw new Error("元素滚动后仍未进入可操作区域");
  }

  /** refreshTarget 重新读取同一个 Locator 的最新视口状态。 */
  private async refreshTarget(
    resolved: ResolvedElement,
  ): Promise<ElementView> {
    return this.locator.view(resolved.page, resolved.locator);
  }

  /** screenshotState 使用当前视口截图摘要判断真实滚轮前后是否有变化。 */
  private async screenshotState(
    page: ResolvedElement["page"],
  ): Promise<JsonObject> {
    const image = await this.screenshot.pageBuffer(page);
    return {
      screenshot_hash: createHash("sha256").update(image).digest("hex"),
    };
  }
}

/** delay 使用 Node 定时器等待，避免浏览器内固定等待命令。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** viewToJson 把元素或页面状态转换为可传输 JSON。 */
function viewToJson(value: ElementView | JsonObject): JsonObject {
  return JSON.parse(JSON.stringify(value)) as JsonObject;
}
