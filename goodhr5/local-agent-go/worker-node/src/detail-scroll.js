/** 本文件负责统一所有招聘平台详情长图的滚动等待节奏。 */

const defaultDetailCaptureWaitMs = 250;
const minimumDetailScrollWaitMs = 120;

/** positiveWaitMilliseconds 将等待参数转换为带下限的有效毫秒数。 */
function positiveWaitMilliseconds(value, fallback) {
  const parsed = Number(value);
  return Math.max(
    minimumDetailScrollWaitMs,
    Number.isFinite(parsed) && parsed > 0 ? parsed : fallback,
  );
}

/** detailScrollWaits 返回全平台详情长图本轮截图和滚动后的等待时长。 */
export function detailScrollWaits(payload = {}) {
  const captureWaitMs = positiveWaitMilliseconds(
    payload.detail_capture_wait_ms,
    defaultDetailCaptureWaitMs,
  );
  return {
    captureWaitMs,
    initialCaptureWaitMs: Math.min(600, captureWaitMs),
  };
}

/** effectiveDetailWheelDistance 根据详情容器剩余空间选择能产生位移的滚轮方向。 */
export function effectiveDetailWheelDistance(distance, state = {}) {
  const value = Number(distance || 0);
  if (value > 0 && !state.can_scroll_down && state.can_scroll_up) {
    return -Math.abs(value);
  }
  if (value < 0 && !state.can_scroll_up && state.can_scroll_down) {
    return Math.abs(value);
  }
  return value;
}
