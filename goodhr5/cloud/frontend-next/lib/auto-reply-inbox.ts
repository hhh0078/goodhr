/** 文件作用说明：读取当前账号可处理的自动回复待办汇总与游标分页，不把聊天原文存入浏览器缓存。 */
"use client";

import { cloudRequest, getToken } from "./admin-api";

export type ReplyInboxPosition = { position_id: string; position_name: string; platform_id: string; is_open: boolean; pending_count: number };
export type ReplyInboxSummary = { pending_count: number; positions: ReplyInboxPosition[] };
export type ReplyQuestion = {
  id: string; position_id: string; question: string; answer: string; status: string;
  publish_target: string; error: string; version: number;
  source_messages: Array<{ message_id: string; text: string; candidate_name: string }>;
};
export type ReplyInboxView = "pending" | "all";
export type ReplyInboxPage = { questions: ReplyQuestion[]; next_cursor: string; pending_count: number };

/** objectValue 仅在外部响应边界接受普通对象。 */
function objectValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

/** countValue 拒绝缺失或无效计数，避免把读取失败显示成零。 */
function countValue(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new Error("待确认数量暂时没读明白，可以重试。");
  return value;
}

/** textValue 将可选字符串收紧为空值安全的显示文本。 */
function textValue(value: unknown): string { return typeof value === "string" ? value : ""; }

/** normalizeReplyQuestion 收紧列表与保存后的同一份问题合同，缺少主键或版本时不展示不完整结果。 */
export function normalizeReplyQuestion(value: unknown): ReplyQuestion {
  const item = objectValue(value);
  if (!textValue(item.id) || !textValue(item.question) || typeof item.version !== "number" || !Number.isSafeInteger(item.version) || item.version < 1) throw new Error("待确认问题暂时没读完整，可以重试。");
  const sources = Array.isArray(item.source_messages) ? item.source_messages : [];
  return { id: textValue(item.id), position_id: textValue(item.position_id), question: textValue(item.question), answer: textValue(item.answer), status: textValue(item.status), publish_target: textValue(item.publish_target), error: textValue(item.error), version: item.version,
    source_messages: sources.map(source => { const row = objectValue(source); return { message_id: textValue(row.message_id), text: textValue(row.text), candidate_name: textValue(row.candidate_name) }; }) };
}

/** requestInbox 在响应返回时再次核对登录凭证，旧账号的结果不能进入新账号页面。 */
async function requestInbox(path: string, signal: AbortSignal): Promise<Record<string, unknown>> {
  const token = getToken();
  if (!token) throw new Error("登录后才能处理待确认问题。");
  const data = await cloudRequest(path, { signal });
  if (getToken() !== token) throw new Error("登录账号已变化，请重新打开自动回复。");
  return objectValue(data);
}

/** loadReplyInboxSummary 只读取数量和岗位名称，关闭岗位仍由服务器按可答权限返回。 */
export async function loadReplyInboxSummary(signal: AbortSignal): Promise<ReplyInboxSummary> {
  const data = await requestInbox("/api/auto-reply/questions/summary", signal);
  if (!Array.isArray(data.positions)) throw new Error("待确认岗位暂时没读出来，可以重试。");
  return {
    pending_count: countValue(data.pending_count),
    positions: data.positions.map(value => {
      const item = objectValue(value);
      if (!textValue(item.position_id)) throw new Error("待确认岗位暂时没读出来，可以重试。");
      return { position_id: textValue(item.position_id), position_name: textValue(item.position_name) || "未命名岗位", platform_id: textValue(item.platform_id), is_open: item.is_open === true, pending_count: countValue(item.pending_count) };
    }),
  };
}

/** loadReplyInboxPage 只读取当前岗位当前页的原话；服务器核对游标归属后界定分页。 */
export async function loadReplyInboxPage(positionID: string, view: ReplyInboxView, cursor: string, signal: AbortSignal): Promise<ReplyInboxPage> {
  const query = new URLSearchParams({ position_id: positionID, view, limit: "10" });
  if (cursor) query.set("cursor", cursor);
  const data = await requestInbox(`/api/auto-reply/questions?${query}`, signal);
  if (!Array.isArray(data.questions)) throw new Error("待确认问题暂时没读出来，可以重试。");
  return { next_cursor: textValue(data.next_cursor), pending_count: countValue(data.pending_count), questions: data.questions.map(normalizeReplyQuestion) };
}
