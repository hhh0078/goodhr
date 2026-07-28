// 文件作用说明：提供 Browser Worker 统一错误类型、错误码和未知异常规范化能力。

import type { ErrorBody, JsonObject, JsonValue } from "../contracts/common.js";

/** WorkerErrorCode 表示 Go 可以稳定判断的 Worker 错误码。 */
export type WorkerErrorCode =
  | "INVALID_REQUEST"
  | "BROWSER_NOT_RUNNING"
  | "PAGE_NOT_AVAILABLE"
  | "PAGE_CLOSED"
  | "ELEMENT_NOT_FOUND"
  | "ELEMENT_AMBIGUOUS"
  | "ELEMENT_NOT_VISIBLE"
  | "ELEMENT_NOT_ENABLED"
  | "ELEMENT_REF_EXPIRED"
  | "MOVE_FAILED"
  | "CLICK_FAILED"
  | "INPUT_FAILED"
  | "SCROLL_FAILED"
  | "SCREENSHOT_FAILED"
  | "DOWNLOAD_FAILED"
  | "ACTION_TIMEOUT"
  | "ACTION_CANCELLED"
  | "INTERNAL_ERROR";

/** WorkerError 表示 Worker 内部统一抛出和返回的错误。 */
export class WorkerError extends Error {
  readonly code: WorkerErrorCode;
  readonly action: string;
  readonly step: string;
  readonly trace_id: string;
  readonly retryable: boolean;
  readonly details: JsonObject;

  /** 创建一个带稳定错误码和追踪信息的 Worker 错误。 */
  constructor(options: {
    code: WorkerErrorCode;
    message: string;
    action: string;
    step: string;
    trace_id: string;
    retryable?: boolean;
    details?: JsonObject;
    cause?: unknown;
  }) {
    super(options.message, { cause: options.cause });
    this.name = "WorkerError";
    this.code = options.code;
    this.action = options.action;
    this.step = options.step;
    this.trace_id = options.trace_id;
    this.retryable = options.retryable ?? false;
    this.details = options.details ?? {};
  }

  /** toBody 返回可以安全传给 Go 的结构化错误。 */
  toBody(): ErrorBody {
    return {
      code: this.code,
      message: this.message,
      action: this.action,
      step: this.step,
      trace_id: this.trace_id,
      retryable: this.retryable,
      details: this.details,
    };
  }
}

/** normalizeWorkerError 将未知异常统一转换为 WorkerError。 */
export function normalizeWorkerError(
  error: unknown,
  fallback: {
    action: string;
    step: string;
    trace_id: string;
    code?: WorkerErrorCode;
    message?: string;
    retryable?: boolean;
    details?: JsonObject;
  },
): WorkerError {
  if (error instanceof WorkerError) {
    return error;
  }
  const originalMessage =
    error instanceof Error ? error.message : String(error || "");
  const details: JsonObject = { ...(fallback.details ?? {}) };
  if (originalMessage) {
    details.original_error = originalMessage.slice(0, 1000);
  }
  return new WorkerError({
    code: fallback.code ?? "INTERNAL_ERROR",
    message: fallback.message ?? "浏览器操作没处理成功，但问题不大，我们再看看",
    action: fallback.action,
    step: fallback.step,
    trace_id: fallback.trace_id,
    retryable: fallback.retryable ?? false,
    details,
    cause: error,
  });
}

/** safeJsonValue 将未知诊断值转换为可安全记录的 JSON 值。 */
export function safeJsonValue(value: unknown): JsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => safeJsonValue(item));
  }
  if (typeof value === "object") {
    const result: JsonObject = {};
    for (const [key, item] of Object.entries(value)) {
      result[key] = safeJsonValue(item);
    }
    return result;
  }
  return String(value);
}
