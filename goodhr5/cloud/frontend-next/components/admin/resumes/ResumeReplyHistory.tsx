/** 文件作用说明：在授权候选人详情展示云端会话、已确认发送的 AI 消息、操作时间线和目标证据。 */
"use client";

import { Alert, Button, Checkbox, Empty, Flex, Progress, Select, Skeleton, Tabs, Tag, Typography } from "antd";
import { ClockCircleOutlined, MessageOutlined } from "@ant-design/icons";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { cloudRequest, getToken } from "@/lib/admin-api";
import { sourceLabel } from "@/lib/resume-types";
import { historyHasEventAnchor, historyHasMessageAnchor, mergeReplyHistoryRefresh, replyHistoryStatus,
  type ReplyThread as Thread, type ReplyMessage as Message, type ReplyEvent as Event, type ReplyGoal as Goal, type ReplyHistory as History } from "@/lib/resume-reply-history";
import ResumeReplyAttachment from "./ResumeReplyAttachment";
import ResumeReplyProfile from "./ResumeReplyProfile";
import styles from "./resume-reply-history.module.css";

type PageKind = "first" | "messages" | "events" | "eventFilter" | "refresh";

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

/** HistoryStatus 优先使用服务端中文标签，未知动作结果明确提示暂不重复。 */
function HistoryStatus({ status, label }: { status: string; label?: string }) {
  const [text, color] = replyHistoryStatus(status, label);
  return <Tag color={color}>{text}</Tag>;
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
  const position = useRef({ top: 0, atEnd: true });
  useLayoutEffect(() => {
    const node = viewport.current;
    if (!node) return;
    if (first.current || distance.current !== null || position.current.atEnd) node.scrollTop = node.scrollHeight - (distance.current ?? 0);
    else node.scrollTop = position.current.top;
    first.current = false;
    distance.current = null;
    position.current = { top: node.scrollTop, atEnd: node.scrollHeight - node.scrollTop - node.clientHeight < 24 };
  }, [items]);
  /** rememberPosition 在用户阅读旧消息时保留滚动位置，停在底部时才跟随新消息。 */
  function rememberPosition() {
    const node = viewport.current;
    if (node) position.current = { top: node.scrollTop, atEnd: node.scrollHeight - node.scrollTop - node.clientHeight < 24 };
  }
  /** loadEarlier 记录加载前的阅读位置，翻页只会在该位置之前补入消息。 */
  function loadEarlier() {
    if (viewport.current) distance.current = viewport.current.scrollHeight - viewport.current.scrollTop;
    onLoad();
  }
  return <>
    {hasMore && <Button className={styles.more} loading={loading} onClick={loadEarlier}>查看更早消息</Button>}
    <div ref={viewport} className={styles.chatViewport} onScroll={rememberPosition} tabIndex={items.length ? 0 : undefined} aria-label="聊天记录滚动区">
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
      {goal.source_type && <Tag>{goal.source_type === "ai" ? "AI 沟通目标" : "岗位条件"}</Tag>}
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
    <div className={styles.goalHeading}><strong>{event.title || "自动回复操作"}</strong>{event.status && <HistoryStatus status={event.status} label={event.status_label} />}</div>
    {event.description && <p>{event.description.startsWith("AI 工具完成：") ? event.description.split("，结果 ")[0] : event.description}</p>}<div className={styles.meta}><ClockCircleOutlined aria-hidden="true" />
      <time dateTime={event.occurred_at}>{event.occurred_at ? historyTime(event.occurred_at) : "时间未记录"}</time>
      {typeof event.duration_ms === "number" && event.duration_ms >= 0 && <span>耗时 {event.duration_ms < 1000 ? event.duration_ms + " 毫秒" : Number((event.duration_ms / 1000).toFixed(2)) + " 秒"}</span>}</div>
  </li>)}</ol>;
}

/** ResumeReplyHistory 按候选人和会话隔离分页，仅可见时串行更新，保留已加载历史、当前标签和阅读位置。 */
export default function ResumeReplyHistory({ candidateID }: { candidateID: string }) {
  const [history, setHistory] = useState<History | null>(null);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [chosenID, setChosenID] = useState("");
  const [busy, setBusy] = useState<PageKind | null>("first");
  const [error, setError] = useState<{ kind: PageKind; verify: boolean; text: string } | null>(null);
  const [showVerification, setShowVerification] = useState(false);
  const [section, setSection] = useState("messages");
  const [visible, setVisible] = useState(false);
  const panel = useRef<HTMLElement>(null);
  const pending = useRef<{ controller: AbortController; kind: PageKind } | null>(null);
  const generation = useRef(0);
  const visibleNow = useRef(false);
  const current = useRef({ history, chosenID, showVerification });
  current.current = { history, chosenID, showVerification };

  /** load 复用同一云端分页接口，前台翻页优先于轮询，迟到或换账号的响应不更新当前候选人。 */
  async function load(kind: PageKind, threadID: string, verify = current.current.showVerification) {
    if (!visibleNow.current) return;
    if (pending.current) {
      if (kind === "first" || kind !== "refresh" && pending.current.kind === "refresh") {
        pending.current.controller.abort(); pending.current = null;
      } else return;
    }
    const previous = current.current.history;
    if (kind !== "first" && !previous) return;
    const controller = new AbortController();
    const operation = { controller, kind };
    pending.current = operation;
    const epoch = generation.current;
    const token = getToken();
    let timedOut = false;
    const timeout = window.setTimeout(() => { timedOut = true; controller.abort(); }, 15000);
    if (kind !== "refresh") { setBusy(kind); setError(null); }
    const query = new URLSearchParams({ section: "history" });
    if (verify) query.set("event_group", "all");
    if (threadID) query.set("thread_id", threadID);
    if (kind === "messages") query.set("message_cursor", previous!.next_message_cursor);
    if (kind === "events") query.set("event_cursor", previous!.next_event_cursor);
    /** readPage 每次读取都验证固定协议和所选会话，不接受空缺字段或其它候选人的回包。 */
    async function readPage(params: URLSearchParams): Promise<History> {
      const result = await cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}/auto-reply?${params}`, { signal: controller.signal });
      const page = result?.auto_reply as History | undefined;
      if (!page || !Array.isArray(page.threads) || !Array.isArray(page.messages) || !Array.isArray(page.events) || !Array.isArray(page.goals)) throw new Error("沟通记录没读完整，请重试。");
      if (threadID && page.thread_id !== threadID) throw new Error("本次读取的会话不匹配，请重新选择。");
      return page;
    }
    try {
      let page = await readPage(query);
      if (controller.signal.aborted || epoch !== generation.current || token !== getToken()) return;
      if (kind === "refresh" && previous) {
        // 高频新记录超过一页时，先补到已知锚点；最多补四页，不把中间空洞拼成完整历史。
        for (let attempt = 0; attempt < 4 && (!historyHasMessageAnchor(previous, page) || !historyHasEventAnchor(previous, page)); attempt++) {
          const needMessages = !historyHasMessageAnchor(previous, page);
          const needEvents = !historyHasEventAnchor(previous, page);
          if (needMessages && !page.next_message_cursor || needEvents && !page.next_event_cursor) break;
          const next = new URLSearchParams(query);
          if (needMessages) next.set("message_cursor", page.next_message_cursor);
          if (needEvents) next.set("event_cursor", page.next_event_cursor);
          const extra = await readPage(next);
          if (controller.signal.aborted || epoch !== generation.current || token !== getToken()) return;
          page = { ...page,
            messages: needMessages ? uniqueRows(extra.messages, page.messages) : page.messages,
            events: needEvents ? uniqueRows(page.events, extra.events) : page.events,
            next_message_cursor: needMessages ? extra.next_message_cursor : page.next_message_cursor,
            next_event_cursor: needEvents ? extra.next_event_cursor : page.next_event_cursor,
          };
        }
        page = mergeReplyHistoryRefresh(previous, page);
      }
      if (controller.signal.aborted || epoch !== generation.current || token !== getToken() || !visibleNow.current) return;
      if (kind === "first" || kind === "refresh") {
        setHistory(page); setThreads(page.threads);
        if (kind === "refresh") setError(old => old?.kind === "refresh" ? null : old);
      } else if (kind === "eventFilter") {
        setShowVerification(verify);
        setHistory(old => old && ({ ...old, events: page.events, next_event_cursor: page.next_event_cursor }));
      } else if (kind === "messages") setHistory(old => old && ({ ...old, messages: uniqueRows(page.messages, old.messages), next_message_cursor: page.next_message_cursor }));
      else setHistory(old => old && ({ ...old, events: uniqueRows(old.events, page.events), next_event_cursor: page.next_event_cursor }));
    } catch (cause) {
      if ((!controller.signal.aborted || timedOut) && epoch === generation.current && token === getToken() && visibleNow.current) setError({ kind, verify, text: timedOut ? "沟通记录更新超时，已有内容保留，可以重试。" : cause instanceof Error ? cause.message : "沟通记录暂时没读出来，可以重试。" });
    } finally {
      window.clearTimeout(timeout);
      if (pending.current === operation) { pending.current = null; if (kind !== "refresh") setBusy(null); }
    }
  }
  const loadNow = useRef(load);
  loadNow.current = load;

  useEffect(() => {
    generation.current += 1; pending.current?.controller.abort(); pending.current = null;
    current.current.history = null;
    setHistory(null); setThreads([]); setSection("messages"); setShowVerification(false); setError(null); setBusy(null);
    return () => { generation.current += 1; pending.current?.controller.abort(); pending.current = null; };
  }, [candidateID, chosenID]);

  useEffect(() => {
    const node = panel.current;
    if (!node) return;
    let intersecting = false;
    /** updateVisibility 关闭弹框、隐藏标签或后台页面时立即停止在途读取，不丢弃已经展开的内容。 */
    function updateVisibility() {
      const next = intersecting && document.visibilityState === "visible";
      visibleNow.current = next;
      setVisible(next);
      if (!next) { pending.current?.controller.abort(); pending.current = null; setBusy(null); }
    }
    const observer = new IntersectionObserver(entries => {
      intersecting = entries.some(entry => entry.isIntersecting && entry.intersectionRatio > 0);
      updateVisibility();
    });
    observer.observe(node);
    document.addEventListener("visibilitychange", updateVisibility);
    return () => {
      visibleNow.current = false; observer.disconnect(); document.removeEventListener("visibilitychange", updateVisibility);
      pending.current?.controller.abort(); pending.current = null;
    };
  }, []);

  useEffect(() => {
    if (!visible) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    /** pollHistory 沿用后台十秒轻轮询方式，上一次完成后才排下一次，不叠加请求或重置阅读位置。 */
    async function pollHistory() {
      const state = current.current;
      await loadNow.current(state.history ? "refresh" : "first", state.chosenID || state.history?.thread_id || "", state.showVerification);
      if (!stopped) timer = setTimeout(() => void pollHistory(), 10000);
    }
    void pollHistory();
    return () => { stopped = true; clearTimeout(timer); };
  }, [visible, candidateID, chosenID]);

  const activeID = chosenID || history?.thread_id || "";
  const thread = threads.find(item => item.id === activeID);
  const resolved = history?.goals.filter(goalResolved).length || 0;
  return <section ref={panel} className={styles.panel} aria-label="自动回复沟通记录">
    <div className={styles.heading}><Typography.Title level={5}><MessageOutlined aria-hidden="true" /> 沟通记录</Typography.Title>
      {threads.length > 1 && <Select aria-label="选择沟通会话" value={activeID || undefined} onChange={setChosenID} className={styles.select}
        options={threads.map((item, index) => ({ value: item.id, label: [item.platform ? sourceLabel(item.platform) : "", item.position_name || "未记录岗位", historyTime(item.updated_at) === "时间未记录" ? "会话 " + (index + 1) : historyTime(item.updated_at)].filter(Boolean).join(" · ") }))} />}</div>
    {error && <Alert type="warning" showIcon title={error.text} action={<Button onClick={() => void load(error.kind, activeID, error.verify)}>重试</Button>} />}
    {busy === "first" ? <Skeleton active paragraph={{ rows: 3 }} /> : history && (thread ? <>
      <div className={styles.summary}><div><div className={styles.goalHeading}><strong>{thread.position_name || "未记录岗位"}</strong>{thread.status && <HistoryStatus status={thread.status} />}</div>
        <span className={styles.muted}>{[thread.platform ? sourceLabel(thread.platform) : "", thread.updated_at ? "最近更新 " + historyTime(thread.updated_at) : ""].filter(Boolean).join(" · ")}</span></div>
        <div className={styles.progress}><span role="status" aria-live="polite" aria-atomic="true">已核实目标 <b>{resolved} / {history.goals.length}</b></span><Progress percent={history.goals.length ? Math.round(resolved * 100 / history.goals.length) : 0} size="small" showInfo={false} /></div></div>
      <ResumeReplyProfile snapshot={history} formatTime={historyTime} />
      <Tabs activeKey={section} onChange={setSection} items={[
        { key: "messages", label: "聊天记录", children: <ChatPane key={thread.id} items={history.messages} name={thread.candidate_name} candidateID={candidateID} threadID={thread.id} hasMore={Boolean(history.next_message_cursor)} loading={busy === "messages"} onLoad={() => void load("messages", activeID)} /> },
        { key: "events", label: "操作时间线", children: <div aria-busy={busy === "eventFilter"}>
          <Checkbox checked={showVerification} disabled={Boolean(busy)} style={{ marginBottom: 12 }} onChange={e => void load("eventFilter", activeID, e.target.checked)}>查看核验操作</Checkbox>
          {busy === "eventFilter" ? <Skeleton active paragraph={{ rows: 2 }} /> : <OperationList events={history.events} />}
          {history.next_event_cursor && <Button className={styles.more} loading={busy === "events"} disabled={Boolean(busy)} onClick={() => void load("events", activeID)}>查看更多操作</Button>}
        </div> },
        { key: "goals", label: "沟通目标", children: <GoalList goals={history.goals} messages={history.messages} /> },
      ]} />
    </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这份简历还没有自动回复沟通记录。" />)}
  </section>;
}
