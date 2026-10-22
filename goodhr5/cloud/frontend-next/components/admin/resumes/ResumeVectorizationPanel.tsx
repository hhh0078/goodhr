/** 文件作用说明：展示当前权限内简历的向量化数量，并独立提交长期后台任务，不等待全部处理完成。 */
"use client";
import { Alert, Button, Divider, Flex, Spin, Typography } from "antd";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";

type VectorStatus = { total: number; indexed: number; pending: number; running: boolean; task_id?: string; status?: string; message?: string; processed?: number; failed?: number; eligible: boolean; reason?: string; model?: string; input_price_per_million?: number; output_price_per_million?: number };

/** ResumeVectorizationPanel 打开设置时读取数量；点击立即创建任务，后台继续处理缺失及过期版本。 */
export default function ResumeVectorizationPanel({ open, account }: { open: boolean; account: string }) {
  const [status, setStatus] = useState<VectorStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const generation = useRef(0);
  const inFlight = useRef(false);
  useEffect(() => {
    const current = ++generation.current; setStatus(null); setError(""); setSubmitted(false); setStarting(false); inFlight.current = false;
    if (!open) return;
    const controller = new AbortController(); setLoading(true);
    cloudRequest("/api/resume-library/vectors", { signal: controller.signal }).then((data) => {
      if (current !== generation.current || controller.signal.aborted) return;
      if (![data.total, data.indexed, data.pending].every((value) => Number.isSafeInteger(value) && value >= 0) || typeof data.eligible !== "boolean") throw new Error("向量化数量没读完整，重新打开设置试试。");
      setStatus(data);
    }).catch((cause) => { if (current === generation.current && !controller.signal.aborted) setError(cause instanceof Error ? cause.message : "向量化状态没读到，重新打开设置试试。"); }).finally(() => { if (current === generation.current) setLoading(false); });
    return () => { generation.current += 1; controller.abort(); };
  }, [open, account]);

  /** start 仅提交一次后台任务，不触碰下载导入开关，弹框可立即关闭。 */
  async function start() {
    if (!status?.eligible || status.running || submitted || inFlight.current) return;
    const current = generation.current; inFlight.current = true; setStarting(true); setError("");
    try {
      const result = await cloudRequest("/api/resume-library/vectors", { method: "POST" });
      if (current !== generation.current) return;
      if (!result.task_id) throw new Error("任务没有确认收到，重新打开设置核对一下。");
      setStatus((previous) => previous ? { ...previous, running: true, task_id: result.task_id, status: result.status } : previous); setSubmitted(true);
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "任务没有确认收到，重新打开设置核对一下。"); }
    finally { if (current === generation.current) { inFlight.current = false; setStarting(false); } }
  }
  return <>
    <Divider />
    <Flex vertical gap={10} data-testid="resume-vectorization">
      <Typography.Text strong>简历向量化</Typography.Text>
      {loading ? <Spin size="small" aria-label="正在读取向量化数量" /> : status ? <>
        <Flex gap={24} wrap><Typography.Text>简历总数：<strong>{status.total}</strong></Typography.Text><Typography.Text>已向量化：<strong>{status.indexed}</strong></Typography.Text><Typography.Text type="secondary">待处理：{status.pending}</Typography.Text></Flex>
        <Typography.Paragraph type="secondary" style={{ margin: 0 }}>把经历整理成 AI 能检索的索引，缺失或简历已更新的内容会重新处理。只使用当前账号的同站点内置 AI，启动前会检查内置配置与余额。</Typography.Paragraph>
        {status.model ? <Typography.Text type="secondary">模型：{status.model}{typeof status.input_price_per_million === "number" ? ` · 输入 ¥${status.input_price_per_million}/百万 Token` : ""}{typeof status.output_price_per_million === "number" ? status.output_price_per_million === 0 ? " · 输出不收费" : ` · 输出 ¥${status.output_price_per_million}/百万 Token` : ""}</Typography.Text> : null}
        {!status.eligible ? <Alert type="info" title={status.reason || "当前还不能向量化，请先检查内置 AI 配置与余额。"} description={<Link href="/admin/personal-config">去个人配置</Link>} /> : null}
        {!status.running && ["failed", "uncertain", "partial_success"].includes(status.status || "") ? <Alert type="warning" title={status.message || "上次没有全部完成。核对错误后可再点击向量化，已经完成的当前版本会保留。"} description={status.failed ? `有 ${status.failed} 份未完成。` : undefined} /> : null}
        {status.running || submitted ? <Alert type="info" title="已经在后台处理，可以关掉弹框，稍后刷新页面查看数量。" /> : null}
        <Flex justify="flex-end"><Button aria-label="简历向量化" onClick={() => void start()} type="primary" loading={starting} disabled={!status.eligible || status.running || submitted || status.pending === 0}>简历向量化</Button></Flex>
      </> : null}
      {error ? <Alert type="warning" title={error} /> : null}
    </Flex>
  </>;
}
