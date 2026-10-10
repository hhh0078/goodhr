/** 文件作用说明：在同一个添加简历弹框中切换 AI 批量上传、手动添加和浏览器插件指引，并保留各方式的未提交草稿。 */
"use client";
import { Button, Tabs, Typography } from "antd";
import { DownloadOutlined } from "@ant-design/icons";
import { useCallback, useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import type { PositionOption, Resume } from "@/lib/resume-types";
import ResumeEditor from "./ResumeEditor";
import ResumeImportDialog from "./ResumeImportDialog";
import styles from "./resumes.module.css";

export type ResumeAddTab = "ai" | "manual" | "plugin";
export interface ResumeAddDialogProps {
  open: boolean;
  initialTab?: ResumeAddTab;
  initialTaskID?: string;
  positions: PositionOption[];
  onClose: () => void;
  onSaved: (candidate: Resume) => void;
  onChanged: () => void;
}

/** ResumeAddDialog 直接切换添加方式，提交时锁定切换与关闭，避免保存中的草稿被丢弃。 */
export default function ResumeAddDialog({ open, initialTab = "ai", initialTaskID = "", positions, onClose, onSaved, onChanged }: ResumeAddDialogProps) {
  const { user } = useAdmin();
  const [tab, setTab] = useState<ResumeAddTab>(initialTab);
  const [importBusy, setImportBusy] = useState(false);
  const [manualBusy, setManualBusy] = useState(false);
  const busy = importBusy || manualBusy;
  const accountKey = user?.id || user?.email || "anonymous";
  const activeAccount = useRef(accountKey);
  activeAccount.current = accountKey;
  // 按发起时的账号核对回调，旧账号的迟到响应不能打开新账号详情或改动新账号的忙状态。
  const importBusyChanged = useCallback((value: boolean) => { if (activeAccount.current === accountKey) setImportBusy(value); }, [accountKey]);
  const manualBusyChanged = useCallback((value: boolean) => { if (activeAccount.current === accountKey) setManualBusy(value); }, [accountKey]);
  const saved = useCallback((candidate: Resume) => { if (activeAccount.current === accountKey) onSaved(candidate); }, [accountKey, onSaved]);
  const changed = useCallback(() => { if (activeAccount.current === accountKey) onChanged(); }, [accountKey, onChanged]);
  useEffect(() => { setImportBusy(false); setManualBusy(false); }, [accountKey]);
  useEffect(() => {
    if (open) setTab(initialTaskID ? "ai" : initialTab);
  }, [open, initialTab, initialTaskID, accountKey]);

  return <AdminDialog open={open} title="添加简历" maxWidth="md" loading={busy} onClose={onClose}>
    <Tabs key={accountKey} className={styles.addTabs} activeKey={tab} destroyOnHidden={false}
      onChange={(next) => { if (!busy) setTab(next as ResumeAddTab); }}
      items={[
        {
          key: "ai", label: "AI 批量上传", forceRender: true, disabled: busy && tab !== "ai",
          children: <ResumeImportDialog embedded open={open} active={tab === "ai"} initialTaskID={initialTaskID} positions={positions} onClose={onClose} onChanged={changed} onBusyChange={importBusyChanged} />,
        },
        {
          key: "manual", label: "手动添加", forceRender: true, disabled: busy && tab !== "manual",
          children: <ResumeEditor embedded open={open} active={tab === "manual"} positions={positions} onClose={onClose} onSaved={saved} onBusyChange={manualBusyChanged} />,
        },
        {
          key: "plugin", label: "浏览器插件一键导入", forceRender: true, disabled: busy && tab !== "plugin",
          children: <div className={styles.pluginGuide}>
            <Typography.Paragraph>在招聘页面打开 GoodHR 插件，核对手机号后，就能一键导入简历库。</Typography.Paragraph>
            <Typography.Paragraph type="secondary">Chrome 已可下载，Firefox 正在准备中。安装方法也在下载页，跟着操作就好。</Typography.Paragraph>
            <Button type="primary" icon={<DownloadOutlined />} href="/download#browser-extension" target="_blank" rel="noopener noreferrer">前往下载</Button>
          </div>,
        },
      ]}
    />
  </AdminDialog>;
}
