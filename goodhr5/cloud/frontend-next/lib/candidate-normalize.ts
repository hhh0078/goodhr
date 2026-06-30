/** 本文件负责把 GoodHR 候选人接口数据统一成前端简历库展示结构。 */

export type NormalizedExperience = {
  companyName?: string;
  positionName?: string;
  projectName?: string;
  roleName?: string;
  schoolName?: string;
  majorName?: string;
  educationLevel?: string;
  content?: string;
  startYm?: string;
  endYm?: string;
};

export type NormalizedCandidate = {
  id: string;
  engagementId: string;
  status: string;
  name: string;
  avatarUrl: string;
  age: string;
  gender: string;
  workRegion: string;
  workYears: string;
  educationLevel: string;
  expectedPosition: string;
  expectedSalary: string;
  workStatus: string;
  onlineStatus: string;
  personalDescription: string;
  workExperiences: NormalizedExperience[];
  educations: NormalizedExperience[];
  projectExperiences: NormalizedExperience[];
  rawText: string;
  resumeText: string;
  aiFirstAnalysis: { score: unknown; reason: string };
  aiSecondAnalysis: { score: unknown; reason: string };
  aiReviewAnalysis: { score: unknown; reason: string };
  createdAt: string;
  updatedAt: string;
  raw: any;
};

/** normalizeCandidate 统一候选人字段命名，避免页面到处猜字段。 */
export function normalizeCandidate(input: any): NormalizedCandidate {
  const source = input || {};
  const resume = source.resume_json || source.resume || source.detail_json || {};
  return {
    id: stringValue(source.id),
    engagementId: stringValue(source.engagement_id || source.engagementId),
    status: stringValue(source.engagement_status || source.status || "created"),
    name: stringValue(source.candidate_name || source.name || resume.candidate_name || resume.name || "未命名候选人"),
    avatarUrl: stringValue(source.avatar_url || source.avatarUrl || resume.avatar_url || resume.avatarUrl),
    age: stringValue(source.age || resume.age),
    gender: stringValue(source.gender || resume.gender),
    workRegion: stringValue(source.work_region || source.workRegion || resume.work_region || resume.workRegion || source.city || resume.city),
    workYears: stringValue(source.work_years || source.workYears || resume.work_years || resume.workYears || source.experience || resume.experience),
    educationLevel: stringValue(source.education_level || source.educationLevel || resume.education_level || resume.educationLevel || source.education || resume.education),
    expectedPosition: stringValue(source.expected_position || source.expectedPosition || resume.expected_position || resume.expectedPosition || source.position_name || source.positionName),
    expectedSalary: salaryText(source),
    workStatus: stringValue(source.work_status || source.workStatus || resume.work_status || resume.workStatus || source.job_status || resume.job_status),
    onlineStatus: stringValue(source.online_status || source.onlineStatus || resume.online_status || resume.onlineStatus),
    personalDescription: stringValue(source.personal_description || source.personalDescription || resume.personal_description || resume.personalDescription || source.summary || resume.summary || source.description),
    workExperiences: arrayValue(source.work_experiences || source.workExperiences || resume.work_experiences || resume.workExperiences).map(normalizeExperience),
    educations: arrayValue(source.educations || source.education_experiences || source.educationExperiences || resume.educations || resume.education_experiences || resume.educationExperiences).map(normalizeExperience),
    projectExperiences: arrayValue(source.project_experiences || source.projectExperiences || source.projects || resume.project_experiences || resume.projectExperiences || resume.projects).map(normalizeExperience),
    rawText: stringValue(source.raw_text || source.rawText || resume.raw_text || resume.rawText),
    resumeText: stringValue(source.resume_text || source.resumeText || source.resume_attachment_extracted_text || resume.resume_text || resume.resumeText),
    aiFirstAnalysis: { score: source.ai_detail_score ?? source.aiFirstScore ?? source.analysis?.detail?.score, reason: stringValue(source.ai_detail_reason || source.aiFirstReason || source.analysis?.detail?.reason) },
    aiSecondAnalysis: { score: source.ai_greet_score ?? source.aiSecondScore ?? source.analysis?.greet?.score, reason: stringValue(source.ai_greet_reason || source.aiSecondReason || source.analysis?.greet?.reason) },
    aiReviewAnalysis: { score: source.ai_review_score ?? source.aiReviewScore ?? source.analysis?.review?.score, reason: stringValue(source.ai_review_reason || source.aiReviewReason || source.analysis?.review?.reason) },
    createdAt: stringValue(source.created_at || source.createdAt),
    updatedAt: stringValue(source.updated_at || source.updatedAt),
    raw: source,
  };
}

/** statusText 返回候选人状态中文文案。 */
export function statusText(status: string) {
  return ({ created: "新建", new: "新建", analyzed: "沟通中", greeted: "沟通中", pooled: "已入库", rejected: "不合适", blacklist: "黑名单", skipped: "不合适", failed: "不合适" } as Record<string, string>)[status] || status || "新建";
}

/** scoreText 返回评分展示文本。 */
export function scoreText(value: unknown) {
  const score = Number(value);
  return Number.isFinite(score) ? `${Math.round(score)}分` : "无";
}

/** experienceLine 返回经历一行摘要。 */
export function experienceLine(item: NormalizedExperience) {
  return [item.companyName || item.schoolName || item.projectName, item.positionName || item.majorName || item.roleName || item.educationLevel, periodText(item)].filter(Boolean).join(" / ");
}

/** periodText 返回经历时间范围。 */
export function periodText(item: NormalizedExperience) {
  if (!item.startYm && !item.endYm) return "";
  return `${item.startYm || ""}${item.startYm || item.endYm ? " - " : ""}${item.endYm || "至今"}`;
}

/** stringValue 安全返回字符串。 */
function stringValue(value: unknown) {
  return value == null ? "" : String(value).trim();
}

/** arrayValue 安全返回数组。 */
function arrayValue(value: unknown): any[] {
  return Array.isArray(value) ? value : [];
}

/** normalizeExperience 统一经历字段命名。 */
function normalizeExperience(item: any): NormalizedExperience {
  return {
    companyName: stringValue(item?.company_name || item?.companyName || item?.company),
    positionName: stringValue(item?.position_name || item?.positionName || item?.position),
    projectName: stringValue(item?.project_name || item?.projectName || item?.name),
    roleName: stringValue(item?.role_name || item?.roleName || item?.role),
    schoolName: stringValue(item?.school_name || item?.schoolName || item?.school),
    majorName: stringValue(item?.major_name || item?.majorName || item?.major),
    educationLevel: stringValue(item?.education_level || item?.educationLevel || item?.degree || item?.education),
    content: stringValue(item?.content || item?.description),
    startYm: stringValue(item?.start_ym || item?.startYm || item?.start_date || item?.startDate),
    endYm: stringValue(item?.end_ym || item?.endYm || item?.end_date || item?.endDate),
  };
}

/** salaryText 返回期望薪资文本。 */
function salaryText(source: any) {
  const min = source.expected_salary_min ?? source.expectedSalaryMin;
  const max = source.expected_salary_max ?? source.expectedSalaryMax;
  if (min && max) return `${min}-${max}/月`;
  if (min) return `${min}+/月`;
  if (max) return `${max}/月以内`;
  return stringValue(source.expected_salary || source.expectedSalary);
}
