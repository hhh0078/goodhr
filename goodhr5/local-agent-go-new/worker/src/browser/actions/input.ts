// 文件作用说明：实现封装输入，统一平铺执行查找、滚动、移动、聚焦、清空、原子输入和结果验证。

import type { ElementInputRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { KeyboardPrimitive } from "../primitives/keyboard.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { ReadPrimitive } from "../primitives/read.js";
import { FindAction } from "./find.js";
import { MoveAction } from "./move.js";
import { ScrollAction } from "./scroll.js";

/** InputResult 表示封装输入结果。 */
export interface InputResult extends JsonObject {
  typed: boolean;
  length: number;
  verified: boolean;
  element_ref: string;
}

/** InputAction 实现所有平台共用的完整输入能力。 */
export class InputAction {
  private readonly read = new ReadPrimitive();

  /** 创建封装输入能力。 */
  constructor(
    private readonly find: FindAction,
    private readonly scroll: ScrollAction,
    private readonly move: MoveAction,
    private readonly mouse: MousePrimitive,
    private readonly keyboard: KeyboardPrimitive,
    private readonly logger: WorkerLogger,
  ) {}

  /** execute 平铺执行查找、滚动、移动、聚焦、清空、输入和验证。 */
  async execute(
    request: ElementInputRequest,
    actionContext: ActionContext,
  ): Promise<InputResult> {
    this.logger.info(actionContext, "input", "start", {
      target_description: request.selector.description,
      text_length: request.text.length,
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
          require_full: true,
        },
        actionContext,
      );
      await this.move.toElement(found.resolved, actionContext);
      await this.mouse.down(found.resolved.page, "left");
      await delay(randomInteger(60, 150));
      await this.mouse.up(found.resolved.page, "left");
      if (request.clear ?? true) {
        const selectAll =
          process.platform === "darwin" ? "Meta+A" : "Control+A";
        await this.keyboard.press(found.resolved.page, selectAll);
        await this.keyboard.press(found.resolved.page, "Backspace");
      }
      await this.typeHumanized(
        found.resolved.page,
        request.text,
        request.min_delay_ms ?? 35,
        request.max_delay_ms ?? 110,
      );
      let verified = true;
      if (request.verify ?? true) {
        const actual = await this.read
          .inputValue(found.resolved.locator)
          .catch(() => "");
        verified = actual === request.text;
        if (!verified) {
          throw new Error("输入后的内容没有通过验证");
        }
      }
      const result: InputResult = {
        typed: true,
        length: request.text.length,
        verified,
        element_ref: found.result.element_ref,
      };
      this.logger.info(actionContext, "input", "success", {
        target_description: request.selector.description,
        text_length: request.text.length,
        verified,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "INPUT_FAILED",
        message: `${request.selector.description} 没输入成功，我已经记下卡住的位置`,
        action: actionContext.action,
        step: "input",
        trace_id: actionContext.trace_id,
        retryable: true,
        details: {
          target_description: request.selector.description,
          text_length: request.text.length,
        },
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** typeHumanized 按词语分段输入，并在词语之间增加真人式停顿。 */
  private async typeHumanized(
    page: Parameters<KeyboardPrimitive["press"]>[0],
    text: string,
    minimumDelay: number,
    maximumDelay: number,
  ): Promise<void> {
    const min = Math.max(0, Math.min(minimumDelay, maximumDelay));
    const max = Math.max(min, maximumDelay);
    const segments = humanTextSegments(text);
    for (const [segmentIndex, segment] of segments.entries()) {
      for (const character of segment) {
        if (/^[\x20-\x7E]$/.test(character)) {
          await this.keyboard.typeCharacter(page, character);
        } else {
          await this.keyboard.insertText(page, character);
        }
        await delay(randomInteger(min, max));
      }
      if (segmentIndex + 1 < segments.length) {
        await delay(randomInteger(180, 450));
      }
    }
  }
}

/** humanTextSegments 使用 Node 原生分词器整理适合逐段输入的文本。 */
export function humanTextSegments(text: string): string[] {
  if (text === "") {
    return [];
  }
  const segmenter = new Intl.Segmenter("zh-CN", { granularity: "word" });
  const segments = [...segmenter.segment(text)]
    .map((item) => item.segment)
    .filter((item) => item !== "");
  return segments.length > 0 ? segments : [...text];
}

/** delay 使用 Node 定时器模拟字符间等待。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** randomInteger 返回包含边界的随机整数。 */
function randomInteger(minimum: number, maximum: number): number {
  return Math.round(minimum + Math.random() * (maximum - minimum));
}
