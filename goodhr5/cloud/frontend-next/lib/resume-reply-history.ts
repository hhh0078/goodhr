/** 文件作用说明：定义候选人自动回复历史的服务器展示协议和增量合并规则，不在本地保存业务数据。 */
import type { ReplyAttachment } from "../components/admin/resumes/ResumeReplyAttachment";

export type ReplyOnlineProfile = {
  candidate_name: string; age: number | null; education_level: string;
  earliest_company_name: string; earliest_company_year: number | null;
  earliest_school_name: string; earliest_school_year: number | null;
  mode: string; match_status: string; is_temporary: boolean; updated_at: string;
};
export type ReplyInfoRequest = { kind: "resume" | "phone" | "wechat"; status: string; status_label: string; updated_at: string };
export type ReplyThreadSnapshot = {
  online_profile?: ReplyOnlineProfile; contacts?: { phone: string; wechat: string }; info_requests?: ReplyInfoRequest[];
};
export type ReplyThread = ReplyThreadSnapshot & { id: string; position_name: string; platform: string; candidate_name: string; status: string; updated_at: string };
export type ReplyMessage = { id: string; side: string; sender_type: string; text: string; sent_at: string; message_type?: string; attachments?: ReplyAttachment[] };
export type ReplyEvent = { id: string; title: string; description: string; occurred_at: string; status: string; status_label?: string; duration_ms?: number | null };
export type ReplyGoal = { id: string; title: string; status: string; source_type?: string; evidence: string; evidence_message_ids?: string[]; updated_at: string };
export type ReplyHistory = ReplyThreadSnapshot & {
  threads: ReplyThread[]; thread_id: string; messages: ReplyMessage[]; events: ReplyEvent[]; goals: ReplyGoal[];
  next_message_cursor: string; next_event_cursor: string;
};

const statuses: Record<string, [string, string]> = {
  pending: ["待确认", "default"], planned: ["待申请", "default"], waiting: ["等待中", "default"],
  waiting_candidate: ["等待候选人", "default"], waiting_hr: ["等待 HR", "warning"], waiting_resume: ["等待简历", "default"],
  running: ["进行中", "processing"], processing: ["处理中", "processing"], start: ["开始", "processing"],
  in_progress: ["进行中", "processing"], active: ["沟通中", "processing"], completed: ["已完成", "success"],
  done: ["已完成", "success"], sent: ["已发送", "success"], received: ["已收到", "success"],
  success: ["成功", "success"], succeeded: ["成功", "success"], matched: ["已满足", "success"],
  unmet: ["不满足", "warning"], unmatched: ["不满足", "warning"], skipped: ["已跳过", "default"],
  failed: ["失败", "error"], error: ["失败", "error"], blocked: ["受阻", "warning"], stopped: ["已停止", "default"],
  ready: ["准备就绪", "success"], loading: ["处理中", "processing"], result: ["已完成", "success"], executing: ["执行中", "processing"],
  requested: ["已申请", "processing"], already_requested: ["已申请过", "default"], declined: ["已拒绝", "default"],
  deferred: ["暂缓申请", "default"], unavailable: ["暂不支持", "default"], unknown: ["不确定，暂不重复", "warning"],
  warning: ["需留意", "warning"], not_requested: ["尚未申请", "default"], idle: ["等待消息", "default"],
};

/** replyHistoryStatus 为已知状态提供中文和语义色，未知协议值不直接显示英文，也不冒充成功。 */
export function replyHistoryStatus(status: string, serverLabel?: string): [string, string] {
  const known = statuses[status];
  if (status === "unknown") return known;
  const label = serverLabel?.trim();
  return [label && /[\u4e00-\u9fff]/.test(label) ? label : known?.[0] || "暂未确认", known?.[1] || "default"];
}

/** historyHasMessageAnchor 判断最新窗口是否接上已有尾部，未接上时应先补页，不能把中间缺口当成完整历史。 */
export function historyHasMessageAnchor(current: ReplyHistory, fresh: ReplyHistory): boolean {
  return !current.messages.length || fresh.messages.some(row => row.id === current.messages.at(-1)?.id);
}

/** historyHasEventAnchor 判断新操作窗口是否接上已有首条，避免高频操作超过一页后漏掉中间事件。 */
export function historyHasEventAnchor(current: ReplyHistory, fresh: ReplyHistory): boolean {
  return !current.events.length || fresh.events.some(row => row.id === current.events[0]?.id);
}

/** mergeReplyHistoryRefresh 仅合并已衔接的最新窗口，保留旧分页、原有行顺序和稳定DOM键，同时更新同编号的回执。 */
export function mergeReplyHistoryRefresh(current: ReplyHistory, fresh: ReplyHistory): ReplyHistory {
  if (current.thread_id !== fresh.thread_id || !historyHasMessageAnchor(current, fresh) || !historyHasEventAnchor(current, fresh)) {
    throw new Error("沟通记录还没接完整，保留当前位置，稍后自动再核对。");
  }
  const latestMessages = new Map(fresh.messages.map(row => [row.id, row]));
  const latestEvents = new Map(fresh.events.map(row => [row.id, row]));
  const messageAnchor = fresh.messages.findIndex(row => row.id === current.messages.at(-1)?.id);
  const eventAnchor = fresh.events.findIndex(row => row.id === current.events[0]?.id);
  return {
    ...fresh,
    messages: current.messages.length ? [...current.messages.map(row => latestMessages.get(row.id) || row), ...fresh.messages.slice(messageAnchor + 1)] : fresh.messages,
    events: current.events.length ? [...fresh.events.slice(0, eventAnchor), ...current.events.map(row => latestEvents.get(row.id) || row)] : fresh.events,
    next_message_cursor: current.messages.length ? current.next_message_cursor : fresh.next_message_cursor,
    next_event_cursor: current.events.length ? current.next_event_cursor : fresh.next_event_cursor,
  };
}
