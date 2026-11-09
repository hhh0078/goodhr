/** 文件作用说明：在授权候选人详情展示云端会话、已确认发送的 AI 消息、操作时间线和目标证据。 */
"use client";

import { Alert, Button, Checkbox, Empty, Flex, Progress, Select, Skeleton, Tabs, Tag, Typography } from "antd";
import { ClockCircleOutlined, MessageOutlined } from "@ant-design/icons";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { sourceLabel } from "@/lib/resume-types";
import ResumeReplyAttachment, { type ReplyAttachment } from "./ResumeReplyAttachment";
import styles from "./resume-reply-history.module.css";

type Thread = { id: string; position_name: string; platform: string; candidate_name: string; status: string; updated_at: string };
type Message = { id: string; side: string; sender_type: string; text: string; sent_at: string; message_type?: string; attachments?: ReplyAttachment[] };
type Event = { id: string; title: string; description: string; occurred_at: string; status: string; duration_ms?: number | null };
type Goal = { id: string; title: string; status: string; evidence: string; evidence_message_ids?: string[]; updated_at: string };
type History = {
  threads: Thread[]; thread_id: string; messages: Message[]; events: Event[]; goals: Goal[];
  next_message_cursor: string; next_event_cursor: string;
};
type PageKind = "first" | "messages" | "events" | "eventFilter";

const statuses: Record<string, [string, string]> = {
  pending: ["待确认", "default"], waiting: ["等待中", "default"], waiting_candidate: ["等待候选人", "default"],
  waiting_hr: ["等待 HR", "warning"], running: ["进行中", "processing"], processing: ["处理中", "processing"],
  in_progress: ["进行中", "processing"], active: ["沟通中", "processing"], completed: ["已完成", "success"],
  done: ["已完成", "success"], sent: ["已发送", "success"], received: ["已收到", "success"],
  success: ["成功", "success"], succeeded: ["成功", "success"], matched: ["已满足", "success"],
  unmet: ["不满足", "warning"], unmatched: ["不满足", "warning"], skipped: ["已跳过", "default"],
  failed: ["失败", "error"], error: ["失败", "error"], blocked: ["受阻", "warning"], stopped: ["已停止", "default"],
  ready: ["准备就绪", "success"], loading: ["处理中", "processing"], result: ["已完成", "success"], executing: ["执行中", "processing"],
};

const dateTime = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
});

/** historyTime 保留操作秒数，缺失的日期和 Go 零值不冒充实际发生时间。 */
function historyTime(value?: string) {
  if (!value || value.startsWith("0001-")) return "时间未记录";
  const date = new Date(value);
  if (!Number.isNaN(date.getTime())) return dateTime.format(date);
  return /^\d{1,2}:\d{2}(:\d{2})?$/.test(value) ? value : "时间未记录";
}

/** HistoryStatus 将服务端状态映射到统一文字，未知状态保留原始名称。 */
function HistoryStatus({ status }: { status: string }) {
  const [label, color] = statuses[status] || [status || "未记录状态", "default"];
  return <Tag color={color}>{label}</Tag>;
}

/** uniqueRows 按服务端顺序合并相邻分页，避免重复呈现跨页边界的同一记录。 */
function uniqueRows<T extends { id: string }>(first: T[], next: T[]) {
  const rows = new Map<string, T>();
  for (const item of [...first, ...next]) if (item?.id && !rows.has(item.id)) rows.set(item.id, item);
  return Array.from(rows.values());
}

/** messageText 展示平台消息正文，只有消息类型的旧记录仍可辨认。 */
function messageText(item: Message) {
  if (item.text?.trim()) return item.text;
  const types: Record<string, string> = { attachment: "附件", file: "文件", resume: "简历附件", image: "图片", voice: "语音", card: "卡片" };
  const label = types[item.message_type || ""];
  return label ? "［" + label + "］" : "这条消息没有记录正文";
}

/** ChatMessages 区分候选人、人工和未确认来源，仅将确证的招聘方 AI 消息标为 AI。 */
function ChatMessages({ items, name, candidateID, threadID }: { items: Message[]; name?: string; candidateID:string; threadID:string }) {
  if (!items.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这段沟通还没有聊天记录。" />;
  return <ol className={styles.messages} aria-label="双方聊天记录，按时间从早到晚">
    {items.map(item => {
      const outgoing = item.side === "recruiter";
      const ai = outgoing && item.sender_type === "ai";
      const sender = outgoing ? (item.sender_type === "human" ? "HR 手动发送" : "招聘方") : item.side === "candidate" ? name || "候选人" : "发送方未记录";
      return <li key={item.id} className={outgoing ? styles.outgoing : ""}>
        <div className={styles.meta}><strong>{sender}</strong>{ai && <Tag color="success">AI 自动发送</Tag>}{outgoing && !ai && item.sender_type !== "human" && <span>来源未记录</span>}<time dateTime={item.sent_at}>{item.sent_at ? historyTime(item.sent_at) : "时间未记录"}</time></div>
        <div className={styles.bubble}>{messageText(item)}</div>
        {item.attachments?.filter(file => file.id && file.source_message_id === item.id).map(file =>
          <ResumeReplyAttachment key={file.id} attachment={file} candidateID={candidateID} threadID={threadID} />)}
      </li>;
    })}
  </ol>;
}

/** ChatPane 首次打开定位到最近的消息，向前加载后保留当前阅读位置。 */
function ChatPane({ items, name, candidateID, threadID, hasMore, loading, onLoad }: {
  items: Message[]; name?: string; candidateID:string; threadID:string; hasMore: boolean; loading: boolean; onLoad: () => void;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const distance = useRef<number | null>(null);
  const first = useRef(true);
  useLayoutEffect(() => {
    const node = viewport.current;
    if (!node) return;
    if (first.current || distance.current !== null) node.scrollTop = node.scrollHeight - (distance.current ?? 0);
    first.current = false;
    distance.current = null;
  }, [items]);
  /** loadEarlier 记录加载前的阅读位置，翻页只会在该位置之前补入消息。 */
  function loadEarlier() {
    if (viewport.current) distance.current = viewport.current.scrollHeight - viewport.current.scrollTop;
    onLoad();
  }
  return <>
    {hasMore && <Button className={styles.more} loading={loading} onClick={loadEarlier}>查看更早消息</Button>}
    <div ref={viewport} className={styles.chatViewport} tabIndex={items.length ? 0 : undefined} aria-label="聊天记录滚动区">
      <ChatMessages items={items} name={name} candidateID={candidateID} threadID={threadID} />
    </div>
  </>;
}

/** goalResolved 仅识别已经得到结论的目标，包括确实不满足的目标。 */
function goalResolved(item: Goal) {
  return ["matched", "unmatched", "unmet", "done", "completed"].includes(item.status);
}

/** GoalList 分开展示条件结论和对应的原始证据，缺少记录时明示。 */
function GoalList({ goals, messages }: { goals: Goal[]; messages: Message[] }) {
  if (!goals.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这段沟通还没有生成目标。" />;
  const messagesByID = new Map(messages.map(message => [message.id, message]));
  return <ul className={styles.goals}>{goals.map(goal => {
    const references = (goal.evidence_message_ids || []).map(id => messagesByID.get(id)).filter((item): item is Message => Boolean(item));
    return <li key={goal.id}><div className={styles.goalHeading}><strong>{goal.title}</strong><HistoryStatus status={goal.status} /></div>
      {goal.evidence && <blockquote>{goal.evidence}</blockquote>}
      {references.length > 0 && <details><summary>查看关联原话（{references.length}）</summary>{references.map(item => <blockquote key={item.id}>{messageText(item)} <small>{historyTime(item.sent_at)}</small></blockquote>)}</details>}
      {!goal.evidence && !references.length && <span className={styles.muted}>{goal.evidence_message_ids?.length ? "关联原话在更早的聊天记录中。" : goalResolved(goal) ? "这项目标没留下判断依据。" : "等待新的简历或聊天证据。"}</span>}
      {goal.updated_at && <time className={styles.muted} dateTime={goal.updated_at}>{historyTime(goal.updated_at)}</time>}
    </li>;
  })}</ul>;
}

/** OperationList 按最近发生的顺序展示操作结果及服务端实际记录的耗时。 */
function OperationList({ events }: { events: Event[] }) {
  if (!events.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有记录操作过程。" />;
  return <ol className={styles.events} aria-label="操作时间线，最近在前">{events.map(event => <li key={event.id}>
    <div className={styles.goalHeading}><strong>{event.title || "自动回复操作"}</strong>{event.status && <HistoryStatus status={event.status} />}</div>
    {event.description && <p>{event.description.startsWith("AI 工具完成：") ? event.description.split("，结果 ")[0] : event.description}</p>}<div className={styles.meta}><ClockCircleOutlined aria-hidden="true" />
      <time dateTime={event.occurred_at}>{event.occurred_at ? historyTime(event.occurred_at) : "时间未记录"}</time>
      {typeof event.duration_ms === "number" && event.duration_ms >= 0 && <span>耗时 {event.duration_ms < 1000 ? event.duration_ms + " 毫秒" : Number((event.duration_ms / 1000).toFixed(2)) + " 秒"}</span>}</div>
  </li>)}</ol>;
}

/** ResumeReplyHistory 按候选人和会话隔离分页，迟到的请求不能覆盖已切换的会话。 */
export default function ResumeReplyHistory({ candidateID }: { candidateID: string }) {
  const [history, setHistory] = useState<History | null>(null);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [chosenID, setChosenID] = useState("");
  const [busy, setBusy] = useState<PageKind | null>("first");
  const [error, setError] = useState<{ kind: PageKind; verify: boolean; text: string } | null>(null);
  const [showVerification, setShowVerification] = useState(false);
  const pending = useRef<AbortController | null>(null);
  const [section, setSection] = useState("messages");

  /** load 读取首屏或向前分页，所有页只使用服务器返回的会话标识和游标。 */
  async function load(kind: PageKind, threadID: string, verify = showVerification) {
    if (kind !== "first" && (!history || pending.current)) return;
    pending.current?.abort();
    const request = new AbortController();
    pending.current = request;
    setBusy(kind); setError(null);
    const query = new URLSearchParams({ section: "history" });
    if (verify) query.set("event_group","all");
    if (threadID) query.set("thread_id", threadID);
    if (kind === "messages") query.set("message_cursor", history!.next_message_cursor);
    if (kind === "events") query.set("event_cursor", history!.next_event_cursor);
    try {
      const result = await cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}/auto-reply?${query}`, {signal:request.signal});
      if (request.signal.aborted) return;
      const page = result?.auto_reply as History | undefined;
      if (!page || !Array.isArray(page.threads) || !Array.isArray(page.messages) || !Array.isArray(page.events) || !Array.isArray(page.goals)) throw new Error("沟通记录没读完整，请重试。");
      if (threadID && page.thread_id !== threadID) throw new Error("本次读取的会话不匹配，请重新选择。");
      if (kind === "first") { setHistory(page); setThreads(page.threads); }
      else if (kind === "eventFilter") {
        setShowVerification(verify);
        setHistory(current => current && ({ ...current, events: page.events, next_event_cursor: page.next_event_cursor }));
      }
      else if (kind === "messages") setHistory(current => current && ({...current, messages: uniqueRows(page.messages, current.messages), next_message_cursor: page.next_message_cursor}));
      else setHistory(current => current && ({...current, events: uniqueRows(current.events, page.events), next_event_cursor: page.next_event_cursor}));
    } catch (cause) {
      if (!request.signal.aborted) setError({ kind, verify, text: cause instanceof Error ? cause.message : "沟通记录暂时没读出来，可以重试。" });
    } finally {
      if (pending.current === request) { pending.current = null; setBusy(null); }
    }
  }
  useEffect(() => {
    setHistory(null); setSection("messages"); setShowVerification(false); void load("first", chosenID, false);
    return () => { pending.current?.abort(); pending.current = null; };
  }, [candidateID, chosenID]);

  const activeID = chosenID || history?.thread_id || "";
  const thread = threads.find(item => item.id === activeID);
  const resolved = history?.goals.filter(goalResolved).length || 0;
  return <section className={styles.panel} aria-label="自动回复沟通记录">
    <div className={styles.heading}><Typography.Title level={5}><MessageOutlined aria-hidden="true" /> 沟通记录</Typography.Title>
      {threads.length > 1 && <Select aria-label="选择沟通会话" value={activeID || undefined} onChange={setChosenID} className={styles.select}
        options={threads.map((item, index) => ({ value:item.id, label:[item.platform ? sourceLabel(item.platform) : "", item.position_name || "未记录岗位", historyTime(item.updated_at) === "时间未记录" ? "会话 " + (index + 1) : historyTime(item.updated_at)].filter(Boolean).join(" · ") }))} />}</div>
    {error && <Alert type="warning" showIcon title={error.text} action={<Button size="small" onClick={() => void load(error.kind, activeID, error.verify)}>重试</Button>} />}
    {busy === "first" ? <Skeleton active paragraph={{rows: 3}} /> : history && (thread ? <>
      <div className={styles.summary}><div><div className={styles.goalHeading}><strong>{thread.position_name || "未记录岗位"}</strong>{thread.status && <HistoryStatus status={thread.status} />}</div>
        <span className={styles.muted}>{[thread.platform ? sourceLabel(thread.platform) : "",thread.updated_at ? "最近更新 " + historyTime(thread.updated_at) : ""].filter(Boolean).join(" · ")}</span></div>
        {history.goals.length > 0 && <div className={styles.progress}><span>已核实目标 <b>{resolved} / {history.goals.length}</b></span><Progress percent={Math.round(resolved * 100 / history.goals.length)} size="small" showInfo={false} /></div>}</div>
      <Tabs activeKey={section} onChange={setSection} items={[
        {key:"messages",label:"聊天记录",children:<ChatPane key={thread.id} items={history.messages} name={thread.candidate_name} candidateID={candidateID} threadID={thread.id} hasMore={Boolean(history.next_message_cursor)} loading={busy === "messages"} onLoad={() => void load("messages",activeID)} />},
        {key:"events",label:"操作时间线",children:<div aria-busy={busy === "eventFilter"}>
          <Checkbox checked={showVerification} disabled={Boolean(busy)} style={{marginBottom:12}} onChange={e => void load("eventFilter",activeID,e.target.checked)}>查看核验操作</Checkbox>
          {busy === "eventFilter" ? <Skeleton active paragraph={{rows:2}} /> : <OperationList events={history.events}/>}
          {history.next_event_cursor && <Button className={styles.more} loading={busy==="events"} disabled={Boolean(busy)} onClick={() => void load("events",activeID)}>查看更多操作</Button>}
        </div>},
        {key:"goals",label:"沟通目标",children:<GoalList goals={history.goals} messages={history.messages}/>},
      ]} />
    </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这份简历还没有自动回复沟通记录。" />)}
  </section>;
}
