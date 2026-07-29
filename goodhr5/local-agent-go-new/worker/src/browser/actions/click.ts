// 文件作用说明：实现封装点击，统一平铺执行查找、滚动、移动、原子点击和结果验证。

import type { ElementClickRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import {
  normalizeWorkerError,
  WorkerError,
} from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { LocatorPrimitive } from "../primitives/locator.js";
import { FindAction } from "./find.js";
import { MoveAction } from "./move.js";
import { ScrollAction } from "./scroll.js";

/** ClickResult 表示封装点击的查找、移动和验证结果。 */
export interface ClickResult extends JsonObject {
  clicked: boolean;
  element_ref: string;
  hold_ms: number;
  verified: boolean;
  new_page_opened: boolean;
  new_page_url: string;
}

/** ClickAction 实现所有平台共用的完整点击能力。 */
export class ClickAction {
  /** 创建封装点击能力。 */
  constructor(
    private readonly find: FindAction,
    private readonly scroll: ScrollAction,
    private readonly move: MoveAction,
    private readonly locator: LocatorPrimitive,
    private readonly mouse: MousePrimitive,
    private readonly logger: WorkerLogger,
  ) {}

  /** execute 平铺执行查找、滚动、移动、按下、松开和验证。 */
  async execute(
    request: ElementClickRequest,
    actionContext: ActionContext,
  ): Promise<ClickResult> {
    this.logger.info(actionContext, "click", "start", {
      target_description: request.selector.description,
    });
    try {
      const found = await this.find.one(
        request.selector,
        actionContext,
        true,
      );
      await this.scroll.ensureVisible(
        found,
        {
          distance: 160,
          max_attempts: 8,
          viewport_margin: request.viewport_margin ?? 0,
          require_full: true,
        },
        actionContext,
      );
      await this.waitForStablePosition(found, actionContext);
      await this.move.toElement(found.resolved, actionContext);
      const newPagePromise = request.wait_for_new_page
        ? found.resolved.page
            .context()
            .waitForEvent("page", {
              timeout: request.new_page_timeout_ms ?? 10_000,
            })
        : null;
      const button = request.button ?? "left";
      const clickCount = Math.max(1, request.click_count ?? 1);
      let totalHold = 0;
      for (let index = 0; index < clickCount; index += 1) {
        const holdMs = randomInteger(70, 190);
        await this.mouse.down(found.resolved.page, button);
        await delay(holdMs);
        await this.mouse.up(found.resolved.page, button);
        totalHold += holdMs;
        if (index + 1 < clickCount) {
          await delay(randomInteger(80, 160));
        }
      }
      const newPage = newPagePromise ? await newPagePromise : null;
      if (newPage) {
        await newPage
          .waitForLoadState("domcontentloaded", {
            timeout: request.new_page_timeout_ms ?? 10_000,
          })
          .catch(() => undefined);
        await newPage.bringToFront();
      }
      const verified = await this.verify(
        request,
        found.resolved.page,
        actionContext,
      );
      if (request.verify && !verified) {
        throw new Error("点击后的页面状态没有通过验证");
      }
      const result: ClickResult = {
        clicked: true,
        element_ref: found.result.element_ref,
        hold_ms: totalHold,
        verified,
        new_page_opened: Boolean(newPage),
        new_page_url: newPage?.url() ?? "",
      };
      this.logger.info(actionContext, "click", "success", {
        target_description: request.selector.description,
        hold_ms: totalHold,
        verified,
        new_page_opened: Boolean(newPage),
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "CLICK_FAILED",
        message: `${request.selector.description} 没点成功，我已经记下卡住的位置`,
        action: actionContext.action,
        step: "click",
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { target_description: request.selector.description },
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** waitForStablePosition 连续读取元素位置，稳定后才允许执行一次原子点击。 */
  private async waitForStablePosition(
    found: Awaited<ReturnType<FindAction["one"]>>,
    actionContext: ActionContext,
  ): Promise<void> {
    let previous = found.resolved.view.box;
    for (let check = 1; check <= 3; check += 1) {
      await delay(100);
      const view = await this.locator.view(
        found.resolved.page,
        found.resolved.locator,
      );
      const current = view.box;
      const stable =
        Math.abs(current.x - previous.x) <= 2 &&
        Math.abs(current.y - previous.y) <= 2 &&
        Math.abs(current.width - previous.width) <= 2 &&
        Math.abs(current.height - previous.height) <= 2;
      this.logger.info(actionContext, "wait_stable", "progress", {
        check,
        stable,
      });
      found.resolved.view = view;
      if (stable && check >= 2) {
        return;
      }
      previous = current;
    }
    throw new Error("元素位置持续变化，已取消本次点击以避免点错");
  }

  /** verify 按请求验证点击后的 URL 或元素状态。 */
  private async verify(
    request: ElementClickRequest,
    page: Parameters<MousePrimitive["down"]>[0],
    actionContext: ActionContext,
  ): Promise<boolean> {
    const verify = request.verify;
    if (!verify) {
      return true;
    }
    const deadline = Date.now() + (verify.timeout_ms ?? 3_000);
    while (Date.now() <= deadline) {
      let accepted = true;
      if (verify.url_contains) {
        accepted = accepted && page.url().includes(verify.url_contains);
      }
      if (verify.target_visible) {
        const visible = await this.isVisible(
          verify.target_visible,
          actionContext,
        );
        accepted = accepted && visible;
      }
      if (verify.target_hidden) {
        const hidden = !(await this.isVisible(
          verify.target_hidden,
          actionContext,
        ));
        accepted = accepted && hidden;
      }
      if (accepted) {
        return true;
      }
      await delay(100);
    }
    return false;
  }

  /** isVisible 只把元素未找到视为不可见，其他浏览器错误继续抛出。 */
  private async isVisible(
    selector: NonNullable<ElementClickRequest["verify"]>["target_visible"],
    actionContext: ActionContext,
  ): Promise<boolean> {
    if (!selector) {
      return false;
    }
    try {
        await this.find.one(selector, actionContext, false, false);
      return true;
    } catch (error) {
      if (
        error instanceof WorkerError &&
        error.code === "ELEMENT_NOT_FOUND"
      ) {
        return false;
      }
      throw error;
    }
  }
}

/** delay 使用 Node 定时器完成点击按下和松开的真人停顿。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** randomInteger 返回包含边界的随机整数。 */
function randomInteger(minimum: number, maximum: number): number {
  return Math.round(minimum + Math.random() * (maximum - minimum));
}
