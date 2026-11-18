// 文件作用说明：通过标准Locator边界裁剪长截图的当前可见画面，限制iframe交集并输出无简历正文的诊断。
import type { Locator, Page } from "playwright-core";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import type { ElementBox, ViewportSize } from "../../contracts/selector.js";
import type { WorkerLogger } from "../../logging/logger.js";
import type { ResolvedElement } from "../primitives/locator.js";
import type { ScreenshotPrimitive } from "../primitives/screenshot.js";
import { assertRequestActive } from "../primitives/request-lifecycle.js";
import type { FindAction } from "./find.js";

/**
 * screenshotClip 取可见交集，未知边界拒绝截图。
 * @param boxes 目标及各层iframe的当前边界。
 * @param viewport 当前页面可见尺寸。
 * @returns 允许截图的CSS像素区域。
 */
export function screenshotClip(boxes: Array<ElementBox | null>, viewport: ViewportSize): ElementBox {
  if (boxes.some(box => !box || ![box.x, box.y, box.width, box.height].every(Number.isFinite) || box.width <= 0 || box.height <= 0)) {
    throw new Error("长截图区域位置还没读完整");
  }
  let left = 0, top = 0, right = viewport.width, bottom = viewport.height;
  for (const box of boxes as ElementBox[]) {
    left = Math.max(left, box.x); top = Math.max(top, box.y);
    right = Math.min(right, box.x + box.width); bottom = Math.min(bottom, box.y + box.height);
  }
  if (![left, top, right, bottom].every(Number.isFinite) || right - left < 1 || bottom - top < 80) {
    throw new Error("详情截图区域太小或暂未显示，不能截取不完整画面");
  }
  return { x: left, y: top, width: right - left, height: bottom - top };
}

/**
 * longScreenshotViewport 使用真实frame路径准备无滚动截图。
 * @param page 当前页面。
 * @param target 已核实的截图目标。
 * @param find 既有封装查找能力。
 * @param primitive 既有截图原子能力。
 * @param context 本次请求的生命周期。
 * @returns 可见高度与每段重新核对边界的截图方法。
 */
export async function longScreenshotViewport(page: Page, target: ResolvedElement, find: FindAction, primitive: ScreenshotPrimitive, context: ActionContext): Promise<{ height: number; capture: () => Promise<{ buffer: Buffer; details: JsonObject }> }> {
  const frames: Locator[] = [];
  const path = target.resolved_spec?.frames ?? [];
  for (const [index, frame] of path.entries()) {
    const found = await find.one({ frames: path.slice(0, index), target: frame, state: "visible", timeout_ms: 1500, description: "长截图框架边界" }, context, true);
    frames.push(found.resolved.locator);
  }
  const initial = screenshotClip([await target.locator.boundingBox(), ...await Promise.all(frames.map(frame => frame.boundingBox()))], target.view.viewport);
  return { height: initial.height,
  /** capture 记录截图前后几何，移动中的画面不进入拼接；只返回尺寸，不返回或记录候选人正文。 */
  capture: async function capture() {
    assertRequestActive(context);
    const before = await target.locator.boundingBox();
    const frameBoxes = await Promise.all(frames.map(frame => frame.boundingBox()));
    const clip = screenshotClip([before, ...frameBoxes], target.view.viewport);
    const buffer = await primitive.clippedPageBuffer(page, clip);
    assertRequestActive(context);
    const after = await target.locator.boundingBox();
    const afterFrames = await Promise.all(frames.map(frame => frame.boundingBox()));
    const changed = JSON.stringify([before, ...frameBoxes]) !== JSON.stringify([after, ...afterFrames]);
    if (changed) throw new Error("长截图期间目标或框架发生移动，本段没有采用");
    return { buffer, details: { capture_mode: "viewport_clip", clip: { ...clip }, target_box: { ...before! }, frame_boxes: frameBoxes.map(box => ({ ...box! })),
      top_edge_visible: before!.y >= clip.y - 1, bottom_edge_visible: before!.y + before!.height <= clip.y + clip.height + 1 } };
  } };
}

/**
 * logLongScreenshotProgress 发送无正文的分段证据到既有诊断链路。
 * @param logger 既有日志器。
 * @param context 本次请求。
 * @param stage 当前采集阶段。
 * @param output 尺寸、耗时和完整性等非正文证据。
 */
export function logLongScreenshotProgress(logger: WorkerLogger, context: ActionContext, stage: string, output: JsonObject): void {
  logger.info(context, "screenshot_diagnostic", "checked", { diagnostic: true, input: { stage }, output });
}
