/** 文件作用说明：展示当前本人岗位的两轮筛选历史，支持独立分数、开始时间查询和固定二十条分页。 */
"use client";
import { Alert, Button, DatePicker, Flex, Form, InputNumber, Pagination, Table, Tag, Typography } from "antd";
import dayjs from "dayjs";
import { useEffect, useId, useRef, useState } from "react";
import AdminDialog from "./AdminDialog";
import { loadPositionScreeningRecords } from "@/lib/position-screening-api";
import { emptyScreeningFilters, screeningDuration, screeningPageSize, screeningQuery, screeningScore, screeningStatusLabels, screeningTime, type ScreeningFilters, type ScreeningRecord, type ScreeningRound, type ScreeningRoundFilters } from "@/lib/position-screening";
import styles from "./position-screening.module.css";

/** ScreeningReason 保留完整原始原因，长文本默认收起且可用键盘展开，不使用 HTML 注入。 */
function ScreeningReason({ reason }: { reason: string }) {
  const [expanded, setExpanded] = useState(false);
  const text = typeof reason === "string" ? reason : "";
  return <div><Typography.Paragraph className={styles.reason}>{text ? expanded || text.length <= 100 ? text : `${text.slice(0, 100)}…` : "未记录原因"}</Typography.Paragraph>{text.length > 100 ? <Button type="link" className={styles.expand} aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "收起原因" : "展开原因"}</Button> : null}</div>;
}

/** ScreeningRoundResult 显示真实轮次状态与独立耗时，未执行不补造分数或日期。 */
function ScreeningRoundResult({ round, name }: { round: ScreeningRound; name: string }) {
  const status = screeningStatusLabels[round.status] || "状态待核对";
  return <div role="group" aria-label={`${name}结果`} className={styles.round}>
    <Flex gap={8} align="center" wrap><Tag color={round.status === "passed" ? "success" : round.status === "failed" ? "error" : round.status === "running" ? "processing" : undefined}>{status}</Tag><Typography.Text strong>{screeningScore(round)}</Typography.Text>{round.kind === "ai" && round.threshold != null ? <Typography.Text type="secondary">阈值 {round.threshold}</Typography.Text> : null}</Flex>
    <div className={styles.meta}>开始：{screeningTime(round.started_at)}<br />完成：{screeningTime(round.finished_at)}<br />耗时：{screeningDuration(round.duration_ms)}</div>
    <ScreeningReason reason={round.reason} />
  </div>;
}

/** RoundFilters 编辑单轮范围，支持只填写下限、上限或一侧开始时间。 */
function RoundFilters({ name, value, onChange }: { name: string; value: ScreeningRoundFilters; onChange: (value: ScreeningRoundFilters) => void }) {
  const prefix = useId();
  return <fieldset className={styles.filterGroup}><legend>{name}</legend><div className={styles.filterFields}>
    <Form.Item layout="vertical" label="最低分" htmlFor={`${prefix}-min`}><InputNumber id={`${prefix}-min`} aria-label={`${name}最低分`} min={0} max={100} value={value.scoreMin} onChange={(score) => onChange({ ...value, scoreMin: score })} placeholder="不限" /></Form.Item>
    <Form.Item layout="vertical" label="最高分" htmlFor={`${prefix}-max`}><InputNumber id={`${prefix}-max`} aria-label={`${name}最高分`} min={0} max={100} value={value.scoreMax} onChange={(score) => onChange({ ...value, scoreMax: score })} placeholder="不限" /></Form.Item>
    <Form.Item layout="vertical" className={styles.dateField} label="开始时间从（包含）" htmlFor={`${prefix}-from`}><DatePicker id={`${prefix}-from`} aria-label={`${name}开始时间从`} showTime needConfirm={false} format="YYYY-MM-DD HH:mm:ss" value={value.startedFrom ? dayjs(value.startedFrom) : null} onChange={(date) => onChange({ ...value, startedFrom: date ? date.toISOString() : "" })} /></Form.Item>
    <Form.Item layout="vertical" className={styles.dateField} label="开始时间至（不包含）" htmlFor={`${prefix}-to`}><DatePicker id={`${prefix}-to`} aria-label={`${name}开始时间至`} showTime needConfirm={false} format="YYYY-MM-DD HH:mm:ss" value={value.startedTo ? dayjs(value.startedTo) : null} onChange={(date) => onChange({ ...value, startedTo: date ? date.toISOString() : "" })} /></Form.Item>
  </div></fieldset>;
}

/** PositionScreeningRecordsDialog 只读服务端历史；父级按账号和岗位提供 key，切换时不保留前一身份内容。 */
export default function PositionScreeningRecordsDialog({ position, onClose }: { position: { id: string; name: string }; onClose: () => void }) {
  const [filters, setFilters] = useState(emptyScreeningFilters);
  const [applied, setApplied] = useState(emptyScreeningFilters);
  const [items, setItems] = useState<ScreeningRecord[]>([]);
  const [page, setPage] = useState(1); const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false); const [error, setError] = useState(""); const [filterError, setFilterError] = useState("");
  const generation = useRef(0); const request = useRef<AbortController | null>(null);

  /** load 用服务端查询真实分页，关闭或新查询后的旧响应不能回填列表。 */
  async function load(nextPage: number, nextFilters: ScreeningFilters) {
    const current = ++generation.current; request.current?.abort(); const controller = new AbortController(); request.current = controller;
    setLoading(true); setError(""); setItems([]); setPage(nextPage);
    try { const result = await loadPositionScreeningRecords(position.id, nextPage, nextFilters, controller.signal); if (current === generation.current) { setItems(result.records); setTotal(result.total); } }
    catch (cause) { if (current === generation.current) { setTotal(0); setError(cause instanceof Error ? cause.message : "记录暂时没读出来，请重试。"); } }
    finally { if (current === generation.current) setLoading(false); }
  }
  useEffect(() => { void load(1, emptyScreeningFilters()); return () => { generation.current += 1; request.current?.abort(); }; }, [position.id]);

  /** query 校验范围后从第一页查询；重置同时清空草稿及已应用条件。 */
  function query(reset = false) {
    const next = reset ? emptyScreeningFilters() : filters;
    try { screeningQuery(1, next); setFilterError(""); if (reset) setFilters(next); setApplied(next); void load(1, next); }
    catch (cause) { setFilterError(cause instanceof Error ? cause.message : "筛选范围没填完整。"); }
  }
  return <AdminDialog open title="岗位筛选记录" description={`当前岗位：${position.name} · 最新在前，每页 20 条。时间按本机时区显示。`} maxWidth="lg" cancelText="关闭" onClose={onClose}>
    <div data-testid="position-screening-records">
      <div className={styles.filters}><RoundFilters name="第一轮" value={filters.first} onChange={(value) => setFilters({ ...filters, first: value })} /><RoundFilters name="第二轮" value={filters.second} onChange={(value) => setFilters({ ...filters, second: value })} /></div>
      <Flex gap={8} className="goodhr-compact-toolbar"><Button type="primary" onClick={() => query()}>查询</Button><Button onClick={() => query(true)}>清空</Button><Typography.Text type="secondary" className={styles.meta}>第一轮为列表预筛，第二轮为详情评分；未执行不会记成零分。</Typography.Text></Flex>
      {filterError ? <Alert type="warning" showIcon title={filterError} className={styles.alert} /> : null}
      {error ? <Alert type="warning" showIcon title={error} className={styles.alert} action={<Button onClick={() => void load(page, applied)}>重试</Button>} /> : null}
      <Typography.Paragraph type="secondary" className={styles.mobileHint}>左右滑动表格，可查看两轮结果和完整时间。</Typography.Paragraph>
      <Table<ScreeningRecord> className={styles.recordTable} rowKey="id" loading={loading} dataSource={items} pagination={false} scroll={{ x: 780 }} locale={{ emptyText: loading ? "正在整理筛选记录" : "暂时没有记录，可以换个条件再看看。" }} columns={[
        { title: "候选人 / 筛选时间", width: 180, render: (_, item) => <Flex vertical gap={6}><Typography.Text strong>{item.candidate_name || "未记录姓名"}</Typography.Text><Typography.Text type="secondary" className={styles.meta}>{screeningTime(item.occurred_at)}</Typography.Text>{!item.completed ? <Tag>本次尚未结束</Tag> : null}{item.termination_reason ? <Typography.Paragraph className={styles.reason}>{item.termination_reason}</Typography.Paragraph> : null}</Flex> },
        { title: "第一轮 · 列表预筛", width: 300, render: (_, item) => <ScreeningRoundResult round={item.first} name="第一轮" /> },
        { title: "第二轮 · 详情评分", width: 300, render: (_, item) => <ScreeningRoundResult round={item.second} name="第二轮" /> },
      ]} />
      <Flex className="goodhr-table-pagination"><Typography.Text type="secondary">共 {total} 条</Typography.Text><Pagination current={page} pageSize={screeningPageSize} total={total} showSizeChanger={false} onChange={(next) => void load(next, applied)} /></Flex>
    </div>
  </AdminDialog>;
}
