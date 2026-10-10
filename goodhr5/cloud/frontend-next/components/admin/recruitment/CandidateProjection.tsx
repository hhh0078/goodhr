/** 文件作用说明：展示招聘接口授权返回的有限简历资料，让面试官无需访问整个简历库。 */
"use client";
import { Descriptions, Flex, Typography } from "antd";
import type { Resume, ResumeExperience } from "@/lib/resume-types";

/** ProjectionExperiences 显示该次招聘允许查看的经历字段，空资料安全显示待补充。 */
function ProjectionExperiences({ title, items }: { title: string; items?: ResumeExperience[] }) {

  return <section>
    <Typography.Title level={5} style={{ margin: "12px 0 6px" }}>
      {title}
    </Typography.Title>
    {Array.isArray(items) && items.length ? items.map((item, index) => <div key={index} style={{ marginBottom: 10 }}>
      <Typography.Text strong>
        {[
          item.company_name || item.school_name || item.project_name,
          item.position_name || item.major_name || item.role_name
        ].filter(Boolean).join(" · ")}
      </Typography.Text>
      <div>
        <Typography.Text type="secondary">
          {[item.start_ym, item.end_ym].filter(Boolean).join(" — ")}
        </Typography.Text>
      </div>
      {item.content ? <Typography.Paragraph style={{ whiteSpace: "pre-wrap", marginBottom: 0 }}>
        {item.content}
      </Typography.Paragraph> : null}
    </div>) : <Typography.Text type="secondary">暂未提供。</Typography.Text>}
  </section>;
}
/** CandidateProjection 仅渲染授权接口已经返回的资料，不额外查询候选人库或内部备注。 */
export default function CandidateProjection({ candidate }: { candidate?: Resume }) {

  if (!candidate) return <Typography.Text type="secondary">这份招聘资料暂时没有读出来，可以刷新进度重试。</Typography.Text>;
  return <Flex vertical gap={4}>
    <Descriptions
      size="small"
      column={{ xs: 1, sm: 2 }}
      items={[
        { key: "name", label: "姓名", children: candidate.candidate_name },
        { key: "phone", label: "联系电话", children: candidate.phone || "待补充" },
        { key: "email", label: "邮箱", children: candidate.email || "待补充" },
        { key: "region", label: "地区", children: candidate.work_region || "待补充" },
        { key: "years", label: "工作年限", children: candidate.work_years || "待补充" },
        { key: "education", label: "学历", children: candidate.education_level || "待补充" }
      ]} />
    {candidate.basic_info || candidate.personal_description ? <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>
      {[candidate.basic_info, candidate.personal_description].filter(Boolean).join("\n")}
    </Typography.Paragraph> : null}
    <ProjectionExperiences title="工作经历" items={candidate.work_experiences} />
    <ProjectionExperiences title="教育经历" items={candidate.educations} />
    <ProjectionExperiences title="项目经历" items={candidate.project_experiences} />
  </Flex>;
}
