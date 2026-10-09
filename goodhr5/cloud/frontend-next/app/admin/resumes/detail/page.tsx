/** 本文件负责 候选人简历详情 的 Ant Design 页面。 */
"use client";
import type { ReactNode } from "react";
import { Avatar, Button, Collapse, Flex, Tag, Typography } from "antd";
import {
  ArrowLeftOutlined,
  DownOutlined,
  EnvironmentOutlined,
} from "@ant-design/icons";
import styles from "./page.module.css";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import JsonTree from "@/components/admin/JsonTree";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  experienceLine,
  normalizeCandidate,
  periodText,
  scoreText,
  statusText,
  type NormalizedCandidate,
  type NormalizedExperience,
} from "@/lib/candidate-normalize";
/** ResumeDetailPage 展示候选人基本信息、经历和分析结果。 */
export default function ResumeDetailPage() {
  const params = useSearchParams();
  const { notify } = useAdmin();
  const [rawCandidate, setRawCandidate] = useState<any>(null);
  const candidateID = params.get("candidate_id") || "";
  const engagementID = params.get("engagement_id") || "";
  const candidate = useMemo(
    () => (rawCandidate ? normalizeCandidate(rawCandidate) : null),
    [rawCandidate],
  );
  useEffect(() => {
    if (!candidateID) return;
    const query = engagementID
      ? `?engagement_id=${encodeURIComponent(engagementID)}`
      : "";
    cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}${query}`)
      .then((data) => setRawCandidate(data.candidate || data))
      .catch((error) => notify(error.message, "error"));
  }, [candidateID, engagementID, notify]);
  if (!candidateID)
    return (
      <SectionPanel>
        <Typography.Text type="danger" style={{ display: "block", margin: 0 }}>
          缺少候选人 ID
        </Typography.Text>
      </SectionPanel>
    );
  if (!candidate)
    return (
      <SectionPanel>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
          }}
        >
          正在读取简历详情...
        </Typography.Text>
      </SectionPanel>
    );
  return (
    <>
      <PageHeader
        title="简历详情"
        actions={
          <Button
            icon={<ArrowLeftOutlined aria-hidden="true" />}
            href="/admin/resumes"
          >
            返回简历库
          </Button>
        }
      />
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <div
          className={styles.layout6}
          style={{ display: "grid", minHeight: 0 }}
        >
          <div className={styles.layout5}>
            <CandidateHeader candidate={candidate} />
            <ResumeSection title="求职意向">
              <Typography.Text style={{ display: "block", margin: 0 }}>
                {[
                  candidate.workRegion,
                  candidate.expectedPosition,
                  candidate.expectedSalary,
                  candidate.workStatus,
                  candidate.onlineStatus,
                ]
                  .filter(Boolean)
                  .join("  |  ") || "暂无求职意向"}
              </Typography.Text>
            </ResumeSection>
            {candidate.personalDescription ? (
              <ResumeSection title="个人优势">
                <Typography.Text
                  style={{
                    display: "block",
                    margin: 0,
                    whiteSpace: "pre-wrap",
                    lineHeight: 1.9,
                  }}
                >
                  {candidate.personalDescription}
                </Typography.Text>
              </ResumeSection>
            ) : null}
            {candidate.workExperiences.length ? (
              <ResumeSection title="工作经历">
                {candidate.workExperiences.map((item, index) => (
                  <Experience key={`work-${index}`} item={item} />
                ))}
              </ResumeSection>
            ) : null}
            {candidate.projectExperiences.length ? (
              <ResumeSection title="项目经历">
                {candidate.projectExperiences.map((item, index) => (
                  <Experience key={`project-${index}`} item={item} project />
                ))}
              </ResumeSection>
            ) : null}
            {candidate.educations.length ? (
              <ResumeSection title="教育经历">
                {candidate.educations.map((item, index) => (
                  <Experience key={`edu-${index}`} item={item} />
                ))}
              </ResumeSection>
            ) : null}
            {candidate.rawText ? (
              <ResumeSection title="原始文本">
                <Typography.Text
                  style={{
                    display: "block",
                    margin: 0,
                    whiteSpace: "pre-wrap",
                    color: "var(--goodhr-text-secondary)",
                    lineHeight: 1.8,
                  }}
                >
                  {candidate.rawText}
                </Typography.Text>
              </ResumeSection>
            ) : null}
            <Collapse
              style={{ marginTop: 24 }}
              items={[
                {
                  key: "raw",
                  label: "查看完整接口数据",
                  children: <JsonTree value={candidate.raw} />,
                },
              ]}
            />
          </div>
          <SidePanel candidate={candidate} />
        </div>
      </SectionPanel>
    </>
  );
}
/** CandidateHeader 展示候选人头像、姓名和基础信息。 */
function CandidateHeader({ candidate }: { candidate: NormalizedCandidate }) {
  const facts = [
    candidate.age ? `${candidate.age}岁` : "",
    candidate.gender,
    candidate.educationLevel,
    candidate.workYears,
    candidate.workStatus,
    candidate.onlineStatus,
  ].filter(Boolean);
  return (
    <Flex
      className={
        styles.layout15 + " " + styles.layout16 + " " + styles.layout15
      }
      gap={2.25 * 8}
    >
      <Avatar
        style={{ width: 74, height: 74, fontSize: 28 }}
        src={candidate.avatarUrl}
      >
        {candidate.name.slice(0, 1)}
      </Avatar>
      <div style={{ minWidth: 0 }}>
        <Flex gap={1 * 8} style={{ alignItems: "center", flexWrap: "wrap" }}>
          <Typography.Title
            level={2}
            style={{
              display: "block",
              margin: 0,
              fontSize: 28,
              fontWeight: 820,
            }}
          >
            {candidate.name}
          </Typography.Title>
          <Tag color={"var(--goodhr-primary)"}>
            {statusText(candidate.status)}
          </Tag>
        </Flex>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            marginTop: 4.8,
            color: "var(--goodhr-text-secondary)",
          }}
        >
          {facts.join("  |  ") || "暂无基础信息"}
        </Typography.Text>
        {candidate.workRegion ? (
          <Typography.Text
            style={{
              display: "block",
              margin: 0,
              marginTop: 8,
              color: "var(--goodhr-text-secondary)",
              fontSize: 13,
            }}
          >
            <EnvironmentOutlined
              aria-hidden="true"
              style={{
                marginRight: 4,
                fontSize: 16,
                verticalAlign: "text-bottom",
              }}
            />
            {candidate.workRegion}
          </Typography.Text>
        ) : null}
      </div>
    </Flex>
  );
}
/** SidePanel 展示候选人状态、AI 判断和经历概览。 */
function SidePanel({ candidate }: { candidate: NormalizedCandidate }) {
  const overview = [
    ...candidate.workExperiences,
    ...candidate.projectExperiences,
    ...candidate.educations,
  ]
    .map(experienceLine)
    .filter(Boolean)
    .slice(0, 8);
  return (
    <div
      className={styles.layout26}
      style={{
        padding: 24,
        borderColor: "var(--goodhr-border)",
        backgroundColor: "var(--goodhr-hover)",
      }}
    >
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 12,
          color: "var(--goodhr-text-secondary)",
          fontWeight: 760,
        }}
      >
        候选人状态
      </Typography.Text>
      <Tag color={"var(--goodhr-primary)"} style={{ marginBottom: 24 }}>
        {statusText(candidate.status)}
      </Tag>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 12,
          fontWeight: 820,
        }}
      >
        记录来源
      </Typography.Text>
      <Flex vertical={true} gap={0.7 * 8} style={{ marginBottom: 24 }}>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 13,
          }}
        >
          创建人：{candidate.creatorEmail || "暂时没记上"}
        </Typography.Text>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 13,
          }}
        >
          创建时间：
          {candidate.createdAt ? formatDate(candidate.createdAt) : "暂时没记上"}
        </Typography.Text>
      </Flex>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 12,
          fontWeight: 820,
        }}
      >
        AI 判断
      </Typography.Text>
      <Flex vertical={true} gap={1.25 * 8}>
        <AIBlock
          title="第一次分析"
          score={candidate.aiFirstAnalysis.score}
          reason={candidate.aiFirstAnalysis.reason}
        />
        <AIBlock
          title="第二次分析"
          score={candidate.aiSecondAnalysis.score}
          reason={candidate.aiSecondAnalysis.reason}
        />
      </Flex>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginTop: 24,
          marginBottom: 12,
          fontWeight: 820,
        }}
      >
        经历概览
      </Typography.Text>
      <Flex vertical={true} gap={1 * 8}>
        {overview.length ? (
          overview.map((item) => (
            <Typography.Text
              style={{
                display: "block",
                margin: 0,
                paddingLeft: 10,
                borderLeft: "3px solid",
                borderColor: "var(--goodhr-primary-soft)",
                fontSize: 13,
                lineHeight: 1.6,
              }}
              key={item}
            >
              {item}
            </Typography.Text>
          ))
        ) : (
          <Typography.Text
            style={{
              display: "block",
              margin: 0,
              color: "var(--goodhr-text-secondary)",
            }}
          >
            暂无经历
          </Typography.Text>
        )}
      </Flex>
    </div>
  );
}
/** AIBlock 展示一个 AI 阶段的分数和原因。 */
function AIBlock({
  title,
  score,
  reason,
}: {
  title: string;
  score: unknown;
  reason: string;
}) {
  return (
    <div
      style={{
        padding: 10,
        borderRadius: "8px",
        backgroundColor: "var(--goodhr-primary-soft)",
      }}
    >
      <Flex style={{ justifyContent: "space-between", gap: 8 }}>
        <Typography.Text
          style={{ display: "block", margin: 0, fontWeight: 760 }}
        >
          {title}
        </Typography.Text>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-primary)",
            fontWeight: 820,
          }}
        >
          {scoreText(score)}
        </Typography.Text>
      </Flex>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginTop: 5.6,
          color: "var(--goodhr-text-secondary)",
          fontSize: 13,
          lineHeight: 1.6,
        }}
      >
        {reason || "暂时没有返回原因"}
      </Typography.Text>
    </div>
  );
}
/** ResumeSection 输出一个有内容的简历区块。 */
function ResumeSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section
      style={{
        marginTop: 32,
        paddingTop: 24,
        borderTop: "1px solid",
        borderColor: "var(--goodhr-border)",
      }}
    >
      <Typography.Title
        level={3}
        style={{
          display: "block",
          margin: 0,
          marginBottom: 16,
          fontSize: 19,
          fontWeight: 820,
        }}
      >
        {title}
      </Typography.Title>
      {children}
    </section>
  );
}
/** Experience 展示一条工作、项目或教育经历。 */
function Experience({
  item,
  project = false,
}: {
  item: NormalizedExperience;
  project?: boolean;
}) {
  const title =
    item.companyName || item.schoolName || item.projectName || "未填写名称";
  const subtitle = [
    item.positionName || item.majorName || item.roleName || item.educationLevel,
    periodText(item),
  ]
    .filter(Boolean)
    .join("  |  ");
  return (
    <div style={{ marginBottom: 20 }}>
      <Flex
        className={styles.layout37}
        style={{ justifyContent: "space-between", gap: 8 }}
      >
        <Typography.Text
          style={{ display: "block", margin: 0, fontWeight: 780 }}
        >
          {title}
        </Typography.Text>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 13,
          }}
        >
          {subtitle}
        </Typography.Text>
      </Flex>
      {item.content ? (
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            marginTop: 6.4,
            color: "var(--goodhr-text-secondary)",
            lineHeight: project ? 1.9 : 1.75,
            whiteSpace: "pre-wrap",
          }}
        >
          {item.content}
        </Typography.Text>
      ) : null}
    </div>
  );
}
