/** 本文件负责新版后台候选人简历详情展示。 */
"use client";

import type { ReactNode } from "react";
import ArrowBackRoundedIcon from "@mui/icons-material/ArrowBackRounded";
import ExpandMoreRoundedIcon from "@mui/icons-material/ExpandMoreRounded";
import LocationOnRoundedIcon from "@mui/icons-material/LocationOnRounded";
import { Accordion, AccordionDetails, AccordionSummary, Avatar, Box, Button, Chip, Stack, Typography } from "@mui/material";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import JsonTree from "@/components/admin/JsonTree";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { experienceLine, normalizeCandidate, periodText, scoreText, statusText, type NormalizedCandidate, type NormalizedExperience } from "@/lib/candidate-normalize";

/** ResumeDetailPage 展示候选人基本信息、经历和分析结果。 */
export default function ResumeDetailPage() {
  const params = useSearchParams();
  const { notify } = useAdmin();
  const [rawCandidate, setRawCandidate] = useState<any>(null);
  const candidateID = params.get("candidate_id") || "";
  const engagementID = params.get("engagement_id") || "";
  const candidate = useMemo(() => rawCandidate ? normalizeCandidate(rawCandidate) : null, [rawCandidate]);

  useEffect(() => {
    if (!candidateID) return;
    const query = engagementID ? `?engagement_id=${encodeURIComponent(engagementID)}` : "";
    cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}${query}`).then((data) => setRawCandidate(data.candidate || data)).catch((error) => notify(error.message, "error"));
  }, [candidateID, engagementID, notify]);

  if (!candidateID) return <SectionPanel><Typography color="error">缺少候选人 ID</Typography></SectionPanel>;
  if (!candidate) return <SectionPanel><Typography color="text.secondary">正在读取简历详情...</Typography></SectionPanel>;

  return <>
    <PageHeader title="简历详情" actions={<Button component={Link} href="/admin/resumes" startIcon={<ArrowBackRoundedIcon />}>返回简历库</Button>} />
    <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1fr) 290px" }, minHeight: 0 }}>
        <Box sx={{ p: { xs: 2, md: 3 } }}>
          <CandidateHeader candidate={candidate} />
          <ResumeSection title="求职意向">
            <Typography>{[candidate.workRegion, candidate.expectedPosition, candidate.expectedSalary, candidate.workStatus, candidate.onlineStatus].filter(Boolean).join("  |  ") || "暂无求职意向"}</Typography>
          </ResumeSection>
          {candidate.personalDescription ? <ResumeSection title="个人优势"><Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.9 }}>{candidate.personalDescription}</Typography></ResumeSection> : null}
          {candidate.workExperiences.length ? <ResumeSection title="工作经历">{candidate.workExperiences.map((item, index) => <Experience key={`work-${index}`} item={item} />)}</ResumeSection> : null}
          {candidate.projectExperiences.length ? <ResumeSection title="项目经历">{candidate.projectExperiences.map((item, index) => <Experience key={`project-${index}`} item={item} project />)}</ResumeSection> : null}
          {candidate.educations.length ? <ResumeSection title="教育经历">{candidate.educations.map((item, index) => <Experience key={`edu-${index}`} item={item} />)}</ResumeSection> : null}
          {candidate.rawText ? <ResumeSection title="原始文本"><Typography sx={{ whiteSpace: "pre-wrap", color: "text.secondary", lineHeight: 1.8 }}>{candidate.rawText}</Typography></ResumeSection> : null}
          <Accordion elevation={0} sx={{ mt: 3, bgcolor: "#f7faf8" }}><AccordionSummary expandIcon={<ExpandMoreRoundedIcon />}><Typography sx={{ fontWeight: 720 }}>查看完整接口数据</Typography></AccordionSummary><AccordionDetails><JsonTree value={candidate.raw} /></AccordionDetails></Accordion>
        </Box>
        <SidePanel candidate={candidate} />
      </Box>
    </SectionPanel>
  </>;
}

/** CandidateHeader 展示候选人头像、姓名和基础信息。 */
function CandidateHeader({ candidate }: { candidate: NormalizedCandidate }) {
  const facts = [candidate.age ? `${candidate.age}岁` : "", candidate.gender, candidate.educationLevel, candidate.workYears, candidate.workStatus, candidate.onlineStatus].filter(Boolean);
  return <Stack direction={{ xs: "column", sm: "row" }} spacing={2.25} sx={{ alignItems: { sm: "center" } }}>
    <Avatar src={candidate.avatarUrl} sx={{ width: 74, height: 74, fontSize: 28 }}>{candidate.name.slice(0, 1)}</Avatar>
    <Box sx={{ minWidth: 0 }}><Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}><Typography component="h2" sx={{ fontSize: 28, fontWeight: 820 }}>{candidate.name}</Typography><Chip size="small" label={statusText(candidate.status)} color="primary" /></Stack><Typography sx={{ mt: 0.6, color: "text.secondary" }}>{facts.join("  |  ") || "暂无基础信息"}</Typography>{candidate.workRegion ? <Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}><LocationOnRoundedIcon sx={{ mr: 0.5, fontSize: 16, verticalAlign: "text-bottom" }} />{candidate.workRegion}</Typography> : null}</Box>
  </Stack>;
}

/** SidePanel 展示候选人状态、AI 判断和经历概览。 */
function SidePanel({ candidate }: { candidate: NormalizedCandidate }) {
  const overview = [...candidate.workExperiences, ...candidate.projectExperiences, ...candidate.educations].map(experienceLine).filter(Boolean).slice(0, 8);
  return <Box sx={{ p: 3, borderLeft: { lg: "1px solid" }, borderTop: { xs: "1px solid", lg: 0 }, borderColor: "divider", bgcolor: "#fbfcfb" }}>
    <Typography sx={{ mb: 1.5, color: "text.secondary", fontWeight: 760 }}>候选人状态</Typography>
    <Chip label={statusText(candidate.status)} color="primary" sx={{ mb: 3 }} />
    <Typography sx={{ mb: 1.5, fontWeight: 820 }}>记录来源</Typography>
    <Stack spacing={0.7} sx={{ mb: 3 }}>
      <Typography sx={{ color: "text.secondary", fontSize: 13 }}>创建人：{candidate.creatorEmail || "暂时没记上"}</Typography>
      <Typography sx={{ color: "text.secondary", fontSize: 13 }}>创建时间：{candidate.createdAt ? formatDate(candidate.createdAt) : "暂时没记上"}</Typography>
    </Stack>
    <Typography sx={{ mb: 1.5, fontWeight: 820 }}>AI 判断</Typography>
    <Stack spacing={1.25}>
      <AIBlock title="第一次分析" score={candidate.aiFirstAnalysis.score} reason={candidate.aiFirstAnalysis.reason} />
      <AIBlock title="第二次分析" score={candidate.aiSecondAnalysis.score} reason={candidate.aiSecondAnalysis.reason} />
    </Stack>
    <Typography sx={{ mt: 3, mb: 1.5, fontWeight: 820 }}>经历概览</Typography>
    <Stack spacing={1}>{overview.length ? overview.map((item) => <Typography key={item} sx={{ pl: 1.25, borderLeft: "3px solid #8fcf9d", fontSize: 13, lineHeight: 1.6 }}>{item}</Typography>) : <Typography color="text.secondary">暂无经历</Typography>}</Stack>
  </Box>;
}

/** AIBlock 展示一个 AI 阶段的分数和原因。 */
function AIBlock({ title, score, reason }: { title: string; score: unknown; reason: string }) {
  return <Box sx={{ p: 1.25, borderRadius: "8px", bgcolor: "#f2f7f3" }}>
    <Stack direction="row" sx={{ justifyContent: "space-between", gap: 1 }}><Typography sx={{ fontWeight: 760 }}>{title}</Typography><Typography sx={{ color: "#16724c", fontWeight: 820 }}>{scoreText(score)}</Typography></Stack>
    <Typography sx={{ mt: 0.7, color: "text.secondary", fontSize: 13, lineHeight: 1.6 }}>{reason || "暂时没有返回原因"}</Typography>
  </Box>;
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
