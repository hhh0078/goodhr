// 文件作用说明：提供 Browser Worker 统一结构化日志并过滤 Cookie、Token 和代理密码等敏感字段。

import type { ActionContext, JsonObject, JsonValue } from "../contracts/common.js";
import {
  safeJsonValue,
  type WorkerError,
} from "../errors/worker-error.js";

const sensitiveKeyPattern =
  /cookie|authorization|token|password|proxy.*(user|pass)|secret/i;

/** sanitizeFields 清理日志中的敏感字段和过长文本。 */
function sanitizeFields(fields: JsonObject): JsonObject {
  const result: JsonObject = {};
  for (const [key, value] of Object.entries(fields)) {
    if (sensitiveKeyPattern.test(key)) {
      result[key] = "[已隐藏]";
      continue;
    }
    result[key] = sanitizeValue(value);
  }
  return result;
}

/** sanitizeValue 递归清理日志字段并限制单个字符串长度。 */
function sanitizeValue(value: JsonValue): JsonValue {
  if (typeof value === "string") {
    return value.length > 1000 ? `${value.slice(0, 1000)}…` : value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeValue(item));
  }
  if (value && typeof value === "object") {
    return sanitizeFields(value);
  }
  return value;
}

/** WorkerLogger 输出统一 JSON 行日志。 */
export class WorkerLogger {
  private readonly recentFailures = new Map<string, number>();

  /** info 输出普通操作日志。 */
  info(
    context: ActionContext,
    step: string,
    status: string,
    fields: JsonObject = {},
  ): void {
    this.write("info", context, step, status, fields);
  }

  /** warn 输出可恢复问题日志。 */
  warn(
    context: ActionContext,
    step: string,
    status: string,
    fields: JsonObject = {},
  ): void {
    this.write("warning", context, step, status, fields);
  }

  /** error 输出操作失败日志。 */
  error(
    context: ActionContext,
    step: string,
    status: string,
    fields: JsonObject = {},
  ): void {
    this.write("error", context, step, status, fields);
  }

  /** failure 输出已经规范化的浏览器错误和中文说明。 */
  failure(context: ActionContext, error: WorkerError): void {
    const key = `${context.trace_id}|${context.action}|${error.step}|${error.code}|${error.message}`;
    const now = Date.now();
    const previous = this.recentFailures.get(key) ?? 0;
    if (now - previous < 500) {
      return;
    }
    this.recentFailures.set(key, now);
    if (this.recentFailures.size > 200) {
      for (const [item, timestamp] of this.recentFailures) {
        if (now - timestamp > 10_000) {
          this.recentFailures.delete(item);
        }
      }
    }
    this.write("error", context, error.step, "failed", {
      ...error.details,
      error_code: error.code,
      error_message: error.message,
    });
  }

  /** fromUnknown 把未知日志字段转换为安全 JSON 对象。 */
  fromUnknown(value: unknown): JsonObject {
    const safe = safeJsonValue(value);
    return safe && typeof safe === "object" && !Array.isArray(safe)
      ? safe
      : { value: safe };
  }

  /** write 写出一行结构化日志。 */
  private write(
    level: string,
    context: ActionContext,
    step: string,
    status: string,
    fields: JsonObject,
  ): void {
    const payload = sanitizeFields({
      timestamp: new Date().toISOString(),
      level,
      trace_id: context.trace_id,
      action: context.action,
      step,
      status,
      duration_ms: Date.now() - context.started_at,
      ...fields,
    });
    process.stdout.write(`${JSON.stringify(payload)}\n`);
  }
}
