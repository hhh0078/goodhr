/** 本文件负责新版后台本地程序与运行组件信息和更新。 */
"use client";

import SystemUpdateAltRoundedIcon from "@mui/icons-material/SystemUpdateAltRounded";
import { Box, Button, Chip, LinearProgress, Stack, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { localRequest } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

const componentNames: Record<string, string> = { node_runtime: "Node 运行环境", node_worker: "浏览器控制 Worker", cloakbrowser: "CloakBrowser 浏览器", ocr: "OCR 组件" };

/** AgentDownloadPage 展示组件状态并触发运行组件更新。 */
export default function AgentDownloadPage() {
  const { agentBase, onboardingConfig, refreshAgent, notify } = useAdmin();
  const [runtime, setRuntime] = useState<any>({});
  const [loading, setLoading] = useState(false);

  /** load 读取本地运行状态和云端组件配置。 */
  async function load() { if (!agentBase) return; setLoading(true); try { setRuntime(await localRequest(agentBase, "/api/v1/runtime/status") || {}); } catch (error) { notify(error instanceof Error ? error.message : "组件信息读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { void load(); }, [agentBase]);

  /** updateRuntime 下载并安装缺失或版本不符的运行组件。 */
  async function updateRuntime() { if (!agentBase) return notify("本地程序未连接", "error"); setLoading(true); try { await localRequest(agentBase, "/api/v1/runtime/install", { method: "POST", body: buildInstallPayload(onboardingConfig) }); notify("组件更新任务已完成", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "组件更新失败", "error"); } finally { setLoading(false); } }

  const components = buildComponents(runtime, onboardingConfig);
  return <><PageHeader title="组件信息" description="查看本机运行组件、安装状态、版本和下载说明。" actions={<><RefreshButton loading={loading} onClick={() => void refreshAgent().then(load)} /><Button variant="contained" startIcon={<SystemUpdateAltRoundedIcon />} disabled={loading || !agentBase} onClick={() => void updateRuntime()}>更新运行组件</Button></>} />{loading ? <LinearProgress sx={{ mb: 2 }} /> : null}{!agentBase ? <SectionPanel><EmptyState text="本地程序未连接" /></SectionPanel> : <><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", md: "repeat(4, 1fr)" }, gap: 2, mb: 2 }}><SectionPanel><Typography color="text.secondary" sx={{ fontSize: 12 }}>本地连接</Typography><Typography sx={{ mt: 1, color: "primary.main", fontWeight: 760 }}>已连接</Typography></SectionPanel><SectionPanel><Typography color="text.secondary" sx={{ fontSize: 12 }}>监听地址</Typography><Typography sx={{ mt: 1, fontWeight: 760 }}>{agentBase}</Typography></SectionPanel><SectionPanel><Typography color="text.secondary" sx={{ fontSize: 12 }}>程序版本</Typography><Typography sx={{ mt: 1, fontWeight: 760 }}>{runtime.version || runtime.agent_version || "--"}</Typography></SectionPanel><SectionPanel><Typography color="text.secondary" sx={{ fontSize: 12 }}>数据目录</Typography><Typography sx={{ mt: 1, fontSize: 12, wordBreak: "break-all" }}>{runtime.data_dir || "--"}</Typography></SectionPanel></Box><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "repeat(2, 1fr)" }, gap: 2 }}>{components.map((item) => <SectionPanel key={item.key}><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "flex-start" }}><Box><Typography component="h2" sx={{ fontSize: 18, fontWeight: 760 }}>{item.name}</Typography><Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 13 }}>{item.note || "暂无版本说明"}</Typography></Box><Chip size="small" color={item.installed ? "success" : item.required ? "error" : "default"} label={item.installed ? "已安装" : item.required ? "未安装" : "可选"} /></Stack><Box component="dl" sx={{ mt: 2, display: "grid", gridTemplateColumns: "86px 1fr", gap: 1, fontSize: 13, "& dt": { color: "text.secondary" }, "& dd": { m: 0, wordBreak: "break-all" } }}><dt>配置版本</dt><dd>{item.configVersion || "--"}</dd><dt>本地版本</dt><dd>{item.installedVersion || "--"}</dd><dt>下载地址</dt><dd>{item.bundled ? "随本地程序内置" : item.url || "未配置"}</dd><dt>本地路径</dt><dd>{item.path || "--"}</dd></Box></SectionPanel>)}</Box></>}</>;
}

/** buildComponents 根据本机系统构建组件展示数据。 */
function buildComponents(runtime: any, config: any) {
  const isWindows = typeof navigator !== "undefined" && navigator.userAgent.toLowerCase().includes("windows");
  const platformKey = isWindows ? "win" : "mac";
  const configured = config.runtime_components || {};
  const installed = runtime.installed_versions || runtime.runtime?.installed_versions || {};
  return Object.keys(componentNames).map((key) => { const asset = configured[key]?.[platformKey] || configured[key]?.[isWindows ? "windows" : "macos"] || {}; const local = installed[key] || {}; const path = runtime[`${key.replace("_runtime", "")}_path`] || runtime.runtime?.[`${key.replace("_runtime", "")}_path`] || (key === "node_worker" ? runtime.worker_entry || runtime.runtime?.worker_entry : ""); return { key, name: componentNames[key], required: key !== "ocr", bundled: key === "node_worker", installed: Boolean(local.version || path || runtime[`${key}_installed`] || runtime.runtime?.[`${key}_installed`]), configVersion: asset.version || "", installedVersion: local.version || "", url: asset.url || "", note: key === "node_worker" ? "随本地程序安装包内置，不需要单独安装。" : asset.note || asset.description || "", path }; });
}

/** buildInstallPayload 将系统教学配置转换为本地程序安装接口所需结构。 */
function buildInstallPayload(config: any) {
  const source = config?.runtime_components || config?.runtimeComponents || config?.local_runtime_components || config?.runtime || {};
  const aliases: Record<string, string[]> = { node_runtime: ["node_runtime", "nodeRuntime", "node"], cloakbrowser: ["cloakbrowser", "cloak_browser", "cloakBrowser", "browser"], ocr: ["ocr", "rapidocr", "rapidOCR"] };
  const platforms: Record<string, string[]> = { "win-x64": ["win-x64", "windows-x64", "win", "windows"], "darwin-arm64": ["darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"] };
  const manifest: Record<string, any> = {};
  for (const [component, componentAliases] of Object.entries(aliases)) {
    const componentConfig = componentAliases.map((key) => source?.[key]).find((value) => value && typeof value === "object") || {};
    manifest[component] = {};
    for (const [platform, platformAliases] of Object.entries(platforms)) {
      const asset = platformAliases.map((key) => componentConfig?.[key]).find((value) => value && typeof value === "object");
      if (asset?.url) manifest[component][platform] = { version: String(asset.version || ""), url: String(asset.url || ""), sha256: String(asset.sha256 || ""), note: String(asset.note || asset.changelog || asset.description || asset.release_note || "") };
    }
  }
  return { manifest };
}
