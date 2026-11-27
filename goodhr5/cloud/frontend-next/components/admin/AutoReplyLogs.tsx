/** 文件作用说明：展示当前登录账号最近千条自动回复日志，串行轮询、游标翻页并展开具体错误。 */
"use client";
import { Alert, Button, Empty, Spin, Tag } from "antd";
import { useEffect, useState } from "react";
import { cloudRequest, getToken } from "@/lib/admin-api";
import styles from "./auto-reply-inbox.module.css";

type ReplyLog = { id: string; cursor: number; title?: string; status?: string; description?: string; error_detail?: string; occurred_at?: string; duration_ms?: number; run_id?: string; position_id?: string; conversation_id?: string; failure_diagnostics?: unknown; ai_request_attempt?: unknown };
const labels: Record<string, string> = { initialize_auto_reply_page: "准备消息页", process_conversation: "处理候选人会话", check_open_conversation: "检查当前聊天", check_open_after_scan: "核对聊天窗口", scan_unread_conversations: "检查会话列表", review_contact_list: "核对消息变化", load_positions: "读取接待岗位", server_run_stopped: "自动回复结束", ai_request_attempt: "AI 请求", sync: "同步聊天", ai: "AI 分析", reply: "准备回复", sent: "消息已回复", manual: "等待人工确认", waiting_hr: "等待补充", awaiting_hr_notice: "登记补充提醒", online_profile: "整理在线简历", resume_ai: "整理简历", resume_review: "复核沟通目标", send_messages: "拟定回复", request_info_resume: "索要简历", request_info_phone: "索要手机号", request_info_wechat: "索要微信" };
const statuses: Record<string, string> = { start: "开始", loading: "处理中", started: "开始", success: "完成", result: "结果", sent: "已发送", completed: "完成", failed: "失败", error: "失败", warning: "提醒", skipped: "跳过", stopped: "停止" };

/** AutoReplyLogs 只在面板可见时读取；请求完成后再安排下一轮，换账号或关闭时取消旧请求。 */
export default function AutoReplyLogs({ active }: { active: boolean }) {
  const [items, setItems] = useState<ReplyLog[]>([]);
  const [cursors, setCursors] = useState<string[]>([""]);
  const [next, setNext] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [retry, setRetry] = useState(0);
  const before = cursors[cursors.length - 1];
  useEffect(() => {
    if (!active) return;
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    let controller: AbortController | undefined;
    const token = getToken();
    setItems([]); setNext("");
    /** readLogs 在同一游标读取日志；第一页自动跟进，翻旧记录时保留阅读位置。 */
    async function readLogs() {
      if (document.visibilityState !== "visible") { timer = setTimeout(() => void readLogs(), 5000); return; }
      controller = new AbortController();
      const timeout = setTimeout(() => controller?.abort(), 15000);
      setLoading(true);
      try {
        const query = new URLSearchParams(); if (before) query.set("before", before);
        const data = await cloudRequest(`/api/auto-reply/logs?${query}`, { signal: controller.signal });
        if (!Array.isArray(data?.items) || typeof data?.next_cursor !== "string") throw new Error("日志没有读完整，可以重试。");
        if (!disposed && token === getToken()) { setItems(data.items); setNext(data.next_cursor); setError(""); }
      } catch (cause) {
        if (!disposed && token === getToken()) setError(cause instanceof Error ? cause.message : "日志暂时没读出来，可以重试。");
      } finally {
        clearTimeout(timeout);
        if (!disposed) { setLoading(false); if (!before) timer = setTimeout(() => void readLogs(), 5000); }
      }
    }
    void readLogs();
    return () => { disposed = true; clearTimeout(timer); controller?.abort(); };
  }, [active, before, retry]);
  return <section aria-label="自动回复运行日志">
    <div className={styles.heading}><h3>运行日志</h3><span className={styles.count}>{loading && items.length > 0 && <Spin size="small" />} 每个账号保留最近 1000 条</span></div>
    {error && <Alert type="error" showIcon title={error} action={<Button size="small" onClick={() => setRetry(value => value + 1)}>重试</Button>} />}
    <div className={styles.logs} aria-busy={loading} data-testid="auto-reply-logs">
      {items.length === 0 && !error && (loading ? <div className={styles.logEmpty}><Spin description="正在翻工作记录…" /></div> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有运行记录，开始接待后我会认真记账。" />)}
      {items.map(item => <details key={item.id} className={styles.logRow}>
        <summary><time dateTime={item.occurred_at}>{item.occurred_at && Number.isFinite(Date.parse(item.occurred_at)) ? new Date(item.occurred_at).toLocaleString("zh-CN", { hour12: false }) : "时间待核对"}</time><Tag color={item.status === "failed" || item.status === "error" ? "error" : item.status === "warning" ? "warning" : undefined}>{statuses[item.status || ""] || item.status || "记录"}</Tag><span className={styles.logMessage}>{labels[item.title || ""] || item.title || "运行进度"}{item.description ? ` · ${item.description}` : ""}</span>{typeof item.duration_ms === "number" && <span className={styles.count}>{(item.duration_ms / 1000).toFixed(1)}s</span>}</summary>
        <div className={styles.logDetail}>
          {item.error_detail && <div role="note" className={styles.logError}><strong>具体原因</strong><pre>{item.error_detail}</pre></div>}
          {item.description && <p>{item.description}</p>}
          <div className={styles.count}>阶段：{item.title || "未提供"} · 状态：{item.status || "未提供"}{item.run_id ? ` · 运行：${item.run_id}` : ""}</div>
          {Boolean(item.failure_diagnostics || item.ai_request_attempt) && <pre>{JSON.stringify(item.failure_diagnostics || item.ai_request_attempt, null, 2)}</pre>}
          {!item.error_detail && (item.status === "failed" || item.status === "warning") && <p>这条旧记录没有保存更细的错误。更新后的运行会记录具体原因。</p>}
        </div>
      </details>)}
    </div>
    <div className={styles.pager}><span className={styles.count}>第 {cursors.length} 页</span><Button disabled={cursors.length === 1 || loading} onClick={() => setCursors(value => value.slice(0, -1))}>较新记录</Button><Button disabled={!next || loading} onClick={() => setCursors(value => [...value, next])}>更早记录</Button></div>
  </section>;
}
