/** 文件作用说明：统一简历库维护、查重、人工推荐和本地导入的前端数据类型与展示规则。 */
export type ResumeExperience = {
  company_name?: string; position_name?: string; school_name?: string; major_name?: string;
  education_level?: string; project_name?: string; role_name?: string;
  start_ym?: string; end_ym?: string; content?: string;
};
export type ResumeNote = { id: string; content: string; author_email?: string; author_name?: string; occurred_at?: string; created_at?: string };
export type ResumeSource = { platform_id?: string; platform_candidate_id?: string; source_type?: string; source_url?: string; created_at?: string };
export type Resume = {
  id: string; candidate_name: string; phone?: string; email?: string; gender?: string; birth_ym?: string; avatar_data_url?: string;
  age?: number | null; current_region?: string; expected_region?: string; work_region?: string;
  work_years?: string; work_years_number?: number | null; education_level?: string; work_status?: string;
  expected_position?: string; expected_salary_min?: number | null; expected_salary_max?: number | null;
  expected_salary_text?: string; current_salary_text?: string; salary_text?: string; personal_description?: string; basic_info?: string; raw_text?: string; ai_summary?: string;
  work_experiences?: ResumeExperience[]; educations?: ResumeExperience[]; project_experiences?: ResumeExperience[];
  certificates?: Array<Record<string, unknown>>; honors?: Array<Record<string, unknown>>;
  tags?: string[]; library_status?: string; source_type?: string; sources?: ResumeSource[];
  platform_id?: string; platform_candidate_id?: string; user_email?: string; user_name?: string;
  created_at?: string; updated_at?: string; row_version?: number; creator_name?: string; notes?: ResumeNote[]; latest_note?: ResumeNote;
  recommendation_count?: number; recommendations?: Recommendation[]; position_name?: string;
  engagement_id?: string; engagement_status?: string;
  ai?: { detail?: { score?: number | null; reason?: string }; greet?: { score?: number | null; reason?: string } };
  events?: Array<Record<string, unknown>>;
};
export type ResumeDraft = Omit<Resume, "id"> & { id?: string };
export type PositionOption = { id: string; name: string };
export type DuplicateMatch = { candidate: Resume; match_type?: string; reasons?: string[]; reason?: string };
export type DuplicateResult = { exact: DuplicateMatch[]; possible: DuplicateMatch[]; hidden_match?: boolean };
export type RecommendationEvent = {
  id: string; from_stage?: string; to_stage?: string; content?: string; note?: string;
  author_email?: string; author_name?: string; occurred_at?: string; created_at?: string;
};
export type Recommendation = {
  id: string; candidate_id?: string; position_id: string; position_name?: string; stage: string;
  note?: string; recommended_at?: string; created_at?: string; updated_at?: string;
  author_email?: string; events?: RecommendationEvent[];
};
export const recommendationStages = [
  { value: "draft", label: "待推荐" }, { value: "recommended", label: "已推荐" },
  { value: "contacting", label: "沟通中" }, { value: "interview", label: "面试中" },
  { value: "offer", label: "Offer" }, { value: "hired", label: "已入职" }, { value: "paused", label: "暂不推进" },
];
export const sourceOptions = [
  { value: "manual", label: "手动添加" }, { value: "ai_import", label: "AI 解析" },
  { value: "browser_extension", label: "浏览器插件" }, { value: "automatic", label: "自动任务" },
];
/** avatarSource 仅展示云端规范化的内嵌PNG/JPEG头像，拒绝远程地址和超出合同大小的内容。 */
export function avatarSource(value?: string) {
  return typeof value === "string" && value.length <= 128 * 1024 && /^data:image\/(?:png|jpeg);base64,[A-Za-z0-9+/]+={0,2}$/.test(value) ? value : undefined;
}
/** sourceLabel 将简历来源或平台标识转成简短中文文案。 */
export function sourceLabel(value?: string) {
  return sourceOptions.find((item) => item.value === value)?.label ||
    ({ liepin: "猎聘", liepin_hr: "猎聘企业端", liepin_headhunter: "猎聘猎头端", zhilian: "智联", "51job": "前程无忧", boss: "BOSS直聘" } as Record<string, string>)[value || ""] || value || "自动任务";
}
/** stageLabel 返回人工推荐阶段，未知阶段保留原值。 */
export function stageLabel(value?: string) {
  return recommendationStages.find((item) => item.value === value)?.label || value || "待推荐";
}
/** genderLabel 统一中文和英文性别显示，空值不占位，未识别的值不推测性别。 */
export function genderLabel(value?: string) {
  const gender = String(value || "").trim().toLowerCase();
  if (!gender) return "";
  if (["男", "男性", "male", "m", "man"].includes(gender)) return "男";
  if (["女", "女性", "female", "f", "woman"].includes(gender)) return "女";
  return "未说明";
}
/** workYearsLabel 给纯数字工作年限补充单位，保留应届等原文，缺失时使用有效数字字段。 */
export function workYearsLabel(resume: Resume) {
  const value = String(resume.work_years || "").trim();
  if (value) return /^\d+(?:\.\d+)?$/.test(value) ? `${value}年经验` : value;
  return resume.work_years_number != null && Number.isFinite(resume.work_years_number) && resume.work_years_number >= 0 ? `${resume.work_years_number}年经验` : "";
}
/** recordTime 展示备注或推荐的发生时间，保存时间仍可在提示中查看，缺失或坏日期安全回退。 */
export function recordTime(value?: string) {
  if (!value) return "--";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
}
/** salaryLabel 只展示有效薪资，缺失字段不会被转换成零。 */
export function salaryLabel(resume: Resume | ResumeDraft) {
  const low = resume.expected_salary_min;
  const high = resume.expected_salary_max;
  if (low != null && high != null) return `${low}–${high}K`;
  if (low != null) return `${low}K 起`;
  if (high != null) return `${high}K 内`;
  return resume.expected_salary_text || resume.salary_text || "未填写";
}
/** normalizePhone 清理手机号的常见分隔符，保留国际号码加号。 */
export function normalizePhone(value?: string) {
  return String(value || "").replace(/[\s()（）-]/g, "").replace(/^(?:\+86|0086)(?=1\d{10}$)/, "");
}
/** validPhone 排除掩码号码与短号码，支持大陆和明确带国家码的国际号码。 */
export function validPhone(value?: string) {
  const phone = normalizePhone(value);
  return /^1[3-9]\d{9}$/.test(phone) || /^\+[1-9]\d{7,14}$/.test(phone);
}
/** experienceLabel 返回一段经历的公司、职位和时间摘要。 */
export function experienceLabel(item: ResumeExperience) {
  return [item.company_name || item.school_name || item.project_name,
    item.position_name || item.major_name || item.role_name,
    item.start_ym || item.end_ym ? `${item.start_ym || ""}–${item.end_ym || "至今"}` : ""].filter(Boolean).join(" · ");
}
/** resumeAge 优先按出生年月计算年龄，未知生日时保留采集到的估计年龄。 */
export function resumeAge(resume: Resume) {
  const birth = /^([12]\d{3})-(\d{2})$/.exec(resume.birth_ym || "");
  if (!birth) return resume.age ? `${resume.age}岁` : "";
  const today = new Date();
  const age = today.getFullYear() - Number(birth[1]) - (today.getMonth() + 1 < Number(birth[2]) ? 1 : 0);
  return age >= 0 && age < 120 ? `${age}岁` : "";
}
/** nonEmptyChanges 比较维护字段，只提交明确填写或主动选择清空的改动。 */
export function nonEmptyChanges(draft: ResumeDraft, previous?: Resume | null, clearFields: string[] = []) {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(draft)) {
    if (["id", "row_version", "creator_name", "notes", "sources", "recommendations", "created_at", "updated_at", "user_email", "user_name", "ai", "events", "platform_id", "platform_candidate_id", "engagement_id", "engagement_status", "recommendation_count", "position_name"].includes(key)) continue;
    if (clearFields.includes(key)) continue;
    if (value == null || (typeof value === "string" && !value.trim()) || (Array.isArray(value) && value.length === 0)) continue;
    if (!previous || JSON.stringify(value) !== JSON.stringify(previous[key as keyof Resume])) result[key] = value;
  }
  return result;
}
