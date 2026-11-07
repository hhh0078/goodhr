// 文件作用说明：实现 Go 可调用的封装按键能力，并统一参数、异常和详细日志。

import type { KeyboardPressRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { LocatorPrimitive } from "../primitives/locator.js";
import { ReadPrimitive } from "../primitives/read.js";
import { assertInputAllowed } from "../primitives/input-lifecycle.js";
import { KeyboardPrimitive } from "../primitives/keyboard.js";
import { BrowserSession } from "../session/browser-session.js";

/** KeyboardPressResult 表示封装按键结果。 */
export interface KeyboardPressResult extends JsonObject {
  pressed: boolean;
  key: string;
}

/** KeyboardAction 实现通用按键封装能力。 */
export class KeyboardAction {
  /** 创建封装按键能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly primitive: KeyboardPrimitive,
    private readonly logger: WorkerLogger,
    private readonly locator = new LocatorPrimitive(),
  ) {}

  /** press 在当前页面执行按键并记录结果。 */
  async press(
    request: KeyboardPressRequest,
    actionContext: ActionContext,
  ): Promise<KeyboardPressResult> {
    const step = "press_key";
    const summary = { key: request.key, diagnostic: request.diagnostic ?? false,
      target_description: request.description ?? "" };
    this.logger.info(actionContext, step, "start", summary);
    let keyStarted = false;
    try {
      const page = await this.session.requirePage(actionContext, step);
      if (request.expected_page_url && page.url() !== request.expected_page_url) throw new Error("操作页面已切换");
      if (request.target) {
        const target = await this.locator.resolve(page, request.target, { ...actionContext, step, require_unique: true, view_mode: "state" });
        if (!await target.locator.isVisible()) throw new Error("原操作目标已隐藏");
        if (request.expected_value !== undefined && await new ReadPrimitive().inputValue(target.locator) !== request.expected_value) throw new Error("输入框草稿已经变化");
        if (request.require_focus && !(await new ReadPrimitive().focused(target.locator))) throw new Error("原输入框已经失去焦点");
      }
      if (request.expected_page_url && page.url() !== request.expected_page_url) throw new Error("操作页面已切换");
      assertInputAllowed(actionContext);
      keyStarted = true;
      await this.primitive.press(page, request.key, request.delay_ms ?? 0);
      const result: KeyboardPressResult = {
        pressed: true,
        key: request.key,
      };
      this.logger.info(actionContext, step, "success", summary);
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        message: `按键 ${request.key} 没执行成功`,
        details: { target_description: request.description ?? "" },
      });
      normalized.details.key_started = keyStarted;
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }
}
