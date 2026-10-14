/** 文件作用说明：封装简历库云端查询、维护、查重和人工推荐接口，避免复用自动任务入库接口。 */
"use client";
import { cloudRequest } from "./admin-api";
import type { DuplicateMatch, DuplicateResult, PositionOption, Recommendation, RecommendationRecord, Resume, ResumeDraft, ResumeNote, ResumeTag } from "./resume-types";
import { isReadOnlyResume } from "./resume-types";

/** loadResume 读取候选人详情，可保留旧自动任务详情参数。 */
export async function loadResume(id: string, engagementID = ""): Promise<Resume> {
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}${engagementID ? `?engagement_id=${encodeURIComponent(engagementID)}` : ""}`);
  return data.candidate || data;
}
/** loadResumePositions 逐页获取有权访问的岗位，不截断岗位选择范围。 */
export async function loadResumePositions(): Promise<PositionOption[]> {
  const result: PositionOption[] = [];
  for (let page = 1; page <= 1000; page += 1) {
    const data = await cloudRequest(`/api/positions?purpose=recommendation&page=${page}&page_size=100`);
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
  if (isReadOnlyResume(id) || fields.is_builtin || fields.readonly) throw new Error("内置示例只能查看，不能修改或复制入库。");
  // 显式清空按云端合同保持字段类型，经历和标签使用空数组，薪资数字使用null。
  const arrayFields = ["tags", "tag_ids", "work_experiences", "educations", "project_experiences", "certificates", "honors", "colleague_communications"];
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
  if (isReadOnlyResume(id)) throw new Error("内置示例只能查看，不能添加备注。");
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
  if (isReadOnlyResume(id)) throw new Error("内置示例只能查看，不能关联或推荐到真实岗位。");
  const data = await cloudRequest(`/api/candidates/${encodeURIComponent(id)}/recommendations`, { method: "POST", body: { position_id: positionID, stage, content: note, idempotency_key: idempotencyKey } });
  return data.recommendation || data;
}
/** advanceRecommendation 明确记录阶段变化和该岗位的跟进备注。 */
export async function advanceRecommendation(id: string, positionID: string, stage: string, note: string, idempotencyKey: string): Promise<Recommendation> {
  return addRecommendation(id, positionID, stage, note, idempotencyKey);
}
/** loadResumeTags 读取本人可用的个人标签和团队标签，以及有权查看的候选人数。 */
export async function loadResumeTags(): Promise<ResumeTag[]> {
  const data = await cloudRequest("/api/candidates/tags");
  return Array.isArray(data.tags) ? data.tags : [];
}
/** saveResumeTag 明确按个人或团队范围新增、维护标签，不在选择时自动创建。 */
export async function saveResumeTag(id: string, name: string, scope: ResumeTag["scope"]): Promise<ResumeTag> {
  const data = await cloudRequest(`/api/candidates/tags${id ? `/${encodeURIComponent(id)}` : ""}`, { method: id ? "PATCH" : "POST", body: { name: name.trim(), scope } });
  if (!data.tag?.id) throw new Error("标签保存结果没读出来，请刷新后核对。");
  return data.tag;
}
/** deleteResumeTag 仅删除标签及关联，候选人简历仍保留在简历库。 */
export async function deleteResumeTag(id: string): Promise<void> {
  await cloudRequest(`/api/candidates/tags/${encodeURIComponent(id)}`, { method: "DELETE" });
}
/** loadRecommendationRecords 读取本人有权查看的全局推荐记录，保留关闭岗位的历史及服务端操作权限。 */
export async function loadRecommendationRecords(query: URLSearchParams): Promise<{ items: RecommendationRecord[]; total: number }> {
  const data = await cloudRequest(`/api/candidate-recommendations?${query}`);
  return { items: Array.isArray(data.items) ? data.items : [], total: Number(data.total) || 0 };
}
/** deleteRecommendationRecord 仅删除推荐及跟进记录，保留候选人和岗位主体。 */
export async function deleteRecommendationRecord(id: string): Promise<void> {
  await cloudRequest(`/api/candidate-recommendations/${encodeURIComponent(id)}`, { method: "DELETE" });
}
