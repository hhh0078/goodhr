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

const DEFAULT_CHUNK_DELAY_MIN_MS = 15;
const DEFAULT_CHUNK_DELAY_MAX_MS = 45;
const CLOAK_UNSAFE_SHIFT_SYMBOLS = new Set([
  "!",
  "@",
  "#",
  "$",
  "%",
  "^",
  "&",
  "*",
  "(",
  ")",
  "_",
  "+",
  "{",
  "}",
  "|",
  ":",
  '"',
  "<",
  ">",
  "?",
  "~",
]);

/** TypingChunk 表示可交给 CloakBrowser 的文本或必须安全插入的特殊符号。 */
export interface TypingChunk {
  text: string;
  cloakbrowser: boolean;
}

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
      const moved = await this.move.toElement(found.resolved, actionContext);
      const focusClickDurationMS = await this.mouse.click(
        found.resolved.page,
        moved.x,
        moved.y,
      );
      if (request.clear ?? true) {
        const selectAll =
          process.platform === "darwin" ? "Meta+A" : "Control+A";
        await this.keyboard.press(found.resolved.page, selectAll);
        await this.keyboard.press(found.resolved.page, "Backspace");
      }
      await this.typeHumanized(
        found.resolved.page,
        request.text,
        request.min_delay_ms ?? DEFAULT_CHUNK_DELAY_MIN_MS,
        request.max_delay_ms ?? DEFAULT_CHUNK_DELAY_MAX_MS,
      );
      let verified = true;
      if (request.verify ?? true) {
        const actual = await this.read.editableValue(found.resolved.locator);
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
        focus_click_duration_ms: focusClickDurationMS,
        typing_mode: "cloakbrowser",
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

  /** typeHumanized 把完整安全文本交给 CloakBrowser，并隔离可能触发脚本兜底的特殊符号。 */
  private async typeHumanized(
    page: Parameters<KeyboardPrimitive["press"]>[0],
    text: string,
    minimumDelay: number,
    maximumDelay: number,
  ): Promise<void> {
    const min = Math.max(0, Math.min(minimumDelay, maximumDelay));
    const max = Math.max(min, maximumDelay);
    const chunks = safeTypingChunks(text);
    for (const [index, chunk] of chunks.entries()) {
      if (chunk.cloakbrowser) {
        await this.keyboard.typeText(page, chunk.text);
      } else {
        await this.keyboard.insertText(page, chunk.text);
      }
      if (index + 1 < chunks.length) {
        await delay(randomInteger(min, max));
      }
    }
  }
}

/** safeTypingChunks 隔离 CloakBrowser 在 CDP 失败时可能执行脚本兜底的 Shift 特殊符号。 */
export function safeTypingChunks(text: string): TypingChunk[] {
  const chunks: TypingChunk[] = [];
  let safeText = "";
  for (const character of text) {
    if (CLOAK_UNSAFE_SHIFT_SYMBOLS.has(character)) {
      if (safeText) {
        chunks.push({ text: safeText, cloakbrowser: true });
        safeText = "";
      }
      chunks.push({ text: character, cloakbrowser: false });
    } else {
      safeText += character;
    }
  }
  if (safeText) {
    chunks.push({ text: safeText, cloakbrowser: true });
  }
  return chunks;
}

/** delay 使用 Node 定时器等待特殊符号前后的输入间隔。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** randomInteger 返回包含边界的随机整数。 */
function randomInteger(minimum: number, maximum: number): number {
  return Math.round(minimum + Math.random() * (maximum - minimum));
}
