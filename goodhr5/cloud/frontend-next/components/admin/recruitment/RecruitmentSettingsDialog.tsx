/** 文件作用说明：在企业招聘页内打开团队招聘设置弹框，共用旧路由设置内容及子编辑窗口。 */
"use client";
import { useState } from "react";
import AdminDialog from "../AdminDialog";
import RecruitmentSettingsContent from "./RecruitmentSettingsContent";

/** RecruitmentSettingsDialog 只在打开时加载设置，保存期间保护窗口，关闭后仍留在招聘列表。 */
export default function RecruitmentSettingsDialog({ open, onClose, onChanged }: { open: boolean; onClose: () => void; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  return <AdminDialog open={open} title="招聘设置" maxWidth="lg" cancelText="关闭" loading={busy} onClose={onClose}>
    {open ? <RecruitmentSettingsContent embedded onChanged={onChanged} onBusyChange={setBusy} /> : null}
  </AdminDialog>;
}
