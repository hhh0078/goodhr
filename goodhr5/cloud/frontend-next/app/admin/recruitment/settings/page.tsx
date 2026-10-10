/** 文件作用说明：提供公司招聘信息、多套流程模板与可编辑邮件模板的团队设置入口。 */
"use client";
import { Alert, Button, Flex, Form, Input, Space, Table, Tabs, Tag, Typography } from "antd";
import Link from "next/link";
import { useEffect, useState } from "react";
import { useAdmin } from "@/components/admin/AdminApp";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import WorkflowEditor from "@/components/admin/recruitment/WorkflowEditor";
import MailTemplateEditor from "@/components/admin/recruitment/MailTemplateEditor";
import { loadRecruitmentWorkflows, recruitmentMailCategories, recruitmentRequest, type RecruitmentCompany, type RecruitmentMailTemplate, type RecruitmentMember, type RecruitmentPlaceholder, type RecruitmentWorkflow } from "@/lib/recruitment-api";

const emptyCompany: RecruitmentCompany = {

  company_name: "",
  contact_name: "",
  contact_phone: "",
  contact_email: "",
  company_address: ""
};
/** RecruitmentSettingsPage 限管理员维护设置，普通成员可以阅读团队使用规则。 */
export default function RecruitmentSettingsPage() {

  const { notify, confirm } = useAdmin();
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [canManage, setCanManage] = useState(false);
  const [company, setCompany] = useState<RecruitmentCompany>(emptyCompany);
  const [workflows, setWorkflows] = useState<RecruitmentWorkflow[]>([]);
  const [members, setMembers] = useState<RecruitmentMember[]>([]);
  const [templates, setTemplates] = useState<RecruitmentMailTemplate[]>([]);
  const [placeholders, setPlaceholders] = useState<RecruitmentPlaceholder[]>([]);
  const [workflowOpen, setWorkflowOpen] = useState(false);
  const [workflow, setWorkflow] = useState<RecruitmentWorkflow | null>(null);
  const [templateOpen, setTemplateOpen] = useState(false);
  const [template, setTemplate] = useState<RecruitmentMailTemplate | null>(null);
  const [error, setError] = useState("");
  /** load 独立读取三类设置，任何失败都会显示而不以空草稿覆盖服务端内容。 */

  async function load() {

    setLoading(true);
    setError("");
    try {

      const results = await Promise.allSettled([
        loadRecruitmentWorkflows(),
        recruitmentRequest("/company"),
        recruitmentRequest("/mail-templates"),
        recruitmentRequest("/placeholders")
      ]);
      const [flow, companyResult, mail, variables] = results;
      if (flow.status === "fulfilled") {

        setWorkflows(flow.value.workflows);
        setMembers(flow.value.members);
        setCanManage(flow.value.can_manage);
      }
      if (companyResult.status === "fulfilled") setCompany({ ...emptyCompany, ...companyResult.value.company });
      if (mail.status === "fulfilled") setTemplates(mail.value.templates || []);
      if (variables.status === "fulfilled") setPlaceholders(variables.value.placeholders || []);
      const failures = results.filter((result) => result.status === "rejected");
      if (failures.length) {

        setError(failures.map((result) => result.status === "rejected" ? result.reason?.message || "部分设置没读出来" : "").join("；"));
        setCanManage(false);
      }
    } finally {

      setLoading(false);
    }
  }

  useEffect(() => {

    void load();
  }, []);
  /** saveCompany 保存对外公司信息，邮件仍由平台邮箱发送并使用联系邮箱接收回复。 */

  async function saveCompany() {

    if (!canManage || busy) return;
    if (!company.company_name.trim() || !company.contact_name.trim() || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(company.contact_email.trim())) return notify("先补上公司名、联系人和有效的联系邮箱，我才能替公司发信。", "warning");
    setBusy(true);
    try {

      const data = await recruitmentRequest("/company", "PUT", company);
      setCompany({ ...emptyCompany, ...data.company });
      notify("公司信息已记好。", "success");
    } catch (cause) {

      notify(cause instanceof Error ? cause.message : "公司信息没保存成功", "error");
    } finally {

      setBusy(false);
    }
  }
  /** saveWorkflow 保存当前模板版本，已有进程继续使用开始时的流程。 */

  async function saveWorkflow(payload: Partial<RecruitmentWorkflow>) {

    if (!canManage || busy) return;
    setBusy(true);
    try {

      await recruitmentRequest(payload.id ? `/workflows/${encodeURIComponent(payload.id)}` : "/workflows", payload.id ? "PUT" : "POST", payload);
      setWorkflowOpen(false);
      notify("招聘流程保存好了。", "success");
      await load();
    } catch (cause) {

      notify(cause instanceof Error ? cause.message : "流程没保存成功", "error");
    } finally {

      setBusy(false);
    }
  }
  /** saveTemplate 保存预置模板修改或用户新建模板，正文仍可继续编辑。 */

  async function saveTemplate(payload: Partial<RecruitmentMailTemplate>) {

    if (!canManage || busy) return;
    setBusy(true);
    try {

      await recruitmentRequest(payload.id ? `/mail-templates/${encodeURIComponent(payload.id)}` : "/mail-templates", payload.id ? "PUT" : "POST", payload);
      setTemplateOpen(false);
      notify("邮件模板已保存。", "success");
      await load();
    } catch (cause) {

      notify(cause instanceof Error ? cause.message : "模板没保存成功", "error");
    } finally {

      setBusy(false);
    }
  }
  /** removeSetting 先确认具体模板，再删除；不触碰历史招聘与已发送邮件。 */

  async function removeSetting(kind: "workflows" | "mail-templates", id: string, name: string) {

    if (!canManage || busy || !await confirm("我小声确认一下", `要删除“${name}”吗？已产生的历史记录会保留。`)) return;
    setBusy(true);
    try {

      await recruitmentRequest(`/${kind}/${encodeURIComponent(id)}`, "DELETE", kind === "workflows" ? { revision: workflows.find((item) => item.id === id)?.revision } : undefined);
      notify("模板已删除。", "success");
      await load();
    } catch (cause) {

      notify(cause instanceof Error ? cause.message : "这次没删除成功", "error");
    } finally {

      setBusy(false);
    }
  }

  return <><PageHeader
    title="招聘设置"
    description="公司信息先填一次，流程和邮件可各存多套。"
    actions={<><Link href="/admin/recruitment">
      <Button>返回招聘进度</Button>
    </Link><RefreshButton loading={loading} onClick={() => void load()} /></>} />{error ? <Alert type="warning" title={error} style={{ marginBottom: 12 }} /> : null}{!loading && !canManage ? <Alert type="info" title="你可以查看设置，修改由团队管理员处理。" style={{ marginBottom: 12 }} /> : null}<SectionPanel>
      <Tabs
        items={[

          {

            key: "company",
            label: "公司信息",
            children: <Form layout="vertical" style={{ maxWidth: 640 }}>
              <Alert
                type="info"
                title="第一期由 GoodHR 平台邮箱发送，标题会带公司名称。候选人回复转到这里填写的联系邮箱。"
                style={{ marginBottom: 16 }} />
              {([
                { key: "company_name", label: "公司名称", required: true },
                { key: "contact_name", label: "联系人", required: true },
                { key: "contact_email", label: "联系邮箱", required: true },
                { key: "contact_phone", label: "联系电话" },
                { key: "company_address", label: "公司地址" }
              ] as Array<{ key: keyof RecruitmentCompany; label: string; required?: boolean }>).map((field) => <Form.Item key={field.key} label={field.label} required={field.required}>
                <Input
                  aria-label={field.label}
                  disabled={!canManage || busy || loading}
                  value={company[field.key]}
                  maxLength={field.key === "company_address" ? 500 : field.key === "contact_email" ? 200 : 100}
                  onChange={(event) => setCompany((current) => ({ ...current, [field.key]: event.target.value }))} />
              </Form.Item>)}
              {canManage ? <Flex justify="flex-end">
                <Button
                  type="primary"
                  loading={busy}
                  disabled={loading || Boolean(error)}
                  onClick={() => void saveCompany()}>保存公司信息</Button>
              </Flex> : null}
            </Form>
          },

          {

            key: "workflows",
            label: "流程模板",
            children: <><Flex justify="space-between" align="center" wrap gap={8} style={{ marginBottom: 12 }}>
              <Typography.Text type="secondary">简单岗位用默认流程，也能为不同岗位另存一套。</Typography.Text>
              {canManage ? <Button type="primary" onClick={() => {

                setWorkflow(null);
                setWorkflowOpen(true);
              }}>新建流程</Button> : null}
            </Flex><Table
                rowKey="id"
                size="small"
                loading={loading}
                dataSource={workflows}
                pagination={false}
                scroll={{ x: 640 }}
                locale={{ emptyText: <EmptyState text="还没有流程，先建一个就能开始。" /> }}
                columns={[
                  {

                    title: "流程",
                    dataIndex: "name",
                    render: (value, row) => <Space>
                      {value}
                      {row.is_default ? <Tag>默认</Tag> : null}
                    </Space>
                  },
                  {

                    title: "节点",
                    render: (_, row) => row.nodes?.map((node) => node.name).join(" → ") || "--"
                  },
                  { title: "版本", dataIndex: "version", width: 70 },
                  {

                    title: "操作",
                    width: 210,
                    render: (_, row) => canManage ? <Space>
                      <Button size="small" onClick={() => {

                        setWorkflow(row);
                        setWorkflowOpen(true);
                      }}>编辑</Button>
                      <Button
                        size="small"
                        onClick={() => {

                          setWorkflow({ ...row, id: "", name: `${row.name}（副本）`, is_default: false });
                          setWorkflowOpen(true);
                        }}>复制</Button>
                      <Button
                        size="small"
                        danger
                        disabled={busy}
                        onClick={() => void removeSetting("workflows", row.id, row.name)}>删除</Button>
                    </Space> : <Typography.Text type="secondary">只读</Typography.Text>
                  }
                ]} /></>
          },

          {

            key: "mail",
            label: "邮件模板",
            children: <><Flex justify="space-between" align="center" wrap gap={8} style={{ marginBottom: 12 }}>
              <Typography.Text type="secondary">初试、复试、Offer、入职都能改文本，也能自建模板。</Typography.Text>
              {canManage ? <Button type="primary" onClick={() => {

                setTemplate(null);
                setTemplateOpen(true);
              }}>新建邮件模板</Button> : null}
            </Flex><Table
                rowKey="id"
                size="small"
                loading={loading}
                dataSource={templates}
                pagination={false}
                scroll={{ x: 680 }}
                columns={[
                  {

                    title: "模板",
                    dataIndex: "name",
                    render: (value, row) => <Space>
                      {value}
                      {row.is_builtin ? <Tag>预置</Tag> : null}
                    </Space>
                  },
                  {

                    title: "场景",
                    dataIndex: "category",
                    render: (value) => recruitmentMailCategories.find((item) => item.value === value)?.label || value
                  },
                  { title: "邮件标题", dataIndex: "subject", ellipsis: true },
                  {

                    title: "操作",
                    width: 210,
                    render: (_, row) => canManage ? <Space>
                      <Button size="small" onClick={() => {

                        setTemplate(row);
                        setTemplateOpen(true);
                      }}>编辑</Button>
                      <Button
                        size="small"
                        onClick={() => {

                          setTemplate({ ...row, id: "", name: `${row.name}（副本）` });
                          setTemplateOpen(true);
                        }}>复制</Button>
                      <Button
                        size="small"
                        danger
                        disabled={busy}
                        onClick={() => void removeSetting("mail-templates", row.id, row.name)}>删除</Button>
                    </Space> : <Typography.Text type="secondary">只读</Typography.Text>
                  }
                ]} /></>
          },

        ]} />
    </SectionPanel><WorkflowEditor
      open={workflowOpen}
      workflow={workflow?.id ? workflow : workflow}
      members={members}
      templates={templates}
      loading={busy}
      onClose={() => setWorkflowOpen(false)}
      onSave={(payload) => void saveWorkflow(payload)} /><MailTemplateEditor
      open={templateOpen}
      template={template}
      placeholders={placeholders}
      loading={busy}
      onClose={() => setTemplateOpen(false)}
      onSave={(payload) => void saveTemplate(payload)} /></>;
}
