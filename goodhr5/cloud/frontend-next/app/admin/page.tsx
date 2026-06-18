/** 本文件负责新版后台控制台概览、账号快捷入口、本地状态和新手步骤。 */
"use client";

import AccountCircleRoundedIcon from "@mui/icons-material/AccountCircleRounded";
import ArticleRoundedIcon from "@mui/icons-material/ArticleRounded";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import LaunchRoundedIcon from "@mui/icons-material/LaunchRounded";
import PlayCircleRoundedIcon from "@mui/icons-material/PlayCircleRounded";
import RefreshRoundedIcon from "@mui/icons-material/RefreshRounded";
import TaskAltRoundedIcon from "@mui/icons-material/TaskAltRounded";
import WorkRoundedIcon from "@mui/icons-material/WorkRounded";
import { Box, Button, Chip, CircularProgress, LinearProgress, Stack, Typography } from "@mui/material";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, localRequest } from "@/lib/admin-api";

/** DashboardPage 展示用户当前最需要关注的招聘和本地运行状态。 */
export default function DashboardPage() {
  const { agentBase, subscription, onboarding, refreshAgent, notify } = useAdmin();
  const [tasks, setTasks] = useState<any[]>([]);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [positions, setPositions] = useState<any[]>([]);
  const [resumeCount, setResumeCount] = useState(0);
  const [runtime, setRuntime] = useState<any>({});
  const [loading, setLoading] = useState(true);

  /** load 读取控制台所需的云端数据。 */
  async function load() {
    setLoading(true);
    try {
      const results = await Promise.allSettled([cloudRequest("/api/tasks"), cloudRequest("/api/platform-accounts"), cloudRequest("/api/positions"), cloudRequest("/api/candidates?page=1&page_size=1")]);
      if (results[0].status === "fulfilled") setTasks(results[0].value.tasks || []);
      if (results[1].status === "fulfilled") setAccounts(results[1].value.accounts || []);
      if (results[2].status === "fulfilled") setPositions(results[2].value.positions || []);
      if (results[3].status === "fulfilled") setResumeCount(Number(results[3].value.total || 0));
    } finally { setLoading(false); }
  }

  /** loadRuntime 在本地连接变化时单独读取运行状态。 */
  async function loadRuntime(baseURL = agentBase) {
    if (!baseURL) { setRuntime({}); return; }
    try { setRuntime(await localRequest(baseURL, "/api/v1/runtime/status")); } catch { setRuntime({}); }
  }

  useEffect(() => { void load(); }, []);
  useEffect(() => { void loadRuntime(); }, [agentBase]);
  const summary = useMemo(() => ({ today: tasks.reduce((sum, item) => sum + Number(item.today_greeted_count || 0), 0), total: tasks.reduce((sum, item) => sum + Number(item.greeted_count || 0), 0), running: tasks.filter((item) => item.status === "running").length }), [tasks]);
  const metrics = [["今日打招呼", summary.today, TaskAltRoundedIcon], ["累计打招呼", summary.total, PlayCircleRoundedIcon], ["运行中任务", summary.running, WorkRoundedIcon], ["简历数量", resumeCount, ArticleRoundedIcon]] as const;

  /** openAccount 使用云端账号 ID 打开对应本地浏览器档案。 */
  async function openAccount(account: any) {
    if (!agentBase) return notify("本地程序未连接", "error");
    try { await localRequest(agentBase, "/api/v1/browser/start", { method: "POST", body: { url: account.login_url || "https://www.zhipin.com/web/chat/recommend", persistent: true, user_data_dir: account.id, platform_account_id: account.id, headless: false, humanize: true } }); notify("账号浏览器已打开", "success"); } catch (error) { notify(error instanceof Error ? error.message : "账号打开失败", "error"); }
  }

  const steps = [{ key: "agent_connected", label: "连接本地程序", done: Boolean(agentBase), href: "/admin/agent-download" }, { key: "platform_account", label: "创建并登录平台账号", done: accounts.length > 0, href: "/admin/accounts" }, { key: "position_template", label: "创建岗位模板", done: positions.length > 0, href: "/admin/positions" }, { key: "task_started", label: "创建并开始任务", done: tasks.length > 0 || Boolean(onboarding.steps?.task_started), href: "/admin/tasks" }];
  const doneCount = steps.filter((item) => item.done).length;

  return <><PageHeader title="控制台" description="今天的招聘进展、本地组件和常用账号都在这里。" actions={<Button variant="outlined" startIcon={<RefreshRoundedIcon />} disabled={loading} onClick={() => void Promise.all([refreshAgent(), load(), loadRuntime()])}>刷新状态</Button>} />
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(4, 1fr)" }, gap: 1.5 }}>{metrics.map(([label, value, Icon]) => <Box key={label} sx={{ p: 2, bgcolor: "#f7faf8", borderRadius: "8px", border: "1px solid", borderColor: "divider" }}><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Typography sx={{ color: "text.secondary", fontSize: 13 }}>{label}</Typography><Icon color="primary" /></Stack><Typography sx={{ mt: 1.5, fontSize: 31, fontWeight: 800 }}>{loading ? <CircularProgress size={22} /> : value}</Typography></Box>)}</Box>
    <Box sx={{ mt: 2, display: "grid", gridTemplateColumns: { xs: "1fr", lg: "1.1fr .9fr" }, gap: 2 }}>
      <SectionPanel><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Box><Typography component="h2" sx={{ fontSize: 19, fontWeight: 780 }}>平台账号快捷入口</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>直接打开已登录的招聘平台账号。</Typography></Box><Button component={Link} href="/admin/accounts">管理账号</Button></Stack><Stack spacing={1} sx={{ mt: 2 }}>{accounts.length ? accounts.slice(0, 6).map((account) => <Stack key={account.id} direction="row" spacing={1.5} sx={{ alignItems: "center", py: 1, borderBottom: "1px solid", borderColor: "divider" }}><AccountCircleRoundedIcon color="primary" /><Box sx={{ flex: 1, minWidth: 0 }}><Typography noWrap sx={{ fontWeight: 730 }}>{account.display_name || "未命名账号"}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{account.platform_id || "未知平台"} · {account.status === "available" ? "已登录" : "需要登录"}</Typography></Box><Button size="small" startIcon={<LaunchRoundedIcon />} onClick={() => void openAccount(account)}>打开</Button></Stack>) : <Typography color="text.secondary">暂无平台账号</Typography>}</Stack></SectionPanel>
      <SectionPanel><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Box><Typography component="h2" sx={{ fontSize: 19, fontWeight: 780 }}>本地程序</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>{agentBase ? `已连接 ${agentBase}` : "尚未检测到本地程序"}</Typography></Box><Chip color={agentBase ? "success" : "error"} label={agentBase ? "已连接" : "未连接"} /></Stack><Box sx={{ mt: 2, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 1.5 }}><StatusItem label="程序版本" value={runtime.version || runtime.agent_version || "--"} /><StatusItem label="会员状态" value={subscription.active ? `${subscription.member_type || "Plus"} 有效` : "免费版"} /><StatusItem label="浏览器组件" value={runtime.cloakbrowser_installed || runtime.runtime?.cloakbrowser_installed ? "已安装" : "待检查"} /><StatusItem label="OCR 组件" value={runtime.ocr_installed || runtime.runtime?.ocr_installed ? "已安装" : "可选组件"} /></Box><Stack direction="row" spacing={1} sx={{ mt: 2 }}><Button component={Link} href="/admin/agent-download" variant="contained">组件与更新</Button><Button component={Link} href="/admin/local-data" variant="outlined">诊断本地数据</Button></Stack></SectionPanel>
    </Box>
    {!onboarding.completed || doneCount < steps.length ? <SectionPanel sx={{ mt: 2 }}><Stack direction={{ xs: "column", sm: "row" }} sx={{ justifyContent: "space-between", gap: 1 }}><Box><Typography component="h2" sx={{ fontSize: 19, fontWeight: 780 }}>新手教学</Typography><Typography sx={{ mt: 0.5, color: "text.secondary" }}>完成以下步骤即可开始第一条招聘任务。</Typography></Box><Typography sx={{ color: "primary.main", fontWeight: 750 }}>{doneCount}/{steps.length}</Typography></Stack><LinearProgress variant="determinate" value={(doneCount / steps.length) * 100} sx={{ mt: 2, height: 8, borderRadius: 4 }} /><Box sx={{ mt: 2, display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(4, 1fr)" }, gap: 1 }}>{steps.map((step, index) => <Button key={step.key} component={Link} href={step.href} color={step.done ? "primary" : "secondary"} variant={step.done ? "outlined" : "text"} startIcon={step.done ? <CheckCircleRoundedIcon /> : undefined} sx={{ justifyContent: "flex-start", borderRadius: "8px" }}>{index + 1}. {step.label}</Button>)}</Box></SectionPanel> : null}
  </>;
}

/** StatusItem 展示本地程序的一项简短状态。 */
function StatusItem({ label, value }: { label: string; value: string }) {
  return <Box sx={{ p: 1.5, bgcolor: "#f7faf8", borderRadius: "8px" }}><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{label}</Typography><Typography sx={{ mt: 0.5, fontWeight: 720 }}>{value}</Typography></Box>;
}
