/** 本文件负责岗位模板完整的新增、编辑、会员校验、模式联动和提示词管理。 */
"use client";

import AddRoundedIcon from "@mui/icons-material/AddRounded";
import AutoFixHighRoundedIcon from "@mui/icons-material/AutoFixHighRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import EditRoundedIcon from "@mui/icons-material/EditRounded";
import ExpandMoreRoundedIcon from "@mui/icons-material/ExpandMoreRounded";
import PlayArrowRoundedIcon from "@mui/icons-material/PlayArrowRounded";
import RestartAltRoundedIcon from "@mui/icons-material/RestartAltRounded";
import StopRoundedIcon from "@mui/icons-material/StopRounded";
import {
  Alert,
  Box,
  Button,
  Collapse,
  CircularProgress,
  Divider,
  FormControlLabel,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
import ClickableImagePreview from "@/components/admin/ClickableImagePreview";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import PlatformLogo, {
  platformIconSrc,
  platformLabel,
} from "@/components/admin/PlatformLogo";
import { CLOUD_API_BASE, cloudRequest, getToken, localRequest } from "@/lib/admin-api";
import { isPlatformOpen, type PlatformConfigLike } from "@/lib/platform-open";
import { reportUserFlow } from "@/lib/user-flow";
import { confirmPlatformLoggedInForPosition, openPlatformPositionBrowser, pickPlatformAuthConfig } from "@/lib/platform-login";
import { evaluatePositionStartGuard, latestLocalAgentRelease } from "@/lib/position-start-guard";

const CHROMIUM_ICON_SRC = "/assets/platforms/chromium.png";
const BOSS_NOTICE_IMAGE_SRC = "/assets/platforms/boss-plugin-notice.jpg";
const HLIEPIN_SHORTCUT_GUIDE_IMAGE_SRC =
  "/assets/help/hliepin-shortcut-search-guide.png";
const PLATFORM_OPEN_ORDER = ["boss", "zhaopin", "hliepin", "liepin"];
const LOG_REFRESH_MS = 3000;
const LOG_LIMIT = 100;
const ALL_LOG_LIMIT = 5000;

type PositionForm = ReturnType<typeof createEmptyForm>;

/** PositionsPage 管理岗位筛选、详情识别和 AI 提示词配置。 */
export default function PositionsPage() {
  const router = useRouter();
  const { subscription, notify, confirm, agentBase, onboardingConfig } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [busyPositionID, setBusyPositionID] = useState("");
  const [expandedLogPositionID, setExpandedLogPositionID] = useState("");
  const [logs, setLogs] = useState<Record<string, any[]>>({});
  const [logLoadingPositionID, setLogLoadingPositionID] = useState("");
  const [allLogs, setAllLogs] = useState<any[]>([]);
  const [allLogPosition, setAllLogPosition] = useState<any | null>(null);
  const [allLogLoading, setAllLogLoading] = useState(false);
  const [startPositionItem, setStartPositionItem] = useState<any | null>(null);
  const [startLoading, setStartLoading] = useState(false);
  const [startStatus, setStartStatus] = useState("");
  const [form, setForm] = useState<PositionForm>(createEmptyForm());
  const [platformConfigs, setPlatformConfigs] = useState<PlatformConfigLike[]>(
    [],
  );
  const [defaults, setDefaults] = useState({
    filter_prompt: "",
    open_detail_prompt: "",
    review_prompt: "",
  });

  /** load 读取岗位模板和系统默认提示词。 */
  async function load() {
    setLoading(true);
    try {
      const [positions, prompts, platformData] = await Promise.all([
        cloudRequest("/api/positions"),
        cloudRequest("/api/system/default-prompts"),
        cloudRequest("/api/platforms/config/", { auth: false }),
      ]);
      setItems(positions.positions || []);
      setDefaults(normalizePrompts(prompts.prompts || prompts || {}));
      setPlatformConfigs(platformData.platforms || platformData.configs || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "岗位模板读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    const expandedPosition = items.find((item) => item.id === expandedLogPositionID);
    if (!expandedPosition || expandedPosition.status !== "running") return undefined;
    const timer = window.setInterval(() => {
      void loadPositionLogs(expandedPosition, { silent: true });
    }, LOG_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [agentBase, expandedLogPositionID, items]);

  /** openCreate 使用免费版可用配置打开新增弹框。 */
  function openCreate() {
    const openPlatformID = firstOpenPlatformID(platformConfigs);
    if (!openPlatformID) {
      notify("暂时没有可用招聘平台，请联系作者", "warning");
      return;
    }
    const next = createEmptyForm();
    next.platform_id = openPlatformID;
    next.mode_default = defaultCreateMode(subscription.active);
    next.detail_mode = defaultCreateDetailMode(
      next.platform_id,
      subscription.active,
    );
    setForm(fillPrompts(next, defaults));
    setAdvancedOpen(false);
    setDialogOpen(true);
  }

  /** openEdit 将岗位完整字段写入弹框并校验会员功能。 */
  async function openEdit(item: any) {
    const next = formFromItem(item, defaults);
    if (
      !subscription.active &&
      (next.mode_default === "ai" || next.detail_mode === "ai")
    ) {
      const go = await confirm(
        "会员功能",
        "该岗位使用了 AI 筛选或 AI 详情识别。当前会员已到期，是否前往订阅页面？",
      );
      if (go) router.push("/admin/subscription");
    }
    setForm(next);
    setAdvancedOpen(false);
    setDialogOpen(true);
  }

  /** save 保存岗位模板并保留旧后端所需字段结构。 */
  async function save() {
    if (!form.name.trim()) return notify("请填写岗位名称", "warning");
    if (!isPlatformOpen(platformConfigs, form.platform_id)) {
      return notify("该平台暂未开放，请联系作者", "warning");
    }
    const detailMode = form.id
      ? normalizeDetailMode(form.platform_id, form.detail_mode)
      : defaultCreateDetailMode(form.platform_id, subscription.active);
    if (
      !subscription.active &&
      (form.mode_default === "ai" || detailMode === "ai")
    )
      return requireMembership();
    setLoading(true);
    try {
      await cloudRequest("/api/positions", {
        method: "POST",
        body: {
          id: form.id,
          platform_id: form.platform_id,
          name: form.name.trim(),
          keywords: splitKeywords(form.keywords),
          exclude_keywords: splitKeywords(form.exclude_keywords),
          description: form.description.trim(),
          greet_message: form.greet_message.trim(),
          is_and_mode: form.is_and_mode,
          common_config: {
            mode_default: form.mode_default,
            detail_mode: detailMode,
            output_structured_resume: form.output_structured_resume,
            hliepin_shortcut_search_name:
              form.hliepin_shortcut_search_name.trim(),
          },
          ai_config: {
            position_requirement: form.position_requirement,
            filter_prompt: form.filter_prompt || defaults.filter_prompt,
            greet_prompt: form.filter_prompt || defaults.filter_prompt,
            click_prompt: form.filter_prompt || defaults.filter_prompt,
            open_detail_prompt:
              form.open_detail_prompt || defaults.open_detail_prompt,
            review_prompt: normalizePrompt(form.review_prompt),
            detail_score_threshold: Number(form.detail_score_threshold || 60),
            greet_score_threshold: Number(form.greet_score_threshold || 70),
          },
          keyword_config: {},
          match_limit: Number(form.match_limit || 50),
          enable_sound: form.enable_sound,
          enable_thinking: form.enable_thinking,
        },
      });
      notify(form.id ? "岗位模板已更新" : "岗位模板已创建", "success");
      setDialogOpen(false);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存岗位失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** remove 删除指定岗位模板。 */
  async function remove(item: any) {
    if (!(await confirm("删除岗位模板", `确认删除“${item.name}”吗？`))) return;
    try {
      await cloudRequest(`/api/positions/${item.id}`, { method: "DELETE" });
      notify("岗位模板已删除", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "删除失败", "error");
    }
  }

  /** openStartPosition 展开岗位日志并打开启动确认弹框。 */
  function openStartPosition(item: any) {
    if (!agentBase) return notify("请先启动本地程序", "warning");
    setExpandedLogPositionID(item.id);
    void loadPositionLogs(item);
    setStartStatus("");
    setStartPositionItem(item);
  }

  /** checkPositionStartGuard 检查 AI 余额和本地程序版本是否满足启动要求。 */
  async function checkPositionStartGuard(item: any) {
    setStartStatus("正在检查 AI 余额和本地程序版本...");
    try {
      let runtimeConfig = onboardingConfig;
      if (!latestLocalAgentRelease(runtimeConfig).version) {
        const runtimePayload = await cloudRequest("/api/runtime/config");
        runtimeConfig = runtimePayload.config || runtimePayload || {};
      }
      const [walletPayload, health] = await Promise.all([
        cloudRequest("/api/ai-wallet"),
        localRequest(agentBase, "/health"),
      ]);
      const release = latestLocalAgentRelease(runtimeConfig);
      const guardFailure = evaluatePositionStartGuard(
        walletPayload.wallet || walletPayload,
        health.version || health.agent_version,
        release.version,
      );
      if (guardFailure) {
        await reportUserFlow({ step: "position_started", status: "blocked", reason_code: guardFailure.code, message: guardFailure.message, source: "position_start_guard", position_id: item.id }).catch(() => undefined);
        setStartStatus(guardFailure.message);
        return false;
      }
      return true;
    } catch (error) {
      const message = error instanceof Error
        ? `启动条件检查没跑完：${error.message}。这次我先不乱启动，你刷新后再试一次。`
        : "启动条件检查没跑完，这次我先不乱启动，你刷新后再试一次。";
      setStartStatus(message);
      await reportUserFlow({ step: "position_started", status: "blocked", reason_code: "position_start_guard_unavailable", message, source: "position_start_guard", position_id: item.id }).catch(() => undefined);
      return false;
    }
  }

  /** confirmStartPosition 在确认弹框中完成启动检查、登录确认和岗位启动。 */
  async function confirmStartPosition() {
    const item = startPositionItem;
    if (!item || !agentBase) return;
    setStartLoading(true);
    setBusyPositionID(item.id);
    setStartStatus("正在检查岗位启动条件...");
    try {
      if (!(await checkPositionStartGuard(item))) return;
      const subscriptionData = await cloudRequest("/api/subscription/status");
      const active = Boolean(subscriptionData.subscription?.active);
      if (!isPlatformOpen(platformConfigs, item.platform_id)) {
        setStartStatus("这个招聘平台暂时还没开放，我先不乱跑，请联系作者看看。");
        return;
      }
      const auth = pickPlatformAuthConfig(platformConfigs, item.platform_id);
      setStartStatus("正在打开招聘平台，我先看看账号有没有登录。");
      await openPlatformPositionBrowser(agentBase, item.platform_id, auth);
      try {
        await confirmPlatformLoggedInForPosition(agentBase, auth, (message) =>
          setStartStatus(message),
        );
        await reportUserFlow({ step: "platform_login_verified", source: "position_start", position_id: item.id });
      } catch (loginError) {
        const message = loginError instanceof Error
          ? loginError.message
          : "招聘平台还没登录，请先在浏览器里完成登录。";
        setStartStatus(message);
        await reportUserFlow({ step: "platform_login_verified", status: "blocked", reason_code: "platform_not_logged_in", message, source: "position_start", position_id: item.id }).catch(() => undefined);
        return;
      }
      const usesAI = item.common_config?.mode_default === "ai" || item.common_config?.detail_mode === "ai";
      if (usesAI && !active) {
        const message = "这个岗位用了会员 AI 功能，订阅后我才能继续开工。";
        setStartStatus(message);
        await reportUserFlow({ step: "position_started", status: "blocked", reason_code: "subscription_expired", message, source: "position_start", position_id: item.id }).catch(() => undefined);
        return;
      }
      if (!active) notify("当前是免费版，今天的打招呼数量会按免费额度来，我会省着点用。", "info");
      setStartStatus("登录确认好了，正在启动岗位...");
      await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(item.id)}/run`, {
        method: "POST",
        body: { cloud_api_base: CLOUD_API_BASE, token: getToken(), enable_greet: true },
      });
      await reportUserFlow({ step: "position_started", source: "position_start", position_id: item.id });
      notify("岗位已经开始跑了，我会老实记日志", "success");
      setStartPositionItem(null);
      setStartStatus("");
      await Promise.all([load(), loadPositionLogs(item)]);
    } catch (error) {
      const message = error instanceof Error ? error.message : "岗位启动失败";
      setStartStatus(message);
      await reportUserFlow({ step: "position_started", status: "blocked", reason_code: positionStartReason(message), message, source: "position_start", position_id: item.id }).catch(() => undefined);
      notify(message, "error");
    } finally {
      setBusyPositionID("");
      setStartLoading(false);
    }
  }

  /** stopPosition 停止本地岗位运行，但保持浏览器打开。 */
  async function stopPosition(item: any) {
    if (!agentBase) return notify("本地程序还没连上", "warning");
    setExpandedLogPositionID("");
    setBusyPositionID(item.id);
    try {
      await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(item.id)}/stop`, {
        method: "POST",
        body: { cloud_api_base: CLOUD_API_BASE, token: getToken() },
      });
      notify("岗位已停下，浏览器先给你留着", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "停止岗位失败", "error");
    } finally {
      setBusyPositionID("");
    }
  }

  /** togglePositionLogs 展开或收起岗位累计日志，不主动清空历史日志。 */
  async function togglePositionLogs(item: any) {
    if (expandedLogPositionID === item.id) {
      setExpandedLogPositionID("");
      return;
    }
    if (!agentBase) return notify("本地程序还没连上", "warning");
    setExpandedLogPositionID(item.id);
    await loadPositionLogs(item);
  }

  /** loadPositionLogs 读取指定岗位最近的本地日志并更新卡片。 */
  async function loadPositionLogs(item: any, options: { silent?: boolean } = {}) {
    if (!agentBase) return;
    if (!options.silent) setLogLoadingPositionID(item.id);
    try {
      const data = await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs?limit=${LOG_LIMIT}`);
      setLogs((current) => ({ ...current, [item.id]: data.logs || [] }));
    } catch (error) {
      if (!options.silent) notify(error instanceof Error ? error.message : "日志读取失败", "error");
    } finally {
      if (!options.silent) setLogLoadingPositionID("");
    }
  }

  /** clearPositionLogs 二次确认后清空指定岗位保存在本地程序中的日志。 */
  async function clearPositionLogs(item: any) {
    if (!agentBase) return notify("本地程序还没连上", "warning");
    const approved = await confirm(
      "清空岗位日志",
      "公主请确认要清空这个岗位的全部本地日志吗？清空后我也找不回来了。",
    );
    if (!approved) return;
    try {
      await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs`, {
        method: "DELETE",
      });
      setLogs((current) => ({ ...current, [item.id]: [] }));
      if (allLogPosition?.id === item.id) setAllLogs([]);
      notify("岗位日志已经清空，我把小本本翻到新的一页了。", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "日志没清空成功，我们再试一次。", "error");
    }
  }

  /** loadAllPositionLogs 读取指定岗位保留的全部本地日志并打开弹框。 */
  async function loadAllPositionLogs(item: any) {
    if (!agentBase) return notify("本地程序还没连上", "warning");
    setAllLogPosition(item);
    setAllLogLoading(true);
    try {
      const data = await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs?limit=${ALL_LOG_LIMIT}`);
      setAllLogs(data.logs || []);
    } catch (error) {
      notify(error instanceof Error ? error.message : "全部日志读取失败", "error");
    } finally {
      setAllLogLoading(false);
    }
  }

  /** copyAllPositionLogs 复制当前弹框中的完整岗位日志。 */
  async function copyAllPositionLogs() {
    try {
      await navigator.clipboard.writeText(buildPositionLogText(allLogs));
      notify("全部日志已复制", "success");
    } catch {
      notify("复制失败，请手动选择日志内容", "warning");
    }
  }

  /** optimizeRequirement 调用用户 AI 配置整理岗位要求。 */
  async function optimizeRequirement() {
    if (!form.position_requirement.trim())
      return notify("请先填写岗位要求", "warning");
    setOptimizing(true);
    try {
      const data = await cloudRequest("/api/positions/optimize-requirement", {
        method: "POST",
        body: {
          text: form.position_requirement,
        },
      });
      setForm((current) => ({
        ...current,
        position_requirement:
          data.optimized || data.text || current.position_requirement,
      }));
      notify("岗位要求已优化", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "AI 优化失败", "error");
    } finally {
      setOptimizing(false);
    }
  }

  /** selectMode 选择筛选模式并执行会员提醒。 */
  async function selectMode(value: string) {
    if (value === "ai" && !subscription.active) return requireMembership();
    setForm((current) => ({ ...current, mode_default: value }));
  }

  /** selectDetailMode 选择详情模式并执行平台与会员联动。 */
  async function selectDetailMode(value: string) {
    if (form.platform_id === "boss" && value === "dom")
      return notify("Boss直聘不支持 DOM 详情识别", "warning");
    if (isDOMOnlyPlatform(form.platform_id) && value !== "dom")
      return notify(
        `${platformLabel(form.platform_id)}只能用 DOM 详情识别`,
        "warning",
      );
    if (value === "ai" && !subscription.active) return requireMembership();
    setForm((current) => ({ ...current, detail_mode: value }));
  }

  /** selectPlatform 切换平台并修正平台不支持的详情模式。 */
  function selectPlatform(value: string) {
    if (!isPlatformOpen(platformConfigs, value)) {
      notify("该平台暂未开放，请联系作者", "warning");
      return;
    }
    setForm((current) => ({
      ...current,
      platform_id: value,
      detail_mode: current.id
        ? normalizeDetailMode(value, current.detail_mode)
        : defaultCreateDetailMode(value, subscription.active),
    }));
  }

  /** requireMembership 引导免费用户前往订阅页面。 */
  async function requireMembership() {
    const go = await confirm(
      "该功能需要订阅会员",
      "AI 筛选和 AI 详情识别属于会员功能，是否前往订阅页面？",
    );
    if (go) router.push("/admin/subscription");
  }

  return (
    <>
      <PageHeader
        title='岗位管理'
        description='岗位模板决定首次筛选、详情识别和最终打招呼判断。'
        actions={
          <>
            <Button
              variant='contained'
              startIcon={<AddRoundedIcon />}
              disabled={loading}
              onClick={openCreate}
            >
              新建岗位
            </Button>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      {items.length ? (
        <Stack spacing={1.5}>
          {items.map((item) => (
            <Box
              key={item.id}
              sx={{
                p: { xs: 1.5, sm: 2 },
                border: "1px solid",
                borderColor: "divider",
                borderRadius: "8px",
                bgcolor: "background.paper",
              }}
            >
              <Stack
                direction='row'
                spacing={2}
                sx={{ alignItems: "flex-start" }}
              >
                <PlatformLogo platformID={item.platform_id} size={42} />
                <Stack
                  direction={{ xs: "column", md: "row" }}
                  spacing={1.5}
                  sx={{
                    flex: 1,
                    minWidth: 0,
                    alignItems: { md: "center" },
                    justifyContent: "space-between",
                  }}
                >
                  <Box sx={{ minWidth: 0 }}>
                    <Typography sx={{ fontWeight: 760 }}>
                      {item.name}
                    </Typography>
                    <Typography
                      sx={{
                        mt: 0.5,
                        color: "text.secondary",
                        fontSize: 13,
                        overflowWrap: "anywhere",
                      }}
                    >
                      {platformLabel(item.platform_id)} ·{" "}
                      {item.common_config?.mode_default === "ai"
                        ? "AI 筛选"
                        : "关键词筛选"}{" "}
                      · 详情：
                      {detailModeLabel(item.common_config?.detail_mode)} ·
                      关键词：{(item.keywords || []).join(" / ") || "无"}
                    </Typography>
                  </Box>
                  <Stack direction='row' spacing={1} sx={{ flexWrap: "wrap" }}>
                    {item.status === "running" ? (
                      <Button
                        color='error'
                        variant='contained'
                        startIcon={<StopRoundedIcon />}
                        disabled={busyPositionID === item.id}
                        onClick={() => void stopPosition(item)}
                      >
                        停止
                      </Button>
                    ) : (
                      <Button
                        color='success'
                        variant='contained'
                        startIcon={<PlayArrowRoundedIcon />}
                        disabled={busyPositionID === item.id}
                        onClick={() => openStartPosition(item)}
                      >
                        开始
                      </Button>
                    )}
                    <Button onClick={() => void togglePositionLogs(item)}>
                      {expandedLogPositionID === item.id ? "收起日志" : "日志"}
                    </Button>
                    <Button
                      startIcon={<EditRoundedIcon />}
                      onClick={() => void openEdit(item)}
                    >
                      编辑
                    </Button>
                    <Button
                      color='error'
                      startIcon={<DeleteOutlineRoundedIcon />}
                      onClick={() => void remove(item)}
                    >
                      删除
                    </Button>
                  </Stack>
                </Stack>
              </Stack>
              <Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}>
                累计扫描 {item.scanned_count || 0} · 打招呼 {item.greeted_count || 0} · 跳过 {item.skipped_count || 0} · 失败 {item.failed_count || 0}
              </Typography>
              <Collapse in={expandedLogPositionID === item.id}>
                <PositionLogPanel
                  logs={logs[item.id] || []}
                  loading={logLoadingPositionID === item.id}
                  onRefresh={() => void loadPositionLogs(item)}
                  onViewAll={() => void loadAllPositionLogs(item)}
                  onClear={() => void clearPositionLogs(item)}
                />
              </Collapse>
            </Box>
          ))}
        </Stack>
      ) : (
        <SectionPanel>
          <EmptyState text='暂无岗位模板' />
        </SectionPanel>
      )}
      <AdminDialog
        open={Boolean(startPositionItem)}
        title='开始招聘岗位'
        confirmText='确认开始'
        loading={startLoading}
        loadingText='启动中'
        onClose={() => {
          if (startLoading) return;
          setStartPositionItem(null);
          setStartStatus("");
        }}
        onConfirm={() => void confirmStartPosition()}
      >
        <Stack spacing={1.5}>
          <Typography>
            确认开始“{startPositionItem?.name || ""}”吗？我会先检查 AI 余额、本地程序版本和招聘平台登录状态。
          </Typography>
          <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
            检查通过后我才会正式开工；如果账号没登录，弹框会留在这里告诉你下一步。
          </Typography>
          {startStatus ? (
            <Typography color={isPositionStartErrorStatus(startStatus) ? "error" : "text.secondary"}>
              {startStatus}
            </Typography>
          ) : null}
        </Stack>
      </AdminDialog>
      <AdminDialog
        open={Boolean(allLogPosition)}
        title='查看全部岗位日志'
        description={`读取当前岗位已保留的全部日志，单次最多 ${ALL_LOG_LIMIT} 条，可复制后发给作者排查。`}
        confirmText='复制全部'
        cancelText='关闭'
        loading={allLogLoading}
        maxWidth='lg'
        onClose={() => {
          setAllLogPosition(null);
          setAllLogs([]);
        }}
        onConfirm={() => void copyAllPositionLogs()}
      >
        <PositionLogList logs={allLogs} maxHeight='60vh' />
      </AdminDialog>
      <AdminDialog
        open={dialogOpen}
        title={form.id ? "编辑岗位模板" : "新建岗位模板"}
        description='按运行顺序填写。只有当前选择模式需要的字段会显示。'
        maxWidth='md'
        confirmText={form.id ? "保存修改" : "创建岗位"}
        loading={loading}
        confirmDisabled={!form.name.trim()}
        onClose={() => setDialogOpen(false)}
        onConfirm={() => void save()}
      >
        <Stack spacing={3}>
          <Alert severity='info' variant='outlined'>
            运行时先读取候选人基础信息，完成第一次筛选并决定是否打开详情；读取详情后再进行第二次分析，决定是否打招呼。请按这个顺序配置下面的内容。
          </Alert>
          <Box>
            <Typography
              component='h3'
              sx={{ mb: 1.5, fontSize: 17, fontWeight: 780 }}
            >
              基础信息
            </Typography>
            <TextField
              label='岗位名称'
              value={form.name}
              onChange={(event) =>
                setForm({ ...form, name: event.target.value })
              }
              fullWidth
              placeholder='例如：服装带货主播'
              helperText='岗位名称必须和平台岗位岗位名称保持一致。(请前往招聘平台复制岗位名称)'
              slotProps={{
                formHelperText: {
                  sx: { color: "error.main", fontSize: 14, fontWeight: "bold" },
                },
              }}
            />
            <Stack direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ mt: 2 }}>
              <TextField
                label='每次打招呼上限'
                type='number'
                value={form.match_limit}
                onChange={(event) => setForm({ ...form, match_limit: Number(event.target.value || 0) })}
                slotProps={{ htmlInput: { min: 1 } }}
                sx={{ width: { sm: 220 } }}
              />
              <FormControlLabel
                control={<Switch checked={form.enable_sound} onChange={(event) => setForm({ ...form, enable_sound: event.target.checked })} />}
                label='完成后提示音'
              />
              <FormControlLabel
                control={<Switch checked={form.enable_thinking} onChange={(event) => setForm({ ...form, enable_thinking: event.target.checked })} />}
                label='思考模式'
              />
            </Stack>
          </Box>
          <ChoiceCards
            label='招聘平台'
            value={form.platform_id}
            columns={3}
            autoWidth
            onChange={(value) => selectPlatform(String(value))}
            options={[
              {
                value: "boss",
                label: "Boss直聘",
                disabled: !isPlatformOpen(platformConfigs, "boss"),
                description: isPlatformOpen(platformConfigs, "boss")
                  ? "支持 OCR 和 AI 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("boss"),
              },
              {
                value: "zhaopin",
                label: "智联招聘",
                disabled: !isPlatformOpen(platformConfigs, "zhaopin"),
                description: isPlatformOpen(platformConfigs, "zhaopin")
                  ? "支持 DOM 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("zhaopin"),
              },
              {
                value: "hliepin",
                label: "猎聘猎头端",
                disabled: !isPlatformOpen(platformConfigs, "hliepin"),
                description: isPlatformOpen(platformConfigs, "hliepin")
                  ? "支持 DOM 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("hliepin"),
              },
              {
                value: "liepin",
                label: "猎聘企业端",
                disabled: !isPlatformOpen(platformConfigs, "liepin"),
                description: isPlatformOpen(platformConfigs, "liepin")
                  ? "支持 DOM 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("liepin"),
              },
            ]}
          />
          <Box
            sx={{
              p: 1.25,
              border: "1px solid",
              borderColor: "#d9c485",
              borderRadius: "8px",
              bgcolor: "#fffaf0",
            }}
          >
            <Typography
              sx={{ mb: 1, color: "#7a4d00", fontSize: 13, fontWeight: 780 }}
            >
              平台提示（不用选择）
            </Typography>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns:
                  form.platform_id === "boss"
                    ? { xs: "1fr", md: "minmax(0, .9fr) minmax(0, 1.1fr)" }
                    : "1fr",
                gap: { xs: 1, md: 1.75 },
              }}
            >
              <PlatformTipCard
                iconSrc={CHROMIUM_ICON_SRC}
                title='浏览器图标'
                text='创建岗位运行后点右下角蓝色浏览器图标，完成对应平台登录。'
              />
              {form.platform_id === "boss" ? (
                <PlatformTipCard
                  imageSrc={BOSS_NOTICE_IMAGE_SRC}
                  title='BOSS 插件、外挂 提示'
                  text='很多账号会提示插件、外挂等招聘辅助工具，这是平台通用公告，不等于封号。点“我已知晓”即可，别高频操作。'
                />
              ) : null}
            </Box>
          </Box>
          <ChoiceCards
            label='基础信息筛选模式   (决定是否打开查看详情)'
            value={form.mode_default}
            onChange={(value) => void selectMode(String(value))}
            options={[
              {
                value: "keyword",
                label: "关键词筛选",
                description: "按关键词和排除词判断，永久免费且速度快。",
              },
              {
                value: "ai",
                label: "AI 筛选（会员功能）",
                description: "AI 先根据基础信息判断是否值得打开详情。",
                memberOnly: true,
              },
            ]}
          />
          {form.id ? (
            <>
              <Typography
                sx={{ mt: -2, color: "text.secondary", fontSize: 13 }}
              >
                选择哪种详情方式就只使用哪一种：DOM 最快，OCR
                在本地识别截图文字，AI 能理解完整页面但耗时更长。
              </Typography>
              <ChoiceCards
                label='详情信息筛选模式  (决定是否打招呼)'
                value={form.detail_mode}
                columns={3}
                onChange={(value) => void selectDetailMode(String(value))}
                options={[
                  {
                    value: "dom",
                    label: "DOM 识别",
                    description: "BOSS直聘不支持DOM识别，速度快，精度高，免费",
                    disabled: form.platform_id === "boss",
                  },
                  {
                    value: "ocr",
                    label: "OCR 识别",
                    description:
                      "离线识别截图文字，速度快。电脑配置低就别选这个。",
                    disabled: isDOMOnlyPlatform(form.platform_id),
                  },
                  {
                    value: "ai",
                    label: "AI 识别（会员功能）",
                    description: "直接理解完整详情截图，效果最好但更慢。",
                    disabled: isDOMOnlyPlatform(form.platform_id),
                    memberOnly: true,
                  },
                ]}
              />
            </>
          ) : null}
          {form.mode_default === "keyword" ? (
            <>
              <Divider />
              <Box>
                <Typography
                  component='h3'
                  sx={{ mb: 1.5, fontSize: 17, fontWeight: 780 }}
                >
                  关键词筛选
                </Typography>
                <Stack spacing={2}>
                  <ChoiceCards
                    label='匹配方式'
                    value={form.is_and_mode}
                    onChange={(value) =>
                      setForm({ ...form, is_and_mode: Boolean(value) })
                    }
                    options={[
                      {
                        value: false,
                        label: "满足任一关键词",
                        description: "命中一个关键词即可通过，适合放宽筛选。",
                      },
                      {
                        value: true,
                        label: "必须同时满足",
                        description: "需要命中全部关键词，适合严格筛选。",
                      },
                    ]}
                  />
                  <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
                    关键词模式是否打开详情，由“个人配置”中的详情查看概率控制。满足任一关键词更宽松，必须同时满足则更严格。
                  </Typography>
                  <Box
                    sx={{
                      display: "grid",
                      gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                      gap: 2,
                    }}
                  >
                    <TextField
                      label='关键词'
                      value={form.keywords}
                      onChange={(event) =>
                        setForm({ ...form, keywords: event.target.value })
                      }
                      multiline
                      minRows={3}
                      helperText='支持空格、中文逗号、英文逗号或换行分隔。'
                    />
                    <TextField
                      label='排除词'
                      value={form.exclude_keywords}
                      onChange={(event) =>
                        setForm({
                          ...form,
                          exclude_keywords: event.target.value,
                        })
                      }
                      multiline
                      minRows={3}
                      helperText='命中排除词后直接跳过。'
                    />
                  </Box>
                  {form.platform_id === "hliepin" ? (
                    <Stack spacing={1.25}>
                      <TextField
                        label='猎聘快捷搜索名'
                        value={form.hliepin_shortcut_search_name}
                        onChange={(event) =>
                          setForm({
                            ...form,
                            hliepin_shortcut_search_name: event.target.value,
                          })
                        }
                        fullWidth
                        placeholder='请填写猎聘搜索页已保存的快捷搜索名称'
                        helperText='填写后，岗位运行会直接选择猎聘页面中完全同名的快捷搜索，不再输入搜索关键词；如果不填，则使用正在发布的岗位进行匹配。'
                      />
                      <HLiepinShortcutSearchGuide
                        visible={Boolean(
                          form.hliepin_shortcut_search_name.trim(),
                        )}
                      />
                    </Stack>
                  ) : null}
                </Stack>
              </Box>
            </>
          ) : null}
          {form.mode_default === "ai" ? (
            <>
              <Divider />
              <Box>
                <Stack
                  direction={{ xs: "column", sm: "row" }}
                  sx={{ mb: 1.5, justifyContent: "space-between", gap: 1 }}
                >
                  <Box>
                    <Typography
                      component='h3'
                      sx={{ fontSize: 17, fontWeight: 780 }}
                    >
                      AI 配置
                    </Typography>
                    <Typography
                      sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}
                    >
                      请将JD岗位要求复制到“岗位要求”中，点击“AI
                      优化岗位要求”按钮，AI会自动优化。
                    </Typography>
                  </Box>
                  <Button
                    startIcon={
                      optimizing ? (
                        <CircularProgress size={16} color='inherit' />
                      ) : (
                        <AutoFixHighRoundedIcon />
                      )
                    }
                    disabled={optimizing || !form.position_requirement.trim()}
                    onClick={() => void optimizeRequirement()}
                  >
                    {optimizing ? "分析中..." : "AI 优化岗位要求"}
                  </Button>
                </Stack>
                <Stack spacing={2}>
                  <TextField
                    label='岗位要求'
                    value={form.position_requirement}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        position_requirement: event.target.value,
                      })
                    }
                    multiline
                    fullWidth
                    placeholder='必须有3年以上教学经验，必须有教师资格证，学历年龄 等基础条件可以在平台提前筛选好，更不要写跟岗位要求无关的 比如 岗位福利，工作环境等。'
                    minRows={7}
                    helperText='建议写清学历、经验、技能、行业、城市、到岗状态和明确的淘汰条件；不要填写“有上进心”等无法从简历判断的内容。'
                  />
                  {form.platform_id === "hliepin" ? (
                    <Stack spacing={1.25}>
                      <TextField
                        label='猎聘快捷搜索名'
                        value={form.hliepin_shortcut_search_name}
                        onChange={(event) =>
                          setForm({
                            ...form,
                            hliepin_shortcut_search_name: event.target.value,
                          })
                        }
                        fullWidth
                        placeholder='请填写猎聘搜索页已保存的快捷搜索名称'
                        helperText='填写后，岗位运行会直接选择猎聘页面中完全同名的快捷搜索，不再输入搜索关键词；如果不填，则使用正在发布的岗位进行匹配。它不参与本地简历的 AI 判断。'
                      />
                      <HLiepinShortcutSearchGuide
                        visible={Boolean(
                          form.hliepin_shortcut_search_name.trim(),
                        )}
                      />
                    </Stack>
                  ) : null}
                  <Box
                    sx={{
                      p: 1.5,
                      borderLeft: "3px solid",
                      borderColor: "primary.main",
                      bgcolor: "#f5f8f6",
                    }}
                  >
                    <Typography sx={{ fontSize: 13, fontWeight: 760 }}>
                      强烈建议 先看右上角的视频教程，了解各项参数的意义
                    </Typography>

                    <Typography
                      sx={{
                        mt: 0.5,
                        color: "text.secondary",
                        fontSize: 13,
                        lineHeight: 1.75,
                      }}
                    >
                      求职意向必须匹配目标岗位；具备 3
                      年以上相关经验；拥有岗位要求的证书或技能；当前状态满足到岗要求。薪资越高或岗位越重要，条件应写得越明确。
                    </Typography>
                  </Box>
                  <Box
                    sx={{
                      border: "1px solid",
                      borderColor: "divider",
                      borderRadius: "8px",
                      overflow: "hidden",
                    }}
                  >
                    <Button
                      fullWidth
                      onClick={() => setAdvancedOpen((value) => !value)}
                      endIcon={
                        <ExpandMoreRoundedIcon
                          sx={{
                            transform: advancedOpen
                              ? "rotate(180deg)"
                              : "rotate(0deg)",
                            transition: "transform .18s ease",
                          }}
                        />
                      }
                      sx={{
                        justifyContent: "space-between",
                        px: 1.5,
                        py: 1.25,
                        color: "text.primary",
                        bgcolor: advancedOpen ? "#f5f8f6" : "transparent",
                      }}
                    >
                      高级设置
                    </Button>
                    <Box sx={{ px: 1.5, pb: advancedOpen ? 1.5 : 1.25 }}>
                      <Typography
                        sx={{
                          color: "text.secondary",
                          fontSize: 13,
                          lineHeight: 1.75,
                        }}
                      >
                        这里是增加 AI
                        准确率的各项设置。如果不理解，先别改它，问题不大。也可以点右上角“视频教程”，看完再回来慢慢调。
                      </Typography>
                    </Box>
                    <Collapse in={advancedOpen} unmountOnExit>
                      <Stack spacing={2} sx={{ px: 1.5, pb: 1.5 }}>
                        <PromptField
                          label='打开详情提示词（一般不需要修改）'
                          value={form.open_detail_prompt}
                          defaultValue={defaults.open_detail_prompt}
                          description='只用于第一次分析，判断候选人是否值得打开详情。普通岗位可以宽松一些，高级岗位可以更严格。'
                          onChange={(value) =>
                            setForm({ ...form, open_detail_prompt: value })
                          }
                        />
                        <TextField
                          label='看详情阈值分'
                          type='number'
                          value={form.detail_score_threshold}
                          onChange={(event) =>
                            setForm({
                              ...form,
                              detail_score_threshold: Number(
                                event.target.value,
                              ),
                            })
                          }
                          slotProps={{ htmlInput: { min: 0, max: 100 } }}
                          helperText='首次评分大于等于该值时打开候选人详情。'
                        />
                        <ChoiceCards
                          label='是否生成简历'
                          value={form.output_structured_resume}
                          onChange={(value) =>
                            setForm({
                              ...form,
                              output_structured_resume: Boolean(value),
                            })
                          }
                          options={[
                            {
                              value: false,
                              label: "不需要",
                              description: "AI消耗少、不会存简历。",
                            },
                            {
                              value: true,
                              label: "需要",
                              description: "AI消耗多，会把信息放到简历库里。",
                            },
                          ]}
                        />
                        <PromptField
                          label='打招呼提示词（一般不需要修改）'
                          value={form.filter_prompt}
                          defaultValue={defaults.filter_prompt}
                          description='用于详情分析并决定候选人的最终分数，直接影响是否执行打招呼。'
                          onChange={(value) =>
                            setForm({ ...form, filter_prompt: value })
                          }
                        />
                        <TextField
                          label='打招呼阈值分'
                          type='number'
                          value={form.greet_score_threshold}
                          onChange={(event) =>
                            setForm({
                              ...form,
                              greet_score_threshold: Number(event.target.value),
                            })
                          }
                          slotProps={{ htmlInput: { min: 0, max: 100 } }}
                          helperText='详情评分大于等于该值时执行打招呼。'
                        />
                        <PromptField
                          label='复核提示词（可选）（一般不需要修改）'
                          value={form.review_prompt}
                          defaultValue=''
                          defaultActionLabel='清空'
                          emptyPlaceholder='可留空，不填写则不会触发复核'
                          description='当详情分数接近打招呼阈值时执行二次复核；留空则不会触发复核。'
                          onChange={(value) =>
                            setForm({ ...form, review_prompt: value })
                          }
                        />
                      </Stack>
                    </Collapse>
                  </Box>
                </Stack>
              </Box>
            </>
          ) : null}
          <Divider />
          <Box>
            <Typography
              component='h3'
              sx={{ mb: 1.5, fontSize: 17, fontWeight: 780 }}
            >
              可选信息
            </Typography>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                gap: 2,
              }}
            >
              <TextField
                label='问候语，暂时不填'
                value={form.greet_message}
                onChange={(event) =>
                  setForm({ ...form, greet_message: event.target.value })
                }
                multiline
                minRows={3}
              />
              <TextField
                label='岗位描述 暂时不填'
                value={form.description}
                onChange={(event) =>
                  setForm({ ...form, description: event.target.value })
                }
                multiline
                minRows={3}
              />
            </Box>
          </Box>
          {!subscription.active &&
          (form.mode_default === "ai" ||
            (form.id && form.detail_mode === "ai")) ? (
            <Alert severity='warning'>
              当前会员已到期，AI 选项无法保存。可以改为关键词筛选和 OCR 识别。
            </Alert>
          ) : null}
        </Stack>
      </AdminDialog>
    </>
  );
}

/** PromptField 输出带恢复系统默认按钮的提示词输入框。 */
function PromptField({
  label,
  value,
  defaultValue,
  defaultActionLabel = "设为系统默认",
  emptyPlaceholder = "系统暂未配置默认提示词",
  description,
  onChange,
}: {
  label: string;
  value: string;
  defaultValue: string;
  defaultActionLabel?: string;
  emptyPlaceholder?: string;
  description: string;
  onChange: (value: string) => void;
}) {
  return (
    <Box>
      <Stack
        direction='row'
        sx={{ mb: 0.75, justifyContent: "space-between", alignItems: "center" }}
      >
        <Typography sx={{ fontSize: 13, fontWeight: 700 }}>{label}</Typography>
        <Button
          size='small'
          startIcon={<RestartAltRoundedIcon />}
          onClick={() => onChange(defaultValue)}
        >
          {defaultActionLabel}
        </Button>
      </Stack>
      <TextField
        value={value}
        onChange={(event) => onChange(event.target.value)}
        multiline
        minRows={6}
        fullWidth
        placeholder={defaultValue ? "已加载系统默认提示词" : emptyPlaceholder}
      />
      <Typography
        sx={{
          mt: 0.75,
          color: "text.secondary",
          fontSize: 12.5,
          lineHeight: 1.6,
        }}
      >
        {description}
      </Typography>
    </Box>
  );
}

/** PlatformTipCard 展示平台选择后的图文提醒。 */
function PlatformTipCard({
  iconSrc,
  imageSrc,
  title,
  text,
}: {
  iconSrc?: string;
  imageSrc?: string;
  title: string;
  text: string;
}) {
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: imageSrc
          ? "minmax(92px, 130px) minmax(0, 1fr)"
          : "40px minmax(0, 1fr)",
        gap: 1.25,
        alignItems: "center",
        minHeight: 72,
      }}
    >
      {imageSrc ? (
        <Box
          component='img'
          src={imageSrc}
          alt={title}
          sx={{
            width: "100%",
            height: 70,
            objectFit: "cover",
            borderRadius: "6px",
            border: "1px solid rgba(0,0,0,.08)",
          }}
        />
      ) : (
        <Box
          component='img'
          src={iconSrc}
          alt={title}
          sx={{ width: 34, height: 34, justifySelf: "center" }}
        />
      )}
      <Box sx={{ minWidth: 0 }}>
        <Typography sx={{ color: "#22372c", fontSize: 13, fontWeight: 780 }}>
          {title}
        </Typography>
        <Typography
          sx={{ mt: 0.35, color: "#54635a", fontSize: 12.5, lineHeight: 1.55 }}
        >
          {text}
        </Typography>
      </Box>
    </Box>
  );
}

/** HLiepinShortcutSearchGuide 在填写猎聘快捷搜索名后展示配置提醒和可放大教程图。 */
function HLiepinShortcutSearchGuide({ visible }: { visible: boolean }) {
  return (
    <Collapse in={visible} unmountOnExit>
      <Box
        sx={{
          p: 1.5,
          border: "1px solid",
          borderColor: "warning.light",
          borderRadius: "8px",
          bgcolor: "#fffaf0",
        }}
      >
        <Typography sx={{ fontSize: 14, fontWeight: 780 }}>
          请先在猎聘创建并保存快捷搜索
        </Typography>
        <Alert severity='warning' sx={{ mt: 1, mb: 1.5 }}>
          <Typography sx={{ fontSize: 13, lineHeight: 1.7 }}>
            请先在猎聘搜索页面配置关键词和全部筛选条件，点击“保存条件”创建快捷搜索，再把保存后的名称完整填写到上方“猎聘快捷搜索名”。岗位运行会直接使用该快捷搜索包含的全部条件，不会再次填写搜索关键词。
          </Typography>
          <Typography sx={{ mt: 0.75, fontSize: 13, lineHeight: 1.7 }}>
            填写的名称必须与猎聘页面显示的快捷搜索名完全一致，否则岗位运行会停止并说明未找到。不同岗位请使用容易区分且不重复的快捷搜索名，避免选错筛选条件。
          </Typography>
        </Alert>
        <ClickableImagePreview
          src={HLIEPIN_SHORTCUT_GUIDE_IMAGE_SRC}
          alt='猎聘保存搜索条件并创建快捷搜索教程'
          hint='点击图片放大查看猎聘快捷搜索创建步骤'
        />
      </Box>
    </Collapse>
  );
}

/** createEmptyForm 返回免费版可用的岗位默认表单。 */
function createEmptyForm() {
  return {
    id: "",
    name: "",
    platform_id: "boss",
    mode_default: "keyword",
    detail_mode: "ocr",
    keywords: "",
    exclude_keywords: "",
    is_and_mode: false,
    position_requirement: "",
    hliepin_shortcut_search_name: "",
    open_detail_prompt: "",
    filter_prompt: "",
    review_prompt: "",
    detail_score_threshold: 60,
    greet_score_threshold: 70,
    output_structured_resume: false,
    greet_message: "",
    description: "",
    match_limit: 50,
    enable_sound: false,
    enable_thinking: false,
  };
}

/** firstOpenPlatformID 返回第一个已经开放的招聘平台。 */
function firstOpenPlatformID(configs: PlatformConfigLike[]) {
  return PLATFORM_OPEN_ORDER.find((platformID) =>
    isPlatformOpen(configs, platformID),
  );
}

/** formFromItem 将后端岗位数据转换为编辑表单。 */
function formFromItem(
  item: any,
  defaults: ReturnType<typeof normalizePrompts>,
): PositionForm {
  const common = item.common_config || {};
  const ai = item.ai_config || {};
  return fillPrompts(
    {
      id: item.id || "",
      name: item.name || "",
      platform_id: item.platform_id || "boss",
      mode_default: common.mode_default || "keyword",
      detail_mode: normalizeDetailMode(
        item.platform_id,
        common.detail_mode || "ocr",
      ),
      output_structured_resume: Boolean(common.output_structured_resume),
      keywords: (item.keywords || []).join(" "),
      exclude_keywords: (item.exclude_keywords || []).join(" "),
      is_and_mode: Boolean(item.is_and_mode),
      position_requirement: ai.position_requirement || "",
      hliepin_shortcut_search_name:
        common.hliepin_shortcut_search_name || "",
      open_detail_prompt: normalizePrompt(ai.open_detail_prompt),
      filter_prompt: normalizePrompt(
        ai.greet_prompt || ai.filter_prompt || ai.click_prompt,
      ),
      review_prompt: normalizePrompt(ai.review_prompt),
      detail_score_threshold: Number(ai.detail_score_threshold ?? 60),
      greet_score_threshold: Number(ai.greet_score_threshold ?? 70),
      greet_message: item.greet_message || "",
      description: item.description || "",
      match_limit: Number(item.match_limit ?? 50),
      enable_sound: Boolean(item.enable_sound),
      enable_thinking: Boolean(item.enable_thinking),
    },
    defaults,
  );
}

/** fillPrompts 为岗位空提示词补充系统默认值。 */
function fillPrompts(
  form: PositionForm,
  defaults: ReturnType<typeof normalizePrompts>,
) {
  return {
    ...form,
    open_detail_prompt: form.open_detail_prompt || defaults.open_detail_prompt,
    filter_prompt: form.filter_prompt || defaults.filter_prompt,
    review_prompt: form.review_prompt || "",
  };
}

/** normalizePrompts 统一系统默认提示词字段。 */
function normalizePrompts(value: any) {
  return {
    filter_prompt: normalizePrompt(value?.filter_prompt),
    open_detail_prompt: normalizePrompt(value?.open_detail_prompt),
    review_prompt: normalizePrompt(value?.review_prompt),
  };
}

/** normalizePrompt 还原历史数据中的字面换行。 */
function normalizePrompt(value: unknown) {
  return String(value || "").replace(/\\n/g, "\n");
}

/** normalizeDetailMode 修正平台不支持的详情模式。 */
function normalizeDetailMode(platformID: string, mode: string) {
  if (isDOMOnlyPlatform(platformID)) return "dom";
  if (platformID === "boss" && mode === "dom") return "ocr";
  return ["dom", "ocr", "ai"].includes(mode) ? mode : "ocr";
}

/** defaultCreateDetailMode 返回新增岗位时自动使用的详情识别模式。 */
function defaultCreateDetailMode(platformID: string, memberActive: boolean) {
  if (platformID === "boss") return memberActive ? "ai" : "ocr";
  return "dom";
}

/** defaultCreateMode 返回新增岗位时自动使用的基础筛选模式。 */
function defaultCreateMode(memberActive: boolean) {
  return memberActive ? "ai" : "keyword";
}

/** isDOMOnlyPlatform 判断平台是否只支持 DOM 详情识别。 */
function isDOMOnlyPlatform(platformID: string) {
  return ["hliepin", "liepin", "zhaopin"].includes(platformID);
}

/** splitKeywords 将多种分隔符转换成忽略大小写的去重关键词数组。 */
function splitKeywords(value: string) {
  const seen = new Set<string>();
  return String(value || "")
    .split(/[,\s，、；;]+/)
    .map((item) => item.trim())
    .filter((item) => {
      const key = item.toLowerCase();
      if (!item || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
}

/** detailModeLabel 返回详情模式中文名称。 */
function detailModeLabel(value: string) {
  return value === "dom" ? "DOM识别" : value === "ai" ? "AI识别" : "OCR识别";
}

/** positionStartReason 将岗位启动错误归一为后台可筛选的失败原因。 */
function positionStartReason(message: string) {
  const value = String(message || "").toLowerCase();
  if (value.includes("会员") || value.includes("订阅")) return "subscription_expired";
  if (value.includes("ai") || value.includes("模型") || value.includes("余额") || value.includes("key")) return "ai_config_invalid";
  if (value.includes("登录") || value.includes("cookie")) return "platform_not_logged_in";
  if (value.includes("组件") || value.includes("runtime") || value.includes("node")) return "runtime_missing";
  if (value.includes("版本")) return "agent_version_outdated";
  return "position_start_failed";
}

/** isPositionStartErrorStatus 判断启动弹框状态是否需要使用错误色提醒。 */
function isPositionStartErrorStatus(message: string) {
  return ["没登录", "失败", "没跑完", "余额不足", "版本过低", "订阅", "没开放", "缺少", "超时"]
    .some((keyword) => String(message || "").includes(keyword));
}

/** PositionLogPanel 渲染岗位最近日志和日志操作入口。 */
function PositionLogPanel(props: {
  logs: any[];
  loading: boolean;
  onRefresh: () => void;
  onViewAll: () => void;
  onClear: () => void;
}) {
  const { logs, loading, onRefresh, onViewAll, onClear } = props;
  return (
    <Box
      sx={{
        mt: 1.5,
        border: "1px solid",
        borderColor: "divider",
        borderRadius: "8px",
        bgcolor: "#f7faf8",
        overflow: "hidden",
      }}
    >
      <Stack
        direction={{ xs: "column", sm: "row" }}
        spacing={1}
        sx={{
          px: 1.5,
          py: 1,
          alignItems: { sm: "center" },
          justifyContent: "space-between",
          borderBottom: "1px solid",
          borderColor: "divider",
        }}
      >
        <Stack direction='row' spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}>
          <Typography sx={{ fontSize: 13, fontWeight: 760 }}>
            本地岗位日志（最近 {LOG_LIMIT} 条）
          </Typography>
          <Button size='small' onClick={onViewAll}>查看全部日志</Button>
        </Stack>
        <Stack direction='row' spacing={0.5}>
          <Button size='small' onClick={onRefresh} disabled={loading}>
            {loading ? "刷新中" : "刷新"}
          </Button>
          <Button color='error' size='small' onClick={onClear}>
            清空
          </Button>
        </Stack>
      </Stack>
      <PositionLogList logs={logs} maxHeight={420} />
    </Box>
  );
}

/** PositionLogList 按时间从旧到新展示日志，并在用户位于底部时跟随最新日志。 */
function PositionLogList(props: { logs: any[]; maxHeight: number | string }) {
  const { logs, maxHeight } = props;
  const listRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);
  const orderedLogs = sortPositionLogsOldestFirst(logs);

  useEffect(() => {
    if (!stickToBottomRef.current) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const list = listRef.current;
      if (list) list.scrollTop = list.scrollHeight;
    });
    return () => window.cancelAnimationFrame(frame);
  }, [logs]);

  return (
    <Stack
      ref={listRef}
      spacing={0}
      onScroll={(event) => {
        const list = event.currentTarget;
        stickToBottomRef.current = list.scrollHeight - list.scrollTop - list.clientHeight <= 32;
      }}
      sx={{ p: 1, maxHeight, overflow: "auto" }}
    >
      {orderedLogs.length ? orderedLogs.map((item, index) => (
        <PositionLogLine
          key={String(item.id || `${item.created_at || item.time}-${index}`)}
          item={item}
          previous={index > 0 ? orderedLogs[index - 1] : null}
        />
      )) : (
        <Typography sx={{ py: 4, color: "text.secondary", fontSize: 13, textAlign: "center" }}>
          这里暂时空空的，开始运行后我再认真记账。
        </Typography>
      )}
    </Stack>
  );
}

/** PositionLogLine 渲染单条带中文等级和耗时的岗位日志。 */
function PositionLogLine(props: { item: any; previous: any | null }) {
  const { item, previous } = props;
  const appearance = positionLogAppearance(item.level);
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", md: "190px 82px 72px minmax(0, 1fr)" },
        gap: 1,
        py: 0.75,
        borderBottom: "1px solid",
        borderColor: appearance.error ? "#f0b4b4" : "divider",
        color: appearance.color,
      }}
    >
      <Typography sx={{ color: appearance.color, fontSize: 12 }}>
        {formatPositionLogTime(item.created_at || item.time)}
      </Typography>
      <Typography sx={{ color: appearance.color, fontSize: 12 }}>
        {positionLogDelta(item, previous)}
      </Typography>
      <Typography sx={{ color: appearance.color, fontSize: 12, fontWeight: appearance.weight }}>
        {appearance.label}
      </Typography>
      <Typography sx={{ color: appearance.color, fontSize: 13, fontWeight: appearance.weight, lineHeight: 1.65, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
        {positionLogMessage(item)}
      </Typography>
    </Box>
  );
}

/** sortPositionLogsOldestFirst 按时间从旧到新排列，让最新日志位于底部。 */
function sortPositionLogsOldestFirst(logs: any[]) {
  return [...logs].sort((left, right) => positionLogTimeMs(left.created_at || left.time) - positionLogTimeMs(right.created_at || right.time));
}

/** positionLogMessage 提取兼容新旧结构的日志正文。 */
function positionLogMessage(item: any) {
  return String(item?.message || item?.msg || item?.detail || "");
}

/** positionLogTimeMs 将日志时间转换成用于排序的毫秒时间戳。 */
function positionLogTimeMs(value: unknown) {
  const time = new Date(String(value || "")).getTime();
  return Number.isNaN(time) ? 0 : time;
}

/** formatPositionLogTime 将日志时间格式化到毫秒。 */
function formatPositionLogTime(value: unknown) {
  const date = new Date(String(value || ""));
  if (Number.isNaN(date.getTime())) return "--";
  const pad = (input: number, size = 2) => String(input).padStart(size, "0");
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`;
}

/** positionLogDelta 返回当前日志与上一条日志的时间间隔。 */
function positionLogDelta(item: any, previous: any | null) {
  if (!previous) return "+0ms";
  const currentMs = positionLogTimeMs(item.created_at || item.time);
  const previousMs = positionLogTimeMs(previous.created_at || previous.time);
  if (!currentMs || !previousMs) return "+--ms";
  return `+${Math.max(0, currentMs - previousMs)}ms`;
}

/** buildPositionLogText 构建可复制的完整岗位日志文本。 */
function buildPositionLogText(logs: any[]) {
  const orderedLogs = sortPositionLogsOldestFirst(logs);
  return orderedLogs.map((item, index) => {
    const previous = index > 0 ? orderedLogs[index - 1] : null;
    return `${formatPositionLogTime(item.created_at || item.time)} ${positionLogDelta(item, previous)} ${positionLogAppearance(item.level).label} ${positionLogMessage(item)}`;
  }).join("\n");
}

/** positionLogAppearance 返回岗位日志等级对应的中文分类和文字样式。 */
function positionLogAppearance(value: unknown) {
  const level = String(value || "info").trim().toLowerCase();
  if (level === "error") return { label: "错误", color: "error.main", weight: 760, error: true };
  if (level === "warning" || level === "warn") return { label: "警告", color: "warning.dark", weight: 600, error: false };
  if (level === "debug") return { label: "调试", color: "text.secondary", weight: 400, error: false };
  return { label: "信息", color: "text.primary", weight: 400, error: false };
}
