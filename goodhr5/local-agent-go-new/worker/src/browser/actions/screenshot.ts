// 文件作用说明：实现封装好的页面或元素截图能力，统一路径安全、查找、保存和错误日志。

import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import type {
  LongScreenshotRequest,
  LongScreenshotResult,
  ScreenshotPart,
  ScreenshotRequest,
  ScreenshotResult,
} from "../../contracts/actions.js";
import type { ActionContext } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { ScreenshotPrimitive } from "../primitives/screenshot.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { ReadPrimitive } from "../primitives/read.js";
import { BrowserSession } from "../session/browser-session.js";
import { FindAction } from "./find.js";
import { MoveAction } from "./move.js";

interface ScrollState {
  position: number;
  total: number;
  viewport: number;
  at_top: boolean;
  at_bottom: boolean;
}

/** ScreenshotAction 实现页面和元素截图封装能力。 */
export class ScreenshotAction {
  private readonly primitive = new ScreenshotPrimitive();

  /** 创建截图封装能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly find: FindAction,
    private readonly move: MoveAction,
    private readonly mouse: MousePrimitive,
    private readonly read: ReadPrimitive,
    private readonly logger: WorkerLogger,
  ) {}

  /** execute 平铺执行路径校验、可选元素查找和截图保存。 */
  async execute(
    request: ScreenshotRequest,
    actionContext: ActionContext,
  ): Promise<ScreenshotResult> {
    const step = "screenshot";
    const filename = safePNGFilename(request.filename);
    const filePath = path.join(request.directory, filename);
    this.logger.info(actionContext, step, "start", {
      target_description: request.target?.description ?? "当前页面",
      filename,
      full_page: request.full_page ?? false,
    });
    try {
      const page = await this.session.requirePage(actionContext, step);
      const size = request.target
        ? await this.primitive.element(
            (
              await this.find.one(
                request.target,
                actionContext,
                true,
              )
            ).resolved.locator,
            filePath,
          )
        : await this.primitive.page(
            page,
            filePath,
            request.full_page ?? false,
          );
      const result: ScreenshotResult = {
        path: filePath,
        filename,
        size,
      };
      this.logger.info(actionContext, step, "success", {
        filename,
        size,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "SCREENSHOT_FAILED",
        message: "截图没保存成功，我已经把原因记下来了",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { filename },
      });
      this.logger.error(actionContext, step, "failed", normalized.details);
      throw normalized;
    }
  }

  /** long 平铺执行查找、回到顶部、分段截图、真实滚轮和到底验证。 */
  async long(
    request: LongScreenshotRequest,
    actionContext: ActionContext,
  ): Promise<LongScreenshotResult> {
    const step = "screenshot_long";
    const baseFilename = safePNGFilename(request.filename).replace(/\.png$/i, "");
    const maxParts = Math.max(1, request.max_parts ?? 20);
    const waitMS = Math.max(50, request.wait_ms ?? 300);
    const parts: ScreenshotPart[] = [];
    this.logger.info(actionContext, step, "start", {
      target_description: request.target.description,
      max_parts: maxParts,
    });
    try {
      const page = await this.session.requirePage(actionContext, step);
      const target = await this.find.one(
        request.target,
        actionContext,
        true,
      );
      const anchor = request.wheel_anchor
        ? await this.find.one(request.wheel_anchor, actionContext, true)
        : target;
      await this.move.toElement(anchor.resolved, actionContext);
      let state = await this.readScrollState(
        page,
        anchor.resolved.locator,
      );
      const distance = Math.max(
        100,
        request.distance ?? Math.floor(Math.max(400, state.viewport * 0.75)),
      );
      for (let attempt = 0; attempt < maxParts && !state.at_top; attempt += 1) {
        const before = state.position;
        await this.mouse.wheel(page, 0, -distance);
        await delay(waitMS);
        state = await this.readScrollState(page, anchor.resolved.locator);
        if (state.at_top || state.position >= before) {
          break;
        }
      }
      let previousHash = "";
      for (let index = 0; index < maxParts; index += 1) {
        const filename = `${baseFilename}.part-${String(index + 1).padStart(3, "0")}.png`;
        const filePath = path.join(request.directory, filename);
        const size = await this.primitive.element(
          target.resolved.locator,
          filePath,
        );
        const hash = await fileHash(filePath);
        if (hash === previousHash) {
          await fs.rm(filePath, { force: true });
          break;
        }
        parts.push({
          path: filePath,
          filename,
          size,
          index,
          scroll_position: state.position,
        });
        previousHash = hash;
        this.logger.info(actionContext, "capture_part", "success", {
          index: index + 1,
          scroll_position: state.position,
          at_bottom: state.at_bottom,
        });
        if (state.at_bottom) {
          break;
        }
        const before = state.position;
        await this.mouse.wheel(page, 0, distance);
        await delay(waitMS);
        state = await this.readScrollState(page, anchor.resolved.locator);
        if (state.position <= before) {
          break;
        }
      }
      const result: LongScreenshotResult = {
        parts,
        count: parts.length,
        complete: state.at_bottom,
      };
      if (result.count === 0) {
        throw new Error("长截图没有生成任何分段");
      }
      if (!result.complete) {
        throw new Error(`长截图在 ${result.count} 个分段后仍未滚动到底`);
      }
      this.logger.info(actionContext, step, "success", {
        count: result.count,
        complete: result.complete,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "SCREENSHOT_FAILED",
        message: "长截图没保存完整，我已经把分段进度记下来了",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { captured_parts: parts.length },
      });
      this.logger.error(actionContext, step, "failed", normalized.details);
      throw normalized;
    }
  }

  /** readScrollState 只读取元素或页面滚动状态，不直接修改滚动位置。 */
  private async readScrollState(
    page: Awaited<ReturnType<BrowserSession["requirePage"]>>,
    locator: Awaited<ReturnType<FindAction["one"]>>["resolved"]["locator"],
  ): Promise<ScrollState> {
    const raw = await this.read.scrollState(page, locator);
    return {
      position: raw.scroll_top,
      total: raw.scroll_height,
      viewport: raw.client_height,
      at_top: raw.scroll_top <= 1,
      at_bottom:
        raw.scroll_top + raw.client_height >= raw.scroll_height - 2,
    };
  }
}

/** safePNGFilename 清理路径字符并确保使用 PNG 后缀。 */
function safePNGFilename(rawName: string): string {
  const base = path
    .basename(rawName || "screenshot.png")
    .replace(/[<>:"/\\|?*\x00-\x1F]/g, "_")
    .trim();
  const name = base || "screenshot.png";
  return name.toLowerCase().endsWith(".png") ? name : `${name}.png`;
}

/** fileHash 计算 PNG 内容哈希，用于停止重复分段。 */
async function fileHash(filePath: string): Promise<string> {
  return createHash("sha256")
    .update(await fs.readFile(filePath))
    .digest("hex");
}

/** delay 使用 Node 定时器等待页面滚动和截图状态稳定。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
