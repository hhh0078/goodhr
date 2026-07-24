/** 本文件负责汇总通用列表项点击前的滚动轨迹，并生成猎聘候选人详情定位失败的可读诊断。 */

/**
 * finiteNumber 将可用数值标准化为有限数字，不可用时返回 null。
 * @param {any} value - 待转换值。
 * @returns {number|null} 标准化后的数字。
 */
function finiteNumber(value) {
  if (value === null || value === undefined || value === "") return null;
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

/**
 * listClickViewDecision 解释一次视口检测究竟卡在横向还是纵向条件。
 * @param {Record<string, any>|null} view - 元素可视状态。
 * @returns {Record<string, any>} 可视边界、失败维度和最终判定。
 */
export function listClickViewDecision(view) {
  const box = view?.box || null;
  const viewport = view?.viewport || null;
  const margin = Math.max(0, finiteNumber(view?.margin) || 0);
  const verticalOnly = Boolean(view?.vertical_only);
  const requireFull = Boolean(view?.require_full);
  const failedDimensions = [];
  if (!view?.visible || !box) {
    failedDimensions.push(view?.reason || "not-measurable");
    return {
      accepted: false,
      vertical_ready: false,
      horizontal_ready: false,
      failed_dimensions: failedDimensions,
      margin,
      vertical_only: verticalOnly,
      require_full: requireFull,
    };
  }

  const left = finiteNumber(box.x) || 0;
  const top = finiteNumber(box.y) || 0;
  const width = finiteNumber(box.width) || 0;
  const height = finiteNumber(box.height) || 0;
  const viewportWidth = finiteNumber(viewport?.width) || 0;
  const viewportHeight = finiteNumber(viewport?.height) || 0;
  const right = left + width;
  const bottom = top + height;
  const safeLeft = margin;
  const safeRight = viewportWidth - margin;
  const safeTop = margin;
  const safeBottom = viewportHeight - margin;
  const verticalReady = requireFull
    ? Boolean(view.vertically_fully_visible)
    : Boolean(view.vertically_visible);
  const horizontalOverlap = Boolean(view.horizontally_visible);
  const horizontalFull = left >= safeLeft && right <= safeRight;
  const horizontalPartial = right > safeLeft && left < safeRight;
  const horizontalReady = verticalOnly
    ? horizontalOverlap
    : requireFull
      ? horizontalFull
      : horizontalPartial;

  if (!verticalReady) failedDimensions.push("vertical");
  if (!horizontalReady) {
    failedDimensions.push(
      requireFull && !verticalOnly ? "horizontal-margin" : "horizontal",
    );
  }
  if (
    failedDimensions.length === 0 &&
    !view.in_viewport
  ) {
    failedDimensions.push("viewport-rule");
  }

  return {
    accepted: Boolean(view.in_viewport),
    vertical_ready: verticalReady,
    horizontal_ready: horizontalReady,
    horizontal_full: horizontalFull,
    horizontal_partial: horizontalPartial,
    horizontal_overlap: horizontalOverlap,
    failed_dimensions: failedDimensions,
    margin,
    vertical_only: verticalOnly,
    require_full: requireFull,
    safe_bounds: {
      left: safeLeft,
      right: safeRight,
      top: safeTop,
      bottom: safeBottom,
    },
    target_bounds: {
      left,
      right,
      top,
      bottom,
    },
    horizontal_overflow: {
      left: Math.max(0, Math.round(safeLeft - left)),
      right: Math.max(0, Math.round(right - safeRight)),
    },
    vertical_overflow: {
      top: Math.max(0, Math.round(safeTop - top)),
      bottom: Math.max(0, Math.round(bottom - safeBottom)),
    },
  };
}

/**
 * directionSwitchCount 统计连续滚轮指令发生了多少次方向切换。
 * @param {Array<Record<string, any>>} attempts - 逐轮滚动轨迹。
 * @returns {number} 方向切换次数。
 */
export function directionSwitchCount(attempts) {
  const directions = (attempts || [])
    .map((item) => Math.sign(finiteNumber(item?.distance) || 0))
    .filter((value) => value !== 0);
  let count = 0;
  for (let index = 1; index < directions.length; index += 1) {
    if (directions[index] !== directions[index - 1]) count += 1;
  }
  return count;
}

/**
 * scrollPositionDelta 计算同一个滚动对象前后的实际滚动距离。
 * @param {Record<string, any>|null} before - 滚动前状态。
 * @param {Record<string, any>|null} after - 滚动后状态。
 * @returns {number|null} 实际滚动距离，无法比较时返回 null。
 */
export function scrollPositionDelta(before, after) {
  if (!before || !after || before.target !== after.target) return null;
  const beforeTop = finiteNumber(before.scroll_top);
  const afterTop = finiteNumber(after.scroll_top);
  if (beforeTop === null || afterTop === null) return null;
  return Math.round(afterTop - beforeTop);
}

/**
 * targetPositionDelta 计算同一个目标元素在一次滚轮前后的纵向位移。
 * @param {Record<string, any>|null} beforeView - 滚动前目标状态。
 * @param {Record<string, any>|null} afterView - 滚动后目标状态。
 * @returns {number|null} 目标纵向位移，无法比较时返回 null。
 */
export function targetPositionDelta(beforeView, afterView) {
  const beforeY = finiteNumber(beforeView?.box?.y);
  const afterY = finiteNumber(afterView?.box?.y);
  if (beforeY === null || afterY === null) return null;
  return Math.round(afterY - beforeY);
}

/**
 * compactViewText 将目标坐标与可视判断压缩成适合岗位错误日志的一段文字。
 * @param {Record<string, any>|null} view - 元素可视状态。
 * @returns {string} 压缩后的诊断文字。
 */
function compactViewText(view) {
  if (!view?.box) return "无法读取";
  const decision = listClickViewDecision(view);
  const box = view.box;
  return (
    `x=${Math.round(box.x)},y=${Math.round(box.y)},w=${Math.round(box.width)},h=${Math.round(box.height)}` +
    `,in=${Boolean(view.in_viewport)},full=${Boolean(view.fully_visible)}` +
    `,verticalFull=${Boolean(view.vertically_fully_visible)},horizontalVisible=${Boolean(view.horizontally_visible)}` +
    `,失败维度=${decision.failed_dimensions.join("|") || "无"}`
  );
}

/**
 * listClickDiagnosisText 返回诊断代码对应的中文结论。
 * @param {string} code - 诊断代码。
 * @returns {string} 中文诊断结论。
 */
export function listClickDiagnosisText(code) {
  const messages = {
    "horizontal-margin-blocked":
      "目标垂直方向已经完整可见，但左或右边缘未满足横向安全边距；继续上下滚动无法解决",
    "direction-oscillation":
      "滚动方向反复切换，目标在上下边界之间来回越界",
    "wheel-not-effective":
      "滚轮指令已发出，但命中的滚动对象基本没有产生实际位移",
    "target-not-measurable":
      "目标元素无法读取有效坐标，可能是选择器失效或元素已被页面替换",
    unclassified: "现有数据仍不能唯一判断，请结合逐轮 Worker 日志查看",
  };
  return messages[code] || messages.unclassified;
}

/**
 * buildListClickScrollFailureDiagnostic 汇总列表项点击前的全部滚动轨迹并生成详细失败文案。
 * @param {Record<string, any>} input - 平台、候选人、选择器、视口和逐轮滚动轨迹。
 * @returns {Record<string, any>} 结构化诊断结果。
 */
export function buildListClickScrollFailureDiagnostic(input = {}) {
  const attempts = Array.isArray(input.attempts) ? input.attempts : [];
  const initialView =
    input.initial_view || attempts.find((item) => item?.before_view)?.before_view || null;
  const finalView =
    input.final_view ||
    [...attempts].reverse().find((item) => item?.after_view)?.after_view ||
    null;
  const views = [
    initialView,
    ...attempts.map((item) => item?.after_view),
    finalView,
  ].filter(Boolean);
  const decisions = views.map(listClickViewDecision);
  const horizontalMarginBlocked = decisions.some(
    (decision) =>
      decision.vertical_ready &&
      !decision.horizontal_ready &&
      decision.failed_dimensions.includes("horizontal-margin"),
  );
  const directionChanges = directionSwitchCount(attempts);
  const requestedDistances = attempts
    .map((item) => finiteNumber(item?.distance))
    .filter((value) => value !== null);
  const actualDeltas = attempts
    .map((item) => scrollPositionDelta(item?.scroll_before, item?.scroll_after))
    .filter((value) => value !== null);
  const ineffectiveAttempts = actualDeltas.filter(
    (value) => Math.abs(value) <= 2,
  ).length;
  const yTrace = views
    .map((view) => finiteNumber(view?.box?.y))
    .filter((value) => value !== null)
    .map((value) => Math.round(value));
  const uniqueY = [...new Set(yTrace)];
  const repeatedPositionHits = Math.max(0, yTrace.length - uniqueY.length);
  const wheelTargets = [
    ...new Set(
      attempts
        .map(
          (item) =>
            item?.mouse?.wheel_target ||
            item?.wheel_target ||
            item?.scroll_before?.target ||
            item?.scroll_after?.target ||
            "",
        )
        .filter(Boolean),
    ),
  ];

  let diagnosisCode = "unclassified";
  if (!finalView?.box || ["not-visible", "no-box"].includes(finalView?.reason)) {
    diagnosisCode = "target-not-measurable";
  } else if (horizontalMarginBlocked) {
    diagnosisCode = "horizontal-margin-blocked";
  } else if (directionChanges >= 2) {
    diagnosisCode = "direction-oscillation";
  } else if (
    actualDeltas.length >= 2 &&
    ineffectiveAttempts >= Math.ceil(actualDeltas.length * 0.6)
  ) {
    diagnosisCode = "wheel-not-effective";
  }

  const viewport = input.viewport || finalView?.viewport || initialView?.viewport || {};
  const viewportWidth = finiteNumber(viewport.width) || 0;
  const viewportHeight = finiteNumber(viewport.height) || 0;
  const platformName = String(input.platform_name || input.platform || "招聘平台");
  const candidateName = String(input.candidate_name || "未知候选人");
  const action = String(input.action || "点击列表项");
  const margin = Math.max(
    0,
    finiteNumber(finalView?.margin) ||
      finiteNumber(initialView?.margin) ||
      finiteNumber(input.margin) ||
      0,
  );
  const diagnosis = listClickDiagnosisText(diagnosisCode);
  const requestedTotal = requestedDistances.reduce(
    (sum, value) => sum + Math.abs(value),
    0,
  );
  const actualTotal = actualDeltas.reduce(
    (sum, value) => sum + Math.abs(value),
    0,
  );
  const message =
    `${platformName}列表项无法滚动到可点击范围：操作=${action}，候选人=${candidateName}` +
    `，目标序号=${Number(input.index || 0) + 1}，DOM列表项=${Number(input.locator_count || 0)}` +
    `，项目选择器=${String(input.item_selector || "未记录")}` +
    `，点击选择器=${String(input.click_selector || "使用列表项本身")}` +
    `，视口=${viewportWidth}x${viewportHeight}，安全边距=${margin}px` +
    `，要求完全可见=${input.require_full === false ? "否" : "是"}，仅判断纵向=${input.vertical_only ? "是" : "否"}` +
    `，尝试=${attempts.length}次，指令累计=${Math.round(requestedTotal)}px` +
    `，实际滚动=${actualDeltas.length ? `${Math.round(actualTotal)}px` : "无法读取"}` +
    `，无效滚动=${ineffectiveAttempts}次，方向切换=${directionChanges}次，重复位置=${repeatedPositionHits}次` +
    `，目标Y轨迹=${uniqueY.join("↔") || "无法读取"}，滚轮落点=${wheelTargets.join("|") || "无法读取"}` +
    `，初始检测=${compactViewText(initialView)}，最终检测=${compactViewText(finalView)}` +
    `，初步判断=${diagnosis}。逐轮坐标、失败维度和滚动对象请查看 browser-worker.log`;

  return {
    action_id: String(input.action_id || ""),
    platform: String(input.platform || ""),
    platform_name: platformName,
    action,
    candidate_name: candidateName,
    index: Number(input.index || 0),
    locator_count: Number(input.locator_count || 0),
    viewport: `${viewportWidth}x${viewportHeight}`,
    margin,
    require_full: input.require_full !== false,
    vertical_only: Boolean(input.vertical_only),
    wheel_attempts: attempts.length,
    requested_total_px: Math.round(requestedTotal),
    actual_total_px: actualDeltas.length ? Math.round(actualTotal) : null,
    ineffective_attempts: ineffectiveAttempts,
    direction_changes: directionChanges,
    repeated_position_hits: repeatedPositionHits,
    y_trace: yTrace,
    unique_y: uniqueY,
    wheel_targets: wheelTargets,
    initial_view: initialView,
    final_view: finalView,
    initial_decision: listClickViewDecision(initialView),
    final_decision: listClickViewDecision(finalView),
    diagnosis_code: diagnosisCode,
    diagnosis,
    message,
  };
}
