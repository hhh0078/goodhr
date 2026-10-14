/** 本文件负责新版后台系统公告，以及区分必要升级和可忽略小补丁的本地程序更新弹框。 */
"use client";

import { NotificationOutlined } from "@ant-design/icons";
import { Alert, Card, Flex, Progress, Tag, Typography } from "antd";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { localRequest } from "@/lib/admin-api";
import {
  isMandatoryLocalAgentUpdate,
  isVersionLower,
  latestLocalAgentRelease,
} from "@/lib/position-start-guard";
import AdminDialog from "./AdminDialog";
import WechatFollowDialog from "./WechatFollowDialog";
import type { WechatOfficialStatus } from "@/lib/wechat-official";

const dismissedAnnouncementKey = "goodhr5_dismissed_announcements";
const dismissedOptionalUpdateKey = "goodhr5_dismissed_optional_agent_update";
const activeUpdateStages = ["queued", "download", "install"];
const updatePollIntervalMS = 1200;

type LoadedUpdateState = { version: string; progress?: any };
type UpdateStateRequest = {
  baseURL: string;
  promise: Promise<LoadedUpdateState & { revision: number }>;
};

type AdminSystemDialogsProps = {
  appConfig: any;
  onboardingConfig: any;
  agentBase: string;
  refreshAgent: () => Promise<void>;
  user: any;
  wechatStatus: WechatOfficialStatus | null;
  wechatOpenSignal: number;
  onWechatStatus: (status: WechatOfficialStatus) => void;
  refreshSession: () => Promise<void>;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
  promotionBlocked: boolean;
  tutorialActive?: boolean;
  tutorialAwaitingChoice?: boolean;
  onCriticalUpdateChange?: (open: boolean) => void;
};

/** AdminSystemDialogs 管理公告已读状态和本地程序更新流程。 */
export default function AdminSystemDialogs({
  appConfig,
  onboardingConfig,
  agentBase,
  refreshAgent,
  user,
  wechatStatus,
  wechatOpenSignal,
  onWechatStatus,
  refreshSession,
  notify,
  promotionBlocked,
  tutorialActive = false,
  tutorialAwaitingChoice = false,
  onCriticalUpdateChange,
}: AdminSystemDialogsProps) {
  const [announcementReady, setAnnouncementReady] = useState(false);
  const [dismissedAnnouncements, setDismissedAnnouncements] = useState<
    string[]
  >([]);
  const [sessionDismissedAnnouncements, setSessionDismissedAnnouncements] =
    useState<string[]>([]);
  const [currentVersion, setCurrentVersion] = useState("");
  const [updateStateReady, setUpdateStateReady] = useState(false);
  const [updateProgress, setUpdateProgress] = useState<any>({});
  const [updateError, setUpdateError] = useState("");
  const [updateStarting, setUpdateStarting] = useState(false);
  const [dismissedOptionalUpdate, setDismissedOptionalUpdate] = useState("");
  const mountedRef = useRef(true);
  const updateStartRef = useRef(false);
  const updateRevisionRef = useRef(0);
  const updateStateRequestRef = useRef<UpdateStateRequest | null>(null);
  const release = useMemo(
    () => latestLocalAgentRelease(onboardingConfig),
    [onboardingConfig],
  );
  const updateRunning = Boolean(
    updateStarting ||
      updateProgress.running ||
      activeUpdateStages.includes(String(updateProgress.stage || "")),
  );
  const versionMismatch = Boolean(
    agentBase &&
      currentVersion &&
      release.version &&
      isVersionLower(currentVersion, release.version),
  );
  const mandatoryUpdate =
    versionMismatch &&
    isMandatoryLocalAgentUpdate(currentVersion, release.version);
  const optionalUpdateIdentity = JSON.stringify([
    agentBase,
    currentVersion,
    release.version,
  ]);
  const canIgnoreUpdate = versionMismatch && !mandatoryUpdate && !updateRunning;
  const updateOpen =
    updateRunning ||
    (!tutorialAwaitingChoice && versionMismatch &&
      (mandatoryUpdate ||
        (!tutorialActive && announcementReady &&
          dismissedOptionalUpdate !== optionalUpdateIdentity)));

  useEffect(() => {
    onCriticalUpdateChange?.(Boolean(updateOpen && (mandatoryUpdate || updateRunning)));
    return () => onCriticalUpdateChange?.(false);
  }, [updateOpen, mandatoryUpdate, updateRunning, onCriticalUpdateChange]);

  useEffect(() => {
    mountedRef.current = true;
    setDismissedAnnouncements(readDismissedAnnouncements());
    setDismissedOptionalUpdate(readDismissedOptionalUpdate());
    setAnnouncementReady(true);
    return () => {
      mountedRef.current = false;
    };
  }, []);

  /** readUpdateState 合并同一轮状态读取，切换端口时也等待旧请求结束，避免慢请求叠加。 */
  const readUpdateState = useCallback(async (baseURL: string) => {
    while (updateStateRequestRef.current) {
      const pending = updateStateRequestRef.current;
      const result = await pending.promise;
      if (pending.baseURL === baseURL) return result;
    }
    const revision = updateRevisionRef.current;
    const request: UpdateStateRequest = {
      baseURL,
      promise: loadUpdateState(baseURL).then((state) => ({
        ...state,
        revision,
      })),
    };
    updateStateRequestRef.current = request;
    try {
      return await request.promise;
    } finally {
      if (updateStateRequestRef.current === request)
        updateStateRequestRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (!agentBase && !updateRunning) {
      setUpdateStateReady(true);
      return;
    }
    setUpdateStateReady(false);
    let active = true;
    let timer: number | undefined;
    /** pollUpdateState 串行读取进度；安装断线后继续探测，旧页面和提交前的状态不得回写。 */
    async function pollUpdateState() {
      let retry = updateRunning;
      try {
        if (updateStartRef.current) return;
        if (!agentBase) {
          await refreshAgent();
          return;
        }
        const state = await readUpdateState(agentBase);
        if (
          !active ||
          !mountedRef.current ||
          state.revision !== updateRevisionRef.current
        )
          return;
        const unavailable = !state.version && state.progress === undefined;
        const updateFinished =
          updateRunning &&
          state.progress !== undefined &&
          !state.progress.running &&
          !activeUpdateStages.includes(String(state.progress.stage || ""));
        retry ||= unavailable;
        if (updateFinished && state.progress.stage !== "failed") {
          // 成功更新须等账号绑定和顶栏版本同步完成，再解除旧的运行锁；临时失败由下一轮重试。
          await refreshAgent();
          if (
            !active ||
            !mountedRef.current ||
            state.revision !== updateRevisionRef.current
          )
            return;
        }
        if (state.version) setCurrentVersion(state.version);
        if (state.progress !== undefined) setUpdateProgress(state.progress);
        if (!unavailable) setUpdateStateReady(true);
        if (unavailable) await refreshAgent();
      } catch {
        // 更新中的暂时断线不解除弹框锁定，下一轮继续重连。
      } finally {
        if (active && mountedRef.current && retry)
          timer = window.setTimeout(
            () => void pollUpdateState(),
            updatePollIntervalMS,
          );
      }
    }
    void pollUpdateState();
    return () => {
      active = false;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [
    agentBase,
    readUpdateState,
    refreshAgent,
    release.version,
    updateRunning,
  ]);

  const visibleAnnouncements = useMemo(() => {
    if (!announcementReady || !appConfig?.announcements_enabled) return [];
    const list = Array.isArray(appConfig.announcements)
      ? appConfig.announcements
      : [];
    return list.filter((item: any) => {
      const id = String(item?.id || "").trim();
      if (!id || !item?.enabled || !String(item?.content || "").trim())
        return false;
      if (sessionDismissedAnnouncements.includes(id)) return false;
      return !(item.once && dismissedAnnouncements.includes(id));
    });
  }, [
    announcementReady,
    appConfig,
    dismissedAnnouncements,
    sessionDismissedAnnouncements,
  ]);

  /** closeAnnouncements 关闭公告，并永久记录一次性公告。 */
  function closeAnnouncements() {
    const ids = visibleAnnouncements.map((item: any) => String(item.id));
    const onceIDs = visibleAnnouncements
      .filter((item: any) => item.once)
      .map((item: any) => String(item.id));
    const nextDismissed = Array.from(
      new Set([...dismissedAnnouncements, ...onceIDs]),
    );
    if (onceIDs.length)
      localStorage.setItem(
        dismissedAnnouncementKey,
        JSON.stringify(nextDismissed),
      );
    setDismissedAnnouncements(nextDismissed);
    setSessionDismissedAnnouncements((current) =>
      Array.from(new Set([...current, ...ids])),
    );
  }

  /** ignoreOptionalUpdate 记住本机当前版本忽略的这一小补丁，必要升级及更新进行中不能关闭。 */
  function ignoreOptionalUpdate() {
    if (!canIgnoreUpdate) return;
    setDismissedOptionalUpdate(optionalUpdateIdentity);
    setUpdateError("");
    try {
      localStorage.setItem(dismissedOptionalUpdateKey, optionalUpdateIdentity);
    } catch {
      // 浏览器禁止存储时仍允许本次会话忽略小补丁。
    }
  }

  /** startUpdate 请求本地程序下载并启动最新安装包。 */
  async function startUpdate() {
    if (updateStartRef.current || updateRunning || !mountedRef.current) return;
    if (!agentBase) {
      setUpdateError("本地程序未连接，无法开始更新");
      return;
    }
    if (!release.url) {
      setUpdateError("当前系统没有配置本地程序更新包下载地址");
      return;
    }
    updateStartRef.current = true;
    updateRevisionRef.current += 1;
    setUpdateError("");
    setUpdateStarting(true);
    try {
      const progress = await localRequest(
        agentBase,
        "/api/v1/app-update/start",
        {
          method: "POST",
          body: {
            url: release.url,
            target_version: release.version,
            release_note: release.note,
            sha256: release.sha256,
          },
        },
      );
      if (mountedRef.current) setUpdateProgress(progress || {});
    } catch (error) {
      if (mountedRef.current)
        setUpdateError(
          error instanceof Error ? error.message : "启动本地程序更新失败",
        );
    } finally {
      updateStartRef.current = false;
      if (mountedRef.current) setUpdateStarting(false);
    }
  }

  const progressPercent = clampPercent(updateProgress.percent);
  return (
    <>
      <AdminDialog
        open={!tutorialActive && !promotionBlocked && !updateOpen && visibleAnnouncements.length > 0}
        title="系统公告"
        description="请留意 GoodHR 的最新通知和功能变化。"
        cancelText="我知道了"
        onClose={closeAnnouncements}
      >
        <Flex vertical gap={12}>
          {visibleAnnouncements.map((item: any) => (
            <Card
              key={item.id}
              size="small"
              onClick={() => openExternalURL(item.url)}
              style={{ cursor: item.url ? "pointer" : undefined }}
            >
              <Flex justify="space-between" gap={8} wrap>
                <Typography.Text strong>
                  <NotificationOutlined /> {item.title || "公告"}
                </Typography.Text>
                {item.created_at ? (
                  <Typography.Text type="secondary">
                    {item.created_at}
                  </Typography.Text>
                ) : null}
              </Flex>
              <Typography.Paragraph
                type="secondary"
                style={{ margin: "10px 0 0", whiteSpace: "pre-wrap" }}
              >
                {item.content}
              </Typography.Paragraph>
            </Card>
          ))}
        </Flex>
      </AdminDialog>
      <AdminDialog
        open={updateOpen}
        title="更新本地程序"
        description={
          updateRunning
            ? "本地程序正在更新，请稍等。"
            : mandatoryUpdate
              ? "这版需要更新，完成后就能继续开始岗位任务。"
              : "这次是小修补，晚点更新也行。"
        }
        confirmText="立即更新"
        loadingText="正在更新"
        loading={updateRunning}
        hideClose={!canIgnoreUpdate}
        showCancel={canIgnoreUpdate}
        cancelText="暂时忽略"
        confirmDisabled={!agentBase}
        onClose={ignoreOptionalUpdate}
        onConfirm={() => void startUpdate()}
      >
        <Flex vertical gap={18}>
          <Flex gap={12} wrap>
            <VersionBox
              label="当前版本"
              value={currentVersion || updateProgress.current_version || "--"}
            />
            <VersionBox
              label={mandatoryUpdate ? "要求版本" : "最新版本"}
              value={release.version || updateProgress.target_version || "--"}
              emphasized
            />
          </Flex>
          {release.note || updateProgress.release_note ? (
            <Alert
              type="info"
              title="本次更新"
              description={
                <Typography.Paragraph
                  style={{ whiteSpace: "pre-wrap", marginBottom: 0 }}
                >
                  {release.note || updateProgress.release_note}
                </Typography.Paragraph>
              }
            />
          ) : null}
          {updateRunning || updateProgress.message ? (
            <div>
              <Typography.Text strong>
                {updateStageName(updateProgress.stage)}
              </Typography.Text>
              <Progress
                percent={progressPercent}
                status={
                  updateProgress.stage === "failed" ? "exception" : undefined
                }
              />
              <Typography.Paragraph type="secondary">
                {updateProgress.message || "正在准备更新"}
              </Typography.Paragraph>
              {formatProgressBytes(updateProgress) ? (
                <Typography.Text type="secondary">
                  {formatProgressBytes(updateProgress)}
                </Typography.Text>
              ) : null}
            </div>
          ) : null}
          {updateError ? (
            <Alert type="error" title={updateError} showIcon />
          ) : null}
          {!updateError && updateProgress.stage === "failed" ? (
            <Alert
              type="error"
              title={updateProgress.message || "本地程序更新失败，请重试"}
              showIcon
            />
          ) : null}
          {updateProgress.stage === "install" ? (
            <Alert
              type="info"
              title="安装器已经启动，本地程序将自动重启，请稍等后台重新连接。"
              showIcon
            />
          ) : null}
        </Flex>
      </AdminDialog>
      <WechatFollowDialog
        user={user}
        status={wechatStatus}
        openSignal={wechatOpenSignal}
        onStatus={onWechatStatus}
        refreshSession={refreshSession}
        notify={notify}
        autoAllowed={
          announcementReady &&
          updateStateReady &&
          !updateOpen &&
          visibleAnnouncements.length === 0 &&
          !promotionBlocked
          && !tutorialActive
        }
      />
    </>
  );
}

/** openExternalURL 新开页面打开公告链接。 */
function openExternalURL(url?: string) {
  const value = String(url || "").trim();
  if (value) window.open(value, "_blank", "noopener,noreferrer");
}

/** VersionBox 展示本地程序当前版本或要求版本。 */
function VersionBox({
  label,
  value,
  emphasized = false,
}: {
  label: string;
  value: string;
  emphasized?: boolean;
}) {
  return (
    <Card
      size="small"
      style={{
        flex: "1 1 180px",
        borderColor: emphasized ? "var(--goodhr-primary)" : undefined,
        background: emphasized ? "var(--goodhr-primary-soft)" : undefined,
      }}
    >
      <Typography.Text type="secondary">{label}</Typography.Text>
      <Flex gap={8} align="center">
        <Typography.Title level={4} style={{ margin: "6px 0 0" }}>
          {value}
        </Typography.Title>
        {emphasized ? <Tag color="processing">最新版</Tag> : null}
      </Flex>
    </Card>
  );
}

/** loadUpdateState 只返回本地版本和进度快照，短超时后由串行轮询继续重试。 */
async function loadUpdateState(agentBase: string): Promise<LoadedUpdateState> {
  const [runtimeResult, progressResult] = await Promise.allSettled([
    localRequest(agentBase, "/api/v1/runtime/status", { timeoutMS: 5000 }),
    localRequest(agentBase, "/api/v1/app-update/status", { timeoutMS: 5000 }),
  ]);
  let version = "";
  if (runtimeResult.status === "fulfilled") {
    const runtime = runtimeResult.value || {};
    version = String(
      runtime.version ||
        runtime.agent_version ||
        runtime.runtime?.version ||
        "",
    );
  }
  if (progressResult.status === "fulfilled") {
    const progress = progressResult.value || {};
    if (progress.current_version) version = String(progress.current_version);
    return { version, progress };
  }
  return { version };
}

/** readDismissedAnnouncements 读取旧版兼容的一次性公告已读记录。 */
function readDismissedAnnouncements() {
  try {
    const value = JSON.parse(
      localStorage.getItem(dismissedAnnouncementKey) || "[]",
    );
    return Array.isArray(value) ? value.map(String) : [];
  } catch {
    return [];
  }
}

/** readDismissedOptionalUpdate 读取忽略的小补丁记录，存储不可用时返回空记录。 */
function readDismissedOptionalUpdate() {
  try {
    return localStorage.getItem(dismissedOptionalUpdateKey) || "";
  } catch {
    return "";
  }
}

/** clampPercent 将更新进度限制在 0 到 100。 */
function clampPercent(value: unknown) {
  const number = Number(value || 0);
  return Number.isFinite(number)
    ? Math.max(0, Math.min(100, Math.round(number)))
    : 0;
}

/** updateStageName 将更新阶段转换为中文。 */
function updateStageName(value: unknown) {
  return (
    (
      {
        idle: "等待更新",
        queued: "准备下载",
        download: "正在下载",
        install: "正在安装",
        failed: "更新失败",
      } as Record<string, string>
    )[String(value || "")] || "本地程序更新"
  );
}

/** formatProgressBytes 格式化更新下载进度字节数。 */
function formatProgressBytes(progress: any) {
  const received = Number(progress?.received || 0);
  const total = Number(progress?.total || 0);
  if (!received && !total) return "";
  return total > 0
    ? `${formatBytes(received)} / ${formatBytes(total)}`
    : `已下载 ${formatBytes(received)}`;
}

/** formatBytes 将字节数转换成易读文本。 */
function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}
