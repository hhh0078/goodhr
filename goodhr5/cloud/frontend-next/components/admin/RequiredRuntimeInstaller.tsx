/** 本文件负责在必要运行组件缺失时引导填写 Key，并展示官方浏览器真实安装进度。 */
"use client";

import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import KeyRoundedIcon from "@mui/icons-material/KeyRounded";
import OpenInNewRoundedIcon from "@mui/icons-material/OpenInNewRounded";
import SettingsRoundedIcon from "@mui/icons-material/SettingsRounded";
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogContent,
  LinearProgress,
  Stack,
  Typography,
} from "@mui/material";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cloudRequest, localRequest } from "@/lib/admin-api";
import {
  buildRuntimeInstallPayload,
  formatRuntimeBytes,
  missingRuntimeNodeURL,
  requiredRuntimeComponents,
} from "@/lib/admin-runtime";
import { reportUserFlow } from "@/lib/user-flow";

type RequiredRuntimeInstallerProps = {
  agentBase: string;
  onboardingConfig: any;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
};

/** RequiredRuntimeInstaller 在 Node、包装器或最新版 Chromium 缺失时阻止开始任务。 */
export default function RequiredRuntimeInstaller({
  agentBase,
  onboardingConfig,
  notify,
}: RequiredRuntimeInstallerProps) {
  const pathname = usePathname();
  const router = useRouter();
  const [runtime, setRuntime] = useState<any>({});
  const [statusLoaded, setStatusLoaded] = useState(false);
  const [licenseKey, setLicenseKey] = useState("");
  const [licenseLoaded, setLicenseLoaded] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [error, setError] = useState("");
  const timerRef = useRef<number | null>(null);
  const installStartedRef = useRef(false);
  const terminalRef = useRef("");
  const components = useMemo(
    () => requiredRuntimeComponents(runtime),
    [runtime],
  );
  const progress = runtime?.install_progress || {};
  const missingLicense = licenseLoaded && !licenseKey;
  const missingComponents = components.some((item) => !item.installed);
  const running = Boolean(installing || progress?.running);
  const visible = Boolean(
    agentBase &&
      statusLoaded &&
      (missingComponents || running || progress?.stage === "failed") &&
      (running || !["/admin/personal-config", "/admin/agent-download"].includes(pathname)),
  );
  const percent = clampPercent(progress?.percent);
  const attemptText =
    Number(progress?.attempt) > 0 && Number(progress?.max_attempts) > 1
      ? `第 ${progress.attempt}/${progress.max_attempts} 次尝试`
      : "";
  const displayError =
    error ||
    (progress?.stage === "failed"
      ? String(progress?.detail || progress?.message || "必要组件安装失败")
      : "");
  const progressBytes = progress?.received
    ? progress.total > 0
      ? `${formatRuntimeBytes(Number(progress.received))} / ${formatRuntimeBytes(Number(progress.total))}`
      : `已下载 ${formatRuntimeBytes(Number(progress.received))}`
    : "";

  /** loadStatus 读取本地运行组件和当前安装进度。 */
  const loadStatus = useCallback(async () => {
    if (!agentBase) {
      setRuntime({});
      return;
    }
    try {
      const status = (await localRequest(agentBase, "/api/v1/runtime/status")) || {};
      setRuntime(status);
      setStatusLoaded(true);
      if (status.install_progress?.running) installStartedRef.current = true;
    } catch (loadError) {
      setError(
        loadError instanceof Error
          ? loadError.message
          : "运行组件状态读取失败",
      );
    }
  }, [agentBase]);

  /** loadLicense 读取云端个人配置中的 Key，不把明文写进本地页面日志。 */
  const loadLicense = useCallback(async () => {
    try {
      const data = await cloudRequest("/api/config/user-preferences");
      setLicenseKey(String(data?.config?.cloakbrowser_license_key || "").trim());
    } catch (loadError) {
      setError(
        loadError instanceof Error
          ? loadError.message
          : "CloakBrowser Key 读取失败",
      );
    } finally {
      setLicenseLoaded(true);
    }
  }, []);

  /** installRuntime 提交安装任务；最终成功与失败由状态轮询确认。 */
  async function installRuntime() {
    if (!agentBase || running) return;
    if (!licenseKey) {
      router.push("/admin/personal-config");
      return;
    }
    setInstalling(true);
    setError("");
    installStartedRef.current = true;
    terminalRef.current = "";
    startPolling();
    try {
      let config = onboardingConfig;
      if (missingRuntimeNodeURL(config, runtime?.platform)) {
        const fresh = await cloudRequest("/api/runtime/config");
        config = fresh.config || {};
      }
      if (
        !runtime?.node_installed &&
        missingRuntimeNodeURL(config, runtime?.platform)
      ) {
        throw new Error(
          "Node 下载地址没拿到。我重新拉了一次还是空，请检查系统配置。",
        );
      }
      const result = await localRequest(
        agentBase,
        "/api/v1/runtime/install",
        {
          method: "POST",
          body: {
            ...buildRuntimeInstallPayload(config),
            license_key: licenseKey,
          },
          timeoutMS: 90_000,
        },
      );
      setRuntime(result || {});
    } catch (installError) {
      installStartedRef.current = false;
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

  /** startPolling 开始每秒读取一次真实安装状态。 */
  function startPolling() {
    if (timerRef.current != null) return;
    void loadStatus();
    timerRef.current = window.setInterval(() => void loadStatus(), 1000);
  }

  /** stopPolling 停止安装状态轮询。 */
  function stopPolling() {
    if (timerRef.current == null) return;
    window.clearInterval(timerRef.current);
    timerRef.current = null;
  }

  useEffect(() => {
    void loadStatus();
  }, [loadStatus]);
  useEffect(() => {
    void loadLicense();
  }, [loadLicense, pathname]);
  useEffect(() => {
    if (agentBase) startPolling();
    else stopPolling();
    return stopPolling;
  }, [agentBase, loadStatus]);
  useEffect(() => {
    if (installing || !installStartedRef.current) return;
    const terminalKey = `${progress?.stage || ""}:${progress?.updated_at || ""}`;
    if (!terminalKey || terminalRef.current === terminalKey) return;
    if (progress?.stage === "failed") {
      terminalRef.current = terminalKey;
      installStartedRef.current = false;
      const message = String(
        progress?.detail || progress?.message || "必要组件安装失败",
      );
      setError(message);
      void reportUserFlow({
        step: "runtime_ready",
        status: "blocked",
        reason_code: "runtime_install_failed",
        message,
        source: "runtime_installer",
      });
      return;
    }
    if (
      progress?.stage === "installed" &&
      !progress?.running &&
      Number(progress?.percent) >= 100
    ) {
      terminalRef.current = terminalKey;
      installStartedRef.current = false;
      setError("");
      void reportUserFlow({
        step: "runtime_ready",
        source: "runtime_installer",
      });
      notify("必要组件真的装好了，可以继续搬砖了", "success");
      void loadStatus();
    }
  }, [progress, installing, loadStatus, notify]);

  return (
    <Dialog
      open={visible}
      fullWidth
      maxWidth='sm'
      onClose={() => undefined}
    >
      <DialogContent sx={{ p: { xs: 2.5, sm: 3 } }}>
        <Stack
          direction='row'
          spacing={1.5}
          sx={{ alignItems: "center", justifyContent: "space-between" }}
        >
          <Box>
            <Typography component='h2' sx={{ fontSize: 22, fontWeight: 800 }}>
              安装必要组件
            </Typography>
            <Typography
              sx={{ mt: 0.75, color: "text.secondary", lineHeight: 1.7 }}
            >
              Node 走国内下载，最新版 Chromium 由你的 Key
              从 CloakBrowser 官方下载。安装时有进度，平时启动不会偷偷下大文件。
            </Typography>
          </Box>
          <Chip
            color={running ? "warning" : "error"}
            label={running ? "安装中" : "必须完成"}
          />
        </Stack>

        <Stack spacing={1.25} sx={{ mt: 2.5 }}>
          {components.map((item) => (
            <Stack
              key={item.key}
              direction='row'
              sx={{
                alignItems: "center",
                justifyContent: "space-between",
                p: 1.25,
                border: "1px solid",
                borderColor: "divider",
                borderRadius: "8px",
                bgcolor: item.installed ? "success.light" : "#fff8ed",
              }}
            >
              <Typography sx={{ fontWeight: 700 }}>{item.name}</Typography>
              <Chip
                size='small'
                color={item.installed ? "success" : "warning"}
                label={item.installed ? "已可用" : "未安装"}
              />
            </Stack>
          ))}
        </Stack>

        {missingLicense ? (
          <Alert severity='warning' icon={<KeyRoundedIcon />} sx={{ mt: 2 }}>
            <Typography sx={{ fontWeight: 760 }}>
              还差一个 CloakBrowser Key
            </Typography>
            <Typography sx={{ mt: 0.5, fontSize: 13 }}>
              用自己的 GitHub 登录免费获取，Key 会发到 GitHub 绑定邮箱。免费版支持
              1 个浏览器会话。
            </Typography>
            <Stack
              direction={{ xs: "column", sm: "row" }}
              spacing={1}
              sx={{ mt: 1.5 }}
            >
              <Button
                size='small'
                variant='contained'
                startIcon={<SettingsRoundedIcon />}
                onClick={() => router.push("/admin/personal-config")}
              >
                去填写 Key
              </Button>
              <Button
                size='small'
                component='a'
                href='https://cloakbrowser.dev/free/'
                target='_blank'
                rel='noreferrer'
                startIcon={<OpenInNewRoundedIcon />}
              >
                免费获取 Key
              </Button>
            </Stack>
          </Alert>
        ) : null}

        {running || progress?.message ? (
          <Box
            sx={{
              mt: 2.5,
              p: 1.5,
              border: "1px solid",
              borderColor: "divider",
              borderRadius: "8px",
              bgcolor: "action.hover",
            }}
          >
            <Stack
              direction='row'
              sx={{
                alignItems: "center",
                justifyContent: "space-between",
                mb: 1,
              }}
            >
              <Typography sx={{ fontWeight: 760 }}>
                {runtimeProgressTitle(progress?.stage)}
              </Typography>
              <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
                {attemptText ? `${attemptText} · ` : ""}
                {percent}%
              </Typography>
            </Stack>
            <LinearProgress
              variant='determinate'
              value={percent}
              sx={{ height: 8, borderRadius: 999 }}
            />
            <Typography
              sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}
            >
              {progress?.message || "正在准备安装"}
            </Typography>
            {progressBytes ? (
              <Typography
                sx={{ mt: 0.5, color: "text.secondary", fontSize: 12 }}
              >
                {progressBytes}
              </Typography>
            ) : null}
          </Box>
        ) : null}

        {displayError ? (
          <Alert severity='error' sx={{ mt: 2, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
            {displayError}
          </Alert>
        ) : null}

        <Button
          fullWidth
          variant='contained'
          size='large'
          startIcon={<DownloadRoundedIcon />}
          disabled={running || missingLicense || !licenseLoaded}
          onClick={() => void installRuntime()}
          sx={{ mt: 2.5 }}
        >
          {running
            ? "正在安装..."
            : displayError
              ? "重试安装"
              : "安装必要组件"}
        </Button>
        {!running ? (
          <Stack direction='row' spacing={1} sx={{ mt: 1 }}>
            <Button startIcon={<SettingsRoundedIcon />} onClick={() => router.push("/admin/personal-config")}>
              修改 Key
            </Button>
            <Button onClick={() => router.push("/admin/agent-download")}>
              管理组件
            </Button>
          </Stack>
        ) : null}
      </DialogContent>
    </Dialog>
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
    install_dependency: "安装控制组件",
    validate_license: "校验 Key",
    resolve_version: "确认最新版",
    smoke_test: "启动检查",
    installed: "安装完成",
    skipped: "已跳过",
    failed: "安装失败",
    idle: "等待安装",
  };
  return names[String(stage || "")] || "安装进度";
}
