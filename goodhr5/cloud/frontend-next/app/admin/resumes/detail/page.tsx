/** 本文件负责新版后台候选人简历详情展示。 */
"use client";

import type { ReactNode } from "react";
import ArrowBackRoundedIcon from "@mui/icons-material/ArrowBackRounded";
import AttachFileRoundedIcon from "@mui/icons-material/AttachFileRounded";
import ContentCopyRoundedIcon from "@mui/icons-material/ContentCopyRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import ExpandMoreRoundedIcon from "@mui/icons-material/ExpandMoreRounded";
import FactCheckRoundedIcon from "@mui/icons-material/FactCheckRounded";
import ForumRoundedIcon from "@mui/icons-material/ForumRounded";
import LinkOffRoundedIcon from "@mui/icons-material/LinkOffRounded";
import LocationOnRoundedIcon from "@mui/icons-material/LocationOnRounded";
import OpenInNewRoundedIcon from "@mui/icons-material/OpenInNewRounded";
import PsychologyRoundedIcon from "@mui/icons-material/PsychologyRounded";
import RecommendRoundedIcon from "@mui/icons-material/RecommendRounded";
import { Accordion, AccordionDetails, AccordionSummary, Avatar, Box, Button, Chip, CircularProgress, Stack, Typography } from "@mui/material";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import JsonTree from "@/components/admin/JsonTree";
import AdminDialog from "@/components/admin/AdminDialog";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudAssetURL, cloudDownload, cloudRequest, formatDate } from "@/lib/admin-api";
import { experienceLine, normalizeCandidate, normalizeCandidateAutoReply, periodText, scoreText, statusText, type CandidateAIRecord, type CandidateAutoReplyDetail, type CandidateConversation, type NormalizedCandidate, type NormalizedExperience } from "@/lib/candidate-normalize";
import type { CandidateRecommendation } from "@/lib/recommendation";

type AutoReplySection = "attachments" | "conversations" | "confirmations" | "ai_records" | "recommendations";

/** ResumeDetailPage 展示候选人基本信息、经历和分析结果。 */
export default function ResumeDetailPage() {
  const params = useSearchParams();
  const { notify } = useAdmin();
  const [rawCandidate, setRawCandidate] = useState<any>(null);
  const [autoReply, setAutoReply] = useState<CandidateAutoReplyDetail>(() => emptyAutoReplyDetail());
  const [autoReplySection, setAutoReplySection] = useState<AutoReplySection | null>(null);
  const [autoReplyLoading, setAutoReplyLoading] = useState<AutoReplySection | null>(null);
  const [autoReplyLoaded, setAutoReplyLoaded] = useState<Partial<Record<AutoReplySection, boolean>>>({});
  const candidateID = params.get("candidate_id") || "";
  const engagementID = params.get("engagement_id") || "";
  const candidate = useMemo(() => rawCandidate ? normalizeCandidate(rawCandidate) : null, [rawCandidate]);

  /** downloadAttachment 下载当前账号有权查看的候选人简历附件。 */
  const downloadAttachment = (path: string, name: string) => {
    cloudDownload(path, name).catch((error) => notify(error instanceof Error ? error.message : "简历附件没下载成功", "error"));
  };

  useEffect(() => {
    if (!candidateID) return;
    setAutoReply(emptyAutoReplyDetail());
    setAutoReplySection(null);
    setAutoReplyLoading(null);
    setAutoReplyLoaded({});
    const query = engagementID ? `?engagement_id=${encodeURIComponent(engagementID)}` : "";
    cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}${query}`).then((data) => setRawCandidate(data.candidate || data)).catch((error) => notify(error.message, "error"));
  }, [candidateID, engagementID, notify]);

  /** openAutoReplySection 打开一个关联资料弹框，并在首次打开时单独加载该区块。 */
  async function openAutoReplySection(section: AutoReplySection) {
    setAutoReplySection(section);
    if (autoReplyLoaded[section] || autoReplyLoading === section) return;
    setAutoReplyLoading(section);
    try {
      const query = new URLSearchParams({ section });
      if (engagementID) query.set("engagement_id", engagementID);
      const data = await cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}/auto-reply?${query}`);
      const loaded = normalizeCandidateAutoReply(data.auto_reply);
      setAutoReply((current) => ({
        attachments: section === "attachments" ? loaded.attachments : current.attachments,
        conversations: section === "conversations" ? loaded.conversations : current.conversations,
        confirmationItems: section === "confirmations" ? loaded.confirmationItems : current.confirmationItems,
        aiRecords: section === "ai_records" ? loaded.aiRecords : current.aiRecords,
        recommendations: section === "recommendations" ? loaded.recommendations : current.recommendations,
      }));
      setAutoReplyLoaded((current) => ({ ...current, [section]: true }));
    } catch (error) {
      notify(error instanceof Error ? error.message : "候选人关联资料没读出来", "error");
    } finally {
      setAutoReplyLoading(null);
    }
  }

  /** copyRecommendationLink 复制一份仍然有效的公开推荐链接。 */
  async function copyRecommendationLink(item: CandidateRecommendation) {
    try {
      await navigator.clipboard.writeText(recommendationURL(item.publicID));
      notify("推荐链接已经复制，可以直接发给面试官。", "success");
    } catch {
      notify("链接没复制成功，你可以打开后手动复制。", "error");
    }
  }

  /** revokeRecommendationLink 二次确认后永久撤销一份公开推荐链接。 */
  async function revokeRecommendationLink(item: CandidateRecommendation) {
    if (!window.confirm(`我小声确认一下，撤销“${item.report.position.name || "这份岗位"}”的推荐链接后，旧链接会立即失效。继续吗？`)) return;
    try {
      await cloudRequest(`/api/auto-reply/recommendations/${encodeURIComponent(item.publicID)}/revoke`, { method: "POST" });
      setAutoReply((current) => ({ ...current, recommendations: current.recommendations.map((recommendation) => recommendation.publicID === item.publicID ? { ...recommendation, shareEnabled: false, status: "revoked" } : recommendation) }));
      notify("公开链接已经撤销，旧地址现在打不开了。", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "推荐链接没撤销成功", "error");
    }
  }

  if (!candidateID) return <SectionPanel><Typography color="error">缺少候选人 ID</Typography></SectionPanel>;
  if (!candidate) return <SectionPanel><Typography color="text.secondary">正在读取简历详情...</Typography></SectionPanel>;

  return <>
    <PageHeader title="简历详情" actions={<Button component={Link} href="/admin/resumes" startIcon={<ArrowBackRoundedIcon />}>返回简历库</Button>} />
    <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1fr) 290px" }, minHeight: 0 }}>
        <Box sx={{ p: { xs: 2, md: 3 } }}>
          <CandidateHeader candidate={candidate} />
          {candidate.phone || candidate.email || candidate.wechat ? <ResumeSection title="联系方式">
            <Stack spacing={0.8}>
              {candidate.phone ? <Typography>手机：{candidate.phone}</Typography> : null}
              {candidate.email ? <Typography>邮箱：{candidate.email}</Typography> : null}
              {candidate.wechat ? <Typography>微信：{candidate.wechat}</Typography> : null}
            </Stack>
          </ResumeSection> : null}
          <ResumeSection title="求职意向">
            <Typography>{[candidate.workRegion, candidate.expectedPosition, candidate.expectedSalary, candidate.workStatus, candidate.onlineStatus].filter(Boolean).join("  |  ") || "暂无求职意向"}</Typography>
          </ResumeSection>
          {candidate.personalDescription ? <ResumeSection title="个人优势"><Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.9 }}>{candidate.personalDescription}</Typography></ResumeSection> : null}
          {candidate.workExperiences.length ? <ResumeSection title="工作经历">{candidate.workExperiences.map((item, index) => <Experience key={`work-${index}`} item={item} />)}</ResumeSection> : null}
          {candidate.projectExperiences.length ? <ResumeSection title="项目经历">{candidate.projectExperiences.map((item, index) => <Experience key={`project-${index}`} item={item} project />)}</ResumeSection> : null}
          {candidate.educations.length ? <ResumeSection title="教育经历">{candidate.educations.map((item, index) => <Experience key={`edu-${index}`} item={item} />)}</ResumeSection> : null}
          {candidate.rawText ? <Accordion elevation={0} sx={{ mt: 4, borderTop: "1px solid", borderColor: "divider", "&:before": { display: "none" } }}><AccordionSummary expandIcon={<ExpandMoreRoundedIcon />} sx={{ px: 0, pt: 2 }}><Typography component="h3" sx={{ fontSize: 19, fontWeight: 820 }}>原始文本</Typography></AccordionSummary><AccordionDetails sx={{ px: 0 }}><Typography sx={{ whiteSpace: "pre-wrap", color: "text.secondary", lineHeight: 1.8 }}>{candidate.rawText}</Typography></AccordionDetails></Accordion> : null}
          <Accordion elevation={0} sx={{ mt: 3, bgcolor: "action.hover" }}><AccordionSummary expandIcon={<ExpandMoreRoundedIcon />}><Typography sx={{ fontWeight: 720 }}>查看完整接口数据</Typography></AccordionSummary><AccordionDetails><JsonTree value={candidate.raw} /></AccordionDetails></Accordion>
        </Box>
        <SidePanel candidate={candidate} loadingSection={autoReplyLoading} onOpenSection={(section) => void openAutoReplySection(section)} />
      </Box>
    </SectionPanel>
    <AutoReplyDialog section={autoReplySection} detail={autoReply} loading={autoReplyLoading === autoReplySection} onClose={() => setAutoReplySection(null)} onDownload={downloadAttachment} onCopyRecommendation={(item) => void copyRecommendationLink(item)} onRevokeRecommendation={(item) => void revokeRecommendationLink(item)} />
  </>;
}

/** emptyAutoReplyDetail 返回互不复用数组的空关联资料。 */
function emptyAutoReplyDetail(): CandidateAutoReplyDetail {
  return { attachments: [], conversations: [], confirmationItems: [], aiRecords: [], recommendations: [] };
}

/** AutoReplyDialog 按用户点击的类型展示已懒加载的候选人关联资料。 */
function AutoReplyDialog({ section, detail, loading, onClose, onDownload, onCopyRecommendation, onRevokeRecommendation }: { section: AutoReplySection | null; detail: CandidateAutoReplyDetail; loading: boolean; onClose: () => void; onDownload: (path: string, name: string) => void; onCopyRecommendation: (item: CandidateRecommendation) => void; onRevokeRecommendation: (item: CandidateRecommendation) => void }) {
  const title = ({ attachments: "简历附件", conversations: "沟通记录", confirmations: "条件确认", ai_records: "AI 处理记录", recommendations: "推荐报告" } as Record<AutoReplySection, string>)[section || "attachments"];
  return <AdminDialog open={Boolean(section)} title={title} maxWidth="lg" cancelText="关闭" onClose={onClose}>
    {loading ? <Stack direction="row" spacing={1.25} sx={{ py: 8, alignItems: "center", justifyContent: "center" }}><CircularProgress size={22} /><Typography color="text.secondary">正在读取，我尽量不让你久等。</Typography></Stack> : null}
    {!loading && section === "attachments" ? <AttachmentRecords detail={detail} onDownload={onDownload} /> : null}
    {!loading && section === "conversations" ? <Stack spacing={1.25}>{detail.conversations.length ? detail.conversations.map((conversation) => <ConversationRecord key={conversation.id} conversation={conversation} />) : <Typography color="text.secondary">暂时没有同步到沟通记录。</Typography>}</Stack> : null}
    {!loading && section === "confirmations" ? <ConfirmationRecords detail={detail} /> : null}
    {!loading && section === "ai_records" ? <Stack spacing={1}>{detail.aiRecords.length ? detail.aiRecords.map((record) => <AIRecord key={record.id} record={record} />) : <Typography color="text.secondary">暂时没有 AI 处理记录。</Typography>}</Stack> : null}
    {!loading && section === "recommendations" ? <RecommendationRecords items={detail.recommendations} onCopy={onCopyRecommendation} onRevoke={onRevokeRecommendation} /> : null}
  </AdminDialog>;
}

/** AttachmentRecords 展示候选人的可下载简历附件。 */
function AttachmentRecords({ detail, onDownload }: { detail: CandidateAutoReplyDetail; onDownload: (path: string, name: string) => void }) {
  return <Stack spacing={1}>{detail.attachments.length ? detail.attachments.map((attachment) => <Stack key={attachment.id} direction={{ xs: "column", sm: "row" }} spacing={1.25} sx={{ p: 1.5, border: "1px solid", borderColor: "divider", borderRadius: "8px", alignItems: { sm: "center" } }}>
    <AttachFileRoundedIcon color="primary" />
    <Box sx={{ minWidth: 0, flex: 1 }}><Typography sx={{ fontWeight: 760, overflowWrap: "anywhere" }}>{attachment.originalName || "候选人简历"}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{formatFileSize(attachment.sizeBytes)} · {attachment.createdAt ? formatDate(attachment.createdAt) : "时间暂时没记上"}</Typography></Box>
    <Button size="small" startIcon={<DownloadRoundedIcon />} onClick={() => onDownload(attachment.downloadURL, attachment.originalName)}>下载附件</Button>
  </Stack>) : <Typography color="text.secondary">这里暂时没有附件，我先不假装看见了。</Typography>}</Stack>;
}

/** ConfirmationRecords 展示 AI 和 HR 对候选人待确认条件的结构化记录。 */
function ConfirmationRecords({ detail }: { detail: CandidateAutoReplyDetail }) {
  return <Stack spacing={1}>{detail.confirmationItems.length ? detail.confirmationItems.map((item) => <Box key={item.id} sx={{ p: 1.5, border: "1px solid", borderColor: "divider", borderRadius: "8px" }}>
    <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}><Chip size="small" label={confirmationTypeText(item.itemType)} /><Chip size="small" color={confirmationColor(item.status)} label={confirmationStatusText(item.status)} /><Typography sx={{ fontWeight: 760 }}>{item.content}</Typography></Stack>
    {item.statusReason || item.summary || item.evidenceText ? <Typography sx={{ mt: 0.8, color: "text.secondary", fontSize: 13, lineHeight: 1.7 }}>{item.statusReason || item.summary || item.evidenceText}</Typography> : null}
    {item.evidenceText && item.evidenceText !== item.statusReason ? <Typography sx={{ mt: 0.4, color: "text.secondary", fontSize: 12 }}>依据：{item.evidenceText}</Typography> : null}
  </Box>) : <Typography color="text.secondary">目前没有需要确认的条件。</Typography>}</Stack>;
}

/** RecommendationRecords 展示候选人已经生成的推荐版本和公开链接状态。 */
function RecommendationRecords({ items, onCopy, onRevoke }: { items: CandidateRecommendation[]; onCopy: (item: CandidateRecommendation) => void; onRevoke: (item: CandidateRecommendation) => void }) {
  if (!items.length) return <Typography color="text.secondary">条件还没全部确认，暂时没有生成推荐报告。</Typography>;
  return <Stack spacing={1.25}>{items.map((item) => <Box key={item.publicID} sx={{ p: 1.75, border: "1px solid", borderColor: "divider", borderRadius: "8px" }}>
    <Stack direction={{ xs: "column", sm: "row" }} spacing={1} sx={{ alignItems: { sm: "center" } }}>
      <Box sx={{ minWidth: 0, flex: 1 }}><Stack direction="row" spacing={0.8} sx={{ mb: 0.5, alignItems: "center", flexWrap: "wrap" }}><Typography sx={{ fontWeight: 800 }}>{item.report.position.name || "未命名岗位"}</Typography><Chip size="small" color={item.shareEnabled ? "success" : "default"} label={item.shareEnabled ? "链接有效" : "已撤销"} /><Chip size="small" label={`第 ${item.version || 1} 版`} /></Stack><Typography sx={{ color: "text.secondary", fontSize: 13 }}>{Math.round(item.matchScore)}分 · {item.recommendationLevel || "待确认"} · {formatDate(item.generatedAt)}</Typography></Box>
      {item.shareEnabled ? <Stack direction="row" spacing={0.6} sx={{ flexWrap: "wrap" }}>
        <Button size="small" component="a" href={recommendationURL(item.publicID)} target="_blank" rel="noreferrer" startIcon={<OpenInNewRoundedIcon />}>查看</Button>
        <Button size="small" startIcon={<ContentCopyRoundedIcon />} onClick={() => onCopy(item)}>复制链接</Button>
        <Button size="small" color="error" startIcon={<LinkOffRoundedIcon />} onClick={() => onRevoke(item)}>撤销</Button>
      </Stack> : null}
    </Stack>
    <Typography sx={{ mt: 1, lineHeight: 1.7 }}>{item.report.executiveSummary || item.summary || "暂时没有推荐摘要"}</Typography>
  </Box>)}</Stack>;
}

/** ConversationRecord 展示一段平台会话及全部已同步消息。 */
function ConversationRecord({ conversation }: { conversation: CandidateConversation }) {
  return <Accordion elevation={0} sx={{ border: "1px solid", borderColor: "divider", borderRadius: "8px !important", "&:before": { display: "none" } }}>
    <AccordionSummary expandIcon={<ExpandMoreRoundedIcon />}><Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}><ForumRoundedIcon color="primary" fontSize="small" /><Typography sx={{ fontWeight: 780 }}>{platformText(conversation.platformID)} · {conversation.positionText || "岗位暂时没认出来"}</Typography><Chip size="small" label={`${conversation.messages.length} 条消息`} /></Stack></AccordionSummary>
    <AccordionDetails><Box sx={{ maxHeight: 460, overflowY: "auto", pr: 0.5 }}><Stack spacing={1}>{conversation.messages.length ? conversation.messages.map((message) => {
      const mine = message.direction === "self";
      return <Box key={message.id} sx={{ display: "flex", justifyContent: mine ? "flex-end" : "flex-start" }}><Box sx={{ maxWidth: "82%", px: 1.5, py: 1, borderRadius: "8px", bgcolor: mine ? "primary.main" : "action.selected", color: mine ? "primary.contrastText" : "text.primary" }}><Typography sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere", lineHeight: 1.65 }}>{message.textContent || `[${messageTypeText(message.messageType)}]`}</Typography><Typography sx={{ mt: 0.35, fontSize: 11, opacity: 0.72 }}>{mine ? "HR" : "候选人"} · {formatDate(message.platformSentAt || message.createdAt)}</Typography></Box></Box>;
    }) : <Typography color="text.secondary">这段会话暂时没有可读消息。</Typography>}</Stack></Box></AccordionDetails>
  </Accordion>;
}

/** AIRecord 展示一次 AI 请求、返回、错误和工具调用。 */
function AIRecord({ record }: { record: CandidateAIRecord }) {
  return <Accordion elevation={0} sx={{ border: "1px solid", borderColor: "divider", borderRadius: "8px !important", "&:before": { display: "none" } }}>
    <AccordionSummary expandIcon={<ExpandMoreRoundedIcon />}><Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap", width: "100%" }}><PsychologyRoundedIcon color="primary" fontSize="small" /><Typography sx={{ fontWeight: 780 }}>{record.positionName || "简历结构化 / 自动回复"}</Typography><Chip size="small" color={record.status === "completed" ? "success" : record.status === "failed" ? "error" : "warning"} label={aiStatusText(record.status)} /><Typography sx={{ ml: "auto !important", color: "text.secondary", fontSize: 12 }}>{record.startedAt ? formatDate(record.startedAt) : ""}</Typography></Stack></AccordionSummary>
    <AccordionDetails><Stack spacing={2}>
      {record.errorMessage ? <Typography color="error.main">错误：{record.errorMessage}</Typography> : null}
      <Box><Typography sx={{ mb: 0.7, fontWeight: 740 }}>发给 AI 的内容</Typography><JsonTree value={record.inputMessages} /></Box>
      <Box><Typography sx={{ mb: 0.7, fontWeight: 740 }}>AI 返回</Typography><JsonTree value={record.outputMessage} /></Box>
      {record.toolCalls.length ? <Box><Typography sx={{ mb: 0.7, fontWeight: 740 }}>工具调用</Typography><Stack spacing={1}>{record.toolCalls.map((tool) => <Box key={tool.id} sx={{ p: 1.25, bgcolor: "action.hover", borderRadius: "8px" }}><Typography sx={{ fontWeight: 720 }}>{tool.sequenceNo}. {tool.toolName} · {aiStatusText(tool.status)}</Typography>{tool.errorMessage ? <Typography color="error.main" sx={{ fontSize: 12 }}>{tool.errorMessage}</Typography> : null}<JsonTree label="参数" value={tool.arguments} /><JsonTree label="结果" value={tool.result} /></Box>)}</Stack></Box> : null}
      <Typography sx={{ color: "text.secondary", fontSize: 12 }}>模型：{record.model || "暂时没记上"} · Token：{record.tokenUsage}</Typography>
    </Stack></AccordionDetails>
  </Accordion>;
}

/** formatFileSize 把附件字节数转换为用户容易理解的大小。 */
function formatFileSize(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "大小暂时没记上";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** platformText 返回招聘平台中文名称。 */
function platformText(value: string) {
  return ({ liepin: "猎聘企业", hliepin: "猎聘猎头", zhaopin: "智联招聘", boss: "BOSS直聘" } as Record<string, string>)[value] || value || "招聘平台";
}

/** messageTypeText 返回非文本消息的中文类型。 */
function messageTypeText(value: string) {
  return ({ resume_card: "简历卡片", file: "文件", image: "图片", voice: "语音", system: "系统消息" } as Record<string, string>)[value] || "消息";
}

/** confirmationTypeText 返回确认项类型中文文案。 */
function confirmationTypeText(value: string) {
  return ({ required: "必须满足", confirm: "需要确认", bonus: "加分项" } as Record<string, string>)[value] || "确认项";
}

/** confirmationStatusText 返回确认项状态中文文案。 */
function confirmationStatusText(value: string) {
  return ({ pending: "待确认", matched: "满足", unmatched: "不满足", not_applicable: "不适用", conflicted: "信息冲突" } as Record<string, string>)[value] || value || "待确认";
}

/** confirmationColor 返回确认项状态对应的 MUI 颜色。 */
function confirmationColor(value: string): "default" | "success" | "error" | "warning" {
  if (value === "matched") return "success";
  if (value === "unmatched") return "error";
  if (value === "conflicted") return "warning";
  return "default";
}

/** aiStatusText 返回 AI 和工具运行状态中文文案。 */
function aiStatusText(value: string) {
  return ({ running: "处理中", completed: "已完成", failed: "失败", notified: "已转人工" } as Record<string, string>)[value] || value || "未知";
}

/** CandidateHeader 展示候选人头像、姓名和基础信息。 */
function CandidateHeader({ candidate }: { candidate: NormalizedCandidate }) {
  const facts = [candidate.age ? `${candidate.age}岁` : "", candidate.gender, candidate.educationLevel, candidate.workYears, candidate.workStatus, candidate.onlineStatus].filter(Boolean);
  return <Stack direction={{ xs: "column", sm: "row" }} spacing={2.25} sx={{ alignItems: { sm: "center" } }}>
    <Avatar src={cloudAssetURL(candidate.avatarUrl)} sx={{ width: 74, height: 74, fontSize: 28 }}>{candidate.name.slice(0, 1)}</Avatar>
    <Box sx={{ minWidth: 0 }}><Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}><Typography component="h2" sx={{ fontSize: 28, fontWeight: 820 }}>{candidate.name}</Typography><Chip size="small" label={statusText(candidate.status)} color="primary" /></Stack><Typography sx={{ mt: 0.6, color: "text.secondary" }}>{facts.join("  |  ") || "暂无基础信息"}</Typography>{candidate.workRegion ? <Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}><LocationOnRoundedIcon sx={{ mr: 0.5, fontSize: 16, verticalAlign: "text-bottom" }} />{candidate.workRegion}</Typography> : null}</Box>
  </Stack>;
}

/** SidePanel 展示候选人状态、按需关联资料、AI 判断和经历概览。 */
function SidePanel({ candidate, loadingSection, onOpenSection }: { candidate: NormalizedCandidate; loadingSection: AutoReplySection | null; onOpenSection: (section: AutoReplySection) => void }) {
  const overview = [...candidate.workExperiences, ...candidate.projectExperiences, ...candidate.educations].map(experienceLine).filter(Boolean).slice(0, 8);
  const sectionButtons: { section: AutoReplySection; label: string; icon: ReactNode }[] = [
    { section: "recommendations", label: "推荐报告", icon: <RecommendRoundedIcon /> },
    { section: "attachments", label: "简历附件", icon: <AttachFileRoundedIcon /> },
    { section: "conversations", label: "沟通记录", icon: <ForumRoundedIcon /> },
    { section: "confirmations", label: "条件确认", icon: <FactCheckRoundedIcon /> },
    { section: "ai_records", label: "AI 记录", icon: <PsychologyRoundedIcon /> },
  ];
  return <Box sx={{ p: 3, borderLeft: { lg: "1px solid" }, borderTop: { xs: "1px solid", lg: 0 }, borderColor: "divider", bgcolor: "action.hover" }}>
    <Typography sx={{ mb: 1.5, color: "text.secondary", fontWeight: 760 }}>候选人状态</Typography>
    <Chip label={statusText(candidate.status)} color="primary" sx={{ mb: 3 }} />
    <Typography sx={{ mb: 1.5, fontWeight: 820 }}>记录来源</Typography>
    <Stack spacing={0.7} sx={{ mb: 3 }}>
      <Typography sx={{ color: "text.secondary", fontSize: 13 }}>创建人：{candidate.creatorEmail || "暂时没记上"}</Typography>
      <Typography sx={{ color: "text.secondary", fontSize: 13 }}>创建时间：{candidate.createdAt ? formatDate(candidate.createdAt) : "暂时没记上"}</Typography>
    </Stack>
    <Typography sx={{ mb: 1.5, fontWeight: 820 }}>关联资料</Typography>
    <Stack spacing={1} sx={{ mb: 3 }}>{sectionButtons.map((item) => <Button key={item.section} variant="outlined" startIcon={loadingSection === item.section ? <CircularProgress size={16} /> : item.icon} onClick={() => onOpenSection(item.section)} sx={{ justifyContent: "flex-start" }}>{item.label}</Button>)}</Stack>
    <Typography sx={{ mb: 1.5, fontWeight: 820 }}>AI 判断</Typography>
    <Stack spacing={1.25}>
      <AIBlock title="第一次分析" score={candidate.aiFirstAnalysis.score} reason={candidate.aiFirstAnalysis.reason} />
      <AIBlock title="第二次分析" score={candidate.aiSecondAnalysis.score} reason={candidate.aiSecondAnalysis.reason} />
    </Stack>
    <Typography sx={{ mt: 3, mb: 1.5, fontWeight: 820 }}>经历概览</Typography>
    <Stack spacing={1}>{overview.length ? overview.map((item) => <Typography key={item} sx={{ pl: 1.25, borderLeft: "3px solid", borderColor: "primary.light", fontSize: 13, lineHeight: 1.6 }}>{item}</Typography>) : <Typography color="text.secondary">暂无经历</Typography>}</Stack>
  </Box>;
}

/** AIBlock 展示一个 AI 阶段的分数和原因。 */
function AIBlock({ title, score, reason }: { title: string; score: unknown; reason: string }) {
  return <Box sx={{ p: 1.25, borderRadius: "8px", bgcolor: "action.selected" }}>
    <Stack direction="row" sx={{ justifyContent: "space-between", gap: 1 }}><Typography sx={{ fontWeight: 760 }}>{title}</Typography><Typography sx={{ color: "primary.main", fontWeight: 820 }}>{scoreText(score)}</Typography></Stack>
    <Typography sx={{ mt: 0.7, color: "text.secondary", fontSize: 13, lineHeight: 1.6 }}>{reason || "暂时没有返回原因"}</Typography>
  </Box>;
}

/** recommendationURL 返回当前前端域名下的一份公开推荐地址。 */
function recommendationURL(publicID: string) {
  const path = `/recommendations/${encodeURIComponent(publicID)}`;
  return typeof window === "undefined" ? path : `${window.location.origin}${path}`;
}

/** ResumeSection 输出一个有内容的简历区块。 */
function ResumeSection({ title, children }: { title: string; children: ReactNode }) {
  return <Box component="section" sx={{ mt: 4, pt: 3, borderTop: "1px solid", borderColor: "divider" }}><Typography component="h3" sx={{ mb: 2, fontSize: 19, fontWeight: 820 }}>{title}</Typography>{children}</Box>;
}

/** Experience 展示一条工作、项目或教育经历。 */
function Experience({ item, project = false }: { item: NormalizedExperience; project?: boolean }) {
  const title = item.companyName || item.schoolName || item.projectName || "未填写名称";
  const subtitle = [item.positionName || item.majorName || item.roleName || item.educationLevel, periodText(item)].filter(Boolean).join("  |  ");
  return <Box sx={{ mb: 2.5 }}>
    <Stack direction={{ xs: "column", sm: "row" }} sx={{ justifyContent: "space-between", gap: 1 }}>
      <Typography sx={{ fontWeight: 780 }}>{title}</Typography>
      <Typography sx={{ color: "text.secondary", fontSize: 13 }}>{subtitle}</Typography>
    </Stack>
    {item.content ? <Typography sx={{ mt: 0.8, color: "text.secondary", lineHeight: project ? 1.9 : 1.75, whiteSpace: "pre-wrap" }}>{item.content}</Typography> : null}
  </Box>;
}
