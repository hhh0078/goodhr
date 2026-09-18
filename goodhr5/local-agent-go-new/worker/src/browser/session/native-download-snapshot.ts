// 文件作用说明：校验浏览器原生下载快照，将外部 JSON 收敛成强类型本地下载记录。

import { createHash } from "node:crypto";
import path from "node:path";
import type { DownloadRecord } from "../../contracts/actions.js";

/** NativeDownloadSnapshot 表示当前浏览器确认过的目录和原生下载状态。 */
export interface NativeDownloadSnapshot {
  session_id: string;
  revision: number;
  directory: string;
  downloads: DownloadRecord[];
}

/** parseNativeDownloadSnapshot 拒绝其他会话、未知格式和无效文件路径。 */
export function parseNativeDownloadSnapshot(value: unknown, sessionID: string): NativeDownloadSnapshot {
  if (!isRecord(value) || value.version !== 1 || value.session_id !== sessionID ||
      typeof value.revision !== "number" || !Number.isSafeInteger(value.revision) || value.revision < 0 ||
      typeof value.directory !== "string" || !path.isAbsolute(value.directory) ||
      !Array.isArray(value.downloads) || value.downloads.length > 100) {
    throw new Error("浏览器原生下载状态格式不正确");
  }
  return {
    session_id: sessionID,
    revision: value.revision,
    directory: value.directory,
    downloads: value.downloads.map(parseNativeDownload),
  };
}

/** parseNativeDownload 根据浏览器实际路径和开始时间生成稳定编号，不移动用户文件。 */
function parseNativeDownload(value: unknown): DownloadRecord {
  if (!isRecord(value) || typeof value.file_path !== "string" || !path.isAbsolute(value.file_path) ||
      typeof value.url !== "string" || typeof value.page_url !== "string" ||
      typeof value.created_at !== "string" || !Number.isFinite(Date.parse(value.created_at)) ||
      typeof value.size !== "number" || !Number.isFinite(value.size) || value.size < 0 ||
      (value.status !== "pending" && value.status !== "saved" && value.status !== "failed") || typeof value.error !== "string") {
    throw new Error("浏览器原生下载记录格式不正确");
  }
  const filename = path.basename(value.file_path);
  const id = `native_download_${createHash("sha256").update(JSON.stringify([value.file_path, value.url, value.created_at])).digest("hex").slice(0, 24)}`;
  return {
    id, filename, file_name: filename, file_path: value.file_path, path: value.file_path,
    suggested_filename: filename, url: value.url, page_url: value.page_url,
    size: value.size, status: value.status, error: value.error, created_at: value.created_at,
  };
}

/** isRecord 在外部边界识别普通 JSON 对象。 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
