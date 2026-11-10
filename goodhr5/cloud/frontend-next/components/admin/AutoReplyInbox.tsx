/** 文件作用说明：在自动回复统一面板按可答岗位处理待确认事项，已关闭岗位与历史答复仍可查看。 */
"use client";

import { Alert, Button, Empty, Radio, Select, Spin, Typography } from "antd";
import { useEffect, useState } from "react";
import type { ReplyInboxSummary, ReplyInboxView } from "@/lib/auto-reply-inbox";
import PositionReplyQuestions from "./PositionReplyQuestions";
import styles from "./auto-reply-inbox.module.css";

/** AutoReplyInbox 只在面板打开时加载题目详情，切换岗位和范围不重建草稿组件。 */
export default function AutoReplyInbox({ summary, loading, error, active, onRetry, onApplied }: {
  summary: ReplyInboxSummary | null; loading: boolean; error: string; active: boolean; onRetry: () => void; onApplied: () => void;
}) {
  const [positionID, setPositionID] = useState("");
  const [view, setView] = useState<ReplyInboxView>("pending");
  const selected = summary?.positions.find(item => item.position_id === positionID)
    || summary?.positions.find(item => item.pending_count > 0) || summary?.positions[0];
  useEffect(() => {
    setPositionID(current => summary?.positions.some(item => item.position_id === current) ? current
      : summary?.positions.find(item => item.pending_count > 0)?.position_id || summary?.positions[0]?.position_id || "");
  }, [summary]);
  return <section>
    <div className={styles.heading}><h3>待确认补充</h3><span className={styles.count}>{error ? "数量暂未读到" : summary ? `${summary.pending_count} 项待处理` : "正在核对数量"}</span></div>
    {error && <Alert type="warning" showIcon title={error} action={<Button loading={loading} onClick={onRetry}>重试</Button>} style={{ marginBottom: 12 }} />}
    {!summary && !error && <div role="status"><Spin size="small" /> <Typography.Text type="secondary">正在读取待确认岗位…</Typography.Text></div>}
    {summary && !summary.positions.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂时没有待确认记录，遇到需要你补充的信息时，我会放在这里。" />}
    {summary && selected && <>
      <div className={`${styles.filters} goodhr-compact-toolbar`}>
        <Select aria-label="待确认所属岗位" className={styles.positionSelect} value={selected.position_id} showSearch optionFilterProp="label" onChange={setPositionID}
          options={summary.positions.map(item => ({ value: item.position_id, label: `${item.position_name}${item.is_open ? "" : " · 已关闭"}${item.pending_count ? `（${item.pending_count}）` : ""}` }))} />
        <Radio.Group aria-label="待确认记录范围" value={view} onChange={event => setView(event.target.value as ReplyInboxView)} optionType="button" options={[{ label: "待补充", value: "pending" }, { label: "全部记录", value: "all" }]} />
      </div>
      <PositionReplyQuestions positionID={selected.position_id} view={view} active={active} onApplied={onApplied} />
    </>}
  </section>;
}
