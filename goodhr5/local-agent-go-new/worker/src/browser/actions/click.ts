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
import { waitForStablePosition } from "./stable-position.js";
import { NewPageWaiter } from "./new-page-waiter.js";
import { clickActionability, pageDiagnostic, selectorDiagnostic } from "./diagnostic-state.js";

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
    let newPageWaiter: NewPageWaiter | undefined;
    let clickStarted = false;
    const diagnosticInput: JsonObject = request.diagnostic ? { selector: selectorDiagnostic(request.selector), attempt: request.diagnostic_attempt ?? 0,
      verification_configured: Boolean(request.verify) } : {};
    this.logger.info(actionContext, "click", "start", {
      target_description: request.selector.description,
      diagnostic: request.diagnostic ?? false, input: request.diagnostic ? diagnosticInput : {},
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
          max_attempts: 24,
          viewport_margin: request.viewport_margin ?? 0,
          // 点击只需要窗口内有安全落点；贴边抽屉行可能有一部分永久位于窗口外。
          require_full: false,
        },
        actionContext,
      );
      await waitForStablePosition(found, this.locator);
      if (request.diagnostic) {
        const actionability = await clickActionability(found.resolved.locator);
        this.logger.info(actionContext, "click_target", "checked", {
          diagnostic: true, input: diagnosticInput, output: { matched_selector: found.resolved.matched_selector.value,
            selector_attempts: this.logger.fromUnknown(found.resolved.attempts).value ?? [],
            target_view: this.logger.fromUnknown(found.resolved.view), actionability, page_state: pageDiagnostic(found.resolved.page) },
        });
      }
      // 点击收紧到可见区域中部，避免随机落在被悬浮入口覆盖的按钮边缘。
      const landing = await this.move.toElement(found.resolved, actionContext, { centered: true });
      if (request.diagnostic) this.logger.info(actionContext, "click_landing", "ready", {
        diagnostic: true, input: diagnosticInput, output: { x: landing.x, y: landing.y, target_view: this.logger.fromUnknown(landing.view) },
      });
      if (request.wait_for_new_page) {
        newPageWaiter = new NewPageWaiter(
          found.resolved.page.context(), actionContext, request.new_page_timeout_ms ?? 10_000,
        );
      }
      const button = request.button ?? "left";
      const clickCount = Math.max(1, request.click_count ?? 1);
      let totalHold = 0;
      for (let index = 0; index < clickCount; index += 1) {
        const holdMs = randomInteger(70, 190);
        try {
          // 一旦尝试按下鼠标，点击结果就可能不确定，调用方不得盲目再次发送。
          clickStarted = true;
          await this.mouse.down(found.resolved.page, button);
          await delay(holdMs);
        } finally {
          await this.mouse.up(found.resolved.page, button);
        }
        totalHold += holdMs;
        if (index + 1 < clickCount) {
          await delay(randomInteger(80, 160));
        }
      }
      const newPage = newPageWaiter ? await newPageWaiter.wait() : null;
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
        throw new WorkerError({
          code: "CLICK_VERIFICATION_FAILED",
          message: "已经点击，但页面结果还没确认，请先检查当前状态",
          action: actionContext.action,
          step: "verify_click",
          trace_id: actionContext.trace_id,
          retryable: false,
          details: { target_description: request.selector.description, clicked: true },
        });
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
        diagnostic: request.diagnostic ?? false,
        ...(request.diagnostic ? { input: diagnosticInput, output: { clicked: true, verified,
          verification_configured: Boolean(request.verify), page_state: pageDiagnostic(found.resolved.page) } } : {}),
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
      normalized.details.click_started = clickStarted;
      this.logger.failure(actionContext, normalized);
      throw normalized;
    } finally {
      newPageWaiter?.cancel();
    }
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

  /** isVisible 将已移除或仍在 DOM 中但隐藏的元素都视为不可见，其余错误继续抛出。 */
  private async isVisible(
    selector: NonNullable<ElementClickRequest["verify"]>["target_visible"],
    actionContext: ActionContext,
  ): Promise<boolean> {
    if (!selector) {
      return false;
    }
    try {
      // 状态验证只检查可见性，不读取坐标；弹窗在检查途中移除时，坐标读取可能等待默认的三十秒。
      const items = await this.find.all(
        { ...selector, state: "visible", timeout_ms: 0 },
        1,
        {},
        actionContext,
        false,
        1,
        0,
        { diagnostic: false, attempt: 0, probe_only: true },
      );
      return items.length > 0;
    } catch (error) {
      if (
        error instanceof WorkerError &&
        (error.code === "ELEMENT_NOT_FOUND" || error.code === "ELEMENT_NOT_VISIBLE")
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
