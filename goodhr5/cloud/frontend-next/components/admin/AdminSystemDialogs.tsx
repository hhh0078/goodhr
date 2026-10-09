/** 本文件负责新版后台系统公告，以及区分必要升级和可忽略小补丁的本地程序更新弹框。 */
"use client";

import { NotificationOutlined } from "@ant-design/icons";
import { Alert, Card, Flex, Progress, Tag, Typography } from "antd";
import { useEffect, useMemo, useState } from "react";
import { localRequest } from "@/lib/admin-api";
import {
  isMandatoryLocalAgentUpdate,
  isVersionLower,
  latestLocalAgentRelease,
} from "@/lib/position-start-guard";
import AdminDialog from "./AdminDialog";

const dismissedAnnouncementKey = "goodhr5_dismissed_announcements";
const dismissedOptionalUpdateKey = "goodhr5_dismissed_optional_agent_update";
const activeUpdateStages = ["queued", "download", "install"];

type AdminSystemDialogsProps = {
  appConfig: any;
  onboardingConfig: any;
  agentBase: string;
  refreshAgent: () => Promise<void>;
};

/** AdminSystemDialogs 管理公告已读状态和本地程序更新流程。 */
export default function AdminSystemDialogs({
  appConfig,
  onboardingConfig,
  agentBase,
  refreshAgent,
}: AdminSystemDialogsProps) {
  const [announcementReady, setAnnouncementReady] = useState(false);
  const [dismissedAnnouncements, setDismissedAnnouncements] = useState<
    string[]
  >([]);
  const [sessionDismissedAnnouncements, setSessionDismissedAnnouncements] =
    useState<string[]>([]);
  const [currentVersion, setCurrentVersion] = useState("");
  const [updateProgress, setUpdateProgress] = useState<any>({});
  const [updateError, setUpdateError] = useState("");
  const [updateStarting, setUpdateStarting] = useState(false);
  const [dismissedOptionalUpdate, setDismissedOptionalUpdate] = useState("");
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
    (versionMismatch &&
      (mandatoryUpdate ||
        (announcementReady &&
          dismissedOptionalUpdate !== optionalUpdateIdentity)));

  useEffect(() => {
    setDismissedAnnouncements(readDismissedAnnouncements());
    setDismissedOptionalUpdate(readDismissedOptionalUpdate());
    setAnnouncementReady(true);
  }, []);

  useEffect(() => {
    if (!agentBase) return;
    void loadUpdateState(agentBase, setCurrentVersion, setUpdateProgress);
  }, [agentBase, release.version]);

  useEffect(() => {
    if (!agentBase || !updateRunning) return;
    const timer = window.setInterval(() => {
      void loadUpdateState(agentBase, setCurrentVersion, setUpdateProgress)
        .then(() => refreshAgent())
        .catch(() => undefined);
    }, 1200);
    return () => window.clearInterval(timer);
  }, [agentBase, refreshAgent, updateRunning]);

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
    if (!agentBase) {
      setUpdateError("本地程序未连接，无法开始更新");
      return;
    }
    if (!release.url) {
      setUpdateError("当前系统没有配置本地程序更新包下载地址");
      return;
    }
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
      setUpdateProgress(progress || {});
    } catch (error) {
      setUpdateError(
        error instanceof Error ? error.message : "启动本地程序更新失败",
      );
    } finally {
      setUpdateStarting(false);
    }
  }

  const progressPercent = clampPercent(updateProgress.percent);
  return (
    <>
      <AdminDialog
        open={!updateOpen && visibleAnnouncements.length > 0}
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

/** loadUpdateState 读取本地版本和应用更新进度。 */
async function loadUpdateState(
  agentBase: string,
  setVersion: (value: string) => void,
  setProgress: (value: any) => void,
) {
  const [runtimeResult, progressResult] = await Promise.allSettled([
    localRequest(agentBase, "/api/v1/runtime/status"),
    localRequest(agentBase, "/api/v1/app-update/status"),
  ]);
  if (runtimeResult.status === "fulfilled") {
    const runtime = runtimeResult.value || {};
    setVersion(
      String(
        runtime.version ||
          runtime.agent_version ||
          runtime.runtime?.version ||
          "",
      ),
    );
  }
  if (progressResult.status === "fulfilled") {
    const progress = progressResult.value || {};
    if (progress.current_version) setVersion(String(progress.current_version));
    setProgress(progress);
  }
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
