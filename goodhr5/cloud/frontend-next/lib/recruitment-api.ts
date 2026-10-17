/** 文件作用说明：统一企业招聘流程、面试、团队模板与候选人邀请的云端接口和类型。 */
"use client";
import { cloudRequest } from "./admin-api";
import type { Resume } from "./resume-types";

export type RecruitmentMember = { email: string; name?: string; role?: string; status?: string };
export type RecruitmentNode = { id: string; name: string; type: "review" | "interview" | "approval" | "offer" | "onboarding"; owner_email: string; interviewer_emails: string[]; approver_emails: string[]; cc_emails: string[]; approval_mode: "all" | "any"; requires_feedback: boolean; mail_template_id: string; status?: string; attempt?: number; approvals?: Array<{ author_email?: string; decision?: string; content?: string }> };
export type RecruitmentWorkflow = { id: string; name: string; version: number; revision: number; is_default: boolean; nodes: RecruitmentNode[] };
export type RecruitmentInterview = { id: string; node_id: string; attempt: number; starts_at: string; duration_minutes: number; mode: string; location: string; meeting_url: string; interviewer_emails: string[]; status: string; confirmation: string; revision: number; feedback: Array<{ author_email?: string; conclusion?: string; content?: string; created_at?: string }> };
export type RecruitmentApplication = { id: string; candidate_id: string; candidate_name: string; avatar_data_url?: string; candidate_email: string; candidate?: Resume; position_id: string; position_name: string; recommendation_id: string; owner_email: string; workflow_id: string; workflow_version: number; workflow_name: string; status: string; stage: string; current_node: number; revision: number; entry_date?: string; nodes: RecruitmentNode[]; interviews: RecruitmentInterview[]; events: Array<{ id?: string; type?: string; event_type?: string; note?: string; author_email?: string; created_at?: string; notify_emails?: string[]; read_by?: string[] }>; permissions: { can_manage: boolean; can_assign?: boolean; can_view_resume?: boolean; can_advance: boolean; can_schedule: boolean; can_feedback: boolean; can_approve: boolean } };
export type RecruitmentCompany = { company_name: string; contact_name: string; contact_phone: string; contact_email: string; company_address: string };
export type RecruitmentMailTemplate = { id: string; name: string; category: string; subject: string; body: string; is_builtin?: boolean; updated_at?: string };
export type RecruitmentPlaceholder = { key: string; label: string; group: string; example: string; required?: boolean; source?: string };
export type RecruitmentPreview = { subject: string; body: string; html?: string; missing_placeholders: string[]; unknown_placeholders: string[]; can_send: boolean; is_example: boolean; warnings?: string[]; recipient?: string; reply_to?: string; context_fingerprint?: string };
export type RecruitmentDelivery = { id: string; application_id: string; interview_id?: string; template_id: string; recipient: string; reply_to: string; subject: string; body: string; status: string; error_message?: string; created_at: string; finished_at?: string };
export const recruitmentNodeTypes = [
  { value: "review", label: "简历审核" },
  { value: "interview", label: "面试" },
  { value: "approval", label: "录用审批" },
  { value: "offer", label: "Offer" },
  { value: "onboarding", label: "入职" }
];
export const recruitmentMailCategories = [
  { value: "interview_first", label: "初试邀请" },
  { value: "interview_followup", label: "复试邀请" },
  { value: "reschedule", label: "面试改期" },
  { value: "cancel", label: "面试取消" },
  { value: "result", label: "结果通知" },
  { value: "offer", label: "Offer 通知" },
  { value: "onboarding", label: "入职通知" },
  { value: "custom", label: "自定义" }
];

/** recruitmentRequest 通过已有云端请求通道访问招聘业务，不依赖本地程序。 */
export async function recruitmentRequest(path: string, method = "GET", body?: unknown) {

  return cloudRequest(`/api/recruitment${path}`, { method, body });
}
/** loadRecruitmentWorkflows 读取团队流程与可选成员，并安全处理空数组。 */
export async function loadRecruitmentWorkflows(): Promise<{ workflows: RecruitmentWorkflow[]; members: RecruitmentMember[]; can_manage: boolean }> {

  const data = await recruitmentRequest("/workflows");
  return {

    workflows: data.workflows || [],
    members: (data.members || []).filter((member: RecruitmentMember) => !member.status || member.status === "active"),
    can_manage: Boolean(data.can_manage)
  };
}
/** loadRecruitmentApplications 根据岗位、候选人或本人待办查询独立招聘进程。 */
export async function loadRecruitmentApplications(filters: Record<string, string> = {}): Promise<RecruitmentApplication[]> {

  const query = new URLSearchParams(Object.entries(filters).filter(([, value]) => Boolean(value)));
  const data = await recruitmentRequest(`/applications?${query}`);
  return data.applications || [];
}
/** loadRecruitmentApplication 读取一条招聘进程的节点、面试与操作权限。 */
export async function loadRecruitmentApplication(id: string): Promise<RecruitmentApplication> {

  const data = await recruitmentRequest(`/applications/${encodeURIComponent(id)}`);
  return { ...data.application, candidate: data.candidate || undefined };
}
/** actRecruitmentApplication 携带当前版本执行人工动作，防止旧页面覆盖最新决定。 */
export async function actRecruitmentApplication(application: RecruitmentApplication, action: string, payload: Record<string, unknown> = {}): Promise<RecruitmentApplication> {

  const data = await recruitmentRequest(`/applications/${encodeURIComponent(application.id)}/actions`, "POST", { revision: application.revision, action, ...payload });
  return data.application;
}
/** newRecruitmentNode 生成可编辑的流程节点，默认不增加审批负担。 */
export function newRecruitmentNode(type: RecruitmentNode["type"] = "interview"): RecruitmentNode {

  return {

    id: crypto.randomUUID(),
    name: recruitmentNodeTypes.find((item) => item.value === type)?.label || "面试",
    type,
    owner_email: "",
    interviewer_emails: [],
    approver_emails: [],
    cc_emails: [],
    approval_mode: "all",
    requires_feedback: type === "interview",
    mail_template_id: ""
  };
}
/** recruitmentStatusLabel 将进程、面试、通知与反馈状态转换为简短中文。 */
export function recruitmentStatusLabel(value?: string): string {

  return ({

    active: "推进中",
    completed: "已完成",
    rejected: "未通过",
    withdrawn: "已退出",
    pending: "待确认",
    scheduled: "已安排",
    cancelled: "已取消",
    confirmed: "确认参加",
    reschedule_requested: "申请改期",
    declined: "不参加",
    sent: "已发送",
    failed: "发送失败",
    uncertain: "发送结果待核实",
    sending: "发送中",
    approved: "已通过",
    pass: "通过",
    fail: "不通过",
    hold: "待定"
  } as Record<string, string>)[value || ""] || value || "待处理";
}
/** recruitmentMemberOptions 生成能按姓名或邮箱搜索的团队成员选项。 */
export function recruitmentMemberOptions(members: RecruitmentMember[]) {

  return members.map((member) => ({

    value: member.email,
    label: member.name ? `${member.name} · ${member.email}` : member.email
  }));
}
/** recruitmentEventText 将业务历史的动作和结论转换成便于团队阅读的中文。 */
export function recruitmentEventText(event: { type?: string; event_type?: string; note?: string }): string {

  const type = event.type || event.event_type || "";
  const label = ({

    started: "开启招聘",
    advanced: "完成节点",
    node_entered: "进入节点",
    returned: "退回节点",
    ended: "结束招聘",
    assigned: "调整节点人员",
    schedule: "安排面试",
    reschedule: "面试改期",
    complete_interview: "面试完成",
    cancel_interview: "取消面试",
    interviewers_assigned: "转交面试官",
    feedback: "提交面评",
    approval: "提交审核",
    entry_date: "登记入职日期",
    mail_sent: "邮件已发送",
    mail_failed: "邮件发送失败",
    mail_uncertain: "邮件发送结果待核实",
    mail_sending: "邮件发送中",
    candidate_confirmed: "候选人确认参加",
    candidate_reschedule_requested: "候选人申请改期",
    candidate_declined: "候选人不参加"
  } as Record<string, string>)[type] || "招聘记录";
  const note = String(event.note || "").replace(/^(pass|fail|hold|approved|rejected|withdrawn)：/, (_, value) => `${recruitmentStatusLabel(value)}：`);
  return note ? `${label}：${note}` : label;
}
