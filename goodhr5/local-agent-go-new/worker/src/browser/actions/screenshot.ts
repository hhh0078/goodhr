// 文件作用说明：实现封装好的页面或元素截图能力，统一路径安全、查找、保存和错误日志。

import fs from "node:fs/promises";
import path from "node:path";
import { inflateSync } from "node:zlib";
import type {
  LongScreenshotRequest,
  LongScreenshotResult,
  ScreenshotPart,
  ScreenshotRequest,
  ScreenshotResult,
} from "../../contracts/actions.js";
import type { ActionContext } from "../../contracts/common.js";
import { normalizeWorkerError, WorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { markRequestProgress } from "../../logging/request-progress.js";
import { assertRequestActive } from "../primitives/request-lifecycle.js";
import { ScreenshotPrimitive, screenshotTimedOut, screenshotTimeoutMS } from "../primitives/screenshot.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { BrowserSession } from "../session/browser-session.js";
import { FindAction } from "./find.js";
import { MoveAction } from "./move.js";
import { variedDistance, wheelGesture, readingPause } from "./wheel-gesture.js";

/** ScreenshotAction 实现页面和元素截图封装能力。 */
export class ScreenshotAction {
  private readonly primitive = new ScreenshotPrimitive();

  /** 创建截图封装能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly find: FindAction,
    private readonly move: MoveAction,
    private readonly mouse: MousePrimitive,
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
      markRequestProgress(actionContext, "require_page");
      const page = await this.session.requirePage(actionContext, step);
      assertRequestActive(actionContext);
      markRequestProgress(actionContext, "screenshot_prepare");
      const size = request.viewport_only
        ? await this.captureVisibleTarget(page, request, filePath, actionContext)
        : request.target
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
        details: { filename, ...(screenshotTimedOut(error) ? { capture_timeout_ms: screenshotTimeoutMS } : {}) },
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }

  /** captureVisibleTarget 核对目标与容器的可见交集，只截完整露出的目标，绝不为截图移动聊天。 */
  private async captureVisibleTarget(page: import("playwright-core").Page, request: ScreenshotRequest, filePath: string, context: ActionContext): Promise<number> {
    if (!request.target) throw new Error("仅视口截图需要指定目标");
    const found = await this.find.one(request.target, context, true);
    const view = found.resolved.view;
    const container = request.clip_container ? (await this.find.one(request.clip_container, context, true)).resolved.view : undefined;
    const box = view.box;
    const left = Math.max(0, container?.box.x ?? 0), top = Math.max(0, container?.box.y ?? 0);
    const right = Math.min(view.viewport.width, container ? container.box.x + container.box.width : view.viewport.width);
    const bottom = Math.min(view.viewport.height, container ? container.box.y + container.box.height : view.viewport.height);
    if (!view.visible || !view.fully_in_viewport || (container && !container.visible) || box.width <= 0 || box.height <= 0 ||
        box.x < left || box.y < top || box.x + box.width > right || box.y + box.height > bottom) {
      throw new WorkerError({ code: "ELEMENT_NOT_VISIBLE", message: "目标没有完整露出，这次不为截图滚动页面", action: context.action, step: "screenshot", trace_id: context.trace_id, retryable: false });
    }
    const size = await this.primitive.clippedPage(page, filePath, box);
    const after = await found.resolved.locator.boundingBox();
    if (!after || ["x", "y", "width", "height"].some(key => after[key as keyof typeof after] !== box[key as keyof typeof box])) {
      throw new WorkerError({ code: "SCREENSHOT_FAILED", message: "截图期间目标位置发生变化，这张图片没有采用", action: context.action, step: "screenshot", trace_id: context.trace_id, retryable: false });
    }
    return size;
  }

  /** long 平铺执行查找、单次分段截图、真实滚轮和重复画面验证。 */
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
      markRequestProgress(actionContext, "require_page");
      const page = await this.session.requirePage(actionContext, step);
      markRequestProgress(actionContext, "screenshot_prepare");
      const target = await this.find.one(
        request.target,
        actionContext,
        true,
      );
      const anchor = request.wheel_anchor
        ? await this.find.one(request.wheel_anchor, actionContext, true)
        : target;
      await this.move.toElement(anchor.resolved, actionContext, { centered: true });
      assertRequestActive(actionContext);
      const box = target.resolved.view.box;
      const viewportHeight = target.resolved.view.viewport.height;
      const visibleHeight = Math.min(viewportHeight, box.y + box.height) - Math.max(0, box.y);
      if (visibleHeight < 80) throw new Error("详情截图区域太小，无法保证相邻画面完整重叠");
      // 每段最多跨过可见高度的 65%，为变速滚动和长图拼接保留重叠。
      const distance = Math.max(40, Math.min(request.distance ?? 520, Math.floor(visibleHeight * 0.65)));
      /** captureActive 等待原生截图后再次核对ALS执行预算，迟到结果不能进入判重或写文件。 */
      const captureActive = async (): Promise<Buffer> => {
        assertRequestActive(actionContext);
        const buffer = await this.primitive.elementBuffer(target.resolved.locator);
        assertRequestActive(actionContext);
        return buffer;
      };
      let topBuffer = await captureActive();
      let topStable = 0;
      for (let attempt = 0; attempt < maxParts + 2 && topStable < 2; attempt++) {
        assertRequestActive(actionContext);
        markRequestProgress(actionContext, "screenshot_top");
        await wheelGesture(this.mouse, page, 0, -variedDistance(distance), actionContext, this.logger);
        await readingPause(waitMS, actionContext);
        const next = await captureActive();
        const duplicate = screenshotsAreDuplicate(topBuffer, next);
        assertRequestActive(actionContext);
        topStable = duplicate ? topStable + 1 : 0;
        topBuffer = next;
      }
      if (topStable < 2) throw new Error("详情截图前未能确认已回到顶部，避免漏掉开头内容");
      this.logger.info(actionContext, "screenshot_top", "success");
      let complete = false;
      let previousBuffer: Buffer | null = null;
      let scrollPosition = 0;
      let duplicateFrames = 0;
      for (let attempt = 0; attempt < maxParts + 2; attempt += 1) {
        assertRequestActive(actionContext);
        const currentBuffer = await captureActive();
        const duplicate = previousBuffer && screenshotsAreDuplicate(previousBuffer, currentBuffer);
        assertRequestActive(actionContext);
        if (duplicate) {
          duplicateFrames++;
          if (duplicateFrames >= 2) { complete = true; break; }
        } else {
          duplicateFrames = 0;
          if (parts.length >= maxParts) break;
          const index = parts.length;
          const filename = `${baseFilename}.part-${String(index + 1).padStart(3, "0")}.png`;
          const filePath = path.join(request.directory, filename);
          markRequestProgress(actionContext, "screenshot_save");
          assertRequestActive(actionContext);
          await fs.mkdir(request.directory, { recursive: true });
          assertRequestActive(actionContext);
          await fs.writeFile(filePath, currentBuffer);
          assertRequestActive(actionContext);
          const size = (await fs.stat(filePath)).size;
          assertRequestActive(actionContext);
          parts.push({ path: filePath, filename, size, index, scroll_position: scrollPosition });
          this.logger.info(actionContext, "capture_part", "success", { index: index + 1, scroll_position: scrollPosition });
        }
        previousBuffer = currentBuffer;
        const stepDistance = variedDistance(distance);
        markRequestProgress(actionContext, "screenshot_advance");
        await wheelGesture(this.mouse, page, 0, stepDistance, actionContext, this.logger);
        scrollPosition += stepDistance;
        await readingPause(waitMS, actionContext);
      }
      const result: LongScreenshotResult = {
        parts,
        count: parts.length,
        complete,
      };
      assertRequestActive(actionContext);
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
        details: { captured_parts: parts.length, ...(screenshotTimedOut(error) ? { capture_timeout_ms: screenshotTimeoutMS } : {}) },
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
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

interface DecodedPNG {
  width: number;
  height: number;
  channels: number;
  data: Buffer;
}

/** screenshotsAreDuplicate 使用像素容差判断滚轮后的画面是否已经停止变化。 */
export function screenshotsAreDuplicate(
  previous: Buffer,
  current: Buffer,
): boolean {
  // 相同PNG无需重复解码；不同压缩或过滤方式仍走原像素容差规则。
  if (previous.equals(current)) return true;
  const previousImage = decodePNG(previous);
  const currentImage = decodePNG(current);
  if (!previousImage || !currentImage) {
    return compressedScreenshotsAreDuplicate(previous, current);
  }
  if (
    previousImage.width !== currentImage.width ||
    previousImage.height !== currentImage.height
  ) {
    return false;
  }
  const width = previousImage.width;
  const height = previousImage.height;
  const startX = Math.floor(width * 0.1);
  const endX = Math.max(startX + 1, Math.floor(width * 0.9));
  const startY = Math.floor(height * 0.05);
  const endY = Math.max(startY + 1, Math.floor(height * 0.95));
  const stepX = Math.max(1, Math.floor((endX - startX) / 90));
  const stepY = Math.max(1, Math.floor((endY - startY) / 90));
  let same = 0;
  let total = 0;
  for (let y = startY; y < endY; y += stepY) {
    for (let x = startX; x < endX; x += stepX) {
      const previousOffset = (y * width + x) * previousImage.channels;
      const currentOffset = (y * width + x) * currentImage.channels;
      const difference =
        Math.abs(
          previousImage.data[previousOffset]! - currentImage.data[currentOffset]!,
        ) +
        Math.abs(
          previousImage.data[previousOffset + 1]! - currentImage.data[currentOffset + 1]!,
        ) +
        Math.abs(
          previousImage.data[previousOffset + 2]! - currentImage.data[currentOffset + 2]!,
        );
      total += 1;
      if (difference <= 24) {
        same += 1;
      }
    }
  }
  return total > 0 && same / total >= 0.98;
}

/** compressedScreenshotsAreDuplicate 在 PNG 解码失败时使用压缩字节做保守兜底。 */
function compressedScreenshotsAreDuplicate(
  previous: Buffer,
  current: Buffer,
): boolean {
  if (previous.length !== current.length) {
    return false;
  }
  const startOffset = Math.floor(previous.length * 0.12);
  const endOffset = Math.floor(previous.length * 0.88);
  if (endOffset <= startOffset) {
    return previous.equals(current);
  }
  const step = Math.max(1, Math.floor((endOffset - startOffset) / 4000));
  let same = 0;
  let total = 0;
  for (let index = startOffset; index < endOffset; index += step) {
    total += 1;
    if (Math.abs(previous.readUInt8(index) - current.readUInt8(index)) <= 8) {
      same += 1;
    }
  }
  return total > 0 && same / total >= 0.985;
}

/** decodePNG 使用Node标准库还原RGB/RGBA原始行，直接采样原通道，避免整图再次扩成RGBA。 */
function decodePNG(buffer: Buffer): DecodedPNG | null {
  try {
    if (
      buffer.length < 33 ||
      buffer.subarray(0, 8).toString("hex") !== "89504e470d0a1a0a"
    ) {
      return null;
    }
    let offset = 8;
    let width = 0;
    let height = 0;
    let colorType = 0;
    const imageData: Buffer[] = [];
    while (offset + 8 <= buffer.length) {
      const length = buffer.readUInt32BE(offset);
      const type = buffer.subarray(offset + 4, offset + 8).toString("ascii");
      const dataStart = offset + 8;
      const dataEnd = dataStart + length;
      if (dataEnd > buffer.length) {
        return null;
      }
      if (type === "IHDR") {
        width = buffer.readUInt32BE(dataStart);
        height = buffer.readUInt32BE(dataStart + 4);
        const bitDepth = buffer.readUInt8(dataStart + 8);
        colorType = buffer.readUInt8(dataStart + 9);
        const interlace = buffer.readUInt8(dataStart + 12);
        if (bitDepth !== 8 || interlace !== 0 || ![2, 6].includes(colorType)) {
          return null;
        }
      } else if (type === "IDAT") {
        imageData.push(buffer.subarray(dataStart, dataEnd));
      } else if (type === "IEND") {
        break;
      }
      offset = dataEnd + 4;
    }
    if (width <= 0 || height <= 0 || imageData.length === 0) {
      return null;
    }
    const channels = colorType === 6 ? 4 : 3;
    const stride = width * channels;
    const inflated = inflateSync(Buffer.concat(imageData));
    if (inflated.length < height * (stride + 1)) return null;
    const raw = Buffer.alloc(height * stride);
    let inputOffset = 0;
    for (let y = 0; y < height; y += 1) {
      const filter = inflated.readUInt8(inputOffset);
      inputOffset += 1;
      const row = inflated.subarray(inputOffset, inputOffset + stride);
      inputOffset += stride;
      unfilterPNGRow(row, raw, y, stride, channels, filter);
    }
    return { width, height, channels, data: raw };
  } catch {
    return null;
  }
}

/** unfilterPNGRow 还原 PNG 每一行使用的标准过滤器。 */
function unfilterPNGRow(
  row: Buffer,
  output: Buffer,
  y: number,
  stride: number,
  channels: number,
  filter: number,
): void {
  const rowStart = y * stride;
  const previousStart = rowStart - stride;
  if (filter < 1 || filter > 4) {
    row.copy(output, rowStart);
    return;
  }
  for (let x = 0; x < stride; x += 1) {
    // 上层已验证完整行长度，邻接偏移均由当前行及channels边界约束。
    const left = x >= channels ? output[rowStart + x - channels]! : 0;
    const up = y > 0 ? output[previousStart + x]! : 0;
    const upLeft = y > 0 && x >= channels ? output[previousStart + x - channels]! : 0;
    let value = row[x]!;
    if (filter === 1) value += left;
    if (filter === 2) value += up;
    if (filter === 3) value += Math.floor((left + up) / 2);
    if (filter === 4) value += paethPredictor(left, up, upLeft);
    output[rowStart + x] = value & 0xff;
  }
}

/** paethPredictor 返回 PNG Paeth 过滤器使用的邻近像素预测值。 */
function paethPredictor(left: number, up: number, upLeft: number): number {
  const prediction = left + up - upLeft;
  const leftDistance = Math.abs(prediction - left);
  const upDistance = Math.abs(prediction - up);
  const upLeftDistance = Math.abs(prediction - upLeft);
  if (leftDistance <= upDistance && leftDistance <= upLeftDistance) {
    return left;
  }
  if (upDistance <= upLeftDistance) {
    return up;
  }
  return upLeft;
}
