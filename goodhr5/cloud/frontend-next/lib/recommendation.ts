/** 本文件定义候选人推荐报告、公开沟通记录和前端安全整理方法。 */

export type RecommendationPoint = {
  title: string;
  detail: string;
  severity: string;
  evidence: string[];
};

export type RecommendationCondition = {
  id: string;
  itemType: string;
  content: string;
  status: string;
  statusReason: string;
  evidence: string[];
};

export type RecommendationExperience = {
  companyName: string;
  positionName: string;
  schoolName: string;
  majorName: string;
  educationLevel: string;
  projectName: string;
  roleName: string;
  content: string;
  startYm: string;
  endYm: string;
};

export type RecommendationCertificate = {
  certificateName: string;
  issuedBy: string;
  issuedYm: string;
};

export type RecommendationHonor = {
  honorName: string;
  issuedBy: string;
  issuedYm: string;
  description: string;
};

export type RecommendationCandidate = {
  name: string;
  avatarURL: string;
  gender: string;
  birthYm: string;
  phone: string;
  email: string;
  wechat: string;
  workRegion: string;
  workYears: string;
  educationLevel: string;
  expectedPosition: string;
  expectedSalaryMin: number | null;
  expectedSalaryMax: number | null;
  workStatus: string;
  onlineStatus: string;
  personalDescription: string;
  basicInfo: string;
  workExperiences: RecommendationExperience[];
  educations: RecommendationExperience[];
  certificates: RecommendationCertificate[];
  honors: RecommendationHonor[];
  projectExperiences: RecommendationExperience[];
  createdAt: string;
  updatedAt: string;
};

export type CandidateRecommendation = {
  publicID: string;
  version: number;
  status: string;
  matchScore: number;
  recommendationLevel: string;
  summary: string;
  shareEnabled: boolean;
  sourceCutoffAt: string;
  generatedAt: string;
  report: {
    candidate: RecommendationCandidate;
    position: { id: string; name: string; platformID: string; description: string; companyName: string; address: string; contact: string; overview: string; extraInfo: string };
    matchScore: number;
    recommendationLevel: string;
    executiveSummary: string;
    strengths: RecommendationPoint[];
    risks: RecommendationPoint[];
    conditions: RecommendationCondition[];
    bonusItems: RecommendationPoint[];
    unconfirmedItems: RecommendationPoint[];
    jobIntentSummary: string;
    stabilitySummary: string;
    communicationSummary: string;
    interviewFocus: RecommendationPoint[];
    suggestedQuestions: string[];
  };
};

export type RecommendationConversation = {
  id: string;
  platformID: string;
  positionText: string;
  candidateName: string;
  messages: Array<{ id: string; direction: string; messageType: string; textContent: string; senderName: string; platformSentAt: string; createdAt: string }>;
};

/** normalizeRecommendation 把云端推荐记录整理成公开页和后台共用的稳定结构。 */
export function normalizeRecommendation(value: unknown): CandidateRecommendation {
  const source = recordValue(value);
  const report = recordValue(source.report);
  const candidate = recordValue(report.candidate);
  const position = recordValue(report.position);
  return {
    publicID: text(source.public_id), version: number(source.version), status: text(source.status),
    matchScore: number(source.match_score), recommendationLevel: text(source.recommendation_level),
    summary: text(source.summary), shareEnabled: source.share_enabled !== false,
    sourceCutoffAt: text(source.source_cutoff_at), generatedAt: text(source.generated_at),
    report: {
      candidate: {
        name: text(candidate.name), avatarURL: text(candidate.avatar_url), gender: text(candidate.gender),
        birthYm: text(candidate.birth_ym), phone: text(candidate.phone), email: text(candidate.email), wechat: text(candidate.wechat),
        workRegion: text(candidate.work_region), workYears: text(candidate.work_years), educationLevel: text(candidate.education_level),
        expectedPosition: text(candidate.expected_position), expectedSalaryMin: nullableNumber(candidate.expected_salary_min),
        expectedSalaryMax: nullableNumber(candidate.expected_salary_max), workStatus: text(candidate.work_status),
        onlineStatus: text(candidate.online_status),
        personalDescription: text(candidate.personal_description), basicInfo: text(candidate.basic_info),
        workExperiences: recordArray(candidate.work_experiences).map(normalizeExperience),
        educations: recordArray(candidate.educations).map(normalizeExperience),
        certificates: recordArray(candidate.certificates).map((item) => ({
          certificateName: text(item.certificate_name), issuedBy: text(item.issued_by), issuedYm: text(item.issued_ym),
        })),
        honors: recordArray(candidate.honors).map((item) => ({
          honorName: text(item.honor_name), issuedBy: text(item.issued_by), issuedYm: text(item.issued_ym), description: text(item.description),
        })),
        projectExperiences: recordArray(candidate.project_experiences).map(normalizeExperience),
        createdAt: text(candidate.created_at), updatedAt: text(candidate.updated_at),
      },
      position: {
        id: text(position.id), name: text(position.name), platformID: text(position.platform_id), description: text(position.description),
        companyName: text(position.company_name), address: text(position.address), contact: text(position.contact),
        overview: text(position.overview), extraInfo: text(position.extra_info),
      },
      matchScore: number(report.match_score), recommendationLevel: text(report.recommendation_level),
      executiveSummary: text(report.executive_summary), strengths: recordArray(report.strengths).map(normalizePoint),
      risks: recordArray(report.risks).map(normalizePoint), conditions: recordArray(report.conditions).map((item) => ({
        id: text(item.id), itemType: text(item.item_type), content: text(item.content), status: text(item.status),
        statusReason: text(item.status_reason), evidence: stringArray(item.evidence),
      })),
      bonusItems: recordArray(report.bonus_items).map(normalizePoint), unconfirmedItems: recordArray(report.unconfirmed_items).map(normalizePoint),
      jobIntentSummary: text(report.job_intent_summary), stabilitySummary: text(report.stability_summary),
      communicationSummary: text(report.communication_summary), interviewFocus: recordArray(report.interview_focus).map(normalizePoint),
      suggestedQuestions: stringArray(report.suggested_questions),
    },
  };
}

/** normalizeRecommendationConversations 整理公开接口返回的真实双方沟通记录。 */
export function normalizeRecommendationConversations(value: unknown): RecommendationConversation[] {
  return recordArray(value).map((item) => ({
    id: text(item.id), platformID: text(item.platform_id), positionText: text(item.position_text), candidateName: text(item.candidate_name),
    messages: recordArray(item.messages).map((message) => ({
      id: text(message.id), direction: text(message.direction), messageType: text(message.message_type),
      textContent: text(message.text_content), senderName: text(message.sender_name),
      platformSentAt: text(message.platform_sent_at), createdAt: text(message.created_at),
    })),
  }));
}

/** recommendationCopyText 生成包含完整联系方式和公共链接的面试转发文本。 */
export function recommendationCopyText(item: CandidateRecommendation, publicURL: string) {
  const candidate = item.report.candidate;
  const lines = [
    `候选人：${candidate.name || "未命名候选人"}`,
    `推荐岗位：${item.report.position.name || "未填写"}`,
    `性别：${candidate.gender || "未填写"}`,
    `出生年月：${candidate.birthYm || "未填写"}`,
    `学历：${candidate.educationLevel || "未填写"}`,
    `工作年限：${candidate.workYears || "未填写"}`,
    `手机：${candidate.phone || "未填写"}`,
    `邮箱：${candidate.email || "未填写"}`,
    `微信：${candidate.wechat || "未填写"}`,
    `当前地区：${candidate.workRegion || "未填写"}`,
    `匹配分：${Math.round(item.matchScore)}分（${item.recommendationLevel || "待确认"}）`,
    `推荐摘要：${item.report.executiveSummary || item.summary || "暂无"}`,
    "",
    `完整推荐报告：${publicURL}`,
  ];
  return lines.join("\n");
}

/** normalizePoint 整理一条优势、风险或面试关注点。 */
function normalizePoint(item: Record<string, unknown>): RecommendationPoint {
  return { title: text(item.title), detail: text(item.detail), severity: text(item.severity), evidence: stringArray(item.evidence) };
}

/** normalizeExperience 整理工作、教育或项目经历字段。 */
function normalizeExperience(item: Record<string, unknown>): RecommendationExperience {
  return {
    companyName: text(item.company_name), positionName: text(item.position_name), schoolName: text(item.school_name),
    majorName: text(item.major_name), educationLevel: text(item.education_level), projectName: text(item.project_name),
    roleName: text(item.role_name), content: text(item.content), startYm: text(item.start_ym), endYm: text(item.end_ym),
  };
}

/** recordValue 把未知值安全收窄为普通对象。 */
function recordValue(value: unknown): Record<string, unknown> {
  return value != null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

/** recordArray 只保留未知数组中的普通对象。 */
function recordArray(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.map(recordValue) : [];
}

/** stringArray 清理未知字符串数组中的空值。 */
function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.map(text).filter(Boolean) : [];
}

/** text 把未知值安全转换为去除首尾空白的文字。 */
function text(value: unknown) {
  return value == null ? "" : String(value).trim();
}

/** number 把未知值转换为有限数字。 */
function number(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

/** nullableNumber 把缺失数字保留为空，避免页面显示虚假的零。 */
function nullableNumber(value: unknown) {
  if (value == null || value === "") return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}
