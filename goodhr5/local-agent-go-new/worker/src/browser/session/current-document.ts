// 文件作用说明：使用当前 Firefox 浏览器会话保存已打开的文档页，并校验下载大小、类型和文件签名。

import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import type { APIResponse, BrowserContext } from "playwright-core";
import type { DownloadRecord, SaveCurrentDocumentRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";
import { safeURL } from "./navigation.js";

/** saveCurrentDocumentFromContext 使用当前会话的登录状态读取文档，校验后原子保存。 */
export async function saveCurrentDocumentFromContext(
  context: BrowserContext,
  pageURL: string,
  directory: string,
  request: SaveCurrentDocumentRequest,
  actionContext: ActionContext,
): Promise<DownloadRecord> {
  assertHTTPURL(pageURL, actionContext);
  const maximumBytes = request.max_bytes ?? 26_214_400;
  const allowed = (request.allowed_content_types ?? ["application/pdf"]).map(normalizeContentType);
  const timeoutMS = request.timeout_ms ?? 30_000;
  const deadline = Date.now() + timeoutMS;
  let response: APIResponse | undefined;
  let temporaryPath = "";
  let savedPath = "";
  let destinationCreated = false;
  try {
    response = await context.request.get(pageURL, {
      headers: { accept: allowed.join(", "), "accept-encoding": "identity" },
      failOnStatusCode: false,
      maxRedirects: 5,
      timeout: timeoutMS,
    });
    const responseURL = response.url();
    assertHTTPURL(responseURL, actionContext);
    if (new URL(pageURL).protocol === "https:" && new URL(responseURL).protocol !== "https:") {
      throw documentError(actionContext, "文档跳转到了不安全的地址，已经停止保存", { page_url: safeURL(responseURL) });
    }
    const status = response.status();
    if (status < 200 || status >= 300) {
      throw documentError(actionContext, `文档页面返回了 HTTP ${status}，暂时没保存下来`, {
        status, page_url: safeURL(responseURL),
      }, status >= 500);
    }
    const headers = response.headers();
    const contentType = normalizeContentType(headers["content-type"] ?? "");
    const contentLength = headers["content-length"]?.trim() ?? "";
    if (/^\d+$/.test(contentLength) && Number(contentLength) > maximumBytes) {
      throw documentError(actionContext, "文档文件太大了，已经停止保存", { max_bytes: maximumBytes });
    }
    const genericType = ["", "application/octet-stream", "binary/octet-stream", "application/download"].includes(contentType);
    if (!allowed.includes(contentType) && !(genericType && allowed.includes("application/pdf"))) {
      throw documentError(actionContext, "文档类型不在允许范围内，已经停止保存", { content_type: contentType || "unknown" });
    }
    const body = await response.body();
    if (Date.now() >= deadline) {
      throw documentError(actionContext, "保存文档页面超过总时限，已经停止读取", { page_url: safeURL(responseURL) });
    }
    if (body.length === 0 || body.length > maximumBytes) {
      throw documentError(actionContext, "文档内容为空或超过大小限制，已经停止保存", { max_bytes: maximumBytes, actual_bytes: body.length });
    }
    if (allowed.includes("application/pdf") && (contentType === "application/pdf" || genericType) && body.subarray(0, 5).toString("latin1") !== "%PDF-") {
      throw documentError(actionContext, "文档内容签名没有通过校验", { page_url: safeURL(responseURL) });
    }
    if (Date.now() >= deadline) {
      throw documentError(actionContext, "保存文档页面超过总时限，已经停止落盘", { page_url: safeURL(responseURL) });
    }
    await fs.mkdir(directory, { recursive: true });
    const filename = safeFilename(request.suggested_filename ?? "document", allowed.includes("application/pdf"));
    temporaryPath = path.join(directory, `.goodhr-${randomUUID()}.part`);
    await fs.writeFile(temporaryPath, body, { flag: "wx", mode: 0o600 });
    if (Date.now() >= deadline) {
      throw documentError(actionContext, "保存文档页面超过总时限，已经停止落盘", { page_url: safeURL(responseURL) });
    }
    savedPath = await linkUniqueDocument(directory, filename, temporaryPath);
    destinationCreated = true;
    await fs.rm(temporaryPath);
    temporaryPath = "";
    return {
      id: randomUUID(), filename: path.basename(savedPath), file_name: path.basename(savedPath),
      file_path: savedPath, path: savedPath, suggested_filename: filename,
      url: safeURL(responseURL), page_url: safeURL(pageURL), size: body.length,
      status: "saved", error: "", created_at: new Date().toISOString(),
    };
  } catch (error) {
    if (temporaryPath) await fs.rm(temporaryPath, { force: true });
    if (destinationCreated) await fs.rm(savedPath, { force: true });
    if (error instanceof WorkerError) throw error;
    throw documentError(actionContext, "当前文档页没保存成功，我已经把原因记下来了", { page_url: safeURL(pageURL) }, true);
  } finally {
    await response?.dispose().catch(() => undefined);
  }
}

/** assertHTTPURL 拒绝从本地文件或浏览器内部页面读取文档。 */
function assertHTTPURL(rawURL: string, context: ActionContext): void {
  try {
    const parsed = new URL(rawURL);
    if (parsed.protocol === "http:" || parsed.protocol === "https:") return;
  } catch { /* 统一在下方给出可读错误。 */ }
  throw documentError(context, "当前页面不是可以保存的网页文档", { page_url: safeURL(rawURL) });
}

/** normalizeContentType 去掉 MIME 类型参数并统一小写。 */
function normalizeContentType(value: string): string {
  return value.toLowerCase().split(";", 1)[0]?.trim() ?? "";
}

/** safeFilename 清理路径、系统保留字符和空文件名。 */
function safeFilename(value: string, pdf: boolean): string {
  const base = (value.split(/[\\/]/).pop() ?? "").replace(/[<>:"/\\|?*\x00-\x1f]/g, "_").trim().replace(/[. ]+$/g, "");
  const name = base || "document";
  return pdf && !path.extname(name) ? `${name}.pdf` : name;
}

/** linkUniqueDocument 以排他硬链接保存文档，遇到同名文件时选择下一个名字。 */
async function linkUniqueDocument(directory: string, filename: string, temporaryPath: string): Promise<string> {
  const extension = path.extname(filename);
  const stem = filename.slice(0, filename.length - extension.length);
  for (let index = 0; index < 100; index += 1) {
    const suffix = index === 0 ? "" : `-${index}`;
    const candidate = path.join(directory, `${stem}${suffix}${extension}`);
    try {
      await fs.link(temporaryPath, candidate);
      return candidate;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
    }
  }
  const candidate = path.join(directory, `${stem}-${randomUUID()}${extension}`);
  await fs.link(temporaryPath, candidate);
  return candidate;
}

/** documentError 返回不包含令牌和下载地址参数的稳定下载错误。 */
function documentError(context: ActionContext, message: string, details: JsonObject, retryable = false): WorkerError {
  return new WorkerError({ code: "DOWNLOAD_FAILED", message, action: context.action,
    step: "save_current_document", trace_id: context.trace_id, retryable, details });
}
