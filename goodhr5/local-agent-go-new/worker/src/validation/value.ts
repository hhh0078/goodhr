// 文件作用说明：提供 Worker HTTP 边界读取 unknown JSON 时使用的最小运行时校验工具。

import { WorkerError } from "../errors/worker-error.js";

/** asRecord 确保未知值是普通 JSON 对象。 */
export function asRecord(
  value: unknown,
  traceId: string,
  action: string,
  field = "body",
): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw invalidRequest(traceId, action, `${field} 必须是对象`);
  }
  return value as Record<string, unknown>;
}

/** requiredString 读取必填非空字符串。 */
export function requiredString(
  record: Record<string, unknown>,
  key: string,
  traceId: string,
  action: string,
): string {
  const value = record[key];
  if (typeof value !== "string" || value.trim() === "") {
    throw invalidRequest(traceId, action, `${key} 不能为空`);
  }
  return value.trim();
}

/** optionalString 读取可选字符串。 */
export function optionalString(
  record: Record<string, unknown>,
  key: string,
): string | undefined {
  const value = record[key];
  return typeof value === "string" && value.trim() !== ""
    ? value.trim()
    : undefined;
}

/** optionalBoolean 读取可选布尔值。 */
export function optionalBoolean(
  record: Record<string, unknown>,
  key: string,
): boolean | undefined {
  return typeof record[key] === "boolean"
    ? (record[key] as boolean)
    : undefined;
}

/** optionalNumber 读取可选有限数字并限制范围。 */
export function optionalNumber(
  record: Record<string, unknown>,
  key: string,
  options: { min?: number; max?: number } = {},
): number | undefined {
  const value = record[key];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return undefined;
  }
  if (options.min !== undefined && value < options.min) {
    return options.min;
  }
  if (options.max !== undefined && value > options.max) {
    return options.max;
  }
  return value;
}

/** stringArray 读取字符串数组并移除空值。 */
export function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter(Boolean);
}

/** invalidRequest 创建统一的请求校验错误。 */
export function invalidRequest(
  traceId: string,
  action: string,
  message: string,
): WorkerError {
  return new WorkerError({
    code: "INVALID_REQUEST",
    message,
    action,
    step: "validate_request",
    trace_id: traceId,
    retryable: false,
  });
}
