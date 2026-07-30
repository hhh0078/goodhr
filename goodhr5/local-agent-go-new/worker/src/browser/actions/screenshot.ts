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
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { ScreenshotPrimitive } from "../primitives/screenshot.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { BrowserSession } from "../session/browser-session.js";
import { FindAction } from "./find.js";
import { MoveAction } from "./move.js";

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
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
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
      const distance = Math.max(
        100,
        request.distance ??
          Math.floor(Math.max(400, target.resolved.view.viewport.height * 0.75)),
      );
      let complete = false;
      let previousBuffer: Buffer | null = null;
      let scrollPosition = 0;
      for (let index = 0; index < maxParts; index += 1) {
        const currentBuffer = await this.primitive.elementBuffer(
          target.resolved.locator,
        );
        if (
          previousBuffer &&
          screenshotsAreDuplicate(previousBuffer, currentBuffer)
        ) {
          complete = true;
          break;
        }
        const filename = `${baseFilename}.part-${String(index + 1).padStart(3, "0")}.png`;
        const filePath = path.join(request.directory, filename);
        await fs.mkdir(request.directory, { recursive: true });
        await fs.writeFile(filePath, currentBuffer);
        const size = (await fs.stat(filePath)).size;
        parts.push({
          path: filePath,
          filename,
          size,
          index,
          scroll_position: scrollPosition,
        });
        this.logger.info(actionContext, "capture_part", "success", {
          index: index + 1,
          scroll_position: scrollPosition,
        });
        previousBuffer = currentBuffer;
        await this.mouse.wheel(page, 0, distance);
        scrollPosition += distance;
        await delay(waitMS);
      }
      const result: LongScreenshotResult = {
        parts,
        count: parts.length,
        complete,
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

/** delay 使用 Node 定时器等待页面滚动和截图状态稳定。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

interface DecodedPNG {
  width: number;
  height: number;
  data: Buffer;
}

/** screenshotsAreDuplicate 使用像素容差判断滚轮后的画面是否已经停止变化。 */
export function screenshotsAreDuplicate(
  previous: Buffer,
  current: Buffer,
): boolean {
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
      const offset = (y * width + x) * 4;
      const difference =
        Math.abs(
          previousImage.data.readUInt8(offset) -
            currentImage.data.readUInt8(offset),
        ) +
        Math.abs(
          previousImage.data.readUInt8(offset + 1) -
            currentImage.data.readUInt8(offset + 1),
        ) +
        Math.abs(
          previousImage.data.readUInt8(offset + 2) -
            currentImage.data.readUInt8(offset + 2),
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

/** decodePNG 使用 Node 标准库把 Playwright PNG 解码为 RGBA 像素。 */
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
    const raw = Buffer.alloc(height * stride);
    let inputOffset = 0;
    for (let y = 0; y < height; y += 1) {
      const filter = inflated.readUInt8(inputOffset);
      inputOffset += 1;
      const row = inflated.subarray(inputOffset, inputOffset + stride);
      inputOffset += stride;
      unfilterPNGRow(row, raw, y, stride, channels, filter);
    }
    const rgba = Buffer.alloc(width * height * 4);
    for (let source = 0, target = 0; source < raw.length; source += channels, target += 4) {
      rgba[target] = raw.readUInt8(source);
      rgba[target + 1] = raw.readUInt8(source + 1);
      rgba[target + 2] = raw.readUInt8(source + 2);
      rgba[target + 3] = channels === 4 ? raw.readUInt8(source + 3) : 255;
    }
    return { width, height, data: rgba };
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
  for (let x = 0; x < stride; x += 1) {
    const left =
      x >= channels ? output.readUInt8(rowStart + x - channels) : 0;
    const up = y > 0 ? output.readUInt8(previousStart + x) : 0;
    const upLeft =
      y > 0 && x >= channels
        ? output.readUInt8(previousStart + x - channels)
        : 0;
    let value = row.readUInt8(x);
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
