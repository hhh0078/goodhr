// 文件作用说明：提供 Browser Worker 统一结构化日志并过滤 Cookie、Token 和代理密码等敏感字段。

import type { ActionContext, JsonObject, JsonValue } from "../contracts/common.js";
import { writeSync } from "node:fs";
import { exitDiagnostics, rememberError, rememberOperation } from "./exit-diagnostics.js";
import {
  safeJsonValue,
  type WorkerError,
} from "../errors/worker-error.js";

const sensitiveKeyPattern =
  /cookie|authorization|token|password|proxy.*(user|pass)|secret|api[_-]?key|message[_-]?text|input[_-]?text|candidate[_-]?name|resume[_-]?text|phone|wechat|email|id[_-]?card|address/i;

const secretTextPattern = /(?:Bearer\s+\S+|(?:token|password|secret|cookie|api[_-]?key)\s*[=:]\s*\S+)/gi;
const urlQueryPattern = /(https?:\/\/[^\s?"<>]+)\?[^\s"<>]*/gi;
const phoneTextPattern = /\b1[3-9][0-9]{9}\b/g;
const emailTextPattern = /[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g;

/** sanitizeFields 清理日志中的敏感字段和过长文本。 */
function sanitizeFields(fields: JsonObject): JsonObject {
  const result: JsonObject = {};
  for (const [key, value] of Object.entries(fields)) {
    if (sensitiveKeyPattern.test(key)) {
      result[key] = "[已隐藏]";
      continue;
    }
    result[key] = sanitizeValue(value, key === "stack" ? 4000 : 1000);
  }
  return result;
}

/** sanitizeValue 递归清理日志字段并限制单个字符串长度。 */
function sanitizeValue(value: JsonValue, limit = 1000): JsonValue {
  if (typeof value === "string") {
    const cleaned = value.replace(secretTextPattern, "[已隐藏]").replace(urlQueryPattern, "$1?[已隐藏]")
      .replace(phoneTextPattern, "[手机号已隐藏]").replace(emailTextPattern, "[邮箱已隐藏]");
    return cleaned.length > limit ? `${cleaned.slice(0, limit)}…` : cleaned;
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeValue(item, limit));
  }
  if (value && typeof value === "object") {
    return sanitizeFields(value);
  }
  return value;
}

/** WorkerLogger 输出统一 JSON 行日志。 */
export class WorkerLogger {
  private readonly recentFailures = new Map<string, number>();

  /** 创建日志器；关键退出日志同步写入，测试时可以替换输出入口。 */
  constructor(private readonly writeLine: (line: string, critical: boolean) => void = emitLine) {}

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
      stack: error.stack ?? "",
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
    const isError = level === "error" || status === "failed" || status === "panic";
    const errorMessage = typeof fields.error_message === "string"
      ? fields.error_message
      : isError && typeof fields.error === "string" ? fields.error : "";
    const input: JsonObject = fields.input && typeof fields.input === "object" && !Array.isArray(fields.input)
      ? { ...(fields.input as JsonObject) }
      : {};
    const output: JsonObject = fields.output && typeof fields.output === "object" && !Array.isArray(fields.output)
      ? { ...(fields.output as JsonObject) }
      : { status, duration_ms: Date.now() - context.started_at };
    for (const key of ["target_description", "candidates", "parent_levels", "frame_levels", "timeout_ms", "max_items"] as const) {
      if (fields[key] !== undefined && input[key] === undefined) input[key] = fields[key];
    }
    for (const key of ["matches_checked", "matched_selector", "count", "verified", "error_code", "poll_attempts", "native_mouse", "step_duration_ms"] as const) {
      if (fields[key] !== undefined && output[key] === undefined) output[key] = fields[key];
    }
    const critical = /^(worker\.crash|worker\.exit|browser\.exit|browser\.crash)$/.test(context.action);
    const payload = sanitizeFields({
      ...fields,
      ...(critical ? { diagnostics: exitDiagnostics() } : {}),
      timestamp: new Date().toISOString(),
      category: fields.category === "异常退出" ? "异常退出" : context.action.startsWith("worker.") ? "worker" : "browser",
      is_error: isError,
      error_message: errorMessage,
      input,
      output,
      level,
      trace_id: context.trace_id,
      request_id: context.request_id ?? "",
      action: context.action,
      step,
      status,
      duration_ms: Date.now() - context.started_at,
    });
    rememberOperation(payload);
    if (context.action === "worker.crash" || context.action === "browser.crash") rememberError(payload);
    if (critical && Buffer.byteLength(JSON.stringify(payload)) > 15_000) {
      payload.diagnostics = { ...exitDiagnostics(), recent_operations: [], last_error: {} };
      payload.input = {};
      payload.diagnostic_truncated = true;
    }
    this.writeLine(`${JSON.stringify(payload)}\n`, critical);
  }
}

/** emitLine 在致命退出前同步落盘管道，普通日志仍使用标准输出缓冲。 */
function emitLine(line: string, critical: boolean): void {
  if (!critical) { process.stdout.write(line); return; }
  try { writeSync(1, line); }
  catch { try { writeSync(2, line); } catch { /* 上级进程已关闭管道时不能再次触发崩溃。 */ } }
}
