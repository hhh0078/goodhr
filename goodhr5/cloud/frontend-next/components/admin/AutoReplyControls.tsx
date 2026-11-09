/** 文件作用说明：经设备通道控制服务器自动回复，展示云端运行状态与岗位配置入口。 */
"use client";
import { Alert, Badge, Button, Flex, Input, Switch, Tag, Tooltip, Typography } from "antd";
import { CommentOutlined, PlayCircleOutlined, SettingOutlined, StopOutlined } from "@ant-design/icons";
import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useAdmin } from "./AdminApp";
import AutoReplyPositionDialog from "./AutoReplyPositionDialog";
import AutoReplyInbox from "./AutoReplyInbox";
import AdminDialog from "./AdminDialog";
import PositionFloatingStatus, { openPositionFloatingWindow, type PositionAnalysisStatus, type PositionFloatingStatusValue } from "./PositionFloatingStatus";
import { ensureLocalAgentReady, getToken, localRequest } from "@/lib/admin-api";
import { canUseAutoReply } from "@/lib/subscription";
import { loadReplyInboxSummary, type ReplyInboxSummary } from "@/lib/auto-reply-inbox";
import { loadPositionAutoReplyConfig, loadPositionAutoReplyStatus, savePositionAutoReplyConfig, type PositionAutoReplyStatus } from "@/lib/auto-reply";
import styles from "./auto-reply-inbox.module.css";

type ReplyTask = {
  status: PositionFloatingStatusValue;
  current_step?: string;
  error_message?: string;
  scanned_count?: number;
  greeted_count?: number;
  skipped_count?: number;
  blocked_report_count?: number;
  blocked_resume_count?: number;
  analysis?: Record<string, unknown>;
};

/** taskStatus 只接受服务器明确定义的任务状态，缺失时安全回到停止状态。 */
function taskStatus(value: unknown): PositionFloatingStatusValue {
  return value === "running" || value === "completed" || value === "failed" ? value : "stopped";
}

/** replyAnalysis 将服务器的阶段摘要转换为共享小窗内容，不在网页缓存聊天正文。 */
function replyAnalysis(task: ReplyTask | null): PositionAnalysisStatus | null {
  const value = task?.analysis;
  if (!value) return null;
  return {
    kind: "auto_reply", phase: value.phase === "loading" || value.phase === "error" ? value.phase : "result",
    terminal: Boolean(value.terminal), candidate_name: String(value.candidate_name || ""),
    reason: String(value.reason || task?.current_step || ""), updated_at: String(value.updated_at || ""),
  };
}

/** AutoReplyGlobalControl 串行读取服务器全局任务，刷新页面后仍能停止已在运行的自动回复。 */
export function AutoReplyGlobalControl() {
  const { agentBase, subscription, notify, confirm, user } = useAdmin();
  const router = useRouter();
  const [task, setTask] = useState<ReplyTask | null>(null);
  const [busy, setBusy] = useState(false);
  const [statusError, setStatusError] = useState("");
  const [accountOpen, setAccountOpen] = useState(false);
  const [replyAccount, setReplyAccount] = useState("");
  const [inboxOpen, setInboxOpen] = useState(false);
  const [summary, setSummary] = useState<ReplyInboxSummary | null>(null);
  const [summaryError, setSummaryError] = useState("");
  const [summaryLoading, setSummaryLoading] = useState(false);
  const summaryRead = useRef<AbortController | null>(null);
  const summaryGeneration = useRef(0);
  const owner = user?.email || "";
  const operation = useRef(false);
  const currentAgent = useRef(agentBase);
  currentAgent.current = agentBase;
  const [pipWindow, setPipWindow] = useState<Window | null>(null);
  const running = task?.status === "running";
  const blockedCount = (task?.blocked_report_count || 0) + (task?.blocked_resume_count || 0);
  const failureReason = task?.error_message?.trim() || (task?.current_step !== "finished" ? task?.current_step?.trim() : "") || "这次没跑顺，请检查本地程序和招聘页面。";
  const currentStep = task?.status === "failed" ? failureReason : task?.current_step;

  /** readSummary 独立读取云端待办计数，未连接本机时也可使用，慢请求不叠加。 */
  const readSummary = useCallback(async () => {
    if (!owner || summaryRead.current) return;
    const controller = new AbortController();
    summaryRead.current = controller;
    const generation = summaryGeneration.current;
    const timeout = window.setTimeout(() => controller.abort(), 15000);
    setSummaryLoading(true);
    try {
      const data = await loadReplyInboxSummary(controller.signal);
      if (generation === summaryGeneration.current) { setSummary(data); setSummaryError(""); }
    } catch (cause) {
      if (generation === summaryGeneration.current) setSummaryError(controller.signal.aborted ? "待确认数量读取超时，打开面板后可以重试。" : cause instanceof Error ? cause.message : "待确认数量暂时没读到。");
    } finally {
      window.clearTimeout(timeout);
      if (summaryRead.current === controller) { summaryRead.current = null; setSummaryLoading(false); }
    }
  }, [owner]);

  /** refreshInbox 在打开面板或完成答复后重读服务器数量，不自行猜测扣减。 */
  const refreshInbox = useCallback(() => { void readSummary(); }, [readSummary]);

  useEffect(() => {
    summaryGeneration.current += 1; summaryRead.current?.abort(); summaryRead.current = null;
    setSummary(null); setSummaryError(""); setInboxOpen(false);
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    /** pollSummary 只轮询轻量汇总，隐藏页面时不读取候选人详情。 */
    async function pollSummary() {
      if (document.visibilityState === "visible") await readSummary();
      if (!disposed) timer = setTimeout(() => void pollSummary(), 10000);
    }
    void pollSummary();
    return () => { disposed = true; clearTimeout(timer); summaryGeneration.current += 1; summaryRead.current?.abort(); summaryRead.current = null; };
  }, [readSummary]);
  useEffect(() => {
    setTask(null); setStatusError(""); setAccountOpen(false); setReplyAccount("");
    if (!agentBase) return;
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    /** poll 仅在上次读取完成后安排下一次，操作期间不以旧状态覆盖启停结果。 */
    async function poll() {
      try {
        if (!operation.current) {
          const response = await localRequest(agentBase, "/api/v1/local/auto-reply/status");
          if (!disposed && !operation.current) { setTask({ ...response, status: taskStatus(response?.status) }); setStatusError(""); }
        }
      } catch { if (!disposed) setStatusError("本地状态暂时没读到，请检查本地程序版本和连接。"); }
      finally { if (!disposed) timer = setTimeout(() => void poll(), 3000); }
    }
    void poll();
    return () => { disposed = true; clearTimeout(timer); };
  }, [agentBase]);

  /** changeRunning 经设备请求服务器启动或安全停止，失败时保留当前状态并给出重试提示。 */
  async function changeRunning(account?: string) {
    if (!agentBase || operation.current) return;
    if (!running && !canUseAutoReply(subscription)) {
      if (await confirm("自动回复需要 Max", "Max 或全功能体验会员可以自动回复。现在去看看套餐吗？")) router.push("/admin/subscription");
      return;
    }
    if (!running && !account?.trim()) { setAccountOpen(true); return; }
    const base = agentBase;
    const stopping = running;
    operation.current = true; setBusy(true);
    const floating = stopping ? Promise.resolve(null) : openPositionFloatingWindow();
    try {
      if (!stopping) {
        await ensureLocalAgentReady(base);
        if (base !== currentAgent.current) throw new Error("本地连接已变化，请重新开始");
      }
      const result = await localRequest(base, `/api/v1/local/auto-reply/${stopping ? "stop" : "start"}`, {
        method: "POST", ...(stopping ? {} : { body: { token: getToken(), platform_account_id: account!.trim() } }), timeoutMS: stopping ? 220000 : 60000,
      });
      if (base !== currentAgent.current) return;
      setTask({ ...result, status: stopping ? taskStatus(result?.status) : "running" });
      setAccountOpen(false);
      const window = await floating;
      if (window) setPipWindow(window);
      notify(stopping ? "收到，处理完当前会话就停。" : "自动回复已开始，我会盯着当前平台已开启岗位的消息。", "success");
    } catch (cause) {
      const window = await floating;
      if (window) window.close();
      if (base === currentAgent.current) notify(cause instanceof Error ? cause.message : "这次没跑顺，可以再试一次。", "error");
    } finally { operation.current = false; setBusy(false); }
  }
  /** retryReports 由HR确认后重试报告和简历待办，复用当前账号；不重新发送已处理的聊天。 */
  async function retryReports() {
    if (!agentBase || operation.current) return;
    if (!await confirm("重试待办", "重新整理简历或生成报告会使用 AI 余额，不会重发已回复的消息。简历待办会在自动回复运行时继续处理。确认后，我再试一次。")) return;
    const base = agentBase;
    operation.current = true; setBusy(true);
    try {
      const result = await localRequest(base, "/api/v1/local/auto-reply/retry-reports", { method: "POST", body: { token: getToken() }, timeoutMS: 15000 });
      if (base !== currentAgent.current) return;
      const count = Math.max(0, Number(result?.retried_count) || 0);
      const resumeCount = Math.max(0, Number(result?.resume_count) || 0);
      const reportCount = Math.max(0, Number(result?.report_count ?? count) || 0);
      setTask((current) => current ? { ...current, blocked_report_count: Math.max(0, (current.blocked_report_count || 0) - reportCount), blocked_resume_count: Math.max(0, (current.blocked_resume_count || 0) - resumeCount) } : current);
      notify(count ? `已重新安排 ${count} 项待办，简历处理需保持自动回复运行。` : "暂未安排新的重试，待办可能仍在处理中，请稍后再试。", "success");
    } catch (cause) {
      if (base === currentAgent.current) notify(cause instanceof Error ? cause.message : "这次没安排成功，可以稍后再试。", "error");
    } finally { operation.current = false; setBusy(false); }
  }
  return <>
    <Flex vertical gap={8} style={{ minWidth: 0, maxWidth: "min(320px, 55vw)" }}>
    <Flex align="center" gap={8} wrap>
    <Tooltip title={summaryError || "打开自动回复，查看运行状态和待确认补充"}>
      <span style={{ paddingInlineEnd: 8 }}><Badge showZero size="small" offset={[0, 7]} color={!summaryError && summary?.pending_count === 0 ? "var(--goodhr-text-secondary)" : undefined}
        count={summaryError ? "?" : summary ? summary.pending_count : "…"} overflowCount={99} title={summaryError || (summary ? `${summary.pending_count} 项待确认补充` : "正在核对待确认数量")}>
        <Button data-testid="auto-reply-global" aria-label="自动回复工作台" icon={running ? <CommentOutlined aria-hidden="true" /> : <PlayCircleOutlined aria-hidden="true" />}
          loading={busy} onClick={() => { setInboxOpen(true); refreshInbox(); }}>
          {running ? "自动回复运行中" : "开始自动回复"}
        </Button>
      </Badge></span>
    </Tooltip>
    </Flex>
    {task?.status === "failed" && <Alert type="error" showIcon role="alert" data-testid="auto-reply-failure"
      title="自动回复已停止" description={<>{failureReason}<br />处理后点击“开始自动回复”重试。</>}
      style={{ overflowWrap: "anywhere" }} />}
    </Flex>
    <AdminDialog open={inboxOpen} title="自动回复" cancelText="关闭" maxWidth="lg" loading={busy} onClose={() => setInboxOpen(false)}>
      <div className={styles.runBar}>
        <div className={styles.runInfo}>
          <div><Typography.Text strong>消息接待</Typography.Text> <Tag color={running ? "success" : task?.status === "failed" ? "error" : undefined}>{running ? "运行中" : task?.status === "failed" ? "已停止" : agentBase ? "未启动" : "本机未连接"}</Tag></div>
          <p>{statusError || (!agentBase ? "本机暂未连接，待确认问题仍可在这里处理。" : task?.status === "failed" ? failureReason : running ? "我会关注当前平台已开启岗位的新消息。" : "开始后，我会按各岗位设置接待候选人。")}</p>
        </div>
        <div className={styles.runActions}>
          {blockedCount > 0 && <Button disabled={busy} onClick={() => void retryReports()}>重试待办（{blockedCount}）</Button>}
          <Button type="primary" danger={running} icon={running ? <StopOutlined aria-hidden="true" /> : <PlayCircleOutlined aria-hidden="true" />}
            loading={busy} disabled={!agentBase || task === null} onClick={() => void changeRunning()}>{running ? "停止自动回复" : "开始自动回复"}</Button>
        </div>
      </div>
      <AutoReplyInbox key={owner} summary={summary} loading={summaryLoading} error={summaryError} active={inboxOpen} onRetry={refreshInbox} onApplied={refreshInbox} />
    </AdminDialog>
    <AdminDialog open={accountOpen} title="确认当前招聘账号" confirmText="确认账号并开始" loading={busy}
      confirmDisabled={!replyAccount.trim()} onClose={() => setAccountOpen(false)} onConfirm={() => void changeRunning(replyAccount)}>
      <Typography.Paragraph>先打开并登录招聘平台，再填写当前账号名或手机号。我只处理这个平台里已开启自动回复的岗位。</Typography.Paragraph>
      <label htmlFor="auto-reply-account">当前招聘账号</label>
      <Input id="auto-reply-account" value={replyAccount} maxLength={200} autoComplete="off" placeholder="招聘页面当前登录的账号名或手机号"
        onChange={(event) => setReplyAccount(event.target.value)} style={{ marginBlock: 8 }} />
      <Alert type="info" showIcon title="聊天记录按这个账号分开保存。换账号前先停止，再修改这里。" />
    </AdminDialog>
    <PositionFloatingStatus autoReply pipWindow={pipWindow} status={task?.status || "stopped"} currentStep={currentStep || "正在等待候选人消息"}
      analysis={replyAnalysis(task)} scannedCount={Number(task?.scanned_count) || 0} greetedCount={Number(task?.greeted_count) || 0}
      skippedCount={Number(task?.skipped_count) || 0} onClosed={() => setPipWindow(null)} />
  </>;
}

/** PositionAutoReplyControl 展示本人岗位的开关和配置，失败时保留配置入口供修正。 */
export function PositionAutoReplyControl({ position }: { position: { id: string; name: string; requirement?: string } }) {
  const { subscription, notify, confirm } = useAdmin();
  const router = useRouter();
  const [status, setStatus] = useState<PositionAutoReplyStatus | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const operation = useRef(false);
  const allowAutoReply = canUseAutoReply(subscription);
  useEffect(() => {
    let disposed = false;
    setStatus(null); setError("");
    void loadPositionAutoReplyStatus(position.id).then((value) => { if (!disposed) setStatus(value); })
      .catch((cause) => { if (!disposed) setError(cause instanceof Error ? cause.message : "自动回复状态没读出来"); });
    return () => { disposed = true; };
  }, [position.id]);
  /** requireMembership 说明开启门槛并由用户选择是否查看套餐。 */
  async function requireMembership() {
    if (await confirm("自动回复需要 Max", "Max 或全功能体验会员可以自动回复。现在去看看套餐吗？")) router.push("/admin/subscription");
  }
  /** toggle 先读完整配置再切换，缺少公司资料时打开配置表单。 */
  async function toggle(enabled: boolean) {
    if (operation.current) return;
    if (enabled && !allowAutoReply) { await requireMembership(); return; }
    operation.current = true; setBusy(true);
    try {
      const config = await loadPositionAutoReplyConfig(position.id);
      if (enabled && !config.company_profile_id) { setOpen(true); return; }
      await savePositionAutoReplyConfig({ ...config, enabled });
      setStatus(await loadPositionAutoReplyStatus(position.id)); setError("");
      notify(enabled ? "这个岗位已开启自动回复，顶部可以开始。" : "这个岗位的自动回复已关闭。", "success");
    } catch (cause) { notify(cause instanceof Error ? cause.message : "开关没改成功，请再试一次。", "error"); }
    finally { operation.current = false; setBusy(false); }
  }
  return <>
    <Flex align="center" gap={8} wrap style={{ marginTop: 8 }} data-testid="position-auto-reply">
      <Tooltip title={error || "关闭后，当前会话处理完就不再接新消息"}>
        <Switch aria-label={`${position.name}自动回复开关`} checked={Boolean(status?.configured_enabled)} loading={busy}
          disabled={!status} onChange={(enabled) => void toggle(enabled)} />
      </Tooltip>
      <Typography.Text type="secondary"><CommentOutlined aria-hidden="true" /> 自动回复</Typography.Text>
      <Button type="text" icon={<SettingOutlined aria-hidden="true" />} onClick={() => setOpen(true)} aria-label={`${position.name}自动回复设置`}>设置</Button>
    </Flex>
    <AutoReplyPositionDialog open={open} position={position} allowAutoReply={allowAutoReply} notify={notify} confirm={confirm}
      onRequireMax={() => void requireMembership()} onClose={() => setOpen(false)} onSaved={(enabled) => {
        setStatus({ configured_enabled: enabled, enabled: enabled && allowAutoReply, allow_auto_reply: allowAutoReply, version: 0 }); setError("");
      }} />
  </>;
}
