/** 文件作用说明：管理账号级简历库设置，保存火狐下载简历的默认自动导入开关。 */
"use client";
import { Alert, Flex, Switch, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { cloudRequest, getToken, localRequest } from "@/lib/admin-api";
import ResumeVectorizationPanel from "./ResumeVectorizationPanel";

/** ResumeLibrarySettingsDialog 仅修改简历库开关，不覆盖账号的其他运行偏好。 */
export default function ResumeLibrarySettingsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { user, agentBase, notify } = useAdmin();
  const [enabled, setEnabled] = useState(true);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [vectorVersion, setVectorVersion] = useState(0);
  const generation = useRef(0);
  useEffect(() => {
    if (!open) return;
    // Modal 关闭期间会缓存子内容；每次打开重建统计区，避免沿用上次账号或任务状态。
    setVectorVersion((value) => value + 1);
    generation.current += 1;
    const controller = new AbortController(); setLoading(true); setSaving(false); setLoaded(false); setError("");
    cloudRequest("/api/resume-library/settings", { signal: controller.signal }).then((result) => {
      if (!controller.signal.aborted) { setEnabled(result.settings?.auto_import_downloads !== false); setLoaded(true); }
    }).catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "设置没读出来，重新打开试试。"); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => { generation.current += 1; controller.abort(); };
  }, [open, user?.id, user?.email]);
  /** saveSettings 保存云端账号开关，并通知已连接本地程序重新读取设置。 */
  async function saveSettings() {
    const current = generation.current;
    setSaving(true); setError("");
    try {
      await cloudRequest("/api/resume-library/settings", { method: "PATCH", body: { auto_import_downloads: enabled } });
      if (current !== generation.current) return;
      if (agentBase) await localRequest(agentBase, "/api/v1/resume-library/settings/refresh", { method: "POST", headers: { Authorization: `Bearer ${getToken()}` }, timeoutMS: 10000 }).catch(() => undefined);
      if (current !== generation.current) return;
      notify(enabled ? "已开启，之后下载的简历我会帮你入库。" : "已关闭，之后下载的文件会留在本地。", "success"); onClose();
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "设置没保存成功，再试一次就好。"); }
    finally { if (current === generation.current) setSaving(false); }
  }
  return <AdminDialog open={open} title="简历库设置" onClose={onClose} onConfirm={() => void saveSettings()} confirmText="保存设置" confirmDisabled={loading || !loaded} loading={saving}>
    {error ? <Alert type="warning" title={error} style={{ marginBottom: 12 }} /> : null}
    <Flex gap={16} justify="space-between" align="start"><div><Typography.Text strong>将下载的简历通过 AI 导入到 GoodHR</Typography.Text><Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>默认开启。GoodHR 火狐从 Boss、猎聘、智联、无忧下载的 PDF、Word，会用当前账号 AI 配置解析，可能产生 AI 费用。入库没成功时，我会通过右上角消息提醒你。</Typography.Paragraph></div><Switch aria-label="将下载的简历通过AI导入到GoodHR" checked={enabled} disabled={loading || saving} onChange={setEnabled} /></Flex>
    <ResumeVectorizationPanel key={vectorVersion} open={open} account={`${user?.tenant_id || ""}:${user?.id || ""}`} />
  </AdminDialog>;
}
