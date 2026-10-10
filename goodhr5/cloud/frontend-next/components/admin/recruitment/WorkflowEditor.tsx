/** 文件作用说明：编辑团队招聘流程模板，配置每个节点的负责人、面试官、审核人与抄送人。 */
"use client";
import { Button, Checkbox, Collapse, Flex, Form, Input, Select, Typography } from "antd";
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { useEffect, useState } from "react";
import AdminDialog from "../AdminDialog";
import { newRecruitmentNode, recruitmentMemberOptions, recruitmentNodeTypes, type RecruitmentMailTemplate, type RecruitmentMember, type RecruitmentNode, type RecruitmentWorkflow } from "@/lib/recruitment-api";

/** WorkflowEditor 提供有序节点编辑，人员只从当前有效团队成员中选择。 */
export default function WorkflowEditor({ open, workflow, members, templates, loading, onClose, onSave }: { open: boolean; workflow: RecruitmentWorkflow | null; members: RecruitmentMember[]; templates: RecruitmentMailTemplate[]; loading: boolean; onClose: () => void; onSave: (payload: Partial<RecruitmentWorkflow>) => void }) {

  const [name, setName] = useState("");
  const [isDefault, setIsDefault] = useState(false);
  const [nodes, setNodes] = useState<RecruitmentNode[]>([]);

  useEffect(() => {

    if (!open) return;
    setName(workflow?.name || "新招聘流程");
    setIsDefault(Boolean(workflow?.is_default));
    setNodes(workflow?.nodes.map((node) => ({ ...node })) || [
      newRecruitmentNode("review"),
      { ...newRecruitmentNode(), name: "初面" },
      { ...newRecruitmentNode(), name: "复面" },
      newRecruitmentNode("offer"),
      newRecruitmentNode("onboarding")
    ]);
  }, [open, workflow]);
  /** updateNode 只更新指定节点的草稿，不触发保存或修改正在进行的招聘。 */

  function updateNode(index: number, patch: Partial<RecruitmentNode>) {

    setNodes((current) => current.map((node, i) => i === index ? { ...node, ...patch } : node));
  }
  /** moveNode 交换相邻节点，让空节点及首尾操作保持安全。 */

  function moveNode(index: number, offset: number) {

    setNodes((current) => {

      const next = [...current];
      const target = index + offset;
      if (target < 0 || target >= next.length) return current;
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }

  const options = recruitmentMemberOptions(members);

  return <AdminDialog
    open={open}
    title={workflow?.id ? "编辑招聘流程" : "新建招聘流程"}
    description="每个岗位可选不同流程；这次修改用于后续新开启的招聘。"
    maxWidth="md"
    loading={loading}
    onClose={onClose}
    onConfirm={() => onSave({

      id: workflow?.id,
      revision: workflow?.revision,
      name: name.trim(),
      is_default: isDefault,
      nodes
    })}
    confirmDisabled={!name.trim() || !nodes.length || nodes.some((node) => !node.name.trim())}>

    <Form layout="vertical">
      <Form.Item label="流程名称" required>
        <Input
          aria-label="流程名称"
          maxLength={80}
          value={name}
          onChange={(event) => setName(event.target.value)} />
      </Form.Item>
      <Checkbox checked={isDefault} onChange={(event) => setIsDefault(event.target.checked)}>作为团队默认流程</Checkbox>
    </Form>

    <Collapse
      style={{ marginTop: 16 }}
      defaultActiveKey={workflow?.nodes?.[0]?.id ? [workflow.nodes[0].id] : []}
      items={nodes.map((node, index) => ({

        key: node.id,
        label: `${index + 1}. ${node.name || "未命名节点"}`,
        extra: <Flex gap={4} onClick={(event) => event.stopPropagation()}>
          <Button
            size="small"
            aria-label={`上移节点 ${index + 1}`}
            icon={<ArrowUpOutlined />}
            disabled={index === 0}
            onClick={() => moveNode(index, -1)} />
          <Button
            size="small"
            aria-label={`下移节点 ${index + 1}`}
            icon={<ArrowDownOutlined />}
            disabled={index === nodes.length - 1}
            onClick={() => moveNode(index, 1)} />
          <Button
            size="small"
            danger
            aria-label={`删除节点 ${index + 1}`}
            icon={<DeleteOutlined />}
            disabled={nodes.length === 1}
            onClick={() => setNodes((current) => current.filter((_, i) => i !== index))} />
        </Flex>,
        children: <Form layout="vertical">
          <Flex gap={12} wrap>
            <Form.Item label="节点名称" required style={{ flex: "1 1 220px" }}>
              <Input
                aria-label={`节点 ${index + 1} 名称`}
                value={node.name}
                maxLength={80}
                onChange={(event) => updateNode(index, { name: event.target.value })} />
            </Form.Item>
            <Form.Item label="节点类型" style={{ flex: "1 1 180px" }}>
              <Select
                aria-label={`节点 ${index + 1} 类型`}
                showSearch
                optionFilterProp="label"
                value={node.type}
                options={recruitmentNodeTypes}
                onChange={(type) => updateNode(index, { type })} />
            </Form.Item>
          </Flex>

          <Form.Item label="负责人">
            <Select
              aria-label={`节点 ${index + 1} 负责人`}
              allowClear
              showSearch
              optionFilterProp="label"
              options={options}
              value={node.owner_email || undefined}
              placeholder="未指定时由招聘负责人处理"
              onChange={(value) => updateNode(index, { owner_email: value || "" })} />
          </Form.Item>

          {node.type === "interview" ? <><Form.Item label="面试官">
            <Select
              aria-label={`节点 ${index + 1} 面试官`}
              mode="multiple"
              showSearch
              optionFilterProp="label"
              options={options}
              value={node.interviewer_emails || []}
              onChange={(value) => updateNode(index, { interviewer_emails: value })} />
          </Form.Item><Checkbox
            checked={node.requires_feedback}
            onChange={(event) => updateNode(index, { requires_feedback: event.target.checked })}>推进前需要面试评价</Checkbox></> : null}

          <Form.Item
            label="审核人"
            help={node.type === "approval" ? "未指定时由招聘负责人审核。" : "留空可直接由负责人推进。"}
            style={{ marginTop: 12 }}>
            <Select
              aria-label={`节点 ${index + 1} 审核人`}
              mode="multiple"
              showSearch
              optionFilterProp="label"
              options={options}
              value={node.approver_emails || []}
              onChange={(value) => updateNode(index, { approver_emails: value })} />
          </Form.Item>

          {node.approver_emails?.length ? <Form.Item label="审核方式">
            <Select
              aria-label={`节点 ${index + 1} 审核方式`}
              value={node.approval_mode}
              options={[{ value: "all", label: "所有审核人通过" }, { value: "any", label: "任一审核人通过" }]}
              onChange={(value) => updateNode(index, { approval_mode: value })} />
          </Form.Item> : null}

          <Form.Item label="内部抄送人">
            <Select
              aria-label={`节点 ${index + 1} 抄送人`}
              mode="multiple"
              showSearch
              optionFilterProp="label"
              options={options}
              value={node.cc_emails || []}
              onChange={(value) => updateNode(index, { cc_emails: value })} />
          </Form.Item>

          <Form.Item label="默认邮件模板">
            <Select
              aria-label={`节点 ${index + 1} 邮件模板`}
              allowClear
              showSearch
              optionFilterProp="label"
              value={node.mail_template_id || undefined}
              options={templates.map((item) => ({ value: item.id, label: item.name }))}
              onChange={(value) => updateNode(index, { mail_template_id: value || "" })}
              placeholder="发送前仍可选择和预览" />
          </Form.Item>

        </Form>
      }))} />

    <Flex justify="space-between" align="center" style={{ marginTop: 12 }}>
      <Typography.Text type="secondary">共
        {nodes.length} 个节点
      </Typography.Text>
      <Button
        icon={<PlusOutlined aria-hidden="true" />}
        disabled={nodes.length >= 30}
        onClick={() => setNodes((current) => [...current, newRecruitmentNode()])}>添加节点</Button>
    </Flex>

  </AdminDialog>;
}
