/** 文件作用说明：在简历右侧追加可折叠企业招聘入口，独立加载岗位进度并保护只读示例。 */
"use client";
import { Alert, Button, Collapse, Empty, Flex, Tag, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { useAdmin } from "../AdminApp";
import { isReadOnlyResume, type PositionOption, type Resume } from "@/lib/resume-types";
import { loadRecruitmentApplications, recruitmentStatusLabel, type RecruitmentApplication } from "@/lib/recruitment-api";
import ApplicationDialog from "./ApplicationDialog";
import StartRecruitmentDialog from "./StartRecruitmentDialog";

/** ResumeRecruitmentPanel 仅展开后读取进程，不干扰已有备注、推荐显示偏好和自动任务。 */
export default function ResumeRecruitmentPanel({ candidate, positions, positionID = "" }: { candidate: Resume; positions: PositionOption[]; positionID?: string }) {

  const { notify } = useAdmin();
  const [expanded, setExpanded] = useState(false);
  const [applications, setApplications] = useState<RecruitmentApplication[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [startOpen, setStartOpen] = useState(false);
  const [detailID, setDetailID] = useState("");
  const generation = useRef(0);
  /** load 查询当前候选人的全部岗位招聘，历史进程保持独立。 */

  async function load() {

    if (isReadOnlyResume(candidate)) return;
    const current = ++generation.current;
    setLoading(true);
    setError("");
    try {

      const data = await loadRecruitmentApplications({ candidate_id: candidate.id });
      if (current === generation.current) setApplications(data);
    } catch (cause) {

      if (current === generation.current) {
        const message = cause instanceof Error ? cause.message : "招聘进度暂时没读出来";
        setError(message);
        notify(message, "warning");
      }
    } finally {

      if (current === generation.current) setLoading(false);
    }
  }

  useEffect(() => {

    setApplications([]);
    setError("");
    setStartOpen(false);
    setDetailID("");
    if (expanded) void load();
    return () => {

      generation.current++;
    };
  }, [candidate.id, expanded]);

  if (isReadOnlyResume(candidate)) return null;

  return <><Collapse
    size="small"
    activeKey={expanded ? ["hr"] : []}
    onChange={(keys) => setExpanded(keys.includes("hr"))}
    data-testid="resume-recruitment-section"
    items={[
      {

        key: "hr",
        label: "企业 HR 招聘进度",
        children: <Flex vertical gap={8}>
          <Typography.Text type="secondary">按岗位推进面试和录用，与上面的推荐分开记。</Typography.Text>
          {error ? <Alert type="warning" title={error} action={<Button loading={loading} onClick={() => void load()}>重试</Button>} /> : null}
          {applications.length ? applications.map((item) => <Flex key={item.id} justify="space-between" gap={8} wrap>
            <div>
              <Typography.Text>
                {item.position_name}
              </Typography.Text>
              <div>
                <Tag>
                  {item.nodes?.[item.current_node]?.name || item.stage || "已结束"}
                </Tag>
                <Typography.Text type="secondary">
                  {recruitmentStatusLabel(item.status)}
                </Typography.Text>
              </div>
            </div>
            <Button size="small" onClick={() => setDetailID(item.id)}>查看招聘进度</Button>
          </Flex>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={loading ? "正在读取招聘进度" : "还没有企业招聘进程。"} />}
          <Flex gap={8} justify="flex-end">
            <Button size="small" type="primary" onClick={() => setStartOpen(true)}>开启招聘</Button>
          </Flex>
        </Flex>
      }
    ]} /><StartRecruitmentDialog
      open={startOpen}
      candidate={candidate}
      positions={positions}
      initialPositionID={positionID}
      onClose={() => setStartOpen(false)}
      onStarted={(id) => {

        setDetailID(id);
        void load();
      }} /><ApplicationDialog
      applicationID={detailID}
      onClose={() => setDetailID("")}
      onChanged={() => void load()} /></>;
}
