// 文件作用说明：监听浏览器下载、保存文件、补全后缀并维护当前会话下载状态。

import { createHash, randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import type { FileHandle } from "node:fs/promises";
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

  /** list 返回 Worker 最近下载记录和处理中数量。 */
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

  /** waitForPending 等待已经监听到的下载全部进入成功或失败终态。 */
  async waitForPending(): Promise<void> {
    while (this.pendingDownloads.size > 0) {
      await Promise.allSettled([...this.pendingDownloads]);
    }
  }

  /** reset 在浏览器会话结束时清理已完成的异步任务引用，记录留给 Go 做最后同步。 */
  reset(): void {
    this.pendingDownloads.clear();
  }

  /** capture 跟踪一次页面下载并异步保存文件。 */
  capture(download: Download, page: Page): void {
    const task = this.save(download, page).finally(() => {
      this.pendingDownloads.delete(task);
    });
    this.pendingDownloads.add(task);
  }

  /** saveFetchedDocument 保存浏览器会话主动读取到的文档，并写入统一下载记录。 */
  async saveFetchedDocument(
    content: AsyncIterable<Uint8Array>,
    maximumBytes: number,
    requiredPrefix: Buffer | undefined,
    sourceURL: string,
    pageURL: string,
    suggestedFilename: string,
    actionContext: ActionContext,
    deadlineAt?: number,
  ): Promise<DownloadRecord> {
    const startedAt = Date.now();
    const record = this.createRecord(suggestedFilename, sourceURL, pageURL);
    const temporaryPath = path.join(
      this.downloadsPath,
      `.goodhr-${randomUUID()}.part`,
    );
    let handle: FileHandle | null = null;
    let savedPath = "";
    let totalBytes = 0;
    let header = Buffer.alloc(0);
    this.logger.info(actionContext, "save_fetched_document", "start", {
      page_url: safeURL(pageURL),
      document_url: safeURL(sourceURL),
      suggested_filename: record.suggested_filename,
      directory: this.downloadsPath,
      max_bytes: maximumBytes,
    });
    try {
      await fs.mkdir(this.downloadsPath, { recursive: true });
      handle = await fs.open(temporaryPath, "wx");
      for await (const value of content) {
        assertDocumentDeadline(deadlineAt);
        const chunk = Buffer.isBuffer(value) ? value : Buffer.from(value);
        if (totalBytes+chunk.length > maximumBytes) {
          throw new Error(`文档实际大小超过 ${maximumBytes} 字节`);
        }
        totalBytes += chunk.length;
        if (header.length < 65_536) {
          header = Buffer.concat([
            header,
            chunk.subarray(0, Math.max(0, 65_536-header.length)),
          ]);
        }
        await writeAll(handle, chunk, deadlineAt);
      }
      assertDocumentDeadline(deadlineAt);
      await handle.close();
      handle = null;
      if (totalBytes === 0) {
        throw new Error("文档页面返回了空文件");
      }
      if (requiredPrefix && !header.subarray(0, requiredPrefix.length).equals(requiredPrefix)) {
        throw new Error("文档内容签名没有通过校验");
      }
      let filename = filenameWithExtension(record.suggested_filename, sourceURL);
      if (!path.extname(filename)) {
        const extension = extensionFromBuffer(header);
        if (extension) {
          filename += extension;
        }
      }
      savedPath = await uniquePath(this.downloadsPath, filename);
      await fs.rename(temporaryPath, savedPath);
      await this.finishRecord(record, savedPath, sourceURL);
      this.logger.info(actionContext, "save_fetched_document", "success", {
        filename: record.filename,
        size: record.size,
        duration_ms: Date.now() - startedAt,
      });
      return record;
    } catch (error) {
      await handle?.close().catch(() => undefined);
      await fs.unlink(temporaryPath).catch(() => undefined);
      if (savedPath) {
        await fs.unlink(savedPath).catch(() => undefined);
      }
      this.failRecord(record, error);
      this.logger.error(actionContext, "save_fetched_document", "failed", {
        error_code: "DOWNLOAD_FAILED",
        message: record.error,
        duration_ms: Date.now() - startedAt,
      });
      throw error;
    }
  }

  /** save 把浏览器下载保存到配置目录并写入成功或失败状态。 */
  private async save(download: Download, page: Page): Promise<void> {
    const startedAt = Date.now();
    const downloadURL = download.url();
    const suggestedFilename = safeFilename(download.suggestedFilename());
    const record = this.createRecord(
      suggestedFilename,
      downloadURL,
      page.isClosed() ? "" : page.url(),
    );
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
      await this.finishRecord(record, savedPath, downloadURL);
      this.logger.info(actionContext, "save_download", "success", {
        filename: record.filename,
        size: record.size,
        duration_ms: Date.now() - startedAt,
      });
    } catch (error) {
      this.failRecord(record, error);
      this.logger.error(actionContext, "save_download", "failed", {
        error_code: "DOWNLOAD_FAILED",
        message: record.error,
        duration_ms: Date.now() - startedAt,
      });
    }
  }

  /** createRecord 创建一条与浏览器普通下载格式一致的待处理记录。 */
  private createRecord(
    suggestedFilename: string,
    sourceURL: string,
    pageURL: string,
  ): DownloadRecord {
    const filename = safeFilename(suggestedFilename);
    const record: DownloadRecord = {
      id: randomUUID(),
      filename,
      file_name: filename,
      file_path: "",
      path: "",
      suggested_filename: filename,
      url: safeURL(sourceURL),
      page_url: safeURL(pageURL),
      size: 0,
      status: "pending",
      error: "",
      created_at: new Date().toISOString(),
    };
    this.downloads.unshift(record);
    this.trim();
    return record;
  }

  /** finishRecord 用最终文件信息把下载记录更新为成功。 */
  private async finishRecord(
    record: DownloadRecord,
    savedPath: string,
    sourceURL: string,
  ): Promise<void> {
    const stat = await fs.stat(savedPath);
    record.id = downloadID(savedPath, sourceURL);
    record.filename = path.basename(savedPath);
    record.file_name = record.filename;
    record.file_path = savedPath;
    record.path = savedPath;
    record.size = stat.size;
    record.status = "saved";
  }

  /** failRecord 把未知保存异常整理到统一失败记录。 */
  private failRecord(record: DownloadRecord, error: unknown): void {
    record.status = "failed";
    record.error = error instanceof Error ? error.message : String(error);
  }

  /** trim 把下载记录限制在最近 100 条。 */
  private trim(): void {
    if (this.downloads.length > 100) {
      this.downloads.length = 100;
    }
  }
}

/** writeAll 循环写完一个数据块，防止 FileHandle.write 短写造成文档缺页。 */
async function writeAll(handle: FileHandle, chunk: Buffer, deadlineAt?: number): Promise<void> {
  let offset = 0;
  while (offset < chunk.length) {
    assertDocumentDeadline(deadlineAt);
    const { bytesWritten } = await handle.write(
      chunk,
      offset,
      chunk.length - offset,
      null,
    );
    if (bytesWritten <= 0) {
      throw new Error("文档临时文件没有完整写入");
    }
    offset += bytesWritten;
  }
}

/** assertDocumentDeadline 保证网络读取和本地落盘共同受同一个总超时约束。 */
function assertDocumentDeadline(deadlineAt?: number): void {
  if (deadlineAt !== undefined && Date.now() >= deadlineAt) {
    throw new Error("保存文档页面超过总时限，已经停止读取");
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
