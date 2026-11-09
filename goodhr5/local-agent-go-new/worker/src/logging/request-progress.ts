// 文件作用说明：在 Worker 内存中按请求编号保留有限的操作进度，只保存步骤和数量。
import { createRequire } from "node:module";
import type { Page } from "playwright-core";
import type { ActionContext, JsonObject } from "../contracts/common.js";

type ReadPhase = "read_card" | "locate_field" | "read_field";
type PendingRead = { worker_phase: ReadPhase; item_index: number; field_key?: string; started: number };
type Entry = { at: number; data: JsonObject; reads: Set<PendingRead>; finishedAt?: number; page?: WeakRef<Page> };
const recent = new Map<string, Entry>();
const safeFields = new Set(["name", "card_text", "basic_info", "education", "university", "description", "platform_candidate_id"]);
/** installedWorkerVersion 读取随包版本；旧安装缺少清单时仍可继续浏览器操作。 */
function installedWorkerVersion(): string | undefined {
  try {
    const version: unknown = createRequire(import.meta.url)("../../package.json").version;
    return typeof version === "string" && /^[0-9][0-9A-Za-z.+-]{0,31}$/.test(version) ? version : undefined;
  } catch { return undefined; }
}
const workerVersion = installedWorkerVersion();

/** requestID 只接受 Go 生成的随机编号或标准 UUID，其他请求头不能作为排障内容。 */
export function requestID(value: unknown): string | undefined {
  return typeof value === "string" && /^(?:[A-Z2-7]{26}|[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12})$/.test(value) ? value : undefined;
}

/** startRequestProgress 为一次收到的浏览器请求建立进度槽，保留最近的 64 个请求。 */
export function startRequestProgress(context: ActionContext): void {
  if (!context.request_id) return;
  recent.set(context.request_id, { at: context.started_at, reads: new Set(), data: { request_id: context.request_id,
    worker_phase: "received", ...(workerVersion ? { worker_version: workerVersion } : {}), node_version: process.version } });
  if (recent.size > 64) recent.delete(recent.keys().next().value!);
}

/** markRequestProgress 更新最近一次实际进入的步骤，字段名必须来自固定清单，绝不读字段值。 */
export function markRequestProgress(context: ActionContext, phase: string, index?: number, field?: string, count?: number): void {
  const entry = recent.get(context.request_id ?? "");
  if (!entry || entry.finishedAt !== undefined || context.signal?.aborted) return;
  entry.at = Date.now();
  entry.data.worker_phase = phase;
  if (index === undefined) delete entry.data.item_index;
  else entry.data.item_index = index;
  if (field !== undefined && safeFields.has(field)) entry.data.field_key = field;
  else delete entry.data.field_key;
  if (count !== undefined) entry.data.item_count = count;
}

/** beginRequestRead 在获得读取槽后登记正在等待的整数序号与字段键，完成即释放；故障后的清理不能覆盖快照。 */
export function beginRequestRead(context: ActionContext, phase: ReadPhase, index: number, field?: string) {
  const entry = recent.get(context.request_id ?? "");
  if (!entry || entry.finishedAt !== undefined || context.signal?.aborted || entry.reads.size >= 4) return;
  markRequestProgress(context, phase, index, field);
  const pending: PendingRead = { worker_phase: phase, item_index: index, started: Date.now() };
  if (field && safeFields.has(field)) pending.field_key = field;
  entry.reads.add(pending);
  return {
    /** readValue 记录已完成定位，接下来开始读取值，但不保存值。 */
    readValue() {
      if (entry.finishedAt !== undefined) return;
      pending.worker_phase = "read_field";
      pending.started = Date.now();
      markRequestProgress(context, "read_field", index, field);
    },
    /** done 释放成功或出错的槽；超时现场保留的是取消前仍待返回的操作。 */
    done() {
      if (entry.finishedAt === undefined) entry.reads.delete(pending);
    },
  };
}

/** observeRequestPage 仅保存弱引用供故障时读取页面连接状态，不读取或保存页面内容。 */
export function observeRequestPage(context: ActionContext, page: Page): void {
  const entry = recent.get(context.request_id ?? "");
  if (entry && entry.finishedAt === undefined) entry.page = new WeakRef(page);
}

/** finishRequestProgress 冻结离开 HTTP 边界时的进度，排空中的读取不能覆盖故障现场。 */
export function finishRequestProgress(id?: string): void {
  const entry = recent.get(id ?? "");
  if (entry && entry.finishedAt === undefined) entry.finishedAt = Date.now();
}

/** requestProgress 只按指定编号返回快照，队列是同一时刻 Worker 全局状态。 */
export function requestProgress(id: string | undefined, queue: JsonObject): JsonObject {
  const entry = recent.get(id ?? "");
  if (!entry) return {};
  const page = entry.page?.deref();
  const state: JsonObject = {};
  if (page) {
    try {
      state.page_closed = page.isClosed();
      state.frame_count = page.frames().length;
      const browser = page.context().browser();
      if (browser) {
        state.browser_connected = browser.isConnected();
        const version = browser.version();
        if (/^[0-9][0-9A-Za-z.+-]{0,31}$/.test(version)) state.browser_version = version;
      }
    } catch { /* 页面已销毁时仍保留此前记下的进度。 */ }
  }
  const now = entry.finishedAt ?? Date.now();
  const pendingReads = Array.from(entry.reads, ({ started, ...operation }) => ({ ...operation, elapsed_ms: Math.max(0, now - started) }));
  return { ...entry.data, ...state, progress_age_ms: Math.max(0, now - entry.at), pending_reads: pendingReads,
    queue_pending: queue.pending ?? 0, queue_blocked: queue.blocked ?? false,
    active_action: queue.active_action ?? "", active_request_id: queue.active_request_id ?? "" };
}
