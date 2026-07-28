// 文件作用说明：在 Worker HTTP 边界安全读取有限大小的 JSON 请求体。

import type { IncomingMessage } from "node:http";
import { WorkerError } from "../errors/worker-error.js";

const maximumBodyBytes = 1_048_576;

/** readJSONBody 读取并解析 JSON，请求体过大或格式错误时返回统一错误。 */
export async function readJSONBody(
  request: IncomingMessage,
  traceID: string,
  action: string,
): Promise<unknown> {
  const chunks: Buffer[] = [];
  let total = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    total += buffer.length;
    if (total > maximumBodyBytes) {
      throw invalidBody(traceID, action, "请求内容太大了，请缩短后再试");
    }
    chunks.push(buffer);
  }
  if (total === 0) {
    return {};
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
  } catch {
    throw invalidBody(traceID, action, "请求内容不是有效 JSON");
  }
}

/** invalidBody 创建请求体解析错误。 */
function invalidBody(
  traceID: string,
  action: string,
  message: string,
): WorkerError {
  return new WorkerError({
    code: "INVALID_REQUEST",
    message,
    action,
    step: "parse_body",
    trace_id: traceID,
    retryable: false,
  });
}
