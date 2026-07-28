// 文件作用说明：监听浏览器下载、保存文件、补全后缀并维护当前会话下载状态。

import { createHash, randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import type { Download, Page } from "playwright-core";
import type {
  DownloadListResult,
  DownloadRecord,
} from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { safeURL } from "./navigation.js";

/** DownloadManager 管理浏览器会话中的下载目录、任务和最近记录。 */
export class DownloadManager {
  private downloadsPath = path.join(os.homedir(), "Downloads");
  private readonly downloads: DownloadRecord[] = [];
  private readonly pendingDownloads = new Set<Promise<void>>();

  /** 创建下载管理器。 */
  constructor(private readonly logger: WorkerLogger) {}

  /** prepare 创建并切换浏览器启动使用的下载目录。 */
  async prepare(directory?: string): Promise<void> {
    const nextDirectory = path.resolve(
      directory?.trim() || path.join(os.homedir(), "Downloads"),
    );
    await fs.mkdir(nextDirectory, { recursive: true });
    this.downloadsPath = nextDirectory;
  }

  /** directory 返回当前下载目录。 */
  directory(): string {
    return this.downloadsPath;
  }

  /** list 返回当前会话最近下载记录和处理中数量。 */
  list(): DownloadListResult {
    return {
      downloads: [...this.downloads],
      count: this.downloads.length,
      pending: this.pendingDownloads.size,
      directory: this.downloadsPath,
    };
  }

  /** configure 校验并切换后续下载目录。 */
  async configure(
    directory: string,
    actionContext: ActionContext,
  ): Promise<JsonObject> {
    if (!directory.trim()) {
      throw new WorkerError({
        code: "INVALID_REQUEST",
        message: "下载目录不能为空",
        action: actionContext.action,
        step: "configure_downloads",
        trace_id: actionContext.trace_id,
        retryable: false,
      });
    }
    await this.prepare(directory);
    this.logger.info(actionContext, "configure_downloads", "success", {
      directory: this.downloadsPath,
    });
    return { configured: true, directory: this.downloadsPath };
  }

  /** clear 清空内存记录，不删除已经下载的文件。 */
  clear(): JsonObject {
    const cleared = this.downloads.length;
    this.downloads.length = 0;
    return { cleared, files_deleted: false };
  }

  /** reset 在浏览器会话结束时清理下载状态。 */
  reset(): void {
    this.downloads.length = 0;
    this.pendingDownloads.clear();
  }

  /** capture 跟踪一次页面下载并异步保存文件。 */
  capture(download: Download, page: Page): void {
    const task = this.save(download, page).finally(() => {
      this.pendingDownloads.delete(task);
    });
    this.pendingDownloads.add(task);
  }

  /** save 把浏览器下载保存到配置目录并写入成功或失败状态。 */
  private async save(download: Download, page: Page): Promise<void> {
    const startedAt = Date.now();
    const downloadURL = download.url();
    const suggestedFilename = safeFilename(download.suggestedFilename());
    const record: DownloadRecord = {
      id: randomUUID(),
      filename: suggestedFilename,
      file_name: suggestedFilename,
      file_path: "",
      path: "",
      suggested_filename: suggestedFilename,
      url: downloadURL,
      page_url: page.isClosed() ? "" : page.url(),
      size: 0,
      status: "pending",
      error: "",
      created_at: new Date().toISOString(),
    };
    this.downloads.unshift(record);
    this.trim();
    const actionContext: ActionContext = {
      trace_id: record.id,
      action: "downloads.capture",
      started_at: startedAt,
    };
    this.logger.info(actionContext, "capture_download", "start", {
      page_url: safeURL(record.page_url),
      download_url: safeURL(downloadURL),
      suggested_filename: suggestedFilename,
      directory: this.downloadsPath,
    });
    try {
      await fs.mkdir(this.downloadsPath, { recursive: true });
      const filename = filenameWithExtension(suggestedFilename, downloadURL);
      const filePath = await uniquePath(this.downloadsPath, filename);
      await download.saveAs(filePath);
      const failure = await download.failure();
      if (failure) {
        throw new Error(failure);
      }
      const savedPath = await ensureDownloadExtension(filePath);
      const stat = await fs.stat(savedPath);
      record.id = downloadID(savedPath, downloadURL);
      record.filename = path.basename(savedPath);
      record.file_name = record.filename;
      record.file_path = savedPath;
      record.path = savedPath;
      record.size = stat.size;
      record.status = "saved";
      this.logger.info(actionContext, "save_download", "success", {
        filename: record.filename,
        size: record.size,
        duration_ms: Date.now() - startedAt,
      });
    } catch (error) {
      record.status = "failed";
      record.error = error instanceof Error ? error.message : String(error);
      this.logger.error(actionContext, "save_download", "failed", {
        error_code: "DOWNLOAD_FAILED",
        message: record.error,
        duration_ms: Date.now() - startedAt,
      });
    }
  }

  /** trim 把下载记录限制在最近 100 条。 */
  private trim(): void {
    if (this.downloads.length > 100) {
      this.downloads.length = 100;
    }
  }
}

/** safeFilename 清理浏览器建议文件名中的危险字符。 */
function safeFilename(rawName: string): string {
  const filename = path
    .basename(rawName || "download")
    .replace(/[<>:"/\\|?*\x00-\x1F]/g, "_")
    .trim();
  return filename || "download";
}

/** uniquePath 为下载生成不覆盖已有文件的保存路径。 */
async function uniquePath(directory: string, filename: string): Promise<string> {
  const parsed = path.parse(filename);
  for (let index = 0; index < 1_000; index += 1) {
    const suffix = index === 0 ? "" : `-${index}`;
    const candidate = path.join(
      directory,
      `${parsed.name || "download"}${suffix}${parsed.ext}`,
    );
    try {
      await fs.access(candidate);
    } catch {
      return candidate;
    }
  }
  return path.join(directory, `${Date.now()}-${filename}`);
}

/** filenameWithExtension 优先从下载 URL 给无后缀文件补充可信后缀。 */
function filenameWithExtension(filename: string, rawURL: string): string {
  const safe = safeFilename(filename);
  if (path.extname(safe)) {
    return safe;
  }
  try {
    const extension = path.extname(new URL(rawURL).pathname).toLowerCase();
    return /^\.[a-z0-9]{1,8}$/.test(extension) ? `${safe}${extension}` : safe;
  } catch {
    return safe;
  }
}

/** ensureDownloadExtension 根据文件头给仍无后缀的下载文件补充常见格式。 */
async function ensureDownloadExtension(filePath: string): Promise<string> {
  if (path.extname(filePath)) {
    return filePath;
  }
  const handle = await fs.open(filePath, "r").catch(() => null);
  if (!handle) {
    return filePath;
  }
  try {
    const buffer = Buffer.alloc(65_536);
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
    const extension = extensionFromBuffer(buffer.subarray(0, bytesRead));
    if (!extension) {
      return filePath;
    }
    const nextPath = await uniquePath(
      path.dirname(filePath),
      `${path.basename(filePath)}${extension}`,
    );
    await fs.rename(filePath, nextPath);
    return nextPath;
  } finally {
    await handle.close().catch(() => undefined);
  }
}

/** extensionFromBuffer 根据常见文件签名识别下载格式。 */
function extensionFromBuffer(buffer: Buffer): string {
  if (buffer.length >= 4 && buffer.subarray(0, 4).toString("latin1") === "%PDF") {
    return ".pdf";
  }
  if (
    buffer.length >= 8 &&
    buffer.subarray(0, 8).equals(
      Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    )
  ) {
    return ".png";
  }
  if (buffer.length >= 3 && buffer[0] === 0xff && buffer[1] === 0xd8 && buffer[2] === 0xff) {
    return ".jpg";
  }
  if (buffer.length >= 6 && /^GIF8[79]a$/.test(buffer.subarray(0, 6).toString("latin1"))) {
    return ".gif";
  }
  if (
    buffer.length >= 8 &&
    buffer.subarray(0, 8).equals(
      Buffer.from([0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1]),
    )
  ) {
    return ".doc";
  }
  if (buffer.length >= 6 && buffer.subarray(0, 6).toString("latin1") === "Rar!\x1a\x07") {
    return ".rar";
  }
  if (
    buffer.length >= 6 &&
    buffer.subarray(0, 6).equals(Buffer.from([0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c]))
  ) {
    return ".7z";
  }
  if (buffer.length >= 2 && buffer[0] === 0x1f && buffer[1] === 0x8b) {
    return ".gz";
  }
  if (buffer.length >= 4 && buffer[0] === 0x50 && buffer[1] === 0x4b) {
    const text = buffer.toString("latin1");
    if (text.includes("word/")) return ".docx";
    if (text.includes("xl/")) return ".xlsx";
    if (text.includes("ppt/")) return ".pptx";
    return ".zip";
  }
  return "";
}

/** downloadID 根据最终路径和来源地址生成短稳定编号。 */
function downloadID(filePath: string, rawURL: string): string {
  return `download_${createHash("sha1")
    .update(`${filePath}|${rawURL}`)
    .digest("hex")
    .slice(0, 16)}`;
}
