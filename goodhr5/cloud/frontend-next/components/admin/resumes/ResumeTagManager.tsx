/** 文件作用说明：集中管理个人与团队简历标签，明确可见范围，删除标签时保留候选人简历。 */
"use client";
import { Alert, Button, Flex, Form, Input, Radio, Table, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { useEffect, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { deleteResumeTag, loadResumeTags, saveResumeTag } from "@/lib/resume-api";
import type { ResumeTag } from "@/lib/resume-types";

/** ResumeTagManager 使用服务端编辑权限展示入口，标签删除不会调用删除简历接口。 */
export default function ResumeTagManager({ open, onClose, onChanged }: { open: boolean; onClose: () => void; onChanged: () => void }) {
  const { notify, user } = useAdmin();
  const [tags, setTags] = useState<ResumeTag[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<ResumeTag | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<ResumeTag | null>(null);
  const [form] = Form.useForm<{ name: string; scope: ResumeTag["scope"] }>();
  const scope = Form.useWatch("scope", form) || "personal";
  const canMakePersonal = !editing || editing.scope === "personal" || Boolean(editing.created_by_email && editing.created_by_email.trim().toLowerCase() === String(user?.email || "").trim().toLowerCase());
  /** reload 获取最新人数及权限，读取失败保留现有列表供重试。 */
  async function reload() {
    setLoading(true); setError("");
    try { setTags(await loadResumeTags()); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "标签没读出来，请重试。"); }
    finally { setLoading(false); }
  }
  useEffect(() => { if (open) { setEditing(undefined); setDeleting(null); void reload(); } }, [open]);
  /** editTag 新建默认个人范围，编辑时恢复标签当前范围。 */
  function editTag(tag: ResumeTag | null) {
    setError(""); setEditing(tag); form.resetFields();
    form.setFieldsValue({ name: tag?.name || "", scope: tag?.scope || "personal" });
  }
  /** persistTag 校验名称和范围，保存后同步标签栏与人数。 */
  async function persistTag() {
    if (busy || editing === undefined) return;
    try {
      const values = await form.validateFields();
      setBusy(true); setError("");
      await saveResumeTag(editing?.id || "", values.name, values.scope);
      setEditing(undefined); await reload(); onChanged(); notify("标签记好了。", "success");
    } catch (cause) {
      if (!(cause && typeof cause === "object" && "errorFields" in cause)) setError(cause instanceof Error ? cause.message : "标签没保存成功，请重试。");
    } finally { setBusy(false); }
  }
  /** removeTag 仅提交标签删除，明确保留关联简历。 */
  async function removeTag() {
    if (!deleting || busy) return;
    setBusy(true); setError("");
    try { await deleteResumeTag(deleting.id); setDeleting(null); await reload(); onChanged(); notify("标签删除了，简历都还在。", "success"); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "标签没删成功，请重试。"); }
    finally { setBusy(false); }
  }
  return <>
    <AdminDialog open={open} title="管理标签" maxWidth="sm" cancelText="关闭" onClose={onClose} loading={busy}
      description="个人标签仅自己可见；团队标签所有成员可见。删除标签不会删除简历。"
      extraActions={<Button type="primary" icon={<PlusOutlined aria-hidden="true" />} disabled={busy || loading} onClick={() => editTag(null)}>新增标签</Button>}>
      {error && editing === undefined && !deleting ? <Alert type="warning" showIcon title={error} action={<Button size="small" onClick={() => void reload()}>重试</Button>} style={{ marginBottom: 10 }} /> : null}
      <Table<ResumeTag> size="small" rowKey="id" pagination={false} dataSource={tags} loading={loading} scroll={{ y: 360 }} columns={[
        { title: "标签", dataIndex: "name", ellipsis: true },
        { title: "范围", width: 90, render: (_, tag) => tag.scope === "team" ? "团队可见" : "仅自己" },
        { title: "人数", dataIndex: "candidate_count", width: 62, render: (count) => Number(count) || 0 },
        { title: "操作", width: 112, render: (_, tag) => tag.can_edit ? <Flex gap={4}><Button type="link" size="small" disabled={busy} onClick={() => editTag(tag)}>编辑</Button><Button type="link" size="small" danger disabled={busy} onClick={() => { setError(""); setDeleting(tag); }}>删除</Button></Flex> : <Typography.Text type="secondary" style={{ fontSize: 12 }}>可选择</Typography.Text> },
      ]} locale={{ emptyText: "标签暂时空空的，先加一个吧。" }} />
    </AdminDialog>
    <AdminDialog open={open && editing !== undefined} title={editing ? "编辑标签" : "新增标签"} maxWidth="xs" onClose={() => setEditing(undefined)} onConfirm={() => void persistTag()} loading={busy}>
      {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
      <Form form={form} layout="vertical">
        <Form.Item label="标签名称" name="name" rules={[{ required: true, whitespace: true, message: "标签名字补一下，我好记住。" }]}><Input maxLength={40} placeholder="比如：近期可联系" /></Form.Item>
        <Form.Item label="可见范围" name="scope"><Radio.Group options={[{ value: "personal", label: "个人标签", disabled: !canMakePersonal }, { value: "team", label: "团队标签" }]} /></Form.Item>
      </Form>
      <Typography.Text type="secondary">{scope === "team" ? "团队所有成员可见、可选择。创建者和管理员可编辑。" : "只有你自己能看到、选择和管理。"}</Typography.Text>
      {!canMakePersonal ? <Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>只有创建者能改为个人标签，你可以修改名称或删除标签。</Typography.Paragraph> : null}
    </AdminDialog>
    <AdminDialog open={open && Boolean(deleting)} title="确认删除标签" maxWidth="xs" onClose={() => setDeleting(null)} onConfirm={() => void removeTag()} confirmText="删除标签" loading={busy}>
      {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
      <Typography.Paragraph>删除“{deleting?.name}”？对应简历会保留，只移除这个标签。</Typography.Paragraph>
    </AdminDialog>
  </>;
}
