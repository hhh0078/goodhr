/** 本文件负责新版后台候选人简历详情展示。 */
"use client";

import type { ReactNode } from "react";
import ArrowBackRoundedIcon from "@mui/icons-material/ArrowBackRounded";
import { Accordion, AccordionDetails, AccordionSummary, Avatar, Box, Button, Chip, Stack, Typography } from "@mui/material";
import ExpandMoreRoundedIcon from "@mui/icons-material/ExpandMoreRounded";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import JsonTree from "@/components/admin/JsonTree";

/** ResumeDetailPage 展示候选人基本信息、经历和分析结果。 */
export default function ResumeDetailPage() {
  const params = useSearchParams();
  const { notify } = useAdmin();
  const [candidate, setCandidate] = useState<any>(null);
  const candidateID = params.get("candidate_id") || "";
  const engagementID = params.get("engagement_id") || "";

  useEffect(() => {
    if (!candidateID) return;
    const query = engagementID ? `?engagement_id=${encodeURIComponent(engagementID)}` : "";
    cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}${query}`).then((data) => setCandidate(data.candidate || data)).catch((error) => notify(error.message, "error"));
  }, [candidateID, engagementID]);

  if (!candidateID) return <SectionPanel><Typography color="error">缺少候选人 ID</Typography></SectionPanel>;
  if (!candidate) return <SectionPanel><Typography color="text.secondary">正在读取简历详情...</Typography></SectionPanel>;
  const resume = candidate.resume_json || candidate.resume || candidate.detail_json || {};
  const experiences = arrayValue(resume.work_experiences || resume.workExperience || candidate.work_experiences);
  const projects = arrayValue(resume.project_experiences || resume.projects || candidate.project_experiences);
  const educations = arrayValue(resume.educations || resume.education_experiences || candidate.educations);
  const skills = arrayValue(resume.skills || candidate.skills).map((item) => typeof item === "string" ? item : item.name).filter(Boolean);
  return <><PageHeader title="简历详情" actions={<Button component={Link} href="/admin/resumes" startIcon={<ArrowBackRoundedIcon />}>返回简历库</Button>} /><SectionPanel><Stack direction={{ xs: "column", sm: "row" }} spacing={2.5} sx={{ alignItems: { sm: "center" } }}><Avatar src={candidate.avatar_url || resume.avatar_url || ""} sx={{ width: 76, height: 76, fontSize: 28 }}>{String(candidate.name || resume.name || "?").slice(0, 1)}</Avatar><Box><Typography component="h2" sx={{ fontSize: 28, fontWeight: 780 }}>{candidate.name || resume.name || "未命名候选人"}</Typography><Typography sx={{ mt: 0.5, color: "text.secondary" }}>{[candidate.age || resume.age ? `${candidate.age || resume.age}岁` : "", candidate.education || resume.education, candidate.experience || resume.experience, candidate.job_status || resume.job_status].filter(Boolean).join(" · ")}</Typography><Stack direction="row" spacing={1} sx={{ mt: 1.25, flexWrap: "wrap" }}>{skills.map((skill) => <Chip key={skill} label={skill} size="small" />)}</Stack></Box></Stack>{textValue(resume.summary || candidate.summary || candidate.description) ? <ResumeSection title="个人优势"><Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.9 }}>{textValue(resume.summary || candidate.summary || candidate.description)}</Typography></ResumeSection> : null}{expectationText(resume, candidate) ? <ResumeSection title="期望职位"><Typography>{expectationText(resume, candidate)}</Typography></ResumeSection> : null}{experiences.length ? <ResumeSection title="工作经历">{experiences.map((item, index) => <Experience key={`${item.company || item.company_name || "work"}-${index}`} item={item} />)}</ResumeSection> : null}{projects.length ? <ResumeSection title="项目经历">{projects.map((item, index) => <Experience key={`${item.name || item.project_name || "project"}-${index}`} item={item} project />)}</ResumeSection> : null}{educations.length ? <ResumeSection title="教育经历">{educations.map((item, index) => <Box key={`${item.school || "school"}-${index}`} sx={{ mb: 2 }}><Typography sx={{ fontWeight: 760 }}>{item.school || "未填写学校"}</Typography><Typography color="text.secondary">{[item.major, item.degree || item.education, item.date_range || [item.start_date, item.end_date].filter(Boolean).join(" - ")].filter(Boolean).join(" · ")}</Typography></Box>)}</ResumeSection> : null}{analysisText(candidate) ? <ResumeSection title="AI 分析"><Stack direction="row" spacing={1} sx={{ mb: 1.5, flexWrap: "wrap" }}>{scoreChip("详情", candidate.ai_detail_score)}{scoreChip("打招呼", candidate.ai_greet_score)}{scoreChip("复核", candidate.ai_review_score)}</Stack><Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.8 }}>{analysisText(candidate)}</Typography></ResumeSection> : null}<Accordion elevation={0} sx={{ mt: 4, bgcolor: "#f7faf8" }}><AccordionSummary expandIcon={<ExpandMoreRoundedIcon />}><Typography sx={{ fontWeight: 720 }}>查看完整结构化数据</Typography></AccordionSummary><AccordionDetails><JsonTree value={resume} /></AccordionDetails></Accordion></SectionPanel></>;
}

/** Experience 展示工作或项目经历。 */
function Experience({ item, project = false }: { item: any; project?: boolean }) { const title = project ? item.name || item.project_name || "未填写项目" : item.company || item.company_name || "未填写公司"; const role = item.position || item.title || item.role || ""; const content = textValue(item.description || item.content || item.achievements || item.responsibilities); return <Box sx={{ mb: 3 }}><Stack direction={{ xs: "column", sm: "row" }} sx={{ justifyContent: "space-between", gap: 1 }}><Typography sx={{ fontWeight: 760 }}>{title}{role ? ` · ${role}` : ""}</Typography><Typography color="text.secondary">{item.date_range || [item.start_date, item.end_date].filter(Boolean).join(" - ")}</Typography></Stack>{content ? <Typography sx={{ mt: 1, whiteSpace: "pre-wrap", lineHeight: 1.8 }}>{content}</Typography> : null}</Box>; }

/** expectationText 合并候选人期望岗位、城市、行业和薪资。 */
function expectationText(resume: any, candidate: any) { return [resume.expected_position || candidate.expected_position, resume.expected_city || candidate.expected_city, resume.expected_industry, resume.expected_salary || candidate.expected_salary].filter(Boolean).join(" · "); }

/** analysisText 合并 AI 分析理由。 */
function analysisText(candidate: any) { return textValue(candidate.analysis?.reason || candidate.analysis_reason || candidate.reason || candidate.ai_greet_reason || candidate.ai_detail_reason); }

/** scoreChip 在有分数时生成评分标签。 */
function scoreChip(label: string, value: unknown) { const score = Number(value); return Number.isFinite(score) && value !== null && value !== "" ? <Chip key={label} size="small" color={score >= 60 ? "success" : "default"} label={`${label} ${score} 分`} /> : null; }

/** ResumeSection 输出一个有内容的简历区块。 */
function ResumeSection({ title, children }: { title: string; children: ReactNode }) { return <Box component="section" sx={{ mt: 4, pt: 3, borderTop: "1px solid", borderColor: "divider" }}><Typography component="h3" sx={{ mb: 2, fontSize: 19, fontWeight: 780 }}>{title}</Typography>{children}</Box>; }

/** arrayValue 将未知值安全转换为数组。 */
function arrayValue(value: unknown): any[] { return Array.isArray(value) ? value : []; }

/** textValue 将未知值安全转换为可展示文本。 */
function textValue(value: unknown) { return typeof value === "string" ? value.trim() : value ? JSON.stringify(value, null, 2) : ""; }
