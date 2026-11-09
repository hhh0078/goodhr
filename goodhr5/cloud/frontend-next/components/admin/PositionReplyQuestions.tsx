/** 本文件按岗位游标分页展示待确认问题，标题展开原话，HR 回复草稿在切换岗位和翻页时留在当前面板内。 */
"use client";

import { Alert, Button, Collapse, Empty, Flex, Input, Spin, Table, Tag, Typography } from "antd";
import { useCallback, useEffect, useRef, useState } from "react";
import { APIRequestError, cloudRequest, getToken } from "@/lib/admin-api";
import { loadReplyInboxPage, normalizeReplyQuestion, type ReplyInboxView, type ReplyQuestion } from "@/lib/auto-reply-inbox";
import { useAdmin } from "./AdminApp";
import styles from "./auto-reply-inbox.module.css";

type Props = { positionID: string; view: ReplyInboxView; active: boolean; onApplied: () => void };
type AnswerDraft = { text: string; version: number };
const statusText: Record<string, string> = { pending: "待回复", processing: "整理中", resolved: "已补充", failed: "待重试" };
const targets: Record<string, string> = { position: "已补充岗位信息", company: "已补充公司信息", both: "已补充岗位与公司信息" };

/** QuestionTitle 点击问题标题展开真实候选人原话；被清理的来源保持明确的空状态。 */
function QuestionTitle({ row }: { row: ReplyQuestion }) {
  return <Collapse ghost className={styles.questionTitle} items={[{ key: row.id, label: row.question, children:
    row.source_messages.length ? row.source_messages.map((source, index) => <div className={styles.source} key={source.message_id || index}>
      <div className={styles.sourceName}>{source.candidate_name || "候选人"}</div>
      <div className={styles.sourceText}>{source.text || "这条原话暂时没有正文。"}</div>
    </div>) : <Typography.Text type="secondary">原话已清理</Typography.Text> }]} />;
}

/** AnswerEditor 共用桌面表格和手机卡片的答复、状态与重试操作。 */
function AnswerEditor({ row, value, busyID, conflict, onChange, onReply, onReview }: {
  row: ReplyQuestion; value: string; busyID: string; conflict: boolean; onChange: (value: string) => void; onReply: () => void; onReview: () => void;
}) {
  return <div className={styles.answer}>
    <Input.TextArea aria-label={`回复${row.question}`} value={value} autoSize={{ minRows: 2, maxRows: 6 }} maxLength={2000}
      disabled={Boolean(busyID)} placeholder="填写已经核实、可以告诉候选人的答案。" onChange={event => onChange(event.target.value)} />
    {conflict && <Alert type="warning" showIcon title="这条答复刚有更新，你的草稿还在。"
      description={<><div className={styles.sourceText}>最新已保存答复：{row.answer || "目前还没有已保存答复。"}</div>
        <Button disabled={Boolean(busyID)} onClick={onReview} style={{ marginTop: 8 }}>已核对，继续用我的草稿</Button></>} />}
    <div className={styles.answerMeta}>
      <Button type="primary" loading={busyID === row.id} disabled={Boolean(busyID) || conflict || !value.trim()} onClick={onReply}>保存回复</Button>
      <Tag color={row.status === "resolved" ? "success" : row.status === "failed" ? "warning" : undefined}>{statusText[row.status] || "待确认"}</Tag>
    </div>
    {row.status === "resolved" && row.publish_target && <div className={styles.answerInfo}>{targets[row.publish_target] || "已保存补充"}</div>}
    {row.error && <Typography.Text type="danger">{row.error}</Typography.Text>}
  </div>;
}

/** PositionReplyQuestions 仅轮询正在打开的当前页，保存和换页均不会丢掉其他题目的未提交草稿。 */
export default function PositionReplyQuestions({ positionID, view, active, onApplied }: Props) {
  const { notify } = useAdmin();
  const [rows, setRows] = useState<ReplyQuestion[] | null>(null);
  const [drafts, setDrafts] = useState<Record<string, AnswerDraft>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);
  const [busyID, setBusyID] = useState("");
  const [loadedScope, setLoadedScope] = useState("");
  const [nextCursor, setNextCursor] = useState("");
  const [pages, setPages] = useState({ scope: "", cursors: [""], index: 0 });
  const operation = useRef(false);
  const epoch = useRef(0);
  const pendingRead = useRef<AbortController | null>(null);
  const scope = `${positionID}:${view}`;
  const pageIndex = pages.scope === scope ? pages.index : 0;
  const cursors = pages.scope === scope ? pages.cursors : [""];
  const cursor = cursors[pageIndex] || "";
  const pageScope = `${scope}:${cursor}`;
  const visibleRows = loadedScope === pageScope ? rows : null;

  /** load 读取当前页；失败保留已展示记录与草稿，迟到响应不能覆盖其他岗位或页码。 */
  const load = useCallback(async (silent = false) => {
    if (!active || !positionID || pendingRead.current) return;
    const controller = new AbortController();
    pendingRead.current = controller;
    const current = epoch.current;
    const timeout = window.setTimeout(() => controller.abort(), 30000);
    if (!silent) setLoading(true);
    try {
      const data = await loadReplyInboxPage(positionID, view, cursor, controller.signal);
      if (current === epoch.current) {
        setRows(data.questions); setNextCursor(data.next_cursor); setLoadedScope(pageScope); setError("");
      }
    } catch (cause) {
      if (current === epoch.current && cursor && cause instanceof APIRequestError && cause.status === 400) {
        setPages({ scope, cursors: [""], index: 0 });
        setNotice("列表刚有更新，已回到第一页，填写的草稿还在。");
        setError("");
        return;
      }
      if (current === epoch.current) setError(controller.signal.aborted ? "待确认问题读取超时，请重试。" : cause instanceof Error ? cause.message : "待确认问题没读出来，可以重试。");
    } finally {
      window.clearTimeout(timeout);
      if (pendingRead.current === controller) { pendingRead.current = null; setLoading(false); }
    }
  }, [active, positionID, view, cursor, pageScope, scope]);

  useEffect(() => { setNotice(""); }, [scope]);

  useEffect(() => {
    epoch.current += 1; pendingRead.current?.abort(); pendingRead.current = null;
    setRows(null); setNextCursor(""); setError(""); setLoadedScope("");
    if (!active) return;
    void load();
    const timer = window.setInterval(() => {
      if (!operation.current && document.visibilityState === "visible") void load(true);
    }, 10000);
    return () => { epoch.current += 1; pendingRead.current?.abort(); pendingRead.current = null; window.clearInterval(timer); };
  }, [active, load]);

  /** reply 使用问题版本保存 HR 答复，成功后自动移出待补充列表，历史及其他草稿继续保留。 */
  async function reply(row: ReplyQuestion) {
    const draft = drafts[row.id];
    const answer = (draft?.text ?? row.answer).trim();
    if (operation.current || !answer || draft && draft.version !== row.version) return;
    const token = getToken();
    operation.current = true; setBusyID(row.id);
    pendingRead.current?.abort(); pendingRead.current = null; setLoading(false);
    const current = ++epoch.current;
    try {
      const data = await cloudRequest(`/api/auto-reply/positions/${encodeURIComponent(positionID)}/questions/reply`, {
        method: "POST", body: { id: row.id, answer, version: draft?.version ?? row.version },
      });
      if (current !== epoch.current || getToken() !== token) return;
      const saved = normalizeReplyQuestion(data.question);
      if (saved.id !== row.id || saved.position_id && saved.position_id !== positionID) throw new Error("答复结果暂时没核实，请稍后查看这条问题。");
      const resolved = saved.status === "resolved";
      // 保存接口不附带原话时，保留当前已核实来源，随后以分页读取结果更新，避免短暂误报“原话已清理”。
      setRows(items => items?.flatMap(item => item.id !== row.id ? [item] : view === "pending" && resolved ? []
        : [{ ...saved, source_messages: Array.isArray(data.question.source_messages) ? saved.source_messages : item.source_messages }]) ?? null);
      if (resolved) setDrafts(items => { const next = { ...items }; delete next[row.id]; return next; });
      // 本次归档失败仍是自己的同一份答复，接受服务端领取版本以便直接重试；并发更新仍由后续轮询再次拦住。
      else setDrafts(items => items[row.id] ? { ...items, [row.id]: { ...items[row.id], version: saved.version } } : items);
      notify(resolved ? "已补入对应资料。这条已处理，可在全部记录里查看。" : saved.error || "答复已保存，归档还没完成，可以稍后再试。", resolved ? "success" : "warning");
      onApplied();
      await load(true);
    } catch (cause) {
      if (current === epoch.current) notify(cause instanceof Error ? cause.message : "这次没补充成功，回答还在，可以重试。", "error");
      if (current === epoch.current && cause instanceof APIRequestError && cause.status === 409) await load(true);
    } finally { operation.current = false; setBusyID(""); }
  }

  /** reviewDraft 仅在 HR 看过当前服务器答复后绑定这次版本，之后再变更仍需重新核对。 */
  function reviewDraft(row: ReplyQuestion) {
    setDrafts(items => items[row.id] ? { ...items, [row.id]: { ...items[row.id], version: row.version } } : items);
  }

  /** answer 为不同屏幕复用同一个问题草稿和写入入口。 */
  function answer(row: ReplyQuestion) {
    const draft = drafts[row.id];
    return <AnswerEditor row={row} value={draft?.text ?? row.answer} busyID={busyID} conflict={Boolean(draft && draft.version !== row.version)}
      onChange={text => setDrafts(values => ({ ...values, [row.id]: { text, version: values[row.id]?.version ?? row.version } }))}
      onReply={() => void reply(row)} onReview={() => reviewDraft(row)} />;
  }

  /** movePage 仅使用当前查询收到的已核对游标翻页，关闭面板不写入任何本地缓存。 */
  function movePage(direction: -1 | 1) {
    if (busyID || loading) return;
    if (direction === 1 && nextCursor) setPages({ scope, cursors: [...cursors.slice(0, pageIndex + 1), nextCursor], index: pageIndex + 1 });
    else if (direction === -1 && pageIndex > 0) setPages({ scope, cursors, index: pageIndex - 1 });
  }

  return <section aria-label="岗位待确认问题">
    {notice && <Alert type="info" showIcon title={notice} style={{ marginBottom: 12 }} />}
    {error && <Alert type="warning" showIcon title={error} action={<Button disabled={Boolean(busyID)} loading={loading} onClick={() => void load()}>重试</Button>} style={{ marginBottom: 12 }} />}
    {visibleRows === null && (!error || loading) && <Flex role="status" align="center" gap={8}><Spin size="small" /><Typography.Text type="secondary">正在读取待确认问题…</Typography.Text></Flex>}
    {visibleRows !== null && (visibleRows.length ? <>
      <div className={styles.desktopTable}><Table<ReplyQuestion> rowKey="id" size="small" dataSource={visibleRows} loading={loading} pagination={false} tableLayout="fixed" columns={[
        { title: "待确认问题 · 点标题看原话", width: "42%", render: (_, row) => <QuestionTitle row={row} /> },
        { title: "HR 回复", render: (_, row) => answer(row) },
      ]} /></div>
      <div className={styles.mobileList}>{visibleRows.map(row => <article className={styles.mobileRow} key={row.id}><QuestionTitle row={row} />{answer(row)}</article>)}</div>
    </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={view === "pending" ? "这个岗位暂时没有需要补充的问题。" : "这个岗位还没有待确认记录。"} />)}
    {visibleRows !== null && (pageIndex > 0 || nextCursor) && <nav aria-label="待确认问题分页" className={`${styles.pager} goodhr-table-pagination`}>
      <Button disabled={Boolean(busyID) || loading || pageIndex === 0} onClick={() => movePage(-1)}>上一页</Button>
      <Typography.Text type="secondary">第 {pageIndex + 1} 页</Typography.Text>
      <Button disabled={Boolean(busyID) || loading || !nextCursor} onClick={() => movePage(1)}>下一页</Button>
    </nav>}
  </section>;
}
