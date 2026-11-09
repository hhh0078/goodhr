/** 本文件展示服务器保存的岗位待确认问题、候选人原话与 HR 回复，并反馈已确认资料的归档结果。 */
"use client";
import { Alert, Button, Collapse, Flex, Input, Spin, Table, Tag, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { useAdmin } from "./AdminApp";

type Question = {
  id: string;
  question: string;
  source_messages: Array<{ message_id: string; text: string; candidate_name: string }>;
  answer: string;
  status: string;
  publish_target: string;
  error?: string;
  version: number;
};

/** PositionReplyQuestions 按登录账号与岗位读取云端问题，提交期间保留输入并防止重复点击。 */
export default function PositionReplyQuestions({ positionID, onApplied }: {
  positionID: string;
  onApplied?: () => void;
}) {
  const { notify } = useAdmin();
  const [rows, setRows] = useState<Question[] | null>(null);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [busyID, setBusyID] = useState("");
  const operation = useRef(false);
  const epoch = useRef(0);
  const pendingRead = useRef<AbortController | null>(null);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => {
      if (!operation.current && document.visibilityState === "visible") void load(true);
    }, 10000);
    return () => { epoch.current += 1; pendingRead.current?.abort(); pendingRead.current = null; window.clearInterval(timer); };
  }, [positionID]);

  /** load 从当前岗位的云端记录读取问题和原话。 */
  async function load(silent = false) {
    if (pendingRead.current) return;
    const controller = new AbortController();
    pendingRead.current = controller;
    const current = epoch.current;
    const timeout = window.setTimeout(() => controller.abort(), 30000);
    if (!silent) setLoading(true);
    try {
      const data = await cloudRequest(`/api/auto-reply/positions/${encodeURIComponent(positionID)}/questions`, {signal: controller.signal});
      if (current === epoch.current) {
        setRows(Array.isArray(data.questions) ? data.questions : []);
        setError("");
      }
    } catch (e) {
      if (current === epoch.current) setError(controller.signal.aborted ? "待确认问题读取超时，请重试。" : e instanceof Error ? e.message : "待确认问题没读出来，请重试。");
    } finally {
      window.clearTimeout(timeout);
      if (pendingRead.current === controller) { pendingRead.current = null; setLoading(false); }
    }
  }

  /** reply 将 HR 已核实的回答交给服务器 AI，成功后显示岗位或公司资料的更新结果。 */
  async function reply(row: Question) {
    const answer = (drafts[row.id] ?? row.answer ?? "").trim();
    if (operation.current || !answer) return;
    operation.current = true;
    setBusyID(row.id);
    // 写操作优先；提交前的轮询结果不能把旧版本覆盖到已保存的答复上。
    pendingRead.current?.abort();
    pendingRead.current = null;
    setLoading(false);
    const current = ++epoch.current;
    try {
      const data = await cloudRequest(`/api/auto-reply/positions/${encodeURIComponent(positionID)}/questions/reply`, {
        method: "POST", body: { id: row.id, answer, version: row.version },
      });
      if (current !== epoch.current) return;
      if (data.question?.id) setRows(items => items?.map(item => item.id === row.id ? data.question : item) ?? null);
      setDrafts(items => { const next = { ...items }; delete next[row.id]; return next; });
      notify("回答已保存，已确认的信息会补入对应资料。", "success");
      onApplied?.();
      await load();
    } catch (e) {
      if (current === epoch.current) notify(e instanceof Error ? e.message : "这次没补充成功，回答还在，可以重试。", "error");
    } finally {
      operation.current = false;
      setBusyID("");
    }
  }

  const statusText: Record<string, string> = { pending: "待回复", processing: "整理中", resolved: "已补充", failed: "待重试" };
  const targets: Record<string, string> = { position: "已补充岗位描述", company: "已补充公司信息", both: "已补充岗位与公司" };
  return <section style={{ marginTop: 16 }}>
    <Flex align="center" justify="space-between" style={{ marginBottom: 8 }}>
      <Typography.Text strong>待确认补充</Typography.Text>
      {error ? <Button size="small" disabled={Boolean(busyID)} loading={loading} onClick={() => void load()}>重试</Button> : null}
    </Flex>
    {error && <Alert type="warning" showIcon title={error} />}
    {rows === null && (!error || loading) && <Flex role="status" align="center" gap={8}><Spin size="small" /><Typography.Text type="secondary">正在读取待确认问题…</Typography.Text></Flex>}
    {rows !== null &&
      <Table<Question> rowKey="id" size="small" dataSource={rows} pagination={rows.length > 10 ? { pageSize: 10 } : false} scroll={{ x: 640 }}
        locale={{ emptyText: "暂时没有需要你补充的问题。" }} columns={[
          { title: "待确认问题", dataIndex: "question", width: 180, render: text => <span style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{text}</span> },
          { title: "候选人原话", width: 200, render: (_, row) => row.source_messages?.length ? <Collapse size="small" ghost items={[{ key: row.id, label: `${row.source_messages.length} 条原话`, children: <Flex vertical gap={8}>
            {row.source_messages.map((source, i) => <div key={source.message_id || i}><Typography.Text type="secondary">{source.candidate_name}</Typography.Text><div style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{source.text}</div></div>)}
          </Flex> }]} /> : <Typography.Text type="secondary">原话已清理</Typography.Text> },
          { title: "HR 回复", width: 260, render: (_, row) => <Flex vertical gap={8}>
            <Input.TextArea aria-label={`回复${row.question}`} value={drafts[row.id] ?? row.answer ?? ""} autoSize={{ minRows: 2, maxRows: 6 }} maxLength={4000} disabled={Boolean(busyID)}
              placeholder="填写已经核实、可以告诉候选人的答案。" onChange={event => setDrafts(values => ({ ...values, [row.id]: event.target.value }))} />
            <Flex gap={8} align="center" wrap>
              <Button size="small" type="primary" loading={busyID === row.id} disabled={Boolean(busyID) || !(drafts[row.id] ?? row.answer ?? "").trim()} onClick={() => void reply(row)}>保存回复</Button>
              <Tag color={row.status === "resolved" ? "success" : undefined}>{statusText[row.status] || row.status}</Tag>
            </Flex>
            {row.publish_target ? <Typography.Text type="secondary">{targets[row.publish_target] || "已保存补充"}</Typography.Text> : null}
            {row.error ? <Typography.Text type="danger">{row.error}</Typography.Text> : null}
          </Flex> },
        ]} />}
  </section>;
}
