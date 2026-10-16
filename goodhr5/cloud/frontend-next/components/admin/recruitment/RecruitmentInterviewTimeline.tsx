/** 文件作用说明：按招聘轮次展示倒序面试时间线、安排信息和逐位评价，全部操作复用详情页原有处理。 */
"use client";
import { Button, Dropdown, Empty, Tag, Typography, type MenuProps } from "antd";
import { CalendarOutlined, CheckCircleFilled, ClockCircleOutlined, DownOutlined, EnvironmentOutlined, VideoCameraOutlined } from "@ant-design/icons";
import { formatDate } from "@/lib/admin-api";
import { recruitmentStatusLabel, type RecruitmentApplication, type RecruitmentInterview, type RecruitmentMember } from "@/lib/recruitment-api";
import styles from "./recruitment-detail.module.css";

type Props = {
  application: RecruitmentApplication;
  members: RecruitmentMember[];
  userEmail?: string;
  busy: boolean;
  hasTemplates: boolean;
  onSchedule: (item: RecruitmentInterview) => void;
  onComplete: (item: RecruitmentInterview) => void;
  onCancel: (item: RecruitmentInterview) => void;
  onTransfer: (item: RecruitmentInterview) => void;
  onFeedback: (item: RecruitmentInterview) => void;
  onMail: (item: RecruitmentInterview) => void;
};

/** interviewTime 生成本地日期与起止时间，无效时间安全回退到待补充。 */
function interviewTime(item: RecruitmentInterview) {
  const start = new Date(item.starts_at);
  if (Number.isNaN(start.getTime())) return "面试时间待补充";
  const end = new Date(start.getTime() + (item.duration_minutes || 0) * 60000);
  const options: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hour12: false };
  return `${start.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit", weekday: "short" })} ${start.toLocaleTimeString("zh-CN", options)}～${end.toLocaleTimeString("zh-CN", options)}`;
}

/** groupInterviews 按节点归组并按最新时间倒序，保留流程退回后的全部历史场次。 */
function groupInterviews(application: RecruitmentApplication) {
  const groups = new Map<string, { id: string; name: string; items: RecruitmentInterview[]; latest: number; complete: boolean }>();
  for (const item of application.interviews || []) {
    const node = (application.nodes || []).find((entry) => entry.id === item.node_id);
    const group = groups.get(item.node_id) || { id: item.node_id, name: node?.name || "面试", items: [], latest: 0, complete: node?.status === "completed" };
    group.items.push(item);
    group.latest = Math.max(group.latest, Date.parse(item.starts_at) || 0);
    groups.set(item.node_id, group);
  }
  return [...groups.values()].sort((left, right) => right.latest - left.latest).map((group) => ({ ...group, items: group.items.sort((left, right) => (Date.parse(right.starts_at) || 0) - (Date.parse(left.starts_at) || 0)) }));
}

/** RecruitmentInterviewTimeline 延用服务端权限，将安排和面评分区展示，不扩展历史节点操作权限。 */
export default function RecruitmentInterviewTimeline({ application, members, userEmail, busy, hasTemplates, onSchedule, onComplete, onCancel, onTransfer, onFeedback, onMail }: Props) {
  const node = application.nodes?.[application.current_node];
  const permissions = application.permissions;
  const active = application.status === "active";
  /** memberName 优先展示团队姓名，离开团队的历史人员保留邮箱。 */
  function memberName(email?: string) { return members.find((member) => member.email === email)?.name || email || "未记录人员"; }
  /** interviewActions 按原有权限和轮次生成菜单，操作继续调用详情页现有函数。 */
  function interviewActions(item: RecruitmentInterview) {
    const items: NonNullable<MenuProps["items"]> = [];
    const current = item.node_id === node?.id && item.attempt === node?.attempt;
    if (active && permissions?.can_schedule && current && item.status !== "cancelled" && item.status !== "completed") {
      items.push({ key: "reschedule", label: "改期", disabled: busy, onClick: () => onSchedule(item) });
      items.push({ key: "complete", label: "完成面试", disabled: busy || new Date(item.starts_at).getTime() > Date.now(), onClick: () => onComplete(item) });
      items.push({ key: "cancel", label: "取消面试", danger: true, disabled: busy, onClick: () => onCancel(item) });
    }
    if (active && permissions?.can_manage && current && item.status !== "cancelled") items.push({ key: "interviewers", label: "转交面试官", disabled: busy, onClick: () => onTransfer(item) });
    if (permissions?.can_manage || permissions?.can_schedule) items.push({ key: "mail", label: "发送通知", disabled: busy || !hasTemplates, onClick: () => onMail(item) });
    return items;
  }
  if (!application.interviews?.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没安排面试。时间确认后，再给候选人发邀请。" />;
  return <div className={styles.interviewTimeline} data-testid="recruitment-interview-timeline">{groupInterviews(application).map((group) => <section key={group.id} className={styles.interviewGroup}>
    <span className={`${styles.timelineDot} ${group.complete ? styles.timelineComplete : ""}`} aria-hidden="true">{group.complete ? <CheckCircleFilled /> : <ClockCircleOutlined />}</span>
    <Typography.Title level={5} className={styles.groupTitle}>{group.name}</Typography.Title>
    {group.items.map((item) => {
      const actions = interviewActions(item);
      const feedbackEntries = item.feedback || [];
      const missingReviewers = [...new Set(item.interviewer_emails || [])].filter((email) => !feedbackEntries.some((feedback) => feedback.author_email === email));
      const canFeedback = active && permissions?.can_feedback && item.node_id === node?.id && item.attempt === node?.attempt && item.interviewer_emails?.includes(userEmail || "") && item.status !== "cancelled";
      return <article key={item.id} className={styles.interviewCard} data-testid={`recruitment-interview-${item.id}`} aria-label={`${group.name}面试安排与评价`}>
        <div className={styles.interviewArrangement}>
          <div className={styles.arrangementHeading}>
            <div className={styles.interviewTime}><Typography.Text strong>{interviewTime(item)}</Typography.Text><Tag icon={item.mode === "video" ? <VideoCameraOutlined /> : item.mode === "onsite" ? <EnvironmentOutlined /> : <CalendarOutlined />}>{item.mode === "onsite" ? "现场面试" : item.mode === "phone" ? "电话面试" : "视频面试"}</Tag></div>
            <div className={styles.interviewControls}><Tag color={item.confirmation === "confirmed" ? "success" : undefined}>{recruitmentStatusLabel(item.confirmation)}</Tag>{actions.length ? <Dropdown trigger={["click"]} menu={{ items: actions }}><Button disabled={busy}>更多操作 <DownOutlined /></Button></Dropdown> : null}</div>
          </div>
          <div className={styles.arrangementFacts}>
            <div><span className={styles.factLabel}>面试官</span><span>{(item.interviewer_emails || []).map(memberName).join("、") || "未分配"}</span></div>
            <div><span className={styles.factLabel}>面试地点</span><span>{item.mode === "onsite" ? item.location || "待补充" : item.mode === "phone" ? "电话沟通" : "线上面试"}</span></div>
            <div><span className={styles.factLabel}>面试状态</span><span>{recruitmentStatusLabel(item.status)} · {item.duration_minutes} 分钟</span></div>
            <div><span className={styles.factLabel}>会议链接</span><span>{item.mode === "video" && /^https?:\/\//i.test(item.meeting_url || "") ? <a href={item.meeting_url} target="_blank" rel="noopener noreferrer">查看会议链接</a> : item.mode === "video" ? "待补充" : "—"}</span></div>
          </div>
        </div>
        <div className={styles.feedbackList}>
          {feedbackEntries.map((feedback, index) => <div key={`${feedback.author_email || "feedback"}-${index}`} className={styles.feedbackEntry}>
            <div className={styles.feedbackHeading}><Typography.Text>{memberName(feedback.author_email)}{members.find((member) => member.email === feedback.author_email)?.name ? <span className={styles.meta}>（{feedback.author_email}）</span> : null}</Typography.Text><Tag color={feedback.conclusion === "pass" ? "success" : feedback.conclusion === "fail" ? "error" : "warning"}>{recruitmentStatusLabel(feedback.conclusion)}</Tag></div>
            {feedback.created_at ? <div className={styles.meta}>评价时间：{formatDate(feedback.created_at)}</div> : null}
            <Typography.Paragraph className={styles.feedbackContent} ellipsis={{ rows: 4, expandable: "collapsible", symbol: (expanded) => expanded ? "收起详情" : "展开详情" }}>{feedback.content || "未填写文字评价"}</Typography.Paragraph>
          </div>)}
          {missingReviewers.map((email) => <div key={email} className={styles.pendingFeedback}><Typography.Text>{memberName(email)}</Typography.Text><Tag>未评价</Tag></div>)}
          {!feedbackEntries.length && !missingReviewers.length ? <Typography.Text type="secondary">还没有面试评价。</Typography.Text> : null}
          {canFeedback ? <div className={styles.feedbackActions}><Button type="link" disabled={busy || new Date(item.starts_at).getTime() > Date.now()} title="面试开始后才可填写评价" onClick={() => onFeedback(item)}>写面试评价</Button></div> : null}
        </div>
      </article>;
    })}
  </section>)}</div>;
}
