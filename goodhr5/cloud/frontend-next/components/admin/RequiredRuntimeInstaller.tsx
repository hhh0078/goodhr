/** 本文件负责在必要运行组件缺失时展示强制安装弹框。 */
"use client";
import {
  AdminOverlayModal as Modal,
  useAdminOverlay,
} from "@/components/admin/AdminOverlay";

import { DownloadOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Flex, Progress, Tag, Typography } from "antd";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cloudRequest, localRequest } from "@/lib/admin-api";
import { subscribeOnboardingEvents } from "@/lib/onboarding-events";
import {
  buildRuntimeInstallPayload,
  formatRuntimeBytes,
  hasMissingDownloadableRuntime,
  missingRequiredRuntimeURLs,
  requiredRuntimeComponents,
  waitForRuntimeInstall,
} from "@/lib/admin-runtime";
import { reportUserFlow } from "@/lib/user-flow";

type RequiredRuntimeInstallerProps = {
  /** 首次邀请和其他必要操作先处理，暂缓弹框但不跳过组件检查。 */
  deferred?: boolean;
  agentBase: string;
  onboardingConfig: any;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
};

/** RequiredRuntimeInstaller 在 Node 或浏览器组件缺失时阻止继续使用后台。 */
export default function RequiredRuntimeInstaller({
  agentBase,
  onboardingConfig,
  notify,
  deferred = false,
}: RequiredRuntimeInstallerProps) {
  const [runtime, setRuntime] = useState<any>({});
  const [loadedAgentBase, setLoadedAgentBase] = useState("");
  const [installing, setInstalling] = useState(false);
  const [checkingSystem, setCheckingSystem] = useState(false);
  const [error, setError] = useState("");
  const timerRef = useRef<number | null>(null);
  const runtimeReadEpochRef = useRef(0);
  const components = useMemo(
    () => requiredRuntimeComponents(runtime),
    [runtime],
  );
  const progress = runtime?.install_progress || {};
  const systemRuntime = runtime?.system_runtime || {};
  const systemRuntimeMissing = Boolean(
    systemRuntime?.required && !systemRuntime?.ready,
  );
  const missingDownloadable = hasMissingDownloadableRuntime(runtime);
  const visible = Boolean(
    !deferred && agentBase &&
      loadedAgentBase === agentBase &&
      components.some((item) => !item.installed),
  );
  const running = Boolean(installing || progress?.running);
  useAdminOverlay(visible || (!deferred && (running || checkingSystem)));
  const percent = clampPercent(progress?.percent);
  const progressBytes = progress?.received
    ? progress.total > 0
      ? `${formatRuntimeBytes(Number(progress.received))} / ${formatRuntimeBytes(Number(progress.total))}`
      : `已下载 ${formatRuntimeBytes(Number(progress.received))}`
    : "";
  const systemDownloadURL = String(systemRuntime?.download_url || "");

  /** loadStatus 读取本地运行组件状态。 */
  const loadStatus = useCallback(async () => {
    const epoch = runtimeReadEpochRef.current;
    if (!agentBase) {
      setRuntime({});
      setLoadedAgentBase("");
      return;
    }
    try {
      const status =
        (await localRequest(agentBase, "/api/v1/runtime/status")) || {};
      if (epoch !== runtimeReadEpochRef.current) return;
      setRuntime(status);
      setLoadedAgentBase(agentBase);
    } catch (loadError) {
      if (epoch !== runtimeReadEpochRef.current) return;
      setError(
        loadError instanceof Error ? loadError.message : "运行组件状态读取失败",
      );
    }
  }, [agentBase]);

  /** installRuntime 触发必要运行组件安装。 */
  async function installRuntime() {
    if (!agentBase || running) return;
    setInstalling(true);
    setError("");
    startPolling();
    try {
      let config = onboardingConfig;
      let missing = missingRequiredRuntimeURLs(config, runtime?.platform);
      if (missing.length) {
        const fresh = await cloudRequest("/api/runtime/config");
        config = fresh.config || {};
        missing = missingRequiredRuntimeURLs(config, runtime?.platform);
      }
      if (missing.length) {
        throw new Error(
          `运行组件下载地址没拿到：${missing.join("、")}。我重新拉了一次还是空，请检查系统配置。`,
        );
      }
      await localRequest(agentBase, "/api/v1/runtime/install", {
        method: "POST",
        body: buildRuntimeInstallPayload(config),
      });
      const finalStatus = await waitForRuntimeInstall(
        () => localRequest(agentBase, "/api/v1/runtime/status"),
        (status) => setRuntime(status),
      );
      setRuntime(finalStatus);
      const finalSystem =
        finalStatus?.system_runtime &&
        typeof finalStatus.system_runtime === "object"
          ? (finalStatus.system_runtime as Record<string, unknown>)
          : {};
      if (finalSystem?.required && !finalSystem?.ready) {
        const message = String(
          finalSystem?.message || "Windows 运行库还没准备好",
        );
        setError(message);
        await reportUserFlow({
          step: "runtime_ready",
          status: "blocked",
          reason_code: "system_runtime_missing",
          message,
          source: "runtime_installer",
        });
        notify("组件装好了，还差 Windows 运行库", "warning");
      } else {
        await reportUserFlow({
          step: "runtime_ready",
          source: "runtime_installer",
        });
        notify("必要组件安装完成，可以继续搬砖了", "success");
      }
    } catch (installError) {
      const message =
        installError instanceof Error
          ? installError.message
          : "必要组件安装失败";
      setError(message);
      await reportUserFlow({
        step: "runtime_ready",
        status: "blocked",
        reason_code: "runtime_install_failed",
        message,
        source: "runtime_installer",
      });
    } finally {
      setInstalling(false);
    }
  }

  /** refreshSystemRuntime 在用户安装或修复运行库后重新执行本机检查。 */
  async function refreshSystemRuntime() {
    if (!agentBase || checkingSystem) return;
    setCheckingSystem(true);
    setError("");
    try {
      const status =
        (await localRequest(agentBase, "/api/v1/runtime/system-check", {
          method: "POST",
          body: {},
        })) || {};
      setRuntime(status);
      const checked = status?.system_runtime || {};
      if (checked?.ready)
        notify("Windows 运行库已经能用了，继续搬砖", "success");
      else
        setError(
          String(checked?.message || "运行库还没准备好，请安装或修复后再检查"),
        );
    } catch (checkError) {
      setError(
        checkError instanceof Error
          ? checkError.message
          : "运行库重新检查没有完成",
      );
    } finally {
      setCheckingSystem(false);
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

  useEffect(() => {
    void loadStatus();
    return () => { runtimeReadEpochRef.current += 1; };
  }, [loadStatus, deferred]);
  useEffect(() => subscribeOnboardingEvents((event) => {
    if (event.type !== "runtime-status-changed" || !agentBase || event.agentBase !== agentBase || !event.runtimeStatus) return;
    // 教程已有检测读到的新事实优先，旧请求不能把组件缺失重新覆盖成“已就绪”。
    runtimeReadEpochRef.current += 1;
    setRuntime(event.runtimeStatus);
    setLoadedAgentBase(agentBase);
  }), [agentBase]);
  useEffect(() => {
    if (visible || running) startPolling();
    else stopPolling();
    return stopPolling;
  }, [visible, running, loadStatus]);
  useEffect(() => {
    if (progress?.stage === "failed")
      setError(String(progress?.message || "必要组件安装失败"));
  }, [progress?.stage, progress?.message]);

  return (
    <Modal
      open={visible}
      title="准备必要组件"
      width={600}
      closable={false}
      mask={{ closable: false }}
      keyboard={false}
      footer={null}
      styles={{
        body: { maxHeight: "calc(100dvh - 160px)", overflowY: "auto" },
      }}
    >
      <Typography.Paragraph type="secondary">
        本地程序需要这些组件才能控制浏览器。没装好前，我先拦一下，免得岗位运行跑到半路掉链子。
      </Typography.Paragraph>
      <Tag color={running || checkingSystem ? "warning" : "error"}>
        {running ? "安装中" : checkingSystem ? "检查中" : "必须完成"}
      </Tag>
      <Flex vertical gap={10} style={{ marginBlock: 20 }}>
        {components.map((item) => (
          <Card key={item.key} size="small">
            <Flex align="center" justify="space-between" gap={8}>
              <Typography.Text strong>{item.name}</Typography.Text>
              <Tag color={item.installed ? "success" : "warning"}>
                {item.installed
                  ? "已可用"
                  : item.key === "system_runtime"
                    ? "需安装或修复"
                    : "未安装"}
              </Tag>
            </Flex>
          </Card>
        ))}
      </Flex>
      {systemRuntimeMissing ? (
        <div data-onboarding="runtime-system-actions">
        <Alert
          type="warning"
          title={String(systemRuntime?.message || "Windows 运行库还没准备好")}
          description={
            <>
            {!systemDownloadURL ? <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>当前没拿到运行库下载地址。可以先到微软官网下载 Microsoft Visual C++ x64 运行库，或联系作者确认安装包；安装或修复后回来重新检查。</Typography.Paragraph> : <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>下载后打开安装包，选择安装；已经装过就选择修复，然后回来重新检查。</Typography.Paragraph>}
            <Flex gap={8} wrap style={{ marginTop: 12 }}>
              {systemDownloadURL ? (
                <Button
                  href={systemDownloadURL}
                  target="_blank"
                  rel="noreferrer"
                  type="primary"
                  icon={<DownloadOutlined />}
                >
                  下载微软运行库
                </Button>
              ) : null}
              <Button
                data-onboarding="runtime-system-check"
                loading={checkingSystem}
                onClick={() => void refreshSystemRuntime()}
              >
                {checkingSystem ? "正在检查..." : "安装或修复后重新检查"}
              </Button>
            </Flex>
            </>
          }
        />
        </div>
      ) : null}
      {running || progress?.message ? (
        <Card size="small" style={{ marginTop: 16 }}>
          <Typography.Text strong>
            {runtimeProgressTitle(progress?.stage)}
          </Typography.Text>
          <Progress percent={percent} />
          <Typography.Paragraph type="secondary">
            {progress?.message || "正在准备安装"}
          </Typography.Paragraph>
          {progressBytes ? (
            <Typography.Text type="secondary">{progressBytes}</Typography.Text>
          ) : null}
        </Card>
      ) : null}
      {error ? (
        <Alert type="error" title={error} showIcon style={{ marginTop: 16 }} />
      ) : null}
      {missingDownloadable ? (
        <Button
          data-onboarding="runtime-install"
          block
          type="primary"
          icon={<DownloadOutlined />}
          loading={running}
          onClick={() => void installRuntime()}
          style={{ marginTop: 20 }}
        >
          {running ? "正在安装..." : "安装必要组件"}
        </Button>
      ) : null}
    </Modal>
  );
}

/** clampPercent 将进度限制在 0 到 100。 */
function clampPercent(value: unknown) {
  const parsed = Number(value || 0);
  return Number.isFinite(parsed)
    ? Math.max(0, Math.min(100, Math.round(parsed)))
    : 0;
}

/** runtimeProgressTitle 返回安装阶段中文名。 */
function runtimeProgressTitle(stage: unknown) {
  const names: Record<string, string> = {
    queued: "准备安装",
    download: "正在下载",
    verify: "正在校验",
    extract: "正在解压",
    installed: "安装完成",
    skipped: "已跳过",
    failed: "失败",
    idle: "空闲",
  };
  return names[String(stage || "")] || "安装进度";
}
