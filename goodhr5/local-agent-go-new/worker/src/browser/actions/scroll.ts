// 文件作用说明：实现封装好的真实滚轮滚动，平铺执行查找、移动、滚轮和结果验证。

import { createHash } from "node:crypto";
import type { ScrollRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import type { ElementView } from "../../contracts/selector.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
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
      const maxAttempts = Math.max(1, request.max_attempts ?? 1);
      let attempts = 0;
      let after: ElementView | JsonObject = before;
      for (let index = 0; index < maxAttempts; index += 1) {
        if (
          target &&
          this.viewAccepted(
            target.resolved.view,
            request.require_full ?? false,
            request.viewport_margin ?? 0,
          )
        ) {
          break;
        }
        const distance = target
          ? this.directedDistance(target.resolved.view, request.distance)
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
        this.logger.info(actionContext, "wheel", "success", {
          attempt: index + 1,
          distance,
          target_in_viewport:
            "in_viewport" in after ? Boolean(after.in_viewport) : false,
        });
        if (
          target &&
          this.viewAccepted(
            after as ElementView,
            request.require_full ?? false,
            request.viewport_margin ?? 0,
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
      if (attempts > 0 && !result.scrolled) {
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
      this.logger.error(actionContext, step, "failed", normalized.details);
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
    if (
      this.viewAccepted(
        found.resolved.view,
        request.require_full ?? false,
        request.viewport_margin ?? 0,
      )
    ) {
      return;
    }
    const anchor = request.wheel_anchor
      ? await this.find.one(request.wheel_anchor, actionContext, true)
      : found;
    await this.move.toElement(anchor.resolved, actionContext);
    const maxAttempts = Math.max(1, request.max_attempts ?? 8);
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      const distance = this.directedDistance(
        found.resolved.view,
        request.distance ?? 160,
      );
      await this.mouse.wheel(found.resolved.page, 0, distance);
      await delay(220);
      found.resolved.view = await this.refreshTarget(found.resolved);
      this.logger.info(actionContext, "ensure_visible", "progress", {
        attempt,
        distance,
        in_viewport: found.resolved.view.in_viewport,
      });
      if (
        this.viewAccepted(
          found.resolved.view,
          request.require_full ?? false,
          request.viewport_margin ?? 0,
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

  /** directedDistance 根据元素在视口上下位置决定滚轮方向。 */
  private directedDistance(view: ElementView, rawDistance: number): number {
    const distance = Math.max(1, Math.abs(rawDistance));
    if (view.box.y < 0) {
      return -distance;
    }
    return distance;
  }

  /** viewAccepted 判断元素是否满足视口安全要求。 */
  private viewAccepted(
    view: ElementView,
    requireFull: boolean,
    margin: number,
  ): boolean {
    if (!view.visible) {
      return false;
    }
    if (!requireFull && margin <= 0) {
      return view.in_viewport;
    }
    const right = view.box.x + view.box.width;
    const bottom = view.box.y + view.box.height;
    return (
      view.box.x >= margin &&
      view.box.y >= margin &&
      right <= view.viewport.width - margin &&
      bottom <= view.viewport.height - margin
    );
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
