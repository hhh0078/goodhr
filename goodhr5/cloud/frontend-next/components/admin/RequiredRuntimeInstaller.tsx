/** 本文件负责在必要运行组件缺失时展示强制安装弹框。 */
"use client";

import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import { Box, Button, Chip, Dialog, DialogContent, LinearProgress, Stack, Typography } from "@mui/material";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { localRequest } from "@/lib/admin-api";
import { buildRuntimeInstallPayload, formatRuntimeBytes, requiredRuntimeComponents } from "@/lib/admin-runtime";

type RequiredRuntimeInstallerProps = {
	agentBase: string;
	onboardingConfig: any;
	notify: (message: string, severity?: "success" | "error" | "warning" | "info") => void;
};

/** RequiredRuntimeInstaller 在 Node 或浏览器组件缺失时阻止继续使用后台。 */
export default function RequiredRuntimeInstaller({ agentBase, onboardingConfig, notify }: RequiredRuntimeInstallerProps) {
	const [runtime, setRuntime] = useState<any>({});
	const [installing, setInstalling] = useState(false);
	const [error, setError] = useState("");
	const timerRef = useRef<number | null>(null);
	const components = useMemo(() => requiredRuntimeComponents(runtime), [runtime]);
	const progress = runtime?.install_progress || {};
	const visible = Boolean(agentBase && components.some((item) => !item.installed));
	const running = Boolean(installing || progress?.running);
	const percent = clampPercent(progress?.percent);
	const progressBytes = progress?.received ? progress.total > 0 ? `${formatRuntimeBytes(Number(progress.received))} / ${formatRuntimeBytes(Number(progress.total))}` : `已下载 ${formatRuntimeBytes(Number(progress.received))}` : "";

	/** loadStatus 读取本地运行组件状态。 */
	const loadStatus = useCallback(async () => {
		if (!agentBase) {
			setRuntime({});
			return;
		}
		try {
			setRuntime(await localRequest(agentBase, "/api/v1/runtime/status") || {});
		} catch (loadError) {
			setError(loadError instanceof Error ? loadError.message : "运行组件状态读取失败");
		}
	}, [agentBase]);

	/** installRuntime 触发必要运行组件安装。 */
	async function installRuntime() {
		if (!agentBase || running) return;
		setInstalling(true);
		setError("");
		startPolling();
		try {
			await localRequest(agentBase, "/api/v1/runtime/install", { method: "POST", body: buildRuntimeInstallPayload(onboardingConfig) });
			await loadStatus();
			notify("必要组件安装完成，可以继续搬砖了", "success");
		} catch (installError) {
			setError(installError instanceof Error ? installError.message : "必要组件安装失败");
		} finally {
			setInstalling(false);
		}
	}

	/** startPolling 开始轮询安装进度。 */
	function startPolling() {
		if (timerRef.current != null) return;
		void loadStatus();
		timerRef.current = window.setInterval(() => void loadStatus(), 1000);
	}

	/** stopPolling 停止轮询安装进度。 */
	function stopPolling() {
		if (timerRef.current == null) return;
		window.clearInterval(timerRef.current);
		timerRef.current = null;
	}

	useEffect(() => { void loadStatus(); }, [loadStatus]);
	useEffect(() => {
		if (visible || running) startPolling();
		else stopPolling();
		return stopPolling;
	}, [visible, running, loadStatus]);
	useEffect(() => {
		if (progress?.stage === "failed") setError(String(progress?.message || "必要组件安装失败"));
	}, [progress?.stage, progress?.message]);

	return <Dialog open={visible} fullWidth maxWidth="sm" onClose={() => undefined}><DialogContent sx={{ p: { xs: 2.5, sm: 3 } }}><Stack direction="row" spacing={1.5} sx={{ alignItems: "center", justifyContent: "space-between" }}><Box><Typography component="h2" sx={{ fontSize: 22, fontWeight: 800 }}>安装必要组件</Typography><Typography sx={{ mt: 0.75, color: "text.secondary", lineHeight: 1.7 }}>本地程序需要这些组件才能控制浏览器。没装好前，我先拦一下，免得任务跑到半路掉链子。</Typography></Box><Chip color={running ? "warning" : "error"} label={running ? "安装中" : "必须完成"} /></Stack><Stack spacing={1.25} sx={{ mt: 2.5 }}>{components.map((item) => <Stack key={item.key} direction="row" sx={{ alignItems: "center", justifyContent: "space-between", p: 1.25, border: "1px solid", borderColor: "divider", borderRadius: "8px", bgcolor: item.installed ? "#f0f7f2" : "#fff8ed" }}><Typography sx={{ fontWeight: 700 }}>{item.name}</Typography><Chip size="small" color={item.installed ? "success" : "warning"} label={item.installed ? "已可用" : "未安装"} /></Stack>)}</Stack>{running || progress?.message ? <Box sx={{ mt: 2.5, p: 1.5, border: "1px solid", borderColor: "divider", borderRadius: "8px", bgcolor: "#f7faf8" }}><Stack direction="row" sx={{ alignItems: "center", justifyContent: "space-between", mb: 1 }}><Typography sx={{ fontWeight: 760 }}>{runtimeProgressTitle(progress?.stage)}</Typography><Typography sx={{ color: "text.secondary", fontSize: 13 }}>{percent}%</Typography></Stack><LinearProgress variant="determinate" value={percent} sx={{ height: 8, borderRadius: 999 }} /><Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}>{progress?.message || "正在准备安装"}</Typography>{progressBytes ? <Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 12 }}>{progressBytes}</Typography> : null}</Box> : null}{error ? <Typography sx={{ mt: 2, color: "error.main", fontSize: 13 }}>{error}</Typography> : null}<Button fullWidth variant="contained" size="large" startIcon={<DownloadRoundedIcon />} disabled={running} onClick={() => void installRuntime()} sx={{ mt: 2.5 }}>{running ? "正在安装..." : "安装必要组件"}</Button></DialogContent></Dialog>;
}

/** clampPercent 将进度限制在 0 到 100。 */
function clampPercent(value: unknown) {
	const parsed = Number(value || 0);
	return Number.isFinite(parsed) ? Math.max(0, Math.min(100, Math.round(parsed))) : 0;
}

/** runtimeProgressTitle 返回安装阶段中文名。 */
function runtimeProgressTitle(stage: unknown) {
	const names: Record<string, string> = { queued: "准备安装", download: "正在下载", verify: "正在校验", extract: "正在解压", installed: "安装完成", skipped: "已跳过", failed: "失败", idle: "空闲" };
	return names[String(stage || "")] || "安装进度";
}
