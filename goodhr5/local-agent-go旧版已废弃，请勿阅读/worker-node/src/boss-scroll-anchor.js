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

/**
 * bossCandidateVerticalGap 计算候选人卡片到上下安全区域的有方向距离。
 * 负数表示卡片在安全区域上方，正数表示在下方，0 表示已经到位。
 * @param {Record<string, any>|null} view - 候选人卡片可视状态。
 * @returns {number|null} 带方向的剩余距离，无法计算时返回 null。
 */
export function bossCandidateVerticalGap(view) {
  const box = view?.box;
  if (!box) return null;
  const margin = Math.max(0, finiteNumber(view?.margin));
  const containerBox = view?.container_box;
  const viewportHeight = Math.max(0, finiteNumber(view?.viewport?.height));
  const topBoundary = containerBox
    ? finiteNumber(containerBox.y) + margin
    : margin;
  const bottomBoundary = containerBox
    ? finiteNumber(containerBox.y) +
      Math.max(0, finiteNumber(containerBox.height)) -
      margin
    : viewportHeight - margin;
  if (bottomBoundary <= topBoundary) return null;
  const top = finiteNumber(box.y);
  const bottom = top + Math.max(0, finiteNumber(box.height));
  if (top < topBoundary) return Math.round(top - topBoundary);
  if (bottom > bottomBoundary) return Math.round(bottom - bottomBoundary);
  return 0;
}

/**
 * bossAdaptiveWheelDistance 根据候选人剩余距离计算更合适的真实滚轮步长。
 * 距离较远时提高步长，接近安全区域后恢复基础步长，避免固定小步长提前耗尽重试。
 * @param {Record<string, any>|null} view - 候选人卡片可视状态。
 * @param {number} baseDistance - 接近目标时使用的基础滚动距离。
 * @param {number} maximumDistance - 单次滚轮允许使用的最大距离。
 * @returns {number} 带方向的滚轮距离。
 */
export function bossAdaptiveWheelDistance(
  view,
  baseDistance = 120,
  maximumDistance = 600,
) {
  const base = Math.max(40, Math.abs(finiteNumber(baseDistance, 120)));
  const maximum = Math.max(base, Math.abs(finiteNumber(maximumDistance, 600)));
  const gap = bossCandidateVerticalGap(view);
  if (gap === null || gap === 0) return base;
  const magnitude = Math.min(
    maximum,
    Math.max(base, Math.ceil(Math.abs(gap) * 0.75)),
  );
  return gap < 0 ? -magnitude : magnitude;
}

/**
 * bossScrollAttemptBudget 根据初始剩余距离扩大候选人滚动轮数。
 * 配置轮数仍作为最低值，远距离目标会获得额外轮数，并保留总上限防止无限滚动。
 * @param {Record<string, any>|null} view - 初次测量的候选人卡片可视状态。
 * @param {number} configuredAttempts - 平台配置的基础轮数。
 * @param {number} maximumDistance - 自适应滚动允许的最大单步距离。
 * @returns {number} 本次候选人定位允许使用的外层轮数。
 */
export function bossScrollAttemptBudget(
  view,
  configuredAttempts = 18,
  maximumDistance = 600,
) {
  const configured = Math.max(
    1,
    Math.min(60, Math.round(finiteNumber(configuredAttempts, 18))),
  );
  const maximum = Math.max(120, Math.abs(finiteNumber(maximumDistance, 600)));
  const gap = bossCandidateVerticalGap(view);
  if (gap === null || gap === 0) return configured;
  const required = Math.ceil(Math.abs(gap) / maximum) + 4;
  return Math.max(configured, Math.min(60, required));
}
