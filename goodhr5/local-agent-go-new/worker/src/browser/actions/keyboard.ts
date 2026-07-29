// 文件作用说明：实现 Go 可调用的封装按键能力，并统一参数、异常和详细日志。

import type { KeyboardPressRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
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
  ) {}

  /** press 在当前页面执行按键并记录结果。 */
  async press(
    request: KeyboardPressRequest,
    actionContext: ActionContext,
  ): Promise<KeyboardPressResult> {
    const step = "press_key";
    this.logger.info(actionContext, step, "start", { key: request.key });
    try {
      const page = await this.session.requirePage(actionContext, step);
      await this.primitive.press(page, request.key, request.delay_ms ?? 0);
      const result: KeyboardPressResult = {
        pressed: true,
        key: request.key,
      };
      this.logger.info(actionContext, step, "success", {
        key: request.key,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        message: `按键 ${request.key} 没执行成功`,
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }
}
