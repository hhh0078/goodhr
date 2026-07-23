/** 本文件负责判断 Boss 候选人滚轮锚点是否完整处于浏览器上下安全区域内。 */

/**
 * finiteNumber 将可用值转换为有限数字，不可用时返回备用值。
 * @param {any} value - 待转换值。
 * @param {number} fallback - 转换失败时使用的备用值。
 * @returns {number} 可安全参与坐标计算的数字。
 */
function finiteNumber(value, fallback = 0) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

/**
 * bossWheelAnchorSafety 计算候选人卡片是否完整处于上下安全边距内。
 * @param {Record<string, any>|null} view - 候选人卡片的可视状态。
 * @param {number} requestedMargin - 页面顶部和底部需要保留的安全边距。
 * @returns {Record<string, any>} 安全判断、边界坐标和拒绝原因。
 */
export function bossWheelAnchorSafety(view, requestedMargin = 0) {
  const viewportWidth = Math.max(0, finiteNumber(view?.viewport?.width));
  const viewportHeight = Math.max(0, finiteNumber(view?.viewport?.height));
  const maximumMargin = Math.max(0, viewportHeight / 2 - 1);
  const margin = Math.min(
    Math.max(0, finiteNumber(requestedMargin)),
    maximumMargin,
  );
  const safeTop = margin;
  const safeBottom = Math.max(safeTop, viewportHeight - margin);
  const box = view?.box || null;

  if (!view?.visible) {
    return {
      safe: false,
      reason: "not-visible",
      margin,
      safe_top: safeTop,
      safe_bottom: safeBottom,
      viewport_width: viewportWidth,
      viewport_height: viewportHeight,
    };
  }
  if (!box || viewportWidth <= 0 || viewportHeight <= 0) {
    return {
      safe: false,
      reason: !box ? "no-box" : "no-viewport",
      margin,
      safe_top: safeTop,
      safe_bottom: safeBottom,
      viewport_width: viewportWidth,
      viewport_height: viewportHeight,
    };
  }

  const left = finiteNumber(box.x);
  const top = finiteNumber(box.y);
  const width = Math.max(0, finiteNumber(box.width));
  const height = Math.max(0, finiteNumber(box.height));
  const right = left + width;
  const bottom = top + height;
  const horizontallyVisible = right > 0 && left < viewportWidth;
  let reason = "safe";
  if (!horizontallyVisible) reason = "outside-horizontal-viewport";
  else if (top < safeTop) reason = "above-safe-area";
  else if (bottom > safeBottom) reason = "below-safe-area";

  return {
    safe: reason === "safe",
    reason,
    margin,
    safe_top: Math.round(safeTop),
    safe_bottom: Math.round(safeBottom),
    viewport_width: Math.round(viewportWidth),
    viewport_height: Math.round(viewportHeight),
    card_top: Math.round(top),
    card_bottom: Math.round(bottom),
    card_height: Math.round(height),
  };
}

/**
 * bossWheelAnchorMoveDecision 判断滚轮锚点是否允许接收鼠标移动。
 * 这里只执行上下安全边距检查，避免把候选人卡片的横向留白误判成危险区域。
 * @param {Record<string, any>|null} view - 候选人卡片的可视状态。
 * @param {number} requestedMargin - 页面顶部和底部需要保留的安全边距。
 * @returns {{allowed:boolean,safety:Record<string, any>}} 移动许可和详细安全判断。
 */
export function bossWheelAnchorMoveDecision(view, requestedMargin = 0) {
  const safety = bossWheelAnchorSafety(view, requestedMargin);
  return {
    allowed: safety.safe,
    safety,
  };
}
