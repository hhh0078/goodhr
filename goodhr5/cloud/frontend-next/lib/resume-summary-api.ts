/** 文件作用说明：通过本地程序生成职业摘要草稿，只提交职业字段，模型密钥和原简历不进入轮询响应。 */
"use client";
import { getToken } from "./admin-api";
import type { Resume } from "./resume-types";
import { isReadOnlyResume } from "./resume-types";

export type ResumeSummaryTask = { id: string; status: "queued" | "running" | "completed" | "failed" | "cancelled"; ai_summary: string; model: string; error: string; possibly_charged: boolean };
/** ResumeSummaryRequestError 保留本地稳定错误码，只在明确未启动任务时允许调整源资料。 */
export class ResumeSummaryRequestError extends Error {
  code: string;
  /** constructor 将安全错误文案和错误码交给草稿界面，不包含原始请求或会话。 */
  constructor(message: string, code: string) { super(message); this.code = code; }
}
/** summarySource 明确列出职业源字段，头像、联系方式、旧AI摘要和自动任务记录不会发往模型。 */
export function summarySource(candidate: Resume) {
  if (isReadOnlyResume(candidate)) throw new Error("内置示例只能查看，不调用AI生成摘要。");
  const fields = ["work_region", "work_years", "education_level", "expected_position", "expected_salary_min", "expected_salary_max", "work_status", "personal_description", "raw_text", "work_experiences", "educations", "project_experiences", "certificates", "honors", "basic_info", "current_region", "expected_region", "current_position", "expected_salary_text", "current_salary_text", "tags"];
  return Object.fromEntries(fields.map((field) => [field, candidate[field as keyof Resume]]).filter(([, value]) => value != null));
}
/** requestResumeSummary 调用当前账号隔离的本地草稿接口，不做隐式网络重试或云端保存。 */
export async function requestResumeSummary(agentBase: string, path: string, method = "GET", body?: unknown): Promise<ResumeSummaryTask> {
  if (!agentBase) throw new Error("本地程序还没连接，打开后点重新连接就好。");
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15000);
  try {
    const response = await fetch(`${agentBase}/api/v1/resume-summary/tasks${path}`, { method, cache: "no-store", signal: controller.signal,
      headers: { Authorization: `Bearer ${getToken()}`, "Content-Type": "application/json" }, body: body == null ? undefined : JSON.stringify(body) });
    const data = await response.json();
    if (!response.ok || data.ok === false) throw new ResumeSummaryRequestError(data.error?.message || "摘要请求没接上，请重试读取，已完成的草稿会保留。", data.error?.code || "");
    if (!data.data?.task?.id) throw new Error("本地程序还不支持摘要，请先更新并重启。");
    return data.data.task;
  } catch (cause) {
    if (cause instanceof Error && cause.name === "AbortError") throw new Error("请求响应有点慢，请重试读取；同一请求不会自动重复调用AI。");
    if (cause instanceof TypeError) throw new Error("本地响应没接完整，请先恢复上次任务，避免另开一次AI调用。");
    throw cause instanceof Error ? cause : new Error("本地程序没连接上，请重新连接后重试。");
  } finally { clearTimeout(timeout); }
}
