/** 本文件负责新版后台控制台概览、醒目新手引导和本地状态。 */
"use client";

import ArticleRoundedIcon from "@mui/icons-material/ArticleRounded";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import CreditCardRoundedIcon from "@mui/icons-material/CreditCardRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import PlayCircleRoundedIcon from "@mui/icons-material/PlayCircleRounded";
import RefreshRoundedIcon from "@mui/icons-material/RefreshRounded";
import SettingsRoundedIcon from "@mui/icons-material/SettingsRounded";
import TaskAltRoundedIcon from "@mui/icons-material/TaskAltRounded";
import WorkRoundedIcon from "@mui/icons-material/WorkRounded";
import { Box, Button, Chip, CircularProgress, LinearProgress, Stack, TextField, Typography } from "@mui/material";
import Link from "next/link";
import { useEffect, useMemo, useState, type ElementType } from "react";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, localRequest } from "@/lib/admin-api";
import { onboardingFinished, readOnboardingProgress, syncOnboardingProgress, type OnboardingProgress, type OnboardingStep } from "@/lib/onboarding";

type GuideStep = {
  key: OnboardingStep;
  title: string;
  description: string;
  tips: string[];
  href: string;
  action: string;
  icon: ElementType;
};

const guideSteps: GuideStep[] = [
  { key: "local_agent", title: "确认本地程序已启动", description: "浏览器控制、截图和 OCR 都依赖本地 GoodHR 程序。", tips: ["启动本地 GoodHR 程序", "连接成功后会自动完成", "未安装时前往组件信息页面"], href: "/admin/agent-download", action: "检查本地程序", icon: DownloadRoundedIcon },
  { key: "personal_config", title: "保存个人配置", description: "配置支持图片识别的 AI 地址、模型和 API Key。", tips: ["前往个人配置", "填写 API 地址、模型和 Key", "测试成功后保存配置"], href: "/admin/personal-config", action: "配置 AI", icon: SettingsRoundedIcon },
  { key: "position_template", title: "创建岗位管理", description: "岗位管理决定筛选条件、岗位要求和打招呼逻辑。", tips: ["进入岗位管理", "点击新建岗位", "填写岗位要求或关键词后保存"], href: "/admin/positions", action: "创建岗位", icon: WorkRoundedIcon },
  { key: "task_started", title: "创建并运行任务", description: "选择岗位创建任务，然后成功启动一次。", tips: ["进入任务列表", "创建任务并选择岗位", "点击开始，成功启动后自动完成"], href: "/admin/tasks", action: "创建任务", icon: PlayCircleRoundedIcon },
  { key: "subscription_viewed", title: "查看订阅页面", description: "查看会员到期时间、可用套餐和自己的支付记录。", tips: ["进入订阅会员", "查看当前会员状态", "需要续期时选择合适套餐"], href: "/admin/subscription", action: "查看订阅", icon: CreditCardRoundedIcon },
];

/** DashboardPage 展示用户当前最需要关注的招聘和本地运行状态。 */
export default function DashboardPage() {
  const { user, agentBase, subscription, onboarding, refreshAgent, notify } = useAdmin();
  const [tasks, setTasks] = useState<any[]>([]);
  const [positions, setPositions] = useState<any[]>([]);
  const [resumeCount, setResumeCount] = useState(0);
  const [runtime, setRuntime] = useState<any>({});
  const [aiConfigured, setAIConfigured] = useState(false);
  const [wallet, setWallet] = useState<any>({});
  const [rechargeAmount, setRechargeAmount] = useState("10");
  const [recharging, setRecharging] = useState(false);
  const [guideProgress, setGuideProgress] = useState<OnboardingProgress>(() => readOnboardingProgress(""));
  const [loading, setLoading] = useState(true);

  /** load 读取控制台概览和新手引导需要的云端数据。 */
  async function load() {
    setLoading(true);
    try {
      const results = await Promise.allSettled([
        cloudRequest("/api/tasks"),
      cloudRequest("/api/positions"),
      cloudRequest("/api/candidates?page=1&page_size=1"),
      cloudRequest("/api/config/user-ai"),
      cloudRequest("/api/ai-wallet"),
      ]);
      if (results[0].status === "fulfilled") setTasks(results[0].value.tasks || []);
      if (results[1].status === "fulfilled") setPositions(results[1].value.positions || []);
      if (results[2].status === "fulfilled") setResumeCount(Number(results[2].value.total || 0));
      if (results[3].status === "fulfilled") {
        const config = results[3].value.config || {};
        setAIConfigured(Boolean(config.api_key_set && config.base_url && config.model && config.enabled !== false));
      }
      if (results[4].status === "fulfilled") setWallet(results[4].value || {});
    } finally {
      setLoading(false);
    }
  }

  /** loadRuntime 在本地连接变化时读取运行组件状态。 */
  async function loadRuntime(baseURL = agentBase) {
    if (!baseURL) { setRuntime({}); return; }
    try {
      const [runtimeResult, healthResult] = await Promise.allSettled([
        localRequest(baseURL, "/api/v1/runtime/status"),
        localRequest(baseURL, "/health"),
      ]);
      const runtimeData = runtimeResult.status === "fulfilled" ? runtimeResult.value : {};
      const healthData = healthResult.status === "fulfilled" ? healthResult.value : {};
      setRuntime({ ...runtimeData, ...healthData });
    } catch { setRuntime({}); }
  }

  useEffect(() => { void load(); }, []);
  useEffect(() => { void loadRuntime(); }, [agentBase]);

  const taskWasStarted = useMemo(() => tasks.some((item) => {
    const status = String(item.status || "").toLowerCase();
    return Boolean(item.started_at || item.last_run_at || ["running", "done", "stopped", "failed"].includes(status));
  }), [tasks]);

  useEffect(() => {
    const email = String(user?.email || "");
    if (!email) return;
    void syncOnboardingProgress(email, {
      local_agent: Boolean(agentBase),
      personal_config: aiConfigured,
      platform_account: true,
      position_template: positions.length > 0,
      task_started: taskWasStarted,
    }, Boolean(onboarding.completed)).then(setGuideProgress);
  }, [agentBase, aiConfigured, onboarding.completed, positions.length, taskWasStarted, user?.email]);

  const summary = useMemo(() => ({ today: tasks.reduce((sum, item) => sum + Number(item.today_greeted_count || 0), 0), total: tasks.reduce((sum, item) => sum + Number(item.greeted_count || 0), 0), running: tasks.filter((item) => item.status === "running").length }), [tasks]);
  const metrics = [["今日打招呼", summary.today, TaskAltRoundedIcon], ["累计打招呼", summary.total, PlayCircleRoundedIcon], ["运行中任务", summary.running, WorkRoundedIcon], ["简历数量", resumeCount, ArticleRoundedIcon]] as const;
  /** rechargeAI 创建内置 AI 余额充值订单。 */
  async function rechargeAI() {
    setRecharging(true);
    try {
      const data = await cloudRequest("/api/payment/ai-balance", { method: "POST", body: { amount_yuan: rechargeAmount || "10" } });
      submitPayment(data.payment);
      notify("充值订单已打开，支付完我再回来认真记账。", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "充值订单没创建成功，我们再试一次。", "error");
    } finally {
      setRecharging(false);
    }
  }
  const doneCount = guideSteps.filter((item) => guideProgress.steps[item.key]).length;
  const showGuide = !onboarding.completed && !onboardingFinished(guideProgress);

  return <>
    <PageHeader title="控制台" description="今天的招聘进展、本地组件和常用账号都在这里。" actions={<Button variant="outlined" startIcon={<RefreshRoundedIcon />} disabled={loading} onClick={() => void Promise.all([refreshAgent(), load(), loadRuntime()])}>刷新状态</Button>} />

    {showGuide ? <OnboardingGuide progress={guideProgress} doneCount={doneCount} /> : null}

    <Box sx={{ mt: showGuide ? 2.5 : 0, display: "grid", gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(4, 1fr)" }, gap: 1.5 }}>{metrics.map(([label, value, Icon]) => <Box key={label} sx={{ p: 2, bgcolor: "#f7faf8", borderRadius: "8px", border: "1px solid", borderColor: "divider" }}><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Typography sx={{ color: "text.secondary", fontSize: 13 }}>{label}</Typography><Icon color="primary" /></Stack><Typography sx={{ mt: 1.5, fontSize: 31, fontWeight: 800 }}>{loading ? <CircularProgress size={22} /> : value}</Typography></Box>)}</Box>
    <Box sx={{ mt: 2, display: "grid", gridTemplateColumns: { xs: "1fr", lg: "repeat(2, minmax(0, 1fr))" }, gap: 2 }}>
      <AIWalletCard wallet={wallet} amount={rechargeAmount} setAmount={setRechargeAmount} loading={recharging} onRecharge={rechargeAI} />
      <SectionPanel><Stack direction={{ xs: "column", sm: "row" }} spacing={1.25} sx={{ justifyContent: "space-between", alignItems: { sm: "center" } }}><Box><Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}><Typography component="h2" sx={{ fontSize: 18, fontWeight: 780 }}>本地程序</Typography><Chip size="small" color={agentBase ? "success" : "error"} label={agentBase ? "已连接" : "未连接"} /></Stack><Typography sx={{ mt: 0.4, color: "text.secondary", fontSize: 12 }}>{agentBase ? `${agentBase} · ${runtime.version || runtime.agent_version || "版本未知"} · ${subscription.active ? `${subscription.member_type || "Plus"} 有效` : "免费版"}` : "尚未检测到本地程序"}</Typography></Box><Stack direction="row" spacing={1} sx={{ flexWrap: "wrap" }}><Button component={Link} href="/admin/agent-download" size="small" variant="contained">组件</Button><Button component={Link} href="/admin/local-data" size="small" variant="outlined">诊断</Button></Stack></Stack><Box sx={{ mt: 1.5, p: 1.25, borderRadius: "8px", bgcolor: "#fff8ed", border: "1px solid #f0d8ac" }}><Typography sx={{ color: "#7a4d00", fontSize: 13, lineHeight: 1.65 }}>我小声提醒一下：由于浏览器限制，在浏览器内下载的文件请到“我的电脑 - 下载”里查看。如果没有，请在以下目录内查看：</Typography><Typography sx={{ mt: 0.5, color: "#5f3b00", fontSize: 12, fontFamily: "monospace", overflowWrap: "anywhere" }}>{runtime.downloadsDir || runtime.downloads_dir || "本地程序未返回下载目录"}</Typography></Box></SectionPanel>
    </Box>
  </>;
}

/** AIWalletCard 展示内置 AI 余额和充值入口。 */
function AIWalletCard({ wallet, amount, setAmount, loading, onRecharge }: { wallet: any; amount: string; setAmount: (value: string) => void; loading: boolean; onRecharge: () => void }) {
  return <SectionPanel><Stack direction={{ xs: "column", md: "row" }} spacing={1.5} sx={{ justifyContent: "space-between", alignItems: { md: "center" } }}><Box><Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}><CreditCardRoundedIcon color="primary" /><Typography component="h2" sx={{ fontSize: 18, fontWeight: 780 }}>AI 余额</Typography><Chip size="small" label={`默认模型：${wallet.default_model || "未配置"}`} sx={{ bgcolor: "#eef6f0", color: "#2f6f4f" }} /></Stack><Typography sx={{ mt: 0.75, fontSize: 30, fontWeight: 850 }}>￥{wallet.balance || "0.00"}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>默认已接入 GoodHR 内置 AI，也可以去个人配置里换成自己的 Key。</Typography></Box><Stack direction={{ xs: "column", sm: "row" }} spacing={1} sx={{ width: { xs: "100%", md: "auto" }, alignItems: { sm: "center" } }}><TextField size="small" label="充值金额（元）" value={amount} onChange={(event) => setAmount(event.target.value)} sx={{ width: { xs: "100%", sm: 150 } }} /><Button variant="contained" disabled={loading} onClick={onRecharge}>{loading ? "正在下单" : "充值"}</Button></Stack></Stack></SectionPanel>;
}

/** submitPayment 创建并提交第三方支付表单。 */
function submitPayment(payment: any) {
  if (!payment?.submit_url) throw new Error("支付平台没有返回可打开的支付地址");
  const form = document.createElement("form");
  form.method = payment.submit_method || "POST";
  form.action = payment.submit_url;
  form.target = "_blank";
  Object.entries(payment.submit_fields || {}).forEach(([key, value]) => {
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = key;
    input.value = String(value ?? "");
    form.appendChild(input);
  });
  document.body.appendChild(form);
  form.submit();
  form.remove();
}

/** OnboardingGuide 展示与旧版步骤一致的醒目新手引导。 */
function OnboardingGuide({ progress, doneCount }: { progress: OnboardingProgress; doneCount: number }) {
  const activeKey = guideSteps.find((item) => !progress.steps[item.key])?.key || "";
  return <Box component="section" sx={{ p: { xs: 2, md: 2.5 }, border: "1px solid #b9d4c1", borderRadius: "8px", bgcolor: "#edf6ef", boxShadow: "0 12px 28px rgba(33, 85, 57, .08)" }}>
    <Stack direction={{ xs: "column", md: "row" }} spacing={2} sx={{ justifyContent: "space-between", alignItems: { md: "center" } }}>
      <Box><Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><Chip size="small" label="新手必看" sx={{ bgcolor: "#1e6545", color: "white", fontWeight: 760 }} /><Typography component="h2" sx={{ fontSize: { xs: 21, md: 24 }, fontWeight: 800 }}>完成 6 步，开始第一条招聘任务</Typography></Stack><Typography sx={{ mt: 0.75, color: "#52665a" }}>每完成一步都会自动记录；全部完成后，这个教程会自动隐藏。</Typography></Box>
      <Box sx={{ minWidth: { md: 210 } }}><Stack direction="row" sx={{ mb: 0.75, justifyContent: "space-between" }}><Typography sx={{ color: "#52665a", fontSize: 13 }}>上手进度</Typography><Typography sx={{ color: "#1e6545", fontWeight: 800 }}>{doneCount}/{guideSteps.length}</Typography></Stack><LinearProgress variant="determinate" value={(doneCount / guideSteps.length) * 100} sx={{ height: 9, borderRadius: "8px", bgcolor: "#d5e5d9" }} /></Box>
    </Stack>
    <Box sx={{ mt: 2.5, display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))", xl: "repeat(3, minmax(0, 1fr))" }, gap: 1.5 }}>
      {guideSteps.map((step, index) => <GuideCard key={step.key} step={step} index={index + 1} done={progress.steps[step.key]} active={activeKey === step.key} />)}
    </Box>
  </Box>;
}

/** GuideCard 展示一个新手步骤、完成状态和操作入口。 */
function GuideCard({ step, index, done, active }: { step: GuideStep; index: number; done: boolean; active: boolean }) {
  const Icon = step.icon;
  return <Box component="article" sx={{ display: "flex", flexDirection: "column", minHeight: 235, p: 2, border: "1px solid", borderColor: done ? "#d7e3da" : active ? "#4d8d68" : "#cbdccf", borderRadius: "8px", bgcolor: done ? "rgba(255,255,255,.58)" : "#fff", boxShadow: active ? "0 10px 24px rgba(36, 94, 61, .11)" : "none", opacity: done ? 0.76 : 1 }}>
    <Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "center" }}><Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><Box sx={{ width: 36, height: 36, display: "grid", placeItems: "center", borderRadius: "8px", bgcolor: done ? "#e7efe9" : "#dcece1", color: "#1e6545" }}><Icon fontSize="small" /></Box><Typography sx={{ color: "#718078", fontSize: 12, fontWeight: 760 }}>第 {index} 步</Typography></Stack>{done ? <Chip size="small" icon={<CheckCircleRoundedIcon />} label="已完成" color="success" variant="outlined" /> : active ? <Chip size="small" label="当前需要" sx={{ bgcolor: "#fff1d6", color: "#8a5b00", fontWeight: 700 }} /> : null}</Stack>
    <Typography component="h3" sx={{ mt: 1.5, fontSize: 17, fontWeight: 790 }}>{step.title}</Typography>
    <Typography sx={{ mt: 0.6, color: "text.secondary", fontSize: 13, lineHeight: 1.55 }}>{step.description}</Typography>
    <Stack component="ol" spacing={0.4} sx={{ mt: 1.25, mb: 1.5, pl: 2.25, color: "text.secondary", fontSize: 12, lineHeight: 1.5 }}>{step.tips.map((tip) => <li key={tip}>{tip}</li>)}</Stack>
    {!done ? <Button component={Link} href={step.href} variant={active ? "contained" : "outlined"} size="small" sx={{ mt: "auto", alignSelf: "flex-start" }}>{step.action}</Button> : null}
  </Box>;
}
