/** 文件作用说明：提供岗位招聘进度、个人待办与抄送入口，支持从真实候选人开启企业招聘。 */
"use client";
import { Alert, Button, Flex, Input, Select, Space, Table, Tabs, Tag, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { SectionPanel } from "@/components/admin/AdminUI";
import ApplicationDialog from "@/components/admin/recruitment/ApplicationDialog";
import StartRecruitmentDialog from "@/components/admin/recruitment/StartRecruitmentDialog";
import RecruitmentSettingsDialog from "@/components/admin/recruitment/RecruitmentSettingsDialog";
import { loadResumePositions } from "@/lib/resume-api";
import { type PositionOption } from "@/lib/resume-types";
import { loadRecruitmentApplications, recruitmentStatusLabel, type RecruitmentApplication } from "@/lib/recruitment-api";

/** RecruitmentPage 使用岗位筛选和本人待办展示独立招聘进程，抄送成员只读查看。 */
export default function RecruitmentPage() {

  const params = useSearchParams();
  const [tab, setTab] = useState("all");
  const [positionID, setPositionID] = useState(params.get("position_id") || "");
  const [query, setQuery] = useState("");
  const [positions, setPositions] = useState<PositionOption[]>([]);
  const [items, setItems] = useState<RecruitmentApplication[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [detailID, setDetailID] = useState(params.get("application_id") || "");
  const [startOpen, setStartOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const generation = useRef(0);
  /** load 查询当前岗位和待办，旧筛选回包不能替换新的查询。 */

  async function load() {

    const current = ++generation.current;
    setLoading(true);
    setError("");
    try {

      const data = await loadRecruitmentApplications({ position_id: positionID, mine: tab === "mine" ? "true" : "" });
      if (current === generation.current) setItems(data);
    } catch (cause) {

      if (current === generation.current) setError(cause instanceof Error ? cause.message : "招聘进度没读出来");
    } finally {

      if (current === generation.current) setLoading(false);
    }
  }

  useEffect(() => {

    void loadResumePositions().then(setPositions).catch((cause) => setError(cause.message || "岗位没读出来"));
  }, []);

  useEffect(() => {

    void load();
    return () => {

      generation.current++;
    };
  }, [tab, positionID]);

  const visible = items.filter((item) => `${item.candidate_name} ${item.position_name} ${item.owner_email}`.toLowerCase().includes(query.trim().toLowerCase()));

  return <><Flex gap={8} className="goodhr-compact-toolbar" data-testid="recruitment-toolbar">
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[{ key: "all", label: "岗位招聘进度" }, { key: "mine", label: "我的待办与抄送" }]} />
        <Select
          aria-label="筛选招聘岗位"
          showSearch
          allowClear
          optionFilterProp="label"
          placeholder="全部岗位"
          value={positionID || undefined}
          onChange={(value) => setPositionID(value || "")}
          options={positions.map((item) => ({ value: item.id, label: item.name }))}
          style={{ width: 180 }} />
        <Input
          aria-label="搜索招聘进度"
          allowClear
          placeholder="姓名、岗位或负责人"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          style={{ width: 220 }} />
      <Flex gap={8} className="goodhr-toolbar-actions">
        <Button onClick={() => setSettingsOpen(true)}>招聘设置</Button>
        <Button type="primary" onClick={() => setStartOpen(true)}>开启招聘</Button>
      </Flex>
    </Flex>{error ? <Alert type="warning" title={error} action={<Button onClick={() => void load()}>重试</Button>} style={{ marginBottom: 12 }} /> : null}<SectionPanel>
      {tab === "mine" ? <Typography.Paragraph type="secondary">面试、审核和负责人待办，以及抄送给你的进度记录。抄送只供查看。</Typography.Paragraph> : null}
      <Table
        size="small"
        rowKey="id"
        loading={loading}
        dataSource={visible}
        scroll={{ x: 880 }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        columns={[
          {

            title: "候选人",
            dataIndex: "candidate_name",
            width: 160,
            render: (value, row) => <Button type="link" style={{ padding: 0 }} onClick={() => setDetailID(row.id)}>
              {value || "未命名候选人"}
            </Button>
          },
          { title: "岗位", dataIndex: "position_name", width: 170 },
          {

            title: "当前节点",
            width: 150,
            render: (_, row) => row.nodes?.[row.current_node]?.name || row.stage || "已结束"
          },
          {

            title: "状态",
            dataIndex: "status",
            width: 100,
            render: (value) => <Tag>
              {recruitmentStatusLabel(value)}
            </Tag>
          },
          { title: "招聘负责人", dataIndex: "owner_email" },
          {

            title: "我的角色",
            width: 160,
            render: (_, row) => <Space wrap size={0}>
              {row.permissions?.can_approve ? <Tag>审核</Tag> : null}
              {row.permissions?.can_feedback ? <Tag>面试</Tag> : null}
              {row.permissions?.can_advance ? <Tag>推进</Tag> : null}
              {!row.permissions?.can_advance && !row.permissions?.can_feedback && !row.permissions?.can_approve ? <Tag>查看 / 抄送</Tag> : null}
            </Space>
          },
          {

            title: "操作",
            width: 120,
            render: (_, row) => <Button size="small" onClick={() => setDetailID(row.id)}>查看进度</Button>
          }
        ]} />
    </SectionPanel><RecruitmentSettingsDialog open={settingsOpen} onClose={() => { setSettingsOpen(false); void load(); }} onChanged={() => void load()} /><StartRecruitmentDialog
      open={startOpen}
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
