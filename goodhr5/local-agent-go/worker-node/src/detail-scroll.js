/** 本文件负责统一所有招聘平台详情长图的滚动等待节奏。 */

const defaultDetailCaptureWaitMs = 250;
const defaultDetailScrollSettleMs = 450;
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
    scrollSettleMs: positiveWaitMilliseconds(
      payload.detail_scroll_settle_ms,
      defaultDetailScrollSettleMs,
    ),
  };
}
