/** 本文件负责新版后台本地下载、规则包和诊断信息展示。 */
"use client";

import SyncRoundedIcon from "@mui/icons-material/SyncRounded";
import { Box, Button, Chip, Stack, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { formatDate, localRequest } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** LocalDataPage 展示本机下载记录、规则包和诊断信息。 */
export default function LocalDataPage() {
  const { agentBase, notify } = useAdmin();
  const [downloads, setDownloads] = useState<any[]>([]);
  const [rules, setRules] = useState<any[]>([]);
  const [diagnostics, setDiagnostics] = useState<any>({});
  const [loading, setLoading] = useState(false);

  /** load 从当前连接的本地程序读取本机数据。 */
  async function load() { if (!agentBase) return; setLoading(true); try { const [downloadData, ruleData, diagnosticData] = await Promise.all([localRequest(agentBase, "/api/v1/local/downloads"), localRequest(agentBase, "/api/v1/local/rules/status"), localRequest(agentBase, "/api/v1/diagnostics")]); setDownloads(downloadData.downloads || []); setRules(ruleData.rules || []); setDiagnostics(diagnosticData || {}); } catch (error) { notify(error instanceof Error ? error.message : "本地数据读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { void load(); }, [agentBase]);

  /** updateRules 触发本地规则包更新。 */
  async function updateRules() { if (!agentBase) return notify("本地程序未连接", "error"); try { const data = await localRequest(agentBase, "/api/v1/local/rules/update", { method: "POST", body: {} }); notify(`规则更新完成：更新 ${data.updated?.length || 0} 个`, "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "规则更新失败", "error"); } }

  /** canOpenDownload 判断下载记录是否已经保存到本机，可以打开。 */
  function canOpenDownload(item: any) { return item?.status === "saved" && Boolean(item?.file_path); }

  /** openDownloadFile 调用本地程序打开文件，或在文件夹里定位文件。 */
  async function openDownloadFile(path: string, endpoint: string) {
    if (!agentBase) return notify("本地程序未连接", "error");
    if (!path) return notify("这条记录没有文件路径，我打不开", "error");
    try {
      await localRequest(agentBase, endpoint, { method: "POST", body: { path } });
      notify(endpoint.endsWith("/reveal") ? "已经在文件夹里帮你选中了" : "已经用默认程序打开了", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "我没打开成功，文件可能已经被移动或删除了", "error");
    }
  }

  return <><PageHeader title="本地数据" description="数据来自当前电脑，不会上传到云端。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} />{!agentBase ? <SectionPanel><EmptyState text="请先启动本地程序" /></SectionPanel> : <Stack spacing={2}><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>本地诊断</Typography><Box component="pre" sx={{ mt: 2, maxHeight: 280, overflow: "auto", p: 2, bgcolor: "action.hover", borderRadius: "8px", whiteSpace: "pre-wrap", fontSize: 12 }}>{JSON.stringify(diagnostics, null, 2)}</Box></SectionPanel><SectionPanel><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>平台规则包</Typography><Button startIcon={<SyncRoundedIcon />} onClick={() => void updateRules()}>更新规则</Button></Stack>{rules.length ? <Stack sx={{ mt: 1 }}>{rules.map((item) => <Stack key={item.platform_id} direction="row" spacing={2} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider", justifyContent: "space-between" }}><Typography sx={{ fontWeight: 700 }}>{item.platform_id}</Typography><Typography>{item.version || "--"}</Typography><Chip size="small" label={item.status || "未知"} /><Typography color="text.secondary">{formatDate(item.updated_at)}</Typography></Stack>)}</Stack> : <EmptyState text="暂无规则包记录" />}</SectionPanel><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>下载记录</Typography>{downloads.length ? <Stack sx={{ mt: 1 }}>{downloads.map((item, index) => <Stack key={item.id || `${item.file_path}-${index}`} direction="row" spacing={1} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider", alignItems: "center", justifyContent: "space-between" }}><Box sx={{ minWidth: 0 }}><Typography sx={{ fontWeight: 700 }}>{item.file_name || fileName(item.file_path)}</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 12, wordBreak: "break-all" }}>{item.status || "--"} · {formatSize(item.size)} · {item.file_path || item.url || ""}</Typography></Box>{canOpenDownload(item) ? <Stack direction="row" spacing={1} sx={{ flexShrink: 0 }}><Button size="small" onClick={() => void openDownloadFile(item.file_path, "/api/v1/files/open")}>打开文件</Button><Button size="small" onClick={() => void openDownloadFile(item.file_path, "/api/v1/files/reveal")}>打开文件夹</Button></Stack> : null}</Stack>)}</Stack> : <EmptyState text="暂无下载记录" />}</SectionPanel></Stack>}</>;
}

/** formatSize 将字节转换为易读大小。 */
function formatSize(value: number) { const size = Number(value || 0); if (size < 1024) return `${size} B`; if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`; return `${(size / 1024 / 1024).toFixed(1)} MB`; }

/** fileName 从路径中提取文件名。 */
function fileName(value: string) { return String(value || "").split(/[\\/]/).filter(Boolean).pop() || "未命名文件"; }
