// 文件作用：读取 Go 在 Worker 启动前检查的原生输入资源信息，只保留诊断白名单，不声称磁盘版本就是运行中版本。
import type { JsonObject } from "../../contracts/common.js";

/** nativeInputSafetyDiagnostic 返回启动前磁盘检查结果，缺失或损坏时明确标记未确认。 */
export function nativeInputSafetyDiagnostic(): JsonObject {
  const result: JsonObject = { source: "disk_checked_before_worker_start", runtime_loaded_version: "unconfirmed", ready: false };
  try {
    const value: unknown = JSON.parse(process.env.GOODHR_NATIVE_INPUT_SAFETY ?? "{}");
    if (!value || typeof value !== "object" || Array.isArray(value)) return result;
    const data = value as Record<string, unknown>;
    result.ready = data.ready === true;
    for (const key of ["expected_version", "installed_version", "resource_sha256", "archive_path", "error"] as const) {
      if (typeof data[key] === "string") result[key] = data[key].slice(0, 500);
    }
  } catch { result.error = "输入保护检查结果无法解析"; }
  return result;
}
