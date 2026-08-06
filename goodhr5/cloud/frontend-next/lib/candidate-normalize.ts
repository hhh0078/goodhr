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

export type CandidateResumeAttachment = {
  id: string;
  originalName: string;
  mimeType: string;
  sizeBytes: number;
  downloadURL: string;
  createdAt: string;
};

export type CandidateMessage = {
  id: string;
  direction: string;
  messageType: string;
  textContent: string;
  senderName: string;
  platformSentAt: string;
  createdAt: string;
};

export type CandidateConfirmationItem = {
  id: string;
  itemType: string;
  content: string;
  status: string;
  sourceType: string;
  evidenceText: string;
  summary: string;
  updatedAt: string;
};

export type CandidateConversation = {
  id: string;
  platformID: string;
  positionText: string;
  status: string;
  createdAt: string;
  updatedAt: string;
  messages: CandidateMessage[];
  confirmationItems: CandidateConfirmationItem[];
};

export type CandidateAIToolCall = {
  id: string;
  sequenceNo: number;
  toolName: string;
  status: string;
  arguments: unknown;
  result: unknown;
  errorMessage: string;
};

export type CandidateAIRecord = {
  id: string;
  positionName: string;
  platformID: string;
  model: string;
  status: string;
  inputMessages: unknown;
  outputMessage: unknown;
  errorMessage: string;
  tokenUsage: number;
  startedAt: string;
  completedAt: string;
  toolCalls: CandidateAIToolCall[];
};

export type CandidateAutoReplyDetail = {
  attachments: CandidateResumeAttachment[];
  conversations: CandidateConversation[];
  aiRecords: CandidateAIRecord[];
};

export type NormalizedCandidate = {
  id: string;
  engagementId: string;
  status: string;
  name: string;
  avatarUrl: string;
  age: string;
  gender: string;
  phone: string;
  email: string;
  wechat: string;
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
  autoReply: CandidateAutoReplyDetail;
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
    gender: stringValue(source.gender),
    phone: stringValue(source.phone),
    email: stringValue(source.email),
    wechat: stringValue(source.wechat),
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
    autoReply: normalizeCandidateAutoReply(source.auto_reply),
    creatorEmail: stringValue(source.user_email),
    createdAt: stringValue(source.created_at),
    updatedAt: stringValue(source.updated_at),
    raw: source,
  };
}

/** normalizeCandidateAutoReply 整理简历附件、沟通记录、确认项和 AI 审计数据。 */
function normalizeCandidateAutoReply(value: unknown): CandidateAutoReplyDetail {
  const source = recordValue(value);
  return {
    attachments: recordArray(source.attachments).map((item) => ({
      id: stringValue(item.id),
      originalName: stringValue(item.original_name),
      mimeType: stringValue(item.mime_type),
      sizeBytes: numberValue(item.size_bytes),
      downloadURL: stringValue(item.download_url),
      createdAt: stringValue(item.created_at),
    })),
    conversations: recordArray(source.conversations).map((item) => ({
      id: stringValue(item.id),
      platformID: stringValue(item.platform_id),
      positionText: stringValue(item.page_position_text),
      status: stringValue(item.status),
      createdAt: stringValue(item.created_at),
      updatedAt: stringValue(item.updated_at),
      messages: recordArray(item.messages).map((message) => ({
        id: stringValue(message.id),
        direction: stringValue(message.direction),
        messageType: stringValue(message.message_type),
        textContent: stringValue(message.text_content),
        senderName: stringValue(message.sender_name),
        platformSentAt: stringValue(message.platform_sent_at),
        createdAt: stringValue(message.created_at),
      })),
      confirmationItems: recordArray(item.confirmation_items).map((confirmation) => ({
        id: stringValue(confirmation.id),
        itemType: stringValue(confirmation.item_type),
        content: stringValue(confirmation.content),
        status: stringValue(confirmation.status),
        sourceType: stringValue(confirmation.source_type),
        evidenceText: stringValue(confirmation.evidence_text),
        summary: stringValue(confirmation.summary),
        updatedAt: stringValue(confirmation.updated_at),
      })),
    })),
    aiRecords: recordArray(source.ai_records).map((item) => ({
      id: stringValue(item.id),
      positionName: stringValue(item.position_name),
      platformID: stringValue(item.platform_id),
      model: stringValue(item.model),
      status: stringValue(item.status),
      inputMessages: item.input_messages,
      outputMessage: item.output_message,
      errorMessage: stringValue(item.error_message),
      tokenUsage: numberValue(item.token_usage),
      startedAt: stringValue(item.started_at),
      completedAt: stringValue(item.completed_at),
      toolCalls: recordArray(item.tool_calls).map((tool) => ({
        id: stringValue(tool.id),
        sequenceNo: numberValue(tool.sequence_no),
        toolName: stringValue(tool.tool_name),
        status: stringValue(tool.status),
        arguments: tool.arguments_json,
        result: tool.result_json,
        errorMessage: stringValue(tool.error_message),
      })),
    })),
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

/** recordValue 把未知 JSON 安全收敛为普通对象。 */
function recordValue(value: unknown): Record<string, unknown> {
  return value != null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

/** recordArray 把未知 JSON 数组中的对象安全收敛为普通对象。 */
function recordArray(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.map(recordValue) : [];
}

/** numberValue 把未知 JSON 安全转换为有限数字。 */
function numberValue(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
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
