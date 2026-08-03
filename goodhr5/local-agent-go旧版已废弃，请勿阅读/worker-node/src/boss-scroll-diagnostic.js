/** 本文件负责汇总 Boss 候选人卡片滚动轨迹，并生成可直接展示在岗位日志中的失败诊断。 */

/**
 * numberOrNull 将可用数值标准化为有限数字，不可用时返回 null。
 * @param {any} value - 待转换值。
 * @returns {number|null} 标准化后的数字。
 */
function numberOrNull(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

/**
 * scrollViewGap 计算候选人卡片距离目标可视区域还差多少像素。
 * @param {Record<string, any>|null} view - 候选人卡片可视状态。
 * @returns {number|null} 剩余垂直距离，已进入范围时为 0。
 */
export function scrollViewGap(view) {
  const box = view?.box;
  if (!box) return null;
  const margin = Math.max(0, numberOrNull(view?.margin) || 0);
  const containerBox = view?.container_box;
  const viewport = view?.viewport;
  const topBoundary = containerBox
    ? (numberOrNull(containerBox.y) || 0) + margin
    : margin;
  const bottomBoundary = containerBox
    ? (numberOrNull(containerBox.y) || 0) +
      (numberOrNull(containerBox.height) || 0) -
      margin
    : (numberOrNull(viewport?.height) || 0) - margin;
  const top = numberOrNull(box.y);
  const height = numberOrNull(box.height);
  if (top === null || height === null || bottomBoundary <= topBoundary) return null;
  const bottom = top + height;
  if (top < topBoundary) return Math.round(topBoundary - top);
  if (bottom > bottomBoundary) return Math.round(bottom - bottomBoundary);
  return 0;
}

/**
 * scrollStateDelta 计算同一个滚动容器前后的实际滚动距离。
 * @param {Record<string, any>|null} before - 滚动前容器状态。
 * @param {Record<string, any>|null} after - 滚动后容器状态。
 * @returns {number|null} 实际滚动距离，无法比较时返回 null。
 */
export function scrollStateDelta(before, after) {
  if (!before || !after || before.target !== after.target) return null;
  const beforeTop = numberOrNull(before.scroll_top);
  const afterTop = numberOrNull(after.scroll_top);
  if (beforeTop === null || afterTop === null) return null;
  return Math.round(afterTop - beforeTop);
}

/**
 * targetPositionDelta 计算目标卡片在一次滚动前后的纵向位移。
 * @param {Record<string, any>|null} beforeView - 滚动前卡片状态。
 * @param {Record<string, any>|null} afterView - 滚动后卡片状态。
 * @returns {number|null} 卡片纵向位移，无法比较时返回 null。
 */
export function targetPositionDelta(beforeView, afterView) {
  const beforeY = numberOrNull(beforeView?.box?.y);
  const afterY = numberOrNull(afterView?.box?.y);
  if (beforeY === null || afterY === null) return null;
  return Math.round(afterY - beforeY);
}

/**
 * bossScrollDiagnosisText 返回滚动诊断代码对应的中文结论。
 * @param {string} code - 诊断代码。
 * @returns {string} 中文诊断结论。
 */
export function bossScrollDiagnosisText(code) {
  const messages = {
    "wheel-not-effective": "滚轮已执行，但页面或列表基本没有移动，优先检查鼠标停靠位置和滚动容器",
    "retry-distance-insufficient": "滚动方向正确且目标正在接近，但重试总距离不足",
    "direction-oscillation": "滚动方向多次切换，目标可能在可视边界附近来回越界",
    "target-not-approaching": "滚动后目标没有接近可视区域，可能滚错容器或目标定位不正确",
    "target-not-measurable": "目标卡片无法读取位置，可能选择器失效或元素不可见",
    unclassified: "现有数据仍不能唯一判断，请结合逐次 Worker 日志查看",
  };
  return messages[code] || messages.unclassified;
}

/**
 * buildBossCandidateScrollFailureDiagnostic 汇总滚动轨迹并生成失败日志字段。
 * @param {Record<string, any>} input - 候选人、视口、容器和逐次滚动轨迹。
 * @returns {Record<string, any>} 结构化诊断结果和岗位日志错误文案。
 */
export function buildBossCandidateScrollFailureDiagnostic(input = {}) {
  const attempts = Array.isArray(input.attempts) ? input.attempts : [];
  const initialView =
    input.initial_view || attempts.find((item) => item?.before_view)?.before_view || null;
  const finalView =
    input.final_view ||
    [...attempts].reverse().find((item) => item?.after_view)?.after_view ||
    null;
  const initialGap = scrollViewGap(initialView);
  const finalGap = scrollViewGap(finalView);
  const requestedDistances = attempts
    .map((item) => numberOrNull(item?.distance))
    .filter((value) => value !== null);
  const measuredAttempts = attempts
    .map((item) => ({
      actual_delta: scrollStateDelta(item?.scroll_before, item?.scroll_after),
      target_delta: targetPositionDelta(item?.before_view, item?.after_view),
    }))
    .filter((item) => item.actual_delta !== null);
  const actualDeltas = measuredAttempts.map((item) => item.actual_delta);
  const requestedTotal = requestedDistances.reduce(
    (sum, value) => sum + Math.abs(value),
    0,
  );
  const actualTotal = actualDeltas.reduce(
    (sum, value) => sum + Math.abs(value),
    0,
  );
  const ineffectiveCount = measuredAttempts.filter(
    (item) =>
      Math.abs(item.actual_delta) <= 2 &&
      (item.target_delta === null || Math.abs(item.target_delta) <= 2),
  ).length;
  const directions = requestedDistances
    .map((value) => Math.sign(value))
    .filter((value) => value !== 0);
  const wheelTargets = [
    ...new Set(
      attempts
        .map(
          (item) =>
            item?.wheel_target ||
            item?.scroll_before?.target ||
            item?.scroll_after?.target ||
            "",
        )
        .filter(Boolean),
    ),
  ];
  const firstScrollState =
    attempts.find((item) => item?.scroll_before?.scroll_top !== undefined)
      ?.scroll_before || null;
  const lastScrollState =
    [...attempts]
      .reverse()
      .find((item) => item?.scroll_after?.scroll_top !== undefined)?.scroll_after ||
    null;
  let directionChanges = 0;
  for (let index = 1; index < directions.length; index += 1) {
    if (directions[index] !== directions[index - 1]) directionChanges += 1;
  }

  let diagnosisCode = "unclassified";
  if (!finalView?.box || ["not-visible", "no-box"].includes(finalView?.reason)) {
    diagnosisCode = "target-not-measurable";
  } else if (
    actualDeltas.length > 0 &&
    ineffectiveCount >= Math.max(2, Math.ceil(actualDeltas.length * 0.6))
  ) {
    diagnosisCode = "wheel-not-effective";
  } else if (directionChanges >= 2) {
    diagnosisCode = "direction-oscillation";
  } else if (
    initialGap !== null &&
    finalGap !== null &&
    finalGap > 0 &&
    finalGap < initialGap
  ) {
    diagnosisCode = "retry-distance-insufficient";
  } else if (
    initialGap !== null &&
    finalGap !== null &&
    finalGap >= initialGap &&
    requestedDistances.length > 0
  ) {
    diagnosisCode = "target-not-approaching";
  }

  const viewportWidth = numberOrNull(input.viewport?.width) || 0;
  const viewportHeight = numberOrNull(input.viewport?.height) || 0;
  const viewportSource = String(input.viewport?.source || "unknown");
  const devicePixelRatio =
    numberOrNull(input.viewport?.device_pixel_ratio) ||
    numberOrNull(initialView?.viewport?.device_pixel_ratio) ||
    0;
  const visualViewportScale =
    numberOrNull(input.viewport?.visual_viewport_scale) ||
    numberOrNull(initialView?.viewport?.visual_viewport_scale) ||
    0;
  const displayText =
    `，视口来源=${viewportSource}` +
    (devicePixelRatio > 0 ? `，DPR=${devicePixelRatio}` : "") +
    (visualViewportScale > 0
      ? `，页面可视缩放=${visualViewportScale}`
      : "");
  const candidateName = String(input.candidate_name || "未知候选人");
  const containerText = input.container?.usable
    ? `${input.container.selector || "已识别容器"}`
    : `未识别(${input.container?.reason || "unknown"})`;
  const initialBoxText = initialView?.box
    ? `y=${Math.round(initialView.box.y)},h=${Math.round(initialView.box.height)}`
    : "无法读取";
  const finalBoxText = finalView?.box
    ? `y=${Math.round(finalView.box.y)},h=${Math.round(finalView.box.height)}`
    : "无法读取";
  const diagnosisText = bossScrollDiagnosisText(diagnosisCode);
  const scrollRangeText =
    firstScrollState && lastScrollState
      ? `${firstScrollState.scroll_top}→${lastScrollState.scroll_top}/最大${lastScrollState.max_top ?? "未知"}`
      : "无法读取";
  const finalStateText = finalView
    ? `${finalView.reason || "measured"},in=${Boolean(finalView.in_viewport)},full=${Boolean(finalView.fully_visible)}`
    : "无法读取";
  const message =
    `候选人卡片滚动定位失败：候选人=${candidateName}，目标序号=${Number(input.requested_card_index || 0) + 1}` +
    `，最终序号=${Number(input.final_card_index || 0) + 1}，DOM卡片数=${Number(input.card_count || 0)}` +
    `，视口=${viewportWidth}x${viewportHeight}${displayText}，滚动容器=${containerText}` +
    `，尝试=${Number(input.outer_attempts || 0)}轮/${attempts.length}次滚轮` +
    `，指令累计=${Math.round(requestedTotal)}px，实际滚动=${actualDeltas.length > 0 ? `${Math.round(actualTotal)}px` : "无法读取"}` +
    `，无效滚动=${ineffectiveCount}次，方向切换=${directionChanges}次` +
    `，滚轮落点=${wheelTargets.join("|") || "无法读取"}，容器位置=${scrollRangeText}` +
    `，初始位置=${initialBoxText}，最终位置=${finalBoxText}` +
    `，初始距离=${initialGap ?? "未知"}px，剩余距离=${finalGap ?? "未知"}px，最终检测=${finalStateText}` +
    `，初步判断=${diagnosisText}。逐次坐标请查看 browser-worker.log`;

  return {
    candidate_name: candidateName,
    requested_card_index: Number(input.requested_card_index || 0),
    final_card_index: Number(input.final_card_index || 0),
    card_count: Number(input.card_count || 0),
    viewport: `${viewportWidth}x${viewportHeight}`,
    viewport_source: viewportSource,
    device_pixel_ratio: devicePixelRatio,
    visual_viewport_scale: visualViewportScale,
    container: containerText,
    outer_attempts: Number(input.outer_attempts || 0),
    wheel_attempts: attempts.length,
    requested_total_px: Math.round(requestedTotal),
    actual_total_px: actualDeltas.length > 0 ? Math.round(actualTotal) : null,
    ineffective_attempts: ineffectiveCount,
    direction_changes: directionChanges,
    wheel_targets: wheelTargets,
    scroll_range: scrollRangeText,
    initial_gap_px: initialGap,
    final_gap_px: finalGap,
    initial_view: initialView,
    final_view: finalView,
    diagnosis_code: diagnosisCode,
    diagnosis: diagnosisText,
    message,
  };
}
