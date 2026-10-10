/** 文件作用说明：封装简历库云端查询、维护、查重和人工推荐接口，避免复用自动任务入库接口。 */
"use client";
import { cloudRequest } from "./admin-api";
import type { DuplicateMatch, DuplicateResult, PositionOption, Recommendation, Resume, ResumeDraft, ResumeNote } from "./resume-types";

/** loadResume 读取候选人详情，可保留旧自动任务详情参数。 */
export async function loadResume(id: string, engagementID = ""): Promise<Resume> {
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}${engagementID ? `?engagement_id=${encodeURIComponent(engagementID)}` : ""}`);
  return data.candidate || data;
}
/** loadResumePositions 逐页获取有权访问的岗位，不截断岗位选择范围。 */
export async function loadResumePositions(): Promise<PositionOption[]> {
  const result: PositionOption[] = [];
  for (let page = 1; page <= 1000; page += 1) {
    const data = await cloudRequest(`/api/positions?page=${page}&page_size=100`);
    const items = Array.isArray(data.positions) ? data.positions : [];
    const fresh = items.filter((item: PositionOption) => !result.some((current) => current.id === item.id));
    result.push(...fresh);
    if (!fresh.length || !Number.isFinite(Number(data.total)) || result.length >= Number(data.total)) break;
  }
  return result;
}
/** checkResumeDuplicates 查找精确和可能重复，隐藏简历只返回安全提示。 */
export async function checkResumeDuplicates(draft: ResumeDraft, excludeID = ""): Promise<DuplicateResult> {
  const data = await cloudRequest("/api/candidates/duplicates", { method: "POST", body: {
    candidate_name: draft.candidate_name, phone: draft.phone, exclude_id: excludeID,
    keyword: [draft.work_experiences?.[0]?.company_name, draft.educations?.[0]?.school_name, draft.expected_position].filter(Boolean).join(" "),
  } });
  /** matches 兼容确定/可能重复响应并保留匹配原因。 */
  function matches(items: any[]): DuplicateMatch[] {
    return (items || []).map((item) => ({ ...item, candidate: item.candidate || item }));
  }
  const all = matches(data.matches || []);
  return { exact: all.filter((item) => item.match_type === "exact"), possible: all.filter((item) => item.match_type === "possible"), hidden_match: Boolean(data.hidden_match) };
}
/** saveResume 在独立候选人接口创建或维护，并为重试保留幂等键。 */
export async function saveResume(id: string, fields: Record<string, unknown>, idempotencyKey: string, version?: number, clearFields: string[] = []): Promise<Resume> {
  // 显式清空按云端合同保持字段类型，经历和标签使用空数组，薪资数字使用null。
  const arrayFields = ["tags", "work_experiences", "educations", "project_experiences", "certificates", "honors", "colleague_communications"];
  const cleared = Object.fromEntries(clearFields.map((field) => [field, arrayFields.includes(field) ? [] : ["expected_salary_min", "expected_salary_max"].includes(field) ? null : ""]));
  const data = await cloudRequest(id ? `/api/candidates/${encodeURIComponent(id)}` : "/api/candidates", {
    method: id ? "PATCH" : "POST", body: { ...fields, ...cleared, idempotency_key: idempotencyKey, ...(id ? { row_version: version } : {}) },
  });
  return data.candidate || data;
}
/** loadResumeNotes 读取独立备注历史。 */
export async function loadResumeNotes(id: string): Promise<ResumeNote[]> {
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}/notes`);
  return data.notes || [];
}
/** addResumeNote 独立追加备注，幂等键避免重试产生重复记录。 */
export async function addResumeNote(id: string, content: string, idempotencyKey: string): Promise<ResumeNote> {
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}/notes`, { method: "POST", body: { content, idempotency_key: idempotencyKey } });
  return data.note;
}
/** loadRecommendations 读取人工岗位推荐与推进历史。 */
export async function loadRecommendations(id: string): Promise<Recommendation[]> {
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}/recommendations`);
  return data.recommendations || [];
}
/** addRecommendation 关联岗位或明确记录推荐动作，不启动自动招聘任务。 */
export async function addRecommendation(id: string, positionID: string, stage: string, note: string, idempotencyKey: string): Promise<Recommendation> {
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}/recommendations`, { method: "POST", body: { position_id: positionID, stage, content: note, idempotency_key: idempotencyKey } });
  return data.recommendation || data;
}
/** advanceRecommendation 明确记录阶段变化和该岗位的跟进备注。 */
export async function advanceRecommendation(id: string, positionID: string, stage: string, note: string, idempotencyKey: string): Promise<Recommendation> {
  return addRecommendation(id, positionID, stage, note, idempotencyKey);
}
