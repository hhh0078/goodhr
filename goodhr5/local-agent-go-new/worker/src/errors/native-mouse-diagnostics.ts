// 文件作用说明：从浏览器原生鼠标错误中提取白名单诊断字段，保留刷新、回执与窗口状态供服务器排障。

import type { JsonObject, JsonValue } from "../contracts/common.js";

const diagnosticMarker = " GOODHR_MOUSE_DIAGNOSTICS=";
const numberFields = ["schema_version", "target_x", "target_y", "buttons", "apz_wait_ms", "mouse_ack_wait_ms", "events_sent", "events_acknowledged", "elapsed_ms", "duplicate_points_skipped", "last_event_id", "activation_wait_ms", "input_timeout_ms", "delta_x", "delta_y", "window_ready_wait_ms", "window_restore_attempts"];
const booleanFields = ["apz_flush_requested", "apz_flush_result", "apz_repaint_received", "recovery_required"];
const stringFields = ["mouse_safety_version", "stage", "event_types", "last_event", "window_recovery_error_type"];

/** nativeMouseDiagnostics 提取有界的原生鼠标证据，不接受页面内容或任意附加字段。 */
export function nativeMouseDiagnostics(error: unknown): JsonObject | undefined {
  const message = currentErrorMessage(error);
  const code = message.match(/\b(GOODHR_MOUSE_(?:(?:APZ|ACK|INPUT)_TIMEOUT|WINDOW_NOT_READY)):([a-z,]+)/);
  const marker = message.indexOf(diagnosticMarker);
  if (!code && marker < 0) return undefined;
  const result: JsonObject = {
    native_error_code: code?.[1] ?? "unknown",
    event_types: code?.[2] ?? "unknown",
    stage: code?.[1] === "GOODHR_MOUSE_WINDOW_NOT_READY" ? "window_ready" : code?.[1] === "GOODHR_MOUSE_APZ_TIMEOUT" ? "apz_repaint" : code?.[1] === "GOODHR_MOUSE_INPUT_TIMEOUT" ? "activation" : "mouse_ack",
    diagnostic_available: false,
  };
  if (marker < 0) return result;
  try {
    const encoded = message.slice(marker + diagnosticMarker.length).split("\n", 1)[0] ?? "";
    if (encoded.length > 8000) return result;
    const value: unknown = JSON.parse(encoded);
    if (!isObject(value) || value.schema_version !== 1) return result;
    for (const key of numberFields) {
      const field = value[key];
      if (typeof field === "number" && Number.isFinite(field)) result[key] = field;
    }
    for (const key of booleanFields) {
      const field = value[key];
      if (typeof field === "boolean" || field === null) result[key] = field;
    }
    for (const key of stringFields) {
      const field = value[key];
      if (typeof field === "string") result[key] = field.slice(0, 80);
    }
    for (const key of ["window_before", "window_after_restore", "window_ready_before_input", "window_at_failure"]) {
      if (isObject(value[key])) result[key] = windowFields(value[key]);
    }
    result.diagnostic_available = true;
  } catch { /* 历史补丁或截断错误仍保留稳定超时类型，不能阻断日志上传。 */ }
  return result;
}

/** nativeMouseErrorText 保留原始错误说明，把较长的原生证据放入结构化出参。 */
export function nativeMouseErrorText(error: unknown): string {
  const message = currentErrorMessage(error);
  const marker = message.indexOf(diagnosticMarker);
  return marker < 0 ? message : message.slice(0, marker);
}

/** windowFields 仅保留浏览器内部窗口和显示区域信息，不保留页面正文。 */
function windowFields(value: Record<string, unknown>): JsonObject {
  const result: JsonObject = {};
  for (const key of ["window_state", "window_minimized", "chrome_has_focus", "chrome_document_hidden", "browser_active", "browser_hidden", "tab_selected", "browser_selected", "device_pixel_ratio", "window_state_unavailable", "headless", "headless_state_unavailable"]) {
    const field = value[key];
    if (field === null || typeof field === "boolean" || (typeof field === "number" && Number.isFinite(field))) result[key] = field as JsonValue;
  }
  if (isObject(value.browser_rect)) {
    const rect: JsonObject = {};
    for (const key of ["x", "y", "width", "height"]) {
      const field = value.browser_rect[key];
      if (typeof field === "number" && Number.isFinite(field)) rect[key] = field;
    }
    result.browser_rect = rect;
  }
  return result;
}

/** currentErrorMessage 排除浏览器关闭错误附带的历史日志，避免把旧鼠标故障误认成本次根因。 */
function currentErrorMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error ?? "");
  return message.split(/\r?\n(?:Browser logs:|Call log:)/, 1)[0] ?? "";
}

/** isObject 排除空值和数组，避免损坏的诊断数据影响原始错误。 */
function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
