/** 文件作用说明：复用本地程序接口提供全局自动回复启停、实时状态与岗位配置入口。 */
"use client";
import { Button, Flex, Switch, Tooltip, Typography } from "antd";
import { CommentOutlined, PlayCircleOutlined, SettingOutlined, StopOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useAdmin } from "./AdminApp";
import AutoReplyPositionDialog from "./AutoReplyPositionDialog";
import PositionFloatingStatus, { openPositionFloatingWindow, type PositionAnalysisStatus, type PositionFloatingStatusValue } from "./PositionFloatingStatus";
import { getToken, localRequest } from "@/lib/admin-api";
import { canUseAutoReply } from "@/lib/subscription";
import { loadPositionAutoReplyConfig, loadPositionAutoReplyStatus, savePositionAutoReplyConfig, type PositionAutoReplyStatus } from "@/lib/auto-reply";

type ReplyTask = {
  status: PositionFloatingStatusValue;
  current_step?: string;
  scanned_count?: number;
  greeted_count?: number;
  skipped_count?: number;
  analysis?: Record<string, unknown>;
};

/** taskStatus 只接受本地程序明确定义的任务状态，缺失时安全回到停止状态。 */
function taskStatus(value: unknown): PositionFloatingStatusValue {
  return value === "running" || value === "completed" || value === "failed" ? value : "stopped";
}

/** replyAnalysis 把本机分析摘要转换为共享小窗内容，不上传或缓存聊天正文。 */
function replyAnalysis(task: ReplyTask | null): PositionAnalysisStatus | null {
  const value = task?.analysis;
  if (!value) return null;
  return {
    kind: "auto_reply", phase: value.phase === "loading" || value.phase === "error" ? value.phase : "result",
    terminal: Boolean(value.terminal), candidate_name: String(value.candidate_name || ""),
    reason: String(value.reason || task?.current_step || ""), updated_at: String(value.updated_at || ""),
  };
}

/** AutoReplyGlobalControl 串行读取本地全局任务，刷新页面后仍能停止已在运行的自动回复。 */
export function AutoReplyGlobalControl() {
  const { agentBase, subscription, notify, confirm } = useAdmin();
  const router = useRouter();
  const [task, setTask] = useState<ReplyTask | null>(null);
  const [busy, setBusy] = useState(false);
  const [statusError, setStatusError] = useState("");
  const operation = useRef(false);
  const currentAgent = useRef(agentBase);
  currentAgent.current = agentBase;
  const [pipWindow, setPipWindow] = useState<Window | null>(null);
  const running = task?.status === "running";
  useEffect(() => {
    setTask(null); setStatusError("");
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

  /** changeRunning 请求本地启动或安全停止，失败时保留当前状态并给出可重试提示。 */
  async function changeRunning() {
    if (!agentBase || operation.current) return;
    if (!running && !canUseAutoReply(subscription)) {
      if (await confirm("自动回复需要 Max", "Max 或全功能体验会员可以自动回复。现在去看看套餐吗？")) router.push("/admin/subscription");
      return;
    }
    const base = agentBase;
    const stopping = running;
    operation.current = true; setBusy(true);
    const floating = stopping ? Promise.resolve(null) : openPositionFloatingWindow();
    try {
      const result = await localRequest(base, `/api/v1/local/auto-reply/${stopping ? "stop" : "start"}`, {
        method: "POST", ...(stopping ? {} : { body: { token: getToken() } }), timeoutMS: stopping ? 220000 : 60000,
      });
      if (base !== currentAgent.current) return;
      setTask({ ...result, status: stopping ? taskStatus(result?.status) : "running" });
      const window = await floating;
      if (window) setPipWindow(window);
      notify(stopping ? "收到，处理完当前会话就停。" : "自动回复已开始，我会盯着当前平台已开启岗位的消息。", "success");
    } catch (cause) {
      const window = await floating;
      if (window) window.close();
      if (base === currentAgent.current) notify(cause instanceof Error ? cause.message : "这次没跑顺，可以再试一次。", "error");
    } finally { operation.current = false; setBusy(false); }
  }
  return <>
    <Tooltip title={agentBase ? statusError || task?.current_step || "只处理当前招聘平台中已开启自动回复的岗位" : "先连接本地程序，再帮你盯消息"}>
      <Button data-testid="auto-reply-global" icon={running ? <StopOutlined aria-hidden="true" /> : <PlayCircleOutlined aria-hidden="true" />}
        danger={running} loading={busy} disabled={!agentBase || task === null} onClick={() => void changeRunning()}>
        {running ? "停止自动回复" : "开始自动回复"}
      </Button>
    </Tooltip>
    <PositionFloatingStatus autoReply pipWindow={pipWindow} status={task?.status || "stopped"} currentStep={task?.current_step || "正在等待候选人消息"}
      analysis={replyAnalysis(task)} scannedCount={Number(task?.scanned_count) || 0} greetedCount={Number(task?.greeted_count) || 0}
      skippedCount={Number(task?.skipped_count) || 0} onClosed={() => setPipWindow(null)} />
  </>;
}

/** PositionAutoReplyControl 展示本人岗位的开关和配置，失败时保留配置入口供修正。 */
export function PositionAutoReplyControl({ position }: { position: { id: string; name: string } }) {
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
