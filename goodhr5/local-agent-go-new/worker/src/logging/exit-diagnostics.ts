// 文件作用说明：保存有限的脱敏操作轨迹和原始错误，为进程或浏览器退出补充诊断。

import type { JsonObject } from "../contracts/common.js";

const recentOperations: JsonObject[] = [];
let lastError: JsonObject = {};
const startedAt = new Date().toISOString();

/** rememberOperation 仅保存操作标识和错误摘要，不保存简历、输入正文或页面内容。 */
export function rememberOperation(payload: JsonObject): void {
  if (payload.step === "http" || /^(http\.|worker\.exit|worker\.crash|browser\.exit|browser\.crash)/.test(String(payload.action)) ||
      /^(health|runtime\.status|browser\.status|downloads\.(list|status))$/.test(String(payload.action))) return;
  const item: JsonObject = {};
  for (const key of ["timestamp", "action", "step", "status", "trace_id", "request_id", "duration_ms", "error_code"] as const) {
    if (payload[key] !== undefined) item[key] = payload[key];
  }
  if (payload.error_message) item.error_message = String(payload.error_message).slice(0, 240);
  if (payload.target_description) item.target_description = String(payload.target_description).slice(0, 120);
  recentOperations.push(item);
  if (recentOperations.length > 20) recentOperations.shift();
  if (payload.is_error) rememberError(payload, item);
}

/** rememberError 保留最后一次错误的原始说明和调用栈，退出时不用依赖缓冲区。 */
export function rememberError(payload: JsonObject, operation: JsonObject = {}): void {
  lastError = { ...operation, action: payload.action ?? "", step: payload.step ?? "", timestamp: payload.timestamp ?? "",
    error_message: String(payload.error_message ?? payload.error ?? "").slice(0, 1000),
    original_error: String(payload.original_error ?? payload.error ?? "").slice(0, 1000),
    stack: String(payload.stack ?? "").slice(0, 3000) };
}

/** exitDiagnostics 返回大小受限的进程信息、最近操作和最后错误。 */
export function exitDiagnostics(): JsonObject {
  const memory = process.memoryUsage();
  const recent = recentOperations.slice(-12);
  const result: JsonObject = {
    pid: process.pid, agent_version: process.env.GOODHR_AGENT_VERSION ?? "unknown",
    node_version: process.version, platform: process.platform, arch: process.arch,
    started_at: startedAt, uptime_ms: Math.round(process.uptime() * 1000),
    rss_bytes: memory.rss, heap_used_bytes: memory.heapUsed,
    recent_operations: recent, last_error: lastError,
  };
  while (recent.length && Buffer.byteLength(JSON.stringify(result)) > 8000) recent.shift();
  return result;
}
