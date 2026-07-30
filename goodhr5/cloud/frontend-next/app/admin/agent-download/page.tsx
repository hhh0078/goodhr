/** 本文件负责展示本地运行组件、扩展安装说明和组件更新入口。 */
"use client";

import ExtensionRoundedIcon from "@mui/icons-material/ExtensionRounded";
import FolderOpenRoundedIcon from "@mui/icons-material/FolderOpenRounded";
import SystemUpdateAltRoundedIcon from "@mui/icons-material/SystemUpdateAltRounded";
import {
  Box,
  Button,
  Chip,
  LinearProgress,
  Stack,
  Typography,
} from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, localRequest } from "@/lib/admin-api";
import {
  buildRuntimeInstallPayload,
  missingRequiredWinRuntimeURLs,
} from "@/lib/admin-runtime";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

type UnknownRecord = Record<string, unknown>;

type RuntimeComponentView = {
  key: string;
  name: string;
  required: boolean;
  bundled: boolean;
  installed: boolean;
  configVersion: string;
  installedVersion: string;
  url: string;
  note: string;
  path: string;
};

const componentNames: Record<string, string> = {
  node_runtime: "Node 运行环境",
  node_worker: "浏览器控制 Worker",
  cloakbrowser: "CloakBrowser 浏览器",
  ocr: "OCR 组件",
};

/** AgentDownloadPage 展示组件状态、扩展安装方法并触发运行组件更新。 */
export default function AgentDownloadPage() {
  const { agentBase, onboardingConfig, refreshAgent, notify } = useAdmin();
  const [runtime, setRuntime] = useState<UnknownRecord>({});
  const [loading, setLoading] = useState(false);
  const [openingExtensions, setOpeningExtensions] = useState(false);

  /** load 读取本地运行状态和云端组件配置。 */
  async function load() {
    if (!agentBase) return;
    setLoading(true);
    try {
      const result: unknown = await localRequest(
        agentBase,
        "/api/v1/runtime/status",
      );
      setRuntime(asRecord(result));
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "组件信息读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, [agentBase]);

  /** updateRuntime 下载并安装缺失或版本不符的运行组件。 */
  async function updateRuntime() {
    if (!agentBase) {
      notify("本地程序未连接", "error");
      return;
    }
    setLoading(true);
    try {
      let config: unknown = onboardingConfig;
      let missing = missingRequiredWinRuntimeURLs(config);
      if (missing.length) {
        const fresh = asRecord(await cloudRequest("/api/runtime/config"));
        config = fresh.config || {};
        missing = missingRequiredWinRuntimeURLs(config);
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
      notify("组件更新岗位运行已完成", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "组件更新失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** openExtensionsDirectory 请求本地程序打开固定的浏览器扩展目录。 */
  async function openExtensionsDirectory() {
    if (!agentBase) {
      notify("本地程序还没连上，我暂时打不开扩展目录", "error");
      return;
    }
    setOpeningExtensions(true);
    try {
      await localRequest(agentBase, "/api/v1/extensions/open-directory", {
        method: "POST",
      });
      notify("扩展目录已经打开，公主请放文件", "success");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "扩展目录没打开成功，我们再试一次",
        "error",
      );
    } finally {
      setOpeningExtensions(false);
    }
  }

  const components = buildComponents(runtime, onboardingConfig);
  const extensionsDirectory = textValue(runtime.extensions_dir);

  return (
    <>
      <PageHeader
        title="组件信息"
        description="查看本机运行组件、安装状态、版本和浏览器扩展说明。"
        actions={
          <>
            <RefreshButton
              loading={loading}
              onClick={() => void refreshAgent().then(load)}
            />
            <Button
              variant="contained"
              startIcon={<SystemUpdateAltRoundedIcon />}
              disabled={loading || !agentBase}
              onClick={() => void updateRuntime()}
            >
              更新运行组件
            </Button>
          </>
        }
      />
      {loading ? <LinearProgress sx={{ mb: 2 }} /> : null}
      {!agentBase ? (
        <SectionPanel>
          <EmptyState text="本地程序未连接" />
        </SectionPanel>
      ) : (
        <>
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: {
                xs: "1fr 1fr",
                md: "repeat(4, 1fr)",
              },
              gap: 2,
              mb: 2,
            }}
          >
            <SectionPanel>
              <Typography color="text.secondary" sx={{ fontSize: 12 }}>
                本地连接
              </Typography>
              <Typography
                sx={{ mt: 1, color: "primary.main", fontWeight: 760 }}
              >
                已连接
              </Typography>
            </SectionPanel>
            <SectionPanel>
              <Typography color="text.secondary" sx={{ fontSize: 12 }}>
                监听地址
              </Typography>
              <Typography sx={{ mt: 1, fontWeight: 760 }}>
                {agentBase}
              </Typography>
            </SectionPanel>
            <SectionPanel>
              <Typography color="text.secondary" sx={{ fontSize: 12 }}>
                程序版本
              </Typography>
              <Typography sx={{ mt: 1, fontWeight: 760 }}>
                {textValue(runtime.version) ||
                  textValue(runtime.agent_version) ||
                  "--"}
              </Typography>
            </SectionPanel>
            <SectionPanel>
              <Typography color="text.secondary" sx={{ fontSize: 12 }}>
                数据目录
              </Typography>
              <Typography
                sx={{ mt: 1, fontSize: 12, wordBreak: "break-all" }}
              >
                {textValue(runtime.data_dir) || "--"}
              </Typography>
            </SectionPanel>
          </Box>

          <SectionPanel sx={{ mb: 2 }}>
            <Stack
              direction={{ xs: "column", sm: "row" }}
              spacing={2}
              sx={{ alignItems: { sm: "flex-start" } }}
            >
              <ExtensionRoundedIcon
                color="primary"
                sx={{ mt: { sm: 0.25 }, fontSize: 28 }}
              />
              <Box sx={{ minWidth: 0, flex: 1 }}>
                <Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>
                  加入浏览器扩展
                </Typography>
                <Typography
                  sx={{ mt: 0.75, color: "text.secondary", lineHeight: 1.75 }}
                >
                  如果下载的是压缩包，请先解压；如果拿到的已经是扩展文件夹，直接放进下面的目录。扩展文件夹里面必须能看到
                  manifest.json。
                </Typography>
                <Box
                  sx={{
                    mt: 1.5,
                    p: 1.25,
                    overflowWrap: "anywhere",
                    border: "1px solid",
                    borderColor: "divider",
                    borderRadius: "6px",
                    bgcolor: "#f4f7f5",
                    fontFamily: "monospace",
                    fontSize: 12,
                  }}
                >
                  {extensionsDirectory || "扩展目录暂时没拿到，请先刷新"}
                </Box>
                <Typography
                  sx={{
                    mt: 1.25,
                    color: "#7a4d00",
                    fontSize: 13,
                    lineHeight: 1.7,
                  }}
                >
                  放好后请关闭并重新打开 CloakBrowser。只刷新招聘页面不会加载新扩展，我先小声提醒一下。
                </Typography>
              </Box>
              <Button
                variant="outlined"
                startIcon={<FolderOpenRoundedIcon />}
                disabled={openingExtensions || !extensionsDirectory}
                onClick={() => void openExtensionsDirectory()}
                sx={{ flexShrink: 0, whiteSpace: "nowrap" }}
              >
                {openingExtensions ? "正在打开" : "打开扩展目录"}
              </Button>
            </Stack>
          </SectionPanel>

          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: { xs: "1fr", lg: "repeat(2, 1fr)" },
              gap: 2,
            }}
          >
            {components.map((item) => (
              <SectionPanel key={item.key}>
                <Stack
                  direction="row"
                  sx={{
                    justifyContent: "space-between",
                    alignItems: "flex-start",
                  }}
                >
                  <Box>
                    <Typography
                      component="h2"
                      sx={{ fontSize: 18, fontWeight: 760 }}
                    >
                      {item.name}
                    </Typography>
                    <Typography
                      sx={{ mt: 0.75, color: "text.secondary", fontSize: 13 }}
                    >
                      {item.note || "暂无版本说明"}
                    </Typography>
                  </Box>
                  <Chip
                    size="small"
                    color={
                      item.installed
                        ? "success"
                        : item.required
                          ? "error"
                          : "default"
                    }
                    label={
                      item.installed
                        ? "已安装"
                        : item.required
                          ? "未安装"
                          : "可选"
                    }
                  />
                </Stack>
                <Box
                  component="dl"
                  sx={{
                    mt: 2,
                    display: "grid",
                    gridTemplateColumns: "86px 1fr",
                    gap: 1,
                    fontSize: 13,
                    "& dt": { color: "text.secondary" },
                    "& dd": { m: 0, wordBreak: "break-all" },
                  }}
                >
                  <dt>配置版本</dt>
                  <dd>{item.configVersion || "--"}</dd>
                  <dt>本地版本</dt>
                  <dd>{item.installedVersion || "--"}</dd>
                  <dt>下载地址</dt>
                  <dd>
                    {item.bundled ? "随本地程序内置" : item.url || "未配置"}
                  </dd>
                  <dt>本地路径</dt>
                  <dd>{item.path || "--"}</dd>
                </Box>
              </SectionPanel>
            ))}
          </Box>
        </>
      )}
    </>
  );
}

/** buildComponents 根据本机系统构建组件展示数据。 */
function buildComponents(
  runtime: UnknownRecord,
  config: unknown,
): RuntimeComponentView[] {
  const isWindows =
    typeof navigator !== "undefined" &&
    navigator.userAgent.toLowerCase().includes("windows");
  const platformKey = isWindows ? "win" : "mac";
  const configured = asRecord(asRecord(config).runtime_components);
  const nestedRuntime = asRecord(runtime.runtime);
  const installed = asRecord(
    runtime.installed_versions || nestedRuntime.installed_versions,
  );

  return Object.keys(componentNames).map((key) => {
    const componentConfig = asRecord(configured[key]);
    const asset = asRecord(
      componentConfig[platformKey] ||
        componentConfig[isWindows ? "windows" : "macos"],
    );
    const local = asRecord(installed[key]);
    const pathKey = `${key.replace("_runtime", "")}_path`;
    const path =
      textValue(runtime[pathKey]) ||
      textValue(nestedRuntime[pathKey]) ||
      (key === "node_worker"
        ? textValue(runtime.worker_entry) ||
          textValue(nestedRuntime.worker_entry)
        : "");
    return {
      key,
      name: componentNames[key],
      required: key !== "ocr",
      bundled: key === "node_worker",
      installed: Boolean(
        textValue(local.version) ||
          path ||
          runtime[`${key}_installed`] ||
          nestedRuntime[`${key}_installed`],
      ),
      configVersion: textValue(asset.version),
      installedVersion: textValue(local.version),
      url: textValue(asset.url),
      note:
        key === "node_worker"
          ? "随本地程序安装包内置，不需要单独安装。"
          : textValue(asset.note) ||
            textValue(asset.description) ||
            "",
      path,
    };
  });
}

/** asRecord 把未知接口数据安全转换为可读取对象。 */
function asRecord(value: unknown): UnknownRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return value as UnknownRecord;
}

/** textValue 把未知字段安全转换为去除首尾空格的字符串。 */
function textValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}
