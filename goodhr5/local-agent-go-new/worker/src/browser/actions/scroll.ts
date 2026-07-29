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

/** ScrollResult 表示真实滚轮滚动次数和前后状态。 */
export interface ScrollResult extends JsonObject {
  scrolled: boolean;
  attempts: number;
  distance: number;
  before: JsonObject;
  after: JsonObject;
}

/** VisibleArea 表示目标元素允许进入的安全可见区域。 */
interface VisibleArea {
  left: number;
  top: number;
  right: number;
  bottom: number;
  width: number;
  height: number;
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
      if (anchor) {
        await this.move.toElement(anchor.resolved, actionContext);
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
        ? this.visibleArea(
            target.resolved.view,
            request.wheel_anchor ? anchor?.resolved.view : undefined,
            request.viewport_margin ?? 0,
          )
        : undefined;
      if (target && visibleArea) {
        this.assertTargetCanFit(
          target.resolved.view,
          visibleArea,
          actionContext,
        );
      }
      const maxAttempts = Math.max(1, request.max_attempts ?? 1);
      let attempts = 0;
      let after: ElementView | JsonObject = before;
      let previousGap =
        target && visibleArea
          ? this.verticalGap(target.resolved.view, visibleArea)
          : 0;
      let noProgressCount = 0;
      for (let index = 0; index < maxAttempts; index += 1) {
        if (
          target &&
          visibleArea &&
          this.viewAccepted(
            target.resolved.view,
            request.require_full ?? false,
            visibleArea,
          )
        ) {
          break;
        }
        const distance = target
          ? this.directedDistance(
              target.resolved.view,
              request.distance,
              visibleArea ??
                this.visibleArea(target.resolved.view, undefined, 0),
            )
          : request.distance;
        this.logger.info(actionContext, "wheel", "start", {
          attempt: index + 1,
          distance,
        });
        await this.mouse.wheel(page, 0, distance);
        attempts += 1;
        await delay(Math.max(50, request.wait_ms ?? 250));
        after = target
          ? await this.refreshTarget(target.resolved)
          : await this.screenshotState(page);
        if (target) {
          const anchorView =
            request.wheel_anchor && anchor
              ? await this.refreshTarget(anchor.resolved)
              : undefined;
          visibleArea = this.visibleArea(
            after as ElementView,
            anchorView,
            request.viewport_margin ?? 0,
          );
          this.assertTargetCanFit(
            after as ElementView,
            visibleArea,
            actionContext,
          );
          const nextGap = this.verticalGap(after as ElementView, visibleArea);
          if (nextGap >= previousGap - 2) {
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
          distance,
          target_in_viewport:
            "in_viewport" in after ? Boolean(after.in_viewport) : false,
        });
        if (
          target &&
          visibleArea &&
          this.viewAccepted(
            after as ElementView,
            request.require_full ?? false,
            visibleArea,
          )
        ) {
          target.resolved.view = after as ElementView;
          break;
        }
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
    let visibleArea = this.visibleArea(
      found.resolved.view,
      anchor?.resolved.view,
      request.viewport_margin ?? 0,
    );
    this.assertTargetCanFit(found.resolved.view, visibleArea, actionContext);
    if (
      this.viewAccepted(
        found.resolved.view,
        request.require_full ?? false,
        visibleArea,
      )
    ) {
      return;
    }
    await this.move.toElement((anchor ?? found).resolved, actionContext);
    const maxAttempts = Math.max(1, request.max_attempts ?? 8);
    let previousGap = this.verticalGap(found.resolved.view, visibleArea);
    let noProgressCount = 0;
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      const distance = this.directedDistance(
        found.resolved.view,
        request.distance ?? 160,
        visibleArea,
      );
      await this.mouse.wheel(found.resolved.page, 0, distance);
      await delay(220);
      found.resolved.view = await this.refreshTarget(found.resolved);
      const anchorView = anchor
        ? await this.refreshTarget(anchor.resolved)
        : undefined;
      visibleArea = this.visibleArea(
        found.resolved.view,
        anchorView,
        request.viewport_margin ?? 0,
      );
      this.assertTargetCanFit(
        found.resolved.view,
        visibleArea,
        actionContext,
      );
      const nextGap = this.verticalGap(found.resolved.view, visibleArea);
      if (nextGap >= previousGap - 2) {
        noProgressCount += 1;
      } else {
        noProgressCount = 0;
      }
      previousGap = nextGap;
      this.logger.info(actionContext, "ensure_visible", "progress", {
        attempt,
        distance,
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
        this.viewAccepted(
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

  /** directedDistance 根据目标和安全区域的位置计算带方向的小步滚轮距离。 */
  private directedDistance(
    view: ElementView,
    rawDistance: number,
    area: VisibleArea,
  ): number {
    const maximumDistance = Math.max(1, Math.abs(rawDistance));
    const topGap = Math.max(0, area.top - view.box.y);
    const bottomGap = Math.max(
      0,
      view.box.y + view.box.height - area.bottom,
    );
    if (topGap > 0) {
      return -Math.min(maximumDistance, Math.max(40, Math.ceil(topGap + 12)));
    }
    return Math.min(
      maximumDistance,
      Math.max(40, Math.ceil(bottomGap + 12)),
    );
  }

  /** viewAccepted 判断元素是否满足视口安全要求。 */
  private viewAccepted(
    view: ElementView,
    requireFull: boolean,
    area: VisibleArea,
  ): boolean {
    if (!view.visible) {
      return false;
    }
    const right = view.box.x + view.box.width;
    const bottom = view.box.y + view.box.height;
    if (!requireFull) {
      return (
        right > area.left &&
        bottom > area.top &&
        view.box.x < area.right &&
        view.box.y < area.bottom
      );
    }
    return (
      view.box.x >= area.left &&
      view.box.y >= area.top &&
      right <= area.right &&
      bottom <= area.bottom
    );
  }

  /** visibleArea 计算浏览器视口和可选滚动容器重叠后的安全区域。 */
  private visibleArea(
    targetView: ElementView,
    containerView: ElementView | undefined,
    rawMargin: number,
  ): VisibleArea {
    const margin = Math.max(0, rawMargin);
    const viewport = targetView.viewport;
    const container = containerView?.box;
    const left = Math.max(margin, container ? container.x + margin : margin);
    const top = Math.max(margin, container ? container.y + margin : margin);
    const right = Math.min(
      viewport.width - margin,
      container ? container.x + container.width - margin : viewport.width - margin,
    );
    const bottom = Math.min(
      viewport.height - margin,
      container ? container.y + container.height - margin : viewport.height - margin,
    );
    return {
      left,
      top,
      right,
      bottom,
      width: Math.max(0, right - left),
      height: Math.max(0, bottom - top),
    };
  }

  /** assertTargetCanFit 在窗口或容器过小时返回明确错误。 */
  private assertTargetCanFit(
    view: ElementView,
    area: VisibleArea,
    actionContext: ActionContext,
  ): void {
    if (
      area.width <= 0 ||
      area.height <= 0 ||
      view.box.width > area.width + 1 ||
      view.box.height > area.height + 1
    ) {
      throw new WorkerError({
        code: "VIEWPORT_TOO_SMALL",
        message: "浏览器窗口太小，目标元素无法完整显示，请放大窗口后再试",
        action: actionContext.action,
        step: "ensure_visible",
        trace_id: actionContext.trace_id,
        retryable: true,
        details: {
          target_width: Math.round(view.box.width),
          target_height: Math.round(view.box.height),
          available_width: Math.round(area.width),
          available_height: Math.round(area.height),
        },
      });
    }
  }

  /** verticalGap 返回目标距离安全区域还差多少纵向像素。 */
  private verticalGap(view: ElementView, area: VisibleArea): number {
    const topGap = Math.max(0, area.top - view.box.y);
    const bottomGap = Math.max(
      0,
      view.box.y + view.box.height - area.bottom,
    );
    return Math.max(topGap, bottomGap);
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
