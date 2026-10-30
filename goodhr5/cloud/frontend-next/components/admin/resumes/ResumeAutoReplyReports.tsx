/** 文件作用说明：在简历工作区按需展示经授权同步的自动回复推荐报告，不请求本机聊天或附件正文。 */
"use client";
import { Alert, Button, Empty, Flex, Tag, Typography } from "antd";
import { CopyOutlined, ExportOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import { useAdmin } from "../AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { normalizeRecommendation, type CandidateRecommendation } from "@/lib/recommendation";

/** ResumeAutoReplyReports 展示本地生成并获准同步的报告，允许复制公开链接或撤销分享。 */
export default function ResumeAutoReplyReports({ candidateID }: { candidateID: string }) {
  const { notify, confirm } = useAdmin();
  const [reports, setReports] = useState<CandidateRecommendation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revoking, setRevoking] = useState("");
  const generation = useRef(0);
  /** load 只请求当前候选人的授权报告，忽略切换候选人后迟到的响应。 */
  async function load() {
    const current = ++generation.current;
    setLoading(true); setError("");
    try {
      const data = await cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}/auto-reply?section=recommendations`);
      if (current !== generation.current) return;
      const items = data?.auto_reply?.recommendations;
      setReports(Array.isArray(items) ? items.map(normalizeRecommendation) : []);
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "推荐报告没读出来，请重试。"); }
    finally { if (current === generation.current) setLoading(false); }
  }
  useEffect(() => { setReports([]); void load(); return () => { generation.current += 1; }; }, [candidateID]);
  /** reportURL 使用当前站点生成可访问的公开报告地址。 */
  function reportURL(publicID: string) { return `${window.location.origin}/recommendations/${encodeURIComponent(publicID)}`; }
  /** copyLink 将当前报告的公开地址复制给 HR，失败时提供手动复制路径。 */
  async function copyLink(publicID: string) {
    try { await navigator.clipboard.writeText(reportURL(publicID)); notify("报告链接已复制。", "success"); }
    catch { notify("复制没成功，可以打开报告后复制地址栏。", "warning"); }
  }
  /** changeSharing 只在 HR 明确确认后开启或撤销公开链接，不把结构化同步视为公开授权。 */
  async function changeSharing(publicID: string, enabled: boolean) {
    if (revoking || !await confirm(enabled ? "开启报告分享" : "撤销报告分享", enabled
      ? "这个链接会公开完整简历、联系方式和推荐结论。拿到链接的人无需登录即可查看，之后可以撤销。确认开启吗？"
      : "公主请确认要撤销这个报告链接吗？撤销后，拿到旧链接的人也无法再看。")) return;
    const current = generation.current;
    setRevoking(publicID);
    try {
      await cloudRequest(`/api/auto-reply/recommendations/${encodeURIComponent(publicID)}/${enabled ? "share" : "revoke"}`, { method: "POST" });
      if (current !== generation.current) return;
      setReports((items) => items.map((item) => item.publicID === publicID ? { ...item, shareEnabled: enabled } : item));
      notify(enabled ? "报告分享已开启，可以复制链接。" : "公开链接已撤销。", "success");
    } catch (cause) { if (current === generation.current) notify(cause instanceof Error ? cause.message : "分享状态没改成功，可以再试一次。", "error"); }
    finally { setRevoking(""); }
  }
  return <Flex vertical gap={12} aria-busy={loading}>
    <Typography.Paragraph type="secondary" style={{ margin: 0 }}>条件确认与 AI 处理留在本机；岗位里将“是否生成简历”选为“需要”后，这里展示已同步的推荐结果。详细运行过程可到运行日志查看。</Typography.Paragraph>
    {error ? <Alert type="warning" showIcon title={error} action={<Button onClick={() => void load()}>重试</Button>} /> : null}
    {!loading && !reports.length && !error ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有同步的自动回复报告，先让本地小助手认真聊一聊。" /> : null}
    {reports.map((item) => <section key={item.publicID} style={{ borderBottom: "1px solid var(--ant-color-border-secondary)", paddingBottom: 12 }}>
      <Flex gap={8} wrap align="center"><Typography.Text strong>{item.report.position.name || "推荐岗位"}</Typography.Text><Tag>{item.shareEnabled ? "可分享" : "未公开"}</Tag><Typography.Text type="secondary">{formatDate(item.generatedAt)}</Typography.Text></Flex>
      <Typography.Paragraph style={{ marginBlock: 8 }}>{item.report.executiveSummary || item.summary || "报告已生成，打开后查看详细结果。"}</Typography.Paragraph>
      <Flex gap={8} wrap>
        <Button icon={<ExportOutlined aria-hidden="true" />} disabled={!item.shareEnabled} href={`/recommendations/${encodeURIComponent(item.publicID)}`} target="_blank" rel="noopener noreferrer">查看报告</Button>
        <Button icon={<CopyOutlined aria-hidden="true" />} disabled={!item.shareEnabled} onClick={() => void copyLink(item.publicID)}>复制链接</Button>
        {item.shareEnabled ? <Button aria-label="撤销分享" danger loading={revoking === item.publicID} disabled={Boolean(revoking)} onClick={() => void changeSharing(item.publicID, false)}>撤销分享</Button> : <Button aria-label="开启分享" loading={revoking === item.publicID} disabled={Boolean(revoking)} onClick={() => void changeSharing(item.publicID, true)}>开启分享</Button>}
      </Flex>
    </section>)}
  </Flex>;
}
