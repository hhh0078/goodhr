/** 文件作用说明：纯展示简历资料，供后台详情、抽屉和免登录分享页共用，不读取账户或内部工作记录。 */
"use client";
import { Avatar, Descriptions, Flex, Space, Tag, Typography } from "antd";
import type { ReactNode } from "react";
import { experienceLabel, genderLabel, resumeAge, resumeAvatarSource, salaryLabel, workYearsLabel, type Resume, type ResumeExperience } from "@/lib/resume-types";
import styles from "./resumes.module.css";

/** ResumeProfile 保留现有紧凑资料布局，维护按钮由后台调用者传入，公开页只展示资料。 */
export default function ResumeProfile({ candidate, headerActions, showTags = false }: { candidate: Resume; headerActions?: ReactNode; showTags?: boolean }) {
  const facts = [resumeAge(candidate), genderLabel(candidate.gender), candidate.education_level, workYearsLabel(candidate)].filter(Boolean);
  return <>
    <Flex gap={12} align="center" className={styles.candidateHeader}>
      <Avatar size={52} src={resumeAvatarSource(candidate)} alt={`${candidate.candidate_name || "候选人"}头像`} style={{ flexShrink: 0 }}>{candidate.candidate_name?.slice(0, 1) || "人"}</Avatar>
      <Flex vertical gap={4} style={{ minWidth: 0, flex: 1 }}>
        <Flex wrap gap={10} align="center"><Typography.Title level={3} style={{ margin: 0 }}>{candidate.candidate_name || "未命名候选人"}</Typography.Title><Typography.Text copyable={candidate.phone ? { text: candidate.phone } : false}>{candidate.phone || "手机号待补充"}</Typography.Text></Flex>
        <Typography.Text type="secondary">{facts.join(" · ") || "基础信息待补充"}</Typography.Text>
        {candidate.email ? <Typography.Text copyable={{ text: candidate.email }}>{candidate.email}</Typography.Text> : null}
      </Flex>
      {headerActions}
    </Flex>
    {showTags && candidate.tags?.length ? <Space wrap style={{ marginTop: 6 }}>{candidate.tags.map((item) => <Tag key={item}>{item}</Tag>)}</Space> : null}
    <section className={styles.resumeSection}><Typography.Title level={5}>求职意向</Typography.Title><Descriptions size="small" column={{ xs: 1, sm: 2 }} items={[
      { key: "current", label: "当前地区", children: candidate.current_region || "未填写" },
      { key: "expected", label: "期望地区", children: candidate.expected_region || candidate.work_region || "未填写" },
      { key: "position", label: "期望职位", children: candidate.expected_position || "未填写" },
      { key: "salary", label: "期望月薪", children: salaryLabel(candidate) },
      { key: "status", label: "求职状态", children: candidate.work_status || "未填写" },
    ]} /></section>
    {(candidate.personal_description || candidate.basic_info) ? <TextSection title="个人优势" text={candidate.personal_description || candidate.basic_info} /> : null}
    <ExperienceSection title="工作经历" items={candidate.work_experiences || []} />
    <ExperienceSection title="项目经历" items={candidate.project_experiences || []} />
    <ExperienceSection title="教育经历" items={candidate.educations || []} />
    {candidate.certificates?.length ? <TextSection title="证书" text={candidate.certificates.map((item) => [item.certificate_name, item.issued_by, item.issued_ym].filter(Boolean).join(" · ")).join("\n")} /> : null}
    {candidate.honors?.length ? <TextSection title="荣誉" text={candidate.honors.map((item) => [item.honor_name, item.issued_by, item.issued_ym, item.description].filter(Boolean).join(" · ")).join("\n")} /> : null}
  </>;
}

/** TextSection 展示完整简历段落并保留原文换行。 */
function TextSection({ title, text }: { title: string; text?: string }) {
  return <section className={styles.resumeSection}><Typography.Title level={5}>{title}</Typography.Title><Typography.Paragraph className={styles.preWrap}>{text}</Typography.Paragraph></section>;
}

/** ExperienceSection 展示结构化经历，保留全部条目和说明。 */
function ExperienceSection({ title, items }: { title: string; items: ResumeExperience[] }) {
  if (!items.length) return null;
  return <section className={styles.resumeSection}><Typography.Title level={5}>{title}</Typography.Title>{items.map((item, index) => <div key={index} className={styles.experience}><Typography.Text>{item.company_name ? <Typography.Text strong>{item.company_name}</Typography.Text> : null}{item.company_name ? experienceLabel(item).slice(item.company_name.length) : experienceLabel(item) || "未填写名称"}</Typography.Text>{item.content ? <Typography.Paragraph className={styles.preWrap}>{item.content}</Typography.Paragraph> : null}</div>)}</section>;
}
