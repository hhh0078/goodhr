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
import { variedDistance, wheelGesture, readingPause } from "./wheel-gesture.js";
import { assertTargetCanFit, directedWheel, intersectsViewport, targetGap, viewAccepted, visibleArea as calculateVisibleArea } from "./scroll-geometry.js";

/** ScrollResult 表示真实滚轮滚动次数和前后状态。 */
export interface ScrollResult extends JsonObject {
  scrolled: boolean;
  attempts: number;
  distance: number;
  before: JsonObject;
  after: JsonObject;
  progress_target_visible?: boolean;
  progress_target_moved?: boolean;
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
      const progress = request.progress_target
        ? await this.find.one(request.progress_target, actionContext, true)
        : null;
      const progressBefore = progress ? progress.resolved.view : undefined;
      const anchor = request.wheel_anchor
        ? await this.find.one(request.wheel_anchor, actionContext, true)
        : target;
      const progressArea = progress
        ? calculateVisibleArea(progress.resolved.view, request.wheel_anchor ? scrollContainerView(anchor) : undefined, 0)
        : undefined;
      if (progress && progressArea && viewAccepted(progress.resolved.view, false, progressArea)) {
        await this.move.toElement(progress.resolved, actionContext, { centered: true, visibleArea: progressArea });
      } else if (anchor && intersectsViewport(anchor.resolved.view)) {
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
            request.wheel_anchor ? scrollContainerView(anchor) : undefined,
            request.viewport_margin ?? 0,
          )
        : undefined;
      if (target && visibleArea) {
        this.logGeometry(actionContext, "initial", target.resolved.view, anchor, visibleArea);
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
              variedDistance(request.distance),
              visibleArea ??
                calculateVisibleArea(target.resolved.view, undefined, 0),
              request.require_full ?? false,
            )
          : { x: 0, y: variedDistance(request.distance) };
        this.logger.info(actionContext, "wheel", "start", {
          attempt: index + 1,
          delta_x: distance.x, delta_y: distance.y,
        });
        await wheelGesture(this.mouse, page, distance.x, distance.y, actionContext, this.logger);
        attempts += 1;
        await readingPause(Math.max(50, request.wait_ms ?? 250), actionContext);
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
            request.wheel_anchor ? scrollContainerView(anchor, anchorView) : undefined,
            request.viewport_margin ?? 0,
          );
          if (visibleArea.width <= 0 || visibleArea.height <= 0) {
            this.logGeometry(actionContext, "after_wheel", after as ElementView, anchor, visibleArea, anchorView);
          }
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
      if (progress && progressBefore) {
        const progressAfter = await this.refreshTarget(progress.resolved);
        result.progress_target_moved = (["x", "y", "width", "height"] as const).some(
          key => Math.abs(progressAfter.box[key] - progressBefore.box[key]) > 1,
        );
        const area = calculateVisibleArea(progressAfter, request.wheel_anchor ? scrollContainerView(anchor, anchor ? await this.refreshTarget(anchor.resolved) : undefined) : undefined, 0);
        const bottom = progressAfter.box.y + progressAfter.box.height;
        result.progress_target_visible = progressAfter.visible && area.height > 0 &&
          bottom <= area.bottom + 1 && bottom > area.top;
        this.logger.info(actionContext, "scroll_progress", "success", {
          progress_target_visible: result.progress_target_visible,
          progress_target_moved: result.progress_target_moved,
          before_y: progressBefore.box.y, after_y: progressAfter.box.y,
        });
      }
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
      scrollContainerView(anchor),
      request.viewport_margin ?? 0,
    );
    this.logGeometry(actionContext, "initial", found.resolved.view, anchor, visibleArea);
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
        variedDistance(request.distance ?? 160),
        visibleArea,
        request.require_full ?? false,
      );
      await wheelGesture(this.mouse, found.resolved.page, distance.x, distance.y, actionContext, this.logger);
      await readingPause(220, actionContext);
      found.resolved.view = await this.refreshTarget(found.resolved);
      const anchorView = anchor
        ? await this.refreshTarget(anchor.resolved)
        : undefined;
      visibleArea = calculateVisibleArea(
        found.resolved.view,
        scrollContainerView(anchor, anchorView),
        request.viewport_margin ?? 0,
      );
      if (visibleArea.width <= 0 || visibleArea.height <= 0) {
        this.logGeometry(actionContext, "after_wheel", found.resolved.view, anchor, visibleArea, anchorView);
      }
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

  /** logGeometry 记录目标、滚轮锚点和安全区域，供不同页面布局下排查滚动误判。 */
  private logGeometry(
    actionContext: ActionContext,
    phase: string,
    targetView: ElementView,
    anchor: FoundElement | null | undefined,
    area: ReturnType<typeof calculateVisibleArea>,
    anchorView: ElementView | undefined = anchor?.resolved.view,
  ): void {
    this.logger.info(actionContext, "ensure_visible", "geometry", {
      phase,
      target_box: {
        x: targetView.box.x, y: targetView.box.y,
        width: targetView.box.width, height: targetView.box.height,
      },
      viewport: {
        width: targetView.viewport.width, height: targetView.viewport.height,
      },
      anchor_selector: anchor?.resolved.matched_selector.value ?? "",
      anchor_box: anchorView ? {
        x: anchorView.box.x, y: anchorView.box.y,
        width: anchorView.box.width, height: anchorView.box.height,
      } : null,
      anchor_in_viewport: anchorView?.in_viewport ?? false,
      anchor_is_document: isDocumentScrollAnchor(anchor),
      available_area: {
        left: area.left, top: area.top, right: area.right, bottom: area.bottom,
        width: area.width, height: area.height,
      },
    });
  }
}

/** scrollContainerView 只让真实滚动容器限制安全区域，整页锚点始终使用浏览器视口。 */
function scrollContainerView(
  anchor: FoundElement | null | undefined,
  currentView: ElementView | undefined = anchor?.resolved.view,
): ElementView | undefined {
  if (!anchor || isDocumentScrollAnchor(anchor)) {
    return undefined;
  }
  return currentView;
}

/** isDocumentScrollAnchor 判断当前匹配项是否为页面根滚动锚点。 */
function isDocumentScrollAnchor(anchor: FoundElement | null | undefined): boolean {
  const matched = anchor?.resolved.matched_selector;
  return Boolean(
    matched?.type === "css" &&
      /^(?:body|html|:root)(?::visible)?$/i.test(matched.value.trim()),
  );
}

/** viewToJson 把元素或页面状态转换为可传输 JSON。 */
function viewToJson(value: ElementView | JsonObject): JsonObject {
  return JSON.parse(JSON.stringify(value)) as JsonObject;
}
