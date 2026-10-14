/** 文件作用说明：集中管理个人与团队简历收藏夹，明确可见范围，删除收藏夹时保留候选人简历。 */
"use client";
import { Alert, Button, Flex, Form, Input, Radio, Table, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { useEffect, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { deleteResumeFolder, loadResumeFolders, saveResumeFolder } from "@/lib/resume-api";
import type { ResumeFolder } from "@/lib/resume-types";

/** ResumeFolderManager 使用服务端编辑权限展示入口，收藏夹删除不会调用删除简历接口。 */
export default function ResumeFolderManager({ open, onClose, onChanged }: { open: boolean; onClose: () => void; onChanged: () => void }) {
  const { notify, user } = useAdmin();
  const [folders, setFolders] = useState<ResumeFolder[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<ResumeFolder | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<ResumeFolder | null>(null);
  const [form] = Form.useForm<{ name: string; scope: ResumeFolder["scope"] }>();
  const scope = Form.useWatch("scope", form) || "personal";
  const canMakePersonal = !editing || editing.scope === "personal" || Boolean(editing.created_by_email && editing.created_by_email.trim().toLowerCase() === String(user?.email || "").trim().toLowerCase());
  /** reload 获取最新人数及权限，读取失败保留现有列表供重试。 */
  async function reload() {
    setLoading(true); setError("");
    try { setFolders(await loadResumeFolders()); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "收藏夹没读出来，请重试。"); }
    finally { setLoading(false); }
  }
  useEffect(() => { if (open) { setEditing(undefined); setDeleting(null); void reload(); } }, [open]);
  /** editFolder 新建默认个人范围，编辑时恢复收藏夹当前范围。 */
  function editFolder(folder: ResumeFolder | null) {
    setError(""); setEditing(folder); form.resetFields();
    form.setFieldsValue({ name: folder?.name || "", scope: folder?.scope || "personal" });
  }
  /** persistFolder 校验名称和范围，保存后同步收藏夹栏与人数。 */
  async function persistFolder() {
    if (busy || editing === undefined) return;
    try {
      const values = await form.validateFields();
      setBusy(true); setError("");
      await saveResumeFolder(editing?.id || "", values.name, values.scope);
      setEditing(undefined); await reload(); onChanged(); notify("收藏夹记好了。", "success");
    } catch (cause) {
      if (!(cause && typeof cause === "object" && "errorFields" in cause)) setError(cause instanceof Error ? cause.message : "收藏夹没保存成功，请重试。");
    } finally { setBusy(false); }
  }
  /** removeFolder 仅提交收藏夹删除，明确保留关联简历。 */
  async function removeFolder() {
    if (!deleting || busy) return;
    setBusy(true); setError("");
    try { await deleteResumeFolder(deleting.id); setDeleting(null); await reload(); onChanged(); notify("收藏夹删除了，简历都还在。", "success"); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "收藏夹没删成功，请重试。"); }
    finally { setBusy(false); }
  }
  return <>
    <AdminDialog open={open} title="管理收藏夹" maxWidth="sm" cancelText="关闭" onClose={onClose} loading={busy}
      description="个人收藏夹仅自己可见；团队收藏夹所有成员可见。删除收藏夹不会删除简历。"
      extraActions={<Button type="primary" icon={<PlusOutlined aria-hidden="true" />} disabled={busy || loading} onClick={() => editFolder(null)}>新增收藏夹</Button>}>
      {error && editing === undefined && !deleting ? <Alert type="warning" showIcon title={error} action={<Button size="small" onClick={() => void reload()}>重试</Button>} style={{ marginBottom: 10 }} /> : null}
      <Table<ResumeFolder> size="small" rowKey="id" pagination={false} dataSource={folders} loading={loading} scroll={{ y: 360 }} columns={[
        { title: "收藏夹", dataIndex: "name", ellipsis: true },
        { title: "范围", width: 90, render: (_, folder) => folder.scope === "team" ? "团队可见" : "仅自己" },
        { title: "人数", dataIndex: "candidate_count", width: 62, render: (count) => Number(count) || 0 },
        { title: "操作", width: 112, render: (_, folder) => folder.can_edit ? <Flex gap={4}><Button type="link" size="small" disabled={busy} onClick={() => editFolder(folder)}>编辑</Button><Button type="link" size="small" danger disabled={busy} onClick={() => { setError(""); setDeleting(folder); }}>删除</Button></Flex> : <Typography.Text type="secondary" style={{ fontSize: 12 }}>可选择</Typography.Text> },
      ]} locale={{ emptyText: "收藏夹暂时空空的，先加一个吧。" }} />
    </AdminDialog>
    <AdminDialog open={open && editing !== undefined} title={editing ? "编辑收藏夹" : "新增收藏夹"} maxWidth="xs" onClose={() => setEditing(undefined)} onConfirm={() => void persistFolder()} loading={busy}>
      {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
      <Form form={form} layout="vertical">
        <Form.Item label="收藏夹名称" name="name" rules={[{ required: true, whitespace: true, message: "收藏夹名字补一下，我好记住。" }]}><Input maxLength={40} placeholder="比如：近期可联系" /></Form.Item>
        <Form.Item label="可见范围" name="scope"><Radio.Group options={[{ value: "personal", label: "个人收藏夹", disabled: !canMakePersonal }, { value: "team", label: "团队收藏夹" }]} /></Form.Item>
      </Form>
      <Typography.Text type="secondary">{scope === "team" ? "团队所有成员可见、可选择。创建者和管理员可编辑。" : "只有你自己能看到、选择和管理。"}</Typography.Text>
      {!canMakePersonal ? <Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>只有创建者能改为个人收藏夹，你可以修改名称或删除收藏夹。</Typography.Paragraph> : null}
    </AdminDialog>
    <AdminDialog open={open && Boolean(deleting)} title="确认删除收藏夹" maxWidth="xs" onClose={() => setDeleting(null)} onConfirm={() => void removeFolder()} confirmText="删除收藏夹" loading={busy}>
      {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
      <Typography.Paragraph>删除“{deleting?.name}”？对应简历会保留，只移除这个收藏夹。</Typography.Paragraph>
    </AdminDialog>
  </>;
}
