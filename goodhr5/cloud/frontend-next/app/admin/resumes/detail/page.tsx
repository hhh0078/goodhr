/** 文件作用说明：保留旧简历详情地址，复用新的简历正文、备注和人工推荐工作区。 */
"use client";
import { Button, Typography } from "antd";
import { ArrowLeftOutlined } from "@ant-design/icons";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import ResumeWorkspace from "@/components/admin/resumes/ResumeWorkspace";
import { loadResumePositions } from "@/lib/resume-api";
import type { PositionOption } from "@/lib/resume-types";
/** ResumeDetailPage 兼容候选人和自动任务参数，并恢复来源列表查询。 */
export default function ResumeDetailPage() {
  const params = useSearchParams(); const { notify } = useAdmin();
  const [positions, setPositions] = useState<PositionOption[]>([]);
  const candidateID = params.get("candidate_id") || "";
  const returnTo = params.get("return_to") || "";
  const safeReturn = /^\/admin\/resumes(?:\?|$)/.test(returnTo) ? returnTo : "/admin/resumes";
  useEffect(() => { void loadResumePositions().then(setPositions).catch((cause) => notify(cause.message || "岗位没读出来，可稍后刷新。", "warning")); }, []);
  return <><PageHeader title="简历详情" actions={<Button icon={<ArrowLeftOutlined />} href={safeReturn}>返回简历库</Button>} /><SectionPanel>{candidateID ? <ResumeWorkspace candidateID={candidateID} engagementID={params.get("engagement_id") || ""} positions={positions} /> : <Typography.Text type="warning">没有找到简历编号，请回简历库重新打开。</Typography.Text>}</SectionPanel></>;
}
