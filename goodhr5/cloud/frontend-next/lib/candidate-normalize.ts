/** 本文件负责把 GoodHR 新版候选人接口数据整理成简历库展示结构。 */

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

export type NormalizedNote = {
  id: string;
  content: string;
  authorEmail: string;
  createdAt: string;
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
  certificates: any[];
  honors: any[];
  projectExperiences: NormalizedExperience[];
  communications: any[];
  rawText: string;
  aiFirstAnalysis: { score: unknown; reason: string };
  aiSecondAnalysis: { score: unknown; reason: string };
  notes: NormalizedNote[];
  creatorEmail: string;
  createdAt: string;
  updatedAt: string;
  raw: any;
};

/** normalizeCandidate 按新版扁平简历模型整理候选人。 */
export function normalizeCandidate(input: any): NormalizedCandidate {
  const source = input || {};
  return {
    id: stringValue(source.id),
    engagementId: stringValue(source.engagement_id),
    status: stringValue(source.engagement_status || "created"),
    name: stringValue(source.candidate_name || "未命名候选人"),
    avatarUrl: "",
    age: ageFromBirthYM(source.birth_ym),
    gender: "",
    workRegion: stringValue(source.work_region),
    workYears: stringValue(source.work_years),
    educationLevel: stringValue(source.education_level),
    expectedPosition: stringValue(source.expected_position),
    expectedSalary: salaryText(source.expected_salary_min, source.expected_salary_max),
    workStatus: stringValue(source.work_status),
    onlineStatus: stringValue(source.online_status),
    personalDescription: stringValue(source.personal_description || source.basic_info),
    workExperiences: arrayValue(source.work_experiences).map((item) => ({
      companyName: stringValue(item.company_name),
      positionName: stringValue(item.position_name),
      content: stringValue(item.content),
      startYm: stringValue(item.start_ym),
      endYm: stringValue(item.end_ym),
    })),
    educations: arrayValue(source.educations).map((item) => ({
      schoolName: stringValue(item.school_name),
      majorName: stringValue(item.major_name),
      educationLevel: stringValue(item.education_level),
      startYm: stringValue(item.start_ym),
      endYm: stringValue(item.end_ym),
    })),
    certificates: arrayValue(source.certificates),
    honors: arrayValue(source.honors),
    projectExperiences: arrayValue(source.project_experiences).map((item) => ({
      projectName: stringValue(item.project_name),
      roleName: stringValue(item.role_name),
      content: stringValue(item.content),
      startYm: stringValue(item.start_ym),
      endYm: stringValue(item.end_ym),
    })),
    communications: arrayValue(source.colleague_communications),
    rawText: stringValue(source.raw_text),
    aiFirstAnalysis: { score: source.ai?.detail?.score, reason: stringValue(source.ai?.detail?.reason) },
    aiSecondAnalysis: { score: source.ai?.greet?.score, reason: stringValue(source.ai?.greet?.reason) },
    notes: arrayValue(source.notes).map((item) => ({
      id: stringValue(item.id),
      content: stringValue(item.content),
      authorEmail: stringValue(item.author_email),
      createdAt: stringValue(item.created_at),
    })),
    creatorEmail: stringValue(source.user_email),
    createdAt: stringValue(source.created_at),
    updatedAt: stringValue(source.updated_at),
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

/** salaryText 返回薪资展示文本。 */
function salaryText(min: unknown, max: unknown) {
  const left = Number(min);
  const right = Number(max);
  if (Number.isFinite(left) && Number.isFinite(right)) return `${left}-${right}K`;
  if (Number.isFinite(left)) return `${left}K起`;
  if (Number.isFinite(right)) return `${right}K以内`;
  return "";
}

/** ageFromBirthYM 根据出生年月粗略计算年龄。 */
function ageFromBirthYM(birthYM: unknown) {
  const text = stringValue(birthYM);
  const year = Number(text.slice(0, 4));
  return Number.isFinite(year) && year > 1900 ? String(new Date().getFullYear() - year) : "";
}
