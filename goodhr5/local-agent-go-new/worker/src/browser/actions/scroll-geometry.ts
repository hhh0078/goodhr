// 文件作用说明：计算真实滚轮的方向、距离与安全区域，避免横向越界时反复纵向滚动。

import type { ActionContext } from "../../contracts/common.js";
import type { ElementView } from "../../contracts/selector.js";
import { WorkerError } from "../../errors/worker-error.js";

/** VisibleArea 表示目标元素允许进入的安全可见区域。 */
export interface VisibleArea {
  left: number;
  top: number;
  right: number;
  bottom: number;
  width: number;
  height: number;
}

/** axisGap 返回单个方向还需要滚动的有符号距离，已满足可见要求时返回零。 */
function axisGap(start: number, size: number, low: number, high: number, full: boolean): number {
  if (full) {
    if (start < low) return start - low;
    if (start + size > high) return start + size - high;
  } else {
    const requiredOverlap = Math.min(8, size, Math.max(0, high - low));
    if (start + size < low + requiredOverlap) return start + size - low - requiredOverlap;
    if (start > high - requiredOverlap) return start - high + requiredOverlap;
  }
  return 0;
}

/** targetGap 同时衡量横纵方向，部分可见模式保留至少八像素的安全落点区域。 */
export function targetGap(view: ElementView, area: VisibleArea, full: boolean): number {
  return Math.hypot(
    axisGap(view.box.x, view.box.width, area.left, area.right, full),
    axisGap(view.box.y, view.box.height, area.top, area.bottom, full),
  );
}

/** directedWheel 按最新位置选择单轴小步滚动，纵向到位后才补齐横向，避免上下振荡。 */
export function directedWheel(view: ElementView, distance: number, area: VisibleArea, full: boolean): { x: number; y: number } {
  const maximum = Math.max(1, Math.abs(distance));
  const gapY = axisGap(view.box.y, view.box.height, area.top, area.bottom, full);
  const gapX = axisGap(view.box.x, view.box.width, area.left, area.right, full);
  const step = (gap: number): number => Math.sign(gap) * Math.min(maximum, Math.ceil(Math.abs(gap)));
  return gapY !== 0 ? { x: 0, y: step(gapY) } : { x: step(gapX), y: 0 };
}

/** viewAccepted 判断目标是否满足当前完整或部分可见要求。 */
export function viewAccepted(view: ElementView, full: boolean, area: VisibleArea): boolean {
  return view.visible && area.width > 0 && area.height > 0 && targetGap(view, area, full) === 0;
}

/** visibleArea 计算窗口与可选滚动容器交集，整页锚点不缩小窗口范围。 */
export function visibleArea(target: ElementView, containerView: ElementView | undefined, rawMargin: number): VisibleArea {
  const margin = Math.max(1, rawMargin);
  const viewport = target.viewport;
  const container = containerView?.box;
  const limitsWidth = Boolean(container && container.width > 0 && container.width < viewport.width);
  const limitsHeight = Boolean(container && container.height > 0 && container.height < viewport.height);
  const physicalLeft = Math.max(0, limitsWidth && container ? container.x : 0);
  const physicalRight = Math.min(viewport.width, limitsWidth && container ? container.x + container.width : viewport.width);
  // 宽卡片允许缩小横向留白，但仍必须完整处于窗口和列表交集内，不能把留白不足误报为窗口太小。
  const horizontalMargin = Math.min(margin, Math.max(0, (physicalRight - physicalLeft - target.box.width) / 2));
  // 边距用于避开窗口固定栏；列表里的卡片常与容器等宽，不能再把容器两侧各扣一次边距。
  const left = Math.max(horizontalMargin, physicalLeft);
  const top = Math.max(margin, limitsHeight && container ? container.y : margin);
  const right = Math.min(viewport.width - horizontalMargin, physicalRight);
  const bottom = Math.min(viewport.height - margin, limitsHeight && container ? container.y + container.height : viewport.height - margin);
  return { left, top, right, bottom, width: Math.max(0, right - left), height: Math.max(0, bottom - top) };
}

/** intersectsViewport 判断可见部分是否足够承接鼠标，极窄边缘改用窗口中央滚动。 */
export function intersectsViewport(view: ElementView): boolean {
  return viewAccepted(view, false, visibleArea(view, undefined, 0));
}

/** assertTargetCanFit 在窗口或容器无法容纳完整目标时明确停止，避免无效滚动。 */
export function assertTargetCanFit(view: ElementView, area: VisibleArea, full: boolean, context: ActionContext): void {
  if (area.width > 0 && area.height > 0 && (!full || (view.box.width <= area.width && view.box.height <= area.height))) return;
  throw new WorkerError({
    code: "VIEWPORT_TOO_SMALL",
    message: "浏览器窗口太小，目标元素无法完整显示，请放大窗口后再试",
    action: context.action, step: "ensure_visible", trace_id: context.trace_id, retryable: true,
    details: {
      target_width: Math.round(view.box.width), target_height: Math.round(view.box.height),
      available_width: Math.round(area.width), available_height: Math.round(area.height),
    },
  });
}
