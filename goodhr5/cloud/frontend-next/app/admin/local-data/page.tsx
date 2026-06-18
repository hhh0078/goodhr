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

  return <><PageHeader title="本地数据" description="数据来自当前电脑，不会上传到云端。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} />{!agentBase ? <SectionPanel><EmptyState text="请先启动本地程序" /></SectionPanel> : <Stack spacing={2}><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>本地诊断</Typography><Box component="pre" sx={{ mt: 2, maxHeight: 280, overflow: "auto", p: 2, bgcolor: "#f4f7f5", borderRadius: "8px", whiteSpace: "pre-wrap", fontSize: 12 }}>{JSON.stringify(diagnostics, null, 2)}</Box></SectionPanel><SectionPanel><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>平台规则包</Typography><Button startIcon={<SyncRoundedIcon />} onClick={() => void updateRules()}>更新规则</Button></Stack>{rules.length ? <Stack sx={{ mt: 1 }}>{rules.map((item) => <Stack key={item.platform_id} direction="row" spacing={2} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider", justifyContent: "space-between" }}><Typography sx={{ fontWeight: 700 }}>{item.platform_id}</Typography><Typography>{item.version || "--"}</Typography><Chip size="small" label={item.status || "未知"} /><Typography color="text.secondary">{formatDate(item.updated_at)}</Typography></Stack>)}</Stack> : <EmptyState text="暂无规则包记录" />}</SectionPanel><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>下载记录</Typography>{downloads.length ? <Stack sx={{ mt: 1 }}>{downloads.map((item, index) => <Box key={item.id || `${item.file_path}-${index}`} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider" }}><Typography sx={{ fontWeight: 700 }}>{item.file_name || fileName(item.file_path)}</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 12, wordBreak: "break-all" }}>{item.status || "--"} · {formatSize(item.size)} · {item.file_path || item.url || ""}</Typography></Box>)}</Stack> : <EmptyState text="暂无下载记录" />}</SectionPanel></Stack>}</>;
}

/** formatSize 将字节转换为易读大小。 */
function formatSize(value: number) { const size = Number(value || 0); if (size < 1024) return `${size} B`; if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`; return `${(size / 1024 / 1024).toFixed(1)} MB`; }

/** fileName 从路径中提取文件名。 */
function fileName(value: string) { return String(value || "").split(/[\\/]/).filter(Boolean).pop() || "未命名文件"; }
