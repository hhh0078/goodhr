/** 本文件负责新版后台系统公告和本地程序强制更新弹框。 */
"use client";

import CampaignRoundedIcon from "@mui/icons-material/CampaignRounded";
import SystemUpdateAltRoundedIcon from "@mui/icons-material/SystemUpdateAltRounded";
import { Alert, Box, Chip, LinearProgress, Stack, Typography } from "@mui/material";
import { useEffect, useMemo, useState } from "react";
import { localRequest } from "@/lib/admin-api";
import AdminDialog from "./AdminDialog";

const dismissedAnnouncementKey = "goodhr5_dismissed_announcements";
const activeUpdateStages = ["queued", "download", "install"];

type AdminSystemDialogsProps = {
  appConfig: any;
  onboardingConfig: any;
  agentBase: string;
  refreshAgent: () => Promise<void>;
};

/** AdminSystemDialogs 管理公告已读状态和本地程序更新流程。 */
export default function AdminSystemDialogs({ appConfig, onboardingConfig, agentBase, refreshAgent }: AdminSystemDialogsProps) {
  const [announcementReady, setAnnouncementReady] = useState(false);
  const [dismissedAnnouncements, setDismissedAnnouncements] = useState<string[]>([]);
  const [sessionDismissedAnnouncements, setSessionDismissedAnnouncements] = useState<string[]>([]);
  const [currentVersion, setCurrentVersion] = useState("");
  const [updateProgress, setUpdateProgress] = useState<any>({});
  const [updateError, setUpdateError] = useState("");
  const release = useMemo(() => latestLocalAgentRelease(onboardingConfig), [onboardingConfig]);
  const updateRunning = Boolean(updateProgress.running || activeUpdateStages.includes(String(updateProgress.stage || "")));
  const versionMismatch = Boolean(agentBase && currentVersion && release.version && isVersionLower(currentVersion, release.version));
  const updateOpen = versionMismatch || updateRunning;

  useEffect(() => {
    setDismissedAnnouncements(readDismissedAnnouncements());
    setAnnouncementReady(true);
  }, []);

  useEffect(() => {
    if (!agentBase) return;
    void loadUpdateState(agentBase, setCurrentVersion, setUpdateProgress);
  }, [agentBase, release.version]);

  useEffect(() => {
    if (!agentBase || !updateRunning) return;
    const timer = window.setInterval(() => {
      void loadUpdateState(agentBase, setCurrentVersion, setUpdateProgress).then(() => refreshAgent()).catch(() => undefined);
    }, 1200);
    return () => window.clearInterval(timer);
  }, [agentBase, refreshAgent, updateRunning]);

  const visibleAnnouncements = useMemo(() => {
    if (!announcementReady || !appConfig?.announcements_enabled) return [];
    const list = Array.isArray(appConfig.announcements) ? appConfig.announcements : [];
    return list.filter((item: any) => {
      const id = String(item?.id || "").trim();
      if (!id || !item?.enabled || !String(item?.content || "").trim()) return false;
      if (sessionDismissedAnnouncements.includes(id)) return false;
      return !(item.once && dismissedAnnouncements.includes(id));
    });
  }, [announcementReady, appConfig, dismissedAnnouncements, sessionDismissedAnnouncements]);

  /** closeAnnouncements 关闭公告，并永久记录一次性公告。 */
  function closeAnnouncements() {
    const ids = visibleAnnouncements.map((item: any) => String(item.id));
    const onceIDs = visibleAnnouncements.filter((item: any) => item.once).map((item: any) => String(item.id));
    const nextDismissed = Array.from(new Set([...dismissedAnnouncements, ...onceIDs]));
    if (onceIDs.length) localStorage.setItem(dismissedAnnouncementKey, JSON.stringify(nextDismissed));
    setDismissedAnnouncements(nextDismissed);
    setSessionDismissedAnnouncements((current) => Array.from(new Set([...current, ...ids])));
  }

  /** startUpdate 请求本地程序下载并启动最新安装包。 */
  async function startUpdate() {
    if (!agentBase) { setUpdateError("本地程序未连接，无法开始更新"); return; }
    if (!release.url) { setUpdateError("当前系统没有配置本地程序更新包下载地址"); return; }
    setUpdateError("");
    try {
      const progress = await localRequest(agentBase, "/api/v1/app-update/start", { method: "POST", body: { url: release.url, target_version: release.version, release_note: release.note } });
      setUpdateProgress(progress || {});
    } catch (error) {
      setUpdateError(error instanceof Error ? error.message : "启动本地程序更新失败");
    }
  }

  const progressPercent = clampPercent(updateProgress.percent);
  return <>
    <AdminDialog open={!updateOpen && visibleAnnouncements.length > 0} title="系统公告" description="请留意 GoodHR 的最新通知和功能变化。" cancelText="我知道了" onClose={closeAnnouncements}>
      <Stack spacing={1.5}>{visibleAnnouncements.map((item: any) => <Box key={item.id} onClick={() => openExternalURL(item.url)} sx={{ p: 2, border: "1px solid", borderColor: "divider", borderRadius: "8px", bgcolor: "#f8faf8", cursor: item.url ? "pointer" : "default" }}><Stack direction="row" spacing={1} sx={{ alignItems: "center", justifyContent: "space-between" }}><Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><CampaignRoundedIcon color="primary" /><Typography sx={{ fontWeight: 780 }}>{item.title || "公告"}</Typography></Stack>{item.created_at ? <Typography sx={{ color: "text.secondary", fontSize: 12 }}>{item.created_at}</Typography> : null}</Stack><Typography sx={{ mt: 1.25, color: "text.secondary", lineHeight: 1.75, whiteSpace: "pre-wrap" }}>{item.content}</Typography></Box>)}</Stack>
    </AdminDialog>

    <AdminDialog open={updateOpen} title="更新本地程序" description="当前版本与后台要求版本不一致，完成更新后才能继续稳定使用。" confirmText={updateRunning ? "正在更新" : "立即更新"} loading={updateRunning} hideClose showCancel={false} confirmDisabled={!agentBase} onClose={() => undefined} onConfirm={() => void startUpdate()}>
      <Stack spacing={2.25}>
        <Stack direction={{ xs: "column", sm: "row" }} spacing={1.5}>
          <VersionBox label="当前版本" value={currentVersion || updateProgress.current_version || "--"} />
          <VersionBox label="要求版本" value={release.version || updateProgress.target_version || "--"} emphasized />
        </Stack>
        {release.note || updateProgress.release_note ? <Box sx={{ p: 2, borderLeft: "3px solid #1e6545", bgcolor: "#f2f7f3" }}><Typography sx={{ fontWeight: 760 }}>本次更新</Typography><Typography sx={{ mt: 0.75, color: "text.secondary", lineHeight: 1.7, whiteSpace: "pre-wrap" }}>{release.note || updateProgress.release_note}</Typography></Box> : null}
        {updateRunning || updateProgress.message ? <Box><Stack direction="row" sx={{ mb: 0.8, justifyContent: "space-between" }}><Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><SystemUpdateAltRoundedIcon color="primary" fontSize="small" /><Typography sx={{ fontWeight: 720 }}>{updateStageName(updateProgress.stage)}</Typography></Stack><Typography sx={{ color: "primary.main", fontWeight: 760 }}>{progressPercent}%</Typography></Stack><LinearProgress variant="determinate" value={progressPercent} sx={{ height: 9, borderRadius: "8px" }} /><Typography sx={{ mt: 0.8, color: "text.secondary", fontSize: 13 }}>{updateProgress.message || "正在准备更新"}</Typography>{formatProgressBytes(updateProgress) ? <Typography sx={{ mt: 0.25, color: "text.secondary", fontSize: 12 }}>{formatProgressBytes(updateProgress)}</Typography> : null}</Box> : null}
        {updateError ? <Alert severity="error">{updateError}</Alert> : null}
        {!updateError && updateProgress.stage === "failed" ? <Alert severity="error">{updateProgress.message || "本地程序更新失败，请重试"}</Alert> : null}
        {updateProgress.stage === "install" ? <Alert severity="info">安装器已经启动，本地程序将自动重启，请稍等后台重新连接。</Alert> : null}
      </Stack>
    </AdminDialog>
  </>;
}

/** openExternalURL 新开页面打开公告链接。 */
function openExternalURL(url?: string) {
  const value = String(url || "").trim();
  if (value) window.open(value, "_blank", "noopener,noreferrer");
}

/** VersionBox 展示本地程序当前版本或要求版本。 */
function VersionBox({ label, value, emphasized = false }: { label: string; value: string; emphasized?: boolean }) {
  return <Box sx={{ flex: 1, p: 2, border: "1px solid", borderColor: emphasized ? "#87aa92" : "divider", borderRadius: "8px", bgcolor: emphasized ? "#edf5ef" : "#fafbfa" }}><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{label}</Typography><Stack direction="row" spacing={1} sx={{ mt: 0.7, alignItems: "center" }}><Typography sx={{ fontSize: 19, fontWeight: 800 }}>{value}</Typography>{emphasized ? <Chip size="small" label="最新版" color="success" /> : null}</Stack></Box>;
}

/** loadUpdateState 读取本地版本和应用更新进度。 */
async function loadUpdateState(agentBase: string, setVersion: (value: string) => void, setProgress: (value: any) => void) {
  const [runtimeResult, progressResult] = await Promise.allSettled([localRequest(agentBase, "/api/v1/runtime/status"), localRequest(agentBase, "/api/v1/app-update/status")]);
  if (runtimeResult.status === "fulfilled") {
    const runtime = runtimeResult.value || {};
    setVersion(String(runtime.version || runtime.agent_version || runtime.runtime?.version || ""));
  }
  if (progressResult.status === "fulfilled") {
    const progress = progressResult.value || {};
    if (progress.current_version) setVersion(String(progress.current_version));
    setProgress(progress);
  }
}

/** latestLocalAgentRelease 读取当前系统对应的最新版本地程序配置。 */
function latestLocalAgentRelease(config: any) {
  const item = Array.isArray(config?.local_agent) ? config.local_agent[0] || {} : {};
  const isWindows = typeof navigator !== "undefined" && navigator.userAgent.toLowerCase().includes("windows");
  return {
    version: String(item.version || "").trim(),
    url: String(isWindows ? item.url_win || item.url_windows || item.url || "" : item.url_mac || item.url_macos || item.url || "").trim(),
    note: String(item.note || item.changelog || item.description || item.release_note || "").trim(),
  };
}

/** isVersionLower 判断当前版本是否低于目标版本。 */
function isVersionLower(current: string, target: string) {
  return compareVersion(target, current) > 0;
}

/** compareVersion 按点分数字比较版本号。 */
function compareVersion(left: string, right: string) {
  const leftParts = parseVersionParts(left);
  const rightParts = parseVersionParts(right);
  const maxLen = Math.max(leftParts.length, rightParts.length);
  for (let index = 0; index < maxLen; index += 1) {
    const leftValue = leftParts[index] || 0;
    const rightValue = rightParts[index] || 0;
    if (leftValue > rightValue) return 1;
    if (leftValue < rightValue) return -1;
  }
  return 0;
}

/** parseVersionParts 将版本号拆成数字片段。 */
function parseVersionParts(value: string) {
  return String(value || "").trim().replace(/^v/i, "").split(".").map((part) => {
    const match = part.trim().match(/^\d+/);
    return match ? Number(match[0]) : 0;
  });
}

/** readDismissedAnnouncements 读取旧版兼容的一次性公告已读记录。 */
function readDismissedAnnouncements() {
  try {
    const value = JSON.parse(localStorage.getItem(dismissedAnnouncementKey) || "[]");
    return Array.isArray(value) ? value.map(String) : [];
  } catch {
    return [];
  }
}

/** clampPercent 将更新进度限制在 0 到 100。 */
function clampPercent(value: unknown) {
  const number = Number(value || 0);
  return Number.isFinite(number) ? Math.max(0, Math.min(100, Math.round(number))) : 0;
}

/** updateStageName 将更新阶段转换为中文。 */
function updateStageName(value: unknown) {
  return ({ idle: "等待更新", queued: "准备下载", download: "正在下载", install: "正在安装", failed: "更新失败" } as Record<string, string>)[String(value || "")] || "本地程序更新";
}

/** formatProgressBytes 格式化更新下载进度字节数。 */
function formatProgressBytes(progress: any) {
  const received = Number(progress?.received || 0);
  const total = Number(progress?.total || 0);
  if (!received && !total) return "";
  return total > 0 ? `${formatBytes(received)} / ${formatBytes(total)}` : `已下载 ${formatBytes(received)}`;
}

/** formatBytes 将字节数转换成易读文本。 */
function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) { value /= 1024; index += 1; }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}
