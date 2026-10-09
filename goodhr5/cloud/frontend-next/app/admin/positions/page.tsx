/** 本文件负责岗位模板完整的新增、编辑、会员校验、模式联动和提示词管理。 */
"use client";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Collapse,
  Divider,
  Flex,
  Form,
  Image,
  Input,
  InputNumber,
  Switch,
  Typography,
} from "antd";
import {
  BulbOutlined,
  CopyOutlined,
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  ExportOutlined,
  LoadingOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  StopOutlined,
} from "@ant-design/icons";
import type { CSSProperties } from "react";
import styles from "../heavy-ui.module.css";
import { useRouter } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";
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
import PositionFloatingStatus, {
  openPositionFloatingWindow,
  type PositionAnalysisStatus,
  type PositionFloatingStatusValue,
} from "@/components/admin/PositionFloatingStatus";
import {
  cloudRequest,
  formatDate,
  getToken,
  localRequest,
} from "@/lib/admin-api";
import { isPlatformOpen, type PlatformConfigLike } from "@/lib/platform-open";
import { reportUserFlow } from "@/lib/user-flow";
import { canUseAI, normalizeSubscription } from "@/lib/subscription";
import {
  confirmPlatformLoggedInForPosition,
  openPlatformPositionBrowser,
  pickPlatformAuthConfig,
} from "@/lib/platform-login";
import {
  evaluatePositionStartGuard,
  latestLocalAgentRelease,
  positionUsesAI,
} from "@/lib/position-start-guard";
const CHROMIUM_ICON_SRC = "/assets/platforms/camoufox.png";
const BOSS_NOTICE_IMAGE_SRC = "/assets/platforms/boss-plugin-notice.jpg";
const HLIEPIN_SHORTCUT_GUIDE_IMAGE_SRC =
  "/assets/help/hliepin-shortcut-search-guide.png";
const PLATFORM_OPEN_ORDER = ["boss", "zhaopin", "hliepin", "liepin"];
const LOG_REFRESH_MS = 3000;
const LOG_LIMIT = 100;
const ALL_LOG_LIMIT = 1000;
/** truncatePositionCardText 按字符保留岗位卡片前 20 个字，超出时追加省略号。 */
function truncatePositionCardText(value: unknown): string {
  const characters = Array.from(String(value ?? ""));
  return characters.length > 20
    ? `${characters.slice(0, 20).join("")}...`
    : characters.join("");
}
/** normalizeFloatingTaskStatus 把本地任务状态转换为置顶小窗支持的明确状态。 */
function normalizeFloatingTaskStatus(
  value: unknown,
): PositionFloatingStatusValue {
  const status = String(value || "")
    .trim()
    .toLowerCase();
  if (
    status === "running" ||
    status === "completed" ||
    status === "stopped" ||
    status === "failed"
  ) {
    return status;
  }
  return "stopped";
}
type PositionForm = ReturnType<typeof createEmptyForm>;
type PositionTaskStats = {
  scanned_count: number;
  greeted_count: number;
  skipped_count: number;
};
type FloatingPositionTask = {
  id: string;
  name: string;
  status: PositionFloatingStatusValue;
  followTask: boolean;
  currentStep: string;
  analysis: PositionAnalysisStatus | null;
  scannedCount: number;
  greetedCount: number;
  skippedCount: number;
};
/** PositionsPage 管理岗位筛选、详情识别和 AI 提示词配置。 */
export default function PositionsPage() {
  const router = useRouter();
  const { user, subscription, notify, confirm, agentBase, onboardingConfig } =
    useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [busyPositionID, setBusyPositionID] = useState("");
  const [expandedLogPositionID, setExpandedLogPositionID] = useState("");
  const [logs, setLogs] = useState<Record<string, any[]>>({});
  const [latestTaskStats, setLatestTaskStats] = useState<
    Record<string, PositionTaskStats>
  >({});
  const [logLoadingPositionID, setLogLoadingPositionID] = useState("");
  const [logSharePositionID, setLogSharePositionID] = useState("");
  const [allLogs, setAllLogs] = useState<any[]>([]);
  const [allLogPosition, setAllLogPosition] = useState<any | null>(null);
  const [allLogLoading, setAllLogLoading] = useState(false);
  const [startPositionItem, setStartPositionItem] = useState<any | null>(null);
  const [startLoading, setStartLoading] = useState(false);
  const [startOpeningPlatform, setStartOpeningPlatform] = useState(false);
  const [startStatus, setStartStatus] = useState("");
  const [startError, setStartError] = useState("");
  const [startRequiresUpdate, setStartRequiresUpdate] = useState(false);
  const [floatingStatusWindow, setFloatingStatusWindow] =
    useState<Window | null>(null);
  const [floatingPositionTask, setFloatingPositionTask] =
    useState<FloatingPositionTask | null>(null);
  const [taskFailure, setTaskFailure] = useState<{
    taskID: string;
    positionName: string;
    message: string;
  } | null>(null);
  const shownTaskFailureIDs = useRef<Set<string>>(new Set());
  const [form, setForm] = useState<PositionForm>(createEmptyForm());
  const [platformConfigs, setPlatformConfigs] = useState<PlatformConfigLike[]>(
    [],
  );
  const [defaults, setDefaults] = useState({
    filter_prompt: "",
    open_detail_prompt: "",
    review_prompt: "",
  });
  const aiMembership = canUseAI(subscription);
  /** load 读取岗位模板和系统默认提示词。 */
  async function load() {
    setLoading(true);
    try {
      const [positions, prompts, platformData] = await Promise.all([
        cloudRequest("/api/positions"),
        cloudRequest("/api/system/default-prompts"),
        cloudRequest("/api/platforms/config/", { auth: false }),
      ]);
      setItems(sortTeamPositions(positions.positions || [], user?.email));
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
    if (!agentBase || items.length === 0) return;
    void loadLatestTaskStats(items);
  }, [agentBase, items.map((item) => item.id).join(","), user?.email]);
  useEffect(() => {
    const expandedPosition = items.find(
      (item) => item.id === expandedLogPositionID,
    );
    if (!expandedPosition || expandedPosition.status !== "running")
      return undefined;
    const timer = window.setInterval(() => {
      void loadPositionLogs(expandedPosition, { silent: true });
    }, LOG_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [agentBase, expandedLogPositionID, items]);
  useEffect(() => {
    if (!agentBase || !floatingPositionTask?.followTask) return undefined;
    const positionID = floatingPositionTask.id;
    let disposed = false;
    /** refreshFloatingPositionStatus 读取本地任务状态并同步到置顶小窗。 */
    async function refreshFloatingPositionStatus() {
      try {
        const task = await localRequest(
          agentBase,
          `/api/v1/local/positions/${encodeURIComponent(positionID)}/status`,
        );
        if (disposed) return;
        const status = normalizeFloatingTaskStatus(task?.status);
        setFloatingPositionTask((current) =>
          current && current.id === positionID
            ? {
                ...current,
                status,
                followTask: status === "running",
                currentStep: String(task?.current_step || "").trim(),
                analysis: nextFloatingAnalysis(
                  current.analysis,
                  normalizeFloatingAnalysis(task?.analysis),
                ),
                scannedCount: Math.max(0, Number(task?.scanned_count) || 0),
                greetedCount: Math.max(0, Number(task?.greeted_count) || 0),
                skippedCount: Math.max(0, Number(task?.skipped_count) || 0),
              }
            : current,
        );
      } catch {
        // 短暂连接失败时保留上一次状态，避免把仍在运行的任务误报为已停止。
      }
    }
    void refreshFloatingPositionStatus();
    const timer = window.setInterval(
      () => void refreshFloatingPositionStatus(),
      LOG_REFRESH_MS,
    );
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [agentBase, floatingPositionTask?.followTask, floatingPositionTask?.id]);
  /** openCreate 使用免费版可用配置打开新增弹框。 */
  function openCreate() {
    const openPlatformID = firstOpenPlatformID(platformConfigs);
    if (!openPlatformID) {
      notify("暂时没有可用招聘平台，请联系作者", "warning");
      return;
    }
    const next = createEmptyForm();
    next.platform_id = openPlatformID;
    next.mode_default = defaultCreateMode(aiMembership);
    next.detail_mode = defaultCreateDetailMode(next.platform_id, aiMembership);
    setForm(fillPrompts(next, defaults));
    setAdvancedOpen(false);
    setDialogOpen(true);
  }
  /** openEdit 将岗位完整字段写入弹框并校验会员功能。 */
  async function openEdit(item: any) {
    const next = formFromItem(item, defaults);
    if (
      !aiMembership &&
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
    if (!Number.isInteger(form.match_limit) || form.match_limit < 1)
      return notify("每次打招呼上限请填写大于 0 的整数", "warning");
    if (
      [
        form.detail_score_threshold,
        form.greet_score_threshold,
        form.request_score_threshold,
      ].some((value) => !Number.isFinite(value) || value < 0 || value > 100)
    )
      return notify("筛选分数请填写 0 到 100 之间的数值", "warning");
    if (!isPlatformOpen(platformConfigs, form.platform_id)) {
      return notify("该平台暂未开放，请联系作者", "warning");
    }
    const detailMode = form.id
      ? normalizeDetailMode(form.platform_id, form.detail_mode)
      : defaultCreateDetailMode(form.platform_id, aiMembership);
    if (!aiMembership && (form.mode_default === "ai" || detailMode === "ai"))
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
            ...(form.platform_id === "hliepin"
              ? {
                  hliepin_hide_viewed: form.hliepin_hide_viewed,
                  hliepin_hide_contacted: form.hliepin_hide_contacted,
                  hliepin_hide_contact_obtained:
                    form.hliepin_hide_contact_obtained,
                }
              : {}),
            request_phone: form.request_phone,
            request_wechat: form.request_wechat,
            request_resume: form.request_resume,
          },
          ai_config: {
            position_requirement: form.position_requirement,
            filter_prompt: form.filter_prompt || defaults.filter_prompt,
            greet_prompt: form.filter_prompt || defaults.filter_prompt,
            click_prompt: form.filter_prompt || defaults.filter_prompt,
            open_detail_prompt:
              form.open_detail_prompt || defaults.open_detail_prompt,
            review_prompt: normalizePrompt(form.review_prompt),
            detail_score_threshold: Number(form.detail_score_threshold ?? 60),
            greet_score_threshold: Number(form.greet_score_threshold ?? 70),
            request_score_threshold: Number(
              form.request_score_threshold ?? form.greet_score_threshold ?? 70,
            ),
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
    setStartError("");
    setStartOpeningPlatform(false);
    setStartRequiresUpdate(false);
    setStartPositionItem(item);
  }
  /** closeStartDialog 清理岗位启动弹框中的运行状态和错误信息。 */
  function closeStartDialog() {
    if (startLoading || startOpeningPlatform) return;
    setStartPositionItem(null);
    setStartStatus("");
    setStartError("");
    setStartOpeningPlatform(false);
    setStartRequiresUpdate(false);
  }
  /** openStartPlatformForFiltering 打开当前岗位对应的招聘平台页面，供用户先手动设置基础筛选条件。 */
  async function openStartPlatformForFiltering() {
    const item = startPositionItem;
    if (!item || !agentBase || startOpeningPlatform) return;
    if (!isPlatformOpen(platformConfigs, item.platform_id)) {
      setStartStatus("这个招聘平台暂时还没开放，请联系作者确认。");
      return;
    }
    setStartOpeningPlatform(true);
    setStartStatus("正在打开当前岗位对应的招聘平台页面...");
    try {
      const auth = pickPlatformAuthConfig(platformConfigs, item.platform_id);
      await openPlatformPositionBrowser(agentBase, item.platform_id, auth);
      setStartStatus(
        "招聘平台已打开。请在平台页面筛选好基础条件，再回来点击“我已筛选好，立即开始”。",
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "打开招聘平台失败";
      setStartStatus(message);
      setStartError(message);
    } finally {
      setStartOpeningPlatform(false);
    }
  }
  /** checkPositionStartGuard 检查 AI 余额和本地程序版本是否满足启动要求。 */
  async function checkPositionStartGuard(item: any) {
    const usesAI = positionUsesAI(item);
    setStartStatus(
      usesAI ? "正在检查 AI 余额和本地程序版本..." : "正在检查本地程序版本...",
    );
    try {
      let runtimeConfig = onboardingConfig;
      if (!latestLocalAgentRelease(runtimeConfig).version) {
        const runtimePayload = await cloudRequest("/api/runtime/config");
        runtimeConfig = runtimePayload.config || runtimePayload || {};
      }
      const [health, walletPayload] = await Promise.all([
        localRequest(agentBase, "/health"),
        usesAI ? cloudRequest("/api/ai-wallet") : Promise.resolve(null),
      ]);
      const release = latestLocalAgentRelease(runtimeConfig);
      const guardFailure = evaluatePositionStartGuard(
        walletPayload?.wallet || walletPayload,
        health.version || health.agent_version,
        release.version,
        usesAI,
      );
      if (guardFailure) {
        await reportUserFlow({
          step: "position_started",
          status: "blocked",
          reason_code: guardFailure.code,
          message: guardFailure.message,
          source: "position_start_guard",
          position_id: item.id,
        }).catch(() => undefined);
        setStartStatus(guardFailure.message);
        setStartRequiresUpdate(guardFailure.code === "agent_version_outdated");
        setStartError(
          guardFailure.code === "agent_version_outdated"
            ? ""
            : guardFailure.message,
        );
        return false;
      }
      setStartRequiresUpdate(false);
      return true;
    } catch (error) {
      const message =
        error instanceof Error
          ? `启动条件检查没跑完：${error.message}。这次我先不乱启动，你刷新后再试一次。`
          : "启动条件检查没跑完，这次我先不乱启动，你刷新后再试一次。";
      setStartStatus(message);
      setStartError(message);
      setStartRequiresUpdate(false);
      await reportUserFlow({
        step: "position_started",
        status: "blocked",
        reason_code: "position_start_guard_unavailable",
        message,
        source: "position_start_guard",
        position_id: item.id,
      }).catch(() => undefined);
      return false;
    }
  }
  /** confirmStartPosition 在确认弹框中完成启动检查、登录确认和岗位启动。 */
  async function confirmStartPosition() {
    if (startRequiresUpdate) {
      window.location.reload();
      return;
    }
    const item = startPositionItem;
    if (!item || !agentBase) return;
    const floatingWindowPromise = openPositionFloatingWindow();
    let started = false;
    setStartLoading(true);
    setBusyPositionID(item.id);
    setStartStatus("正在检查岗位启动条件...");
    setStartError("");
    try {
      const pipWindow = await floatingWindowPromise;
      if (pipWindow) {
        setFloatingStatusWindow(pipWindow);
        setFloatingPositionTask({
          id: item.id,
          name: String(item.name || "当前岗位"),
          status: "running",
          followTask: false,
          currentStep: "正在检查岗位启动条件",
          analysis: null,
          scannedCount: 0,
          greetedCount: 0,
          skippedCount: 0,
        });
      }
      if (!(await checkPositionStartGuard(item))) return;
      const subscriptionData = await cloudRequest("/api/subscription/status");
      const currentSubscription = normalizeSubscription(
        subscriptionData.subscription,
      );
      if (!isPlatformOpen(platformConfigs, item.platform_id)) {
        const message =
          "这个招聘平台暂时还没开放，我先不乱跑，请联系作者看看。";
        setStartStatus(message);
        setStartError(message);
        return;
      }
      const auth = pickPlatformAuthConfig(platformConfigs, item.platform_id);
      setStartStatus("正在打开招聘平台，我先看看账号有没有登录。");
      await openPlatformPositionBrowser(agentBase, item.platform_id, auth);
      try {
        await confirmPlatformLoggedInForPosition(agentBase, auth, (message) =>
          setStartStatus(message),
        );
        await reportUserFlow({
          step: "platform_login_verified",
          source: "position_start",
          position_id: item.id,
        });
      } catch (loginError) {
        const message =
          loginError instanceof Error
            ? loginError.message
            : "招聘平台还没登录，请先在浏览器里完成登录。";
        setStartStatus(message);
        setStartError(message);
        await reportUserFlow({
          step: "platform_login_verified",
          status: "blocked",
          reason_code: "platform_not_logged_in",
          message,
          source: "position_start",
          position_id: item.id,
        }).catch(() => undefined);
        return;
      }
      const usesAI = positionUsesAI(item);
      if (usesAI && !canUseAI(currentSubscription)) {
        const message = "这个岗位用了会员 AI 功能，订阅后我才能继续开工。";
        setStartStatus(message);
        setStartError(message);
        await reportUserFlow({
          step: "position_started",
          status: "blocked",
          reason_code: "subscription_expired",
          message,
          source: "position_start",
          position_id: item.id,
        }).catch(() => undefined);
        return;
      }
      if (!currentSubscription.active)
        notify(
          "当前是免费版，今天的打招呼数量会按免费额度来，我会省着点用。",
          "info",
        );
      setStartStatus("登录确认好了，正在启动岗位...");
      await localRequest(
        agentBase,
        `/api/v1/local/positions/${encodeURIComponent(item.id)}/run`,
        {
          method: "POST",
          body: { token: getToken(), enable_greet: true },
        },
      );
      started = true;
      setFloatingPositionTask((current) =>
        current && current.id === item.id
          ? { ...current, status: "running", followTask: true }
          : current,
      );
      await reportUserFlow({
        step: "position_started",
        source: "position_start",
        position_id: item.id,
      });
      notify("岗位已经开始跑了，我会老实记日志", "success");
      setStartPositionItem(null);
      setStartStatus("");
      setStartError("");
      await Promise.all([load(), loadPositionLogs(item)]);
    } catch (error) {
      const message = error instanceof Error ? error.message : "岗位启动失败";
      setStartStatus(message);
      setStartError(message);
      await reportUserFlow({
        step: "position_started",
        status: "blocked",
        reason_code: positionStartReason(message),
        message,
        source: "position_start",
        position_id: item.id,
      }).catch(() => undefined);
    } finally {
      if (!started) {
        setFloatingPositionTask((current) =>
          current && current.id === item.id
            ? { ...current, status: "stopped", followTask: false }
            : current,
        );
      }
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
      await localRequest(
        agentBase,
        `/api/v1/local/positions/${encodeURIComponent(item.id)}/stop`,
        {
          method: "POST",
          body: { token: getToken() },
          timeoutMS: 220000,
        },
      );
      setFloatingPositionTask((current) =>
        current && current.id === item.id
          ? { ...current, status: "stopped", followTask: false }
          : current,
      );
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
  async function loadPositionLogs(
    item: any,
    options: {
      silent?: boolean;
    } = {},
  ) {
    if (!agentBase) return;
    if (!options.silent) setLogLoadingPositionID(item.id);
    try {
      const data = await localRequest(
        agentBase,
        `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs?limit=${LOG_LIMIT}`,
      );
      setLogs((current) => ({ ...current, [item.id]: data.logs || [] }));
      const task = data.task;
      if (task) {
        setLatestTaskStats((current) => ({
          ...current,
          [item.id]: normalizePositionTaskStats(task),
        }));
      }
      const taskStatus = String(task?.status || "").trim();
      if (taskStatus) {
        setItems((current) => {
          let changed = false;
          const next = current.map((position) => {
            if (position.id !== item.id || position.status === taskStatus)
              return position;
            changed = true;
            return { ...position, status: taskStatus };
          });
          return changed ? next : current;
        });
        if (item.status === "running" && taskStatus !== "running") {
          void load();
        }
      }
      const taskID = String(task?.task_id || "").trim();
      const errorMessage = String(task?.error_message || "").trim();
      if (
        taskStatus === "failed" &&
        taskID &&
        errorMessage &&
        !shownTaskFailureIDs.current.has(taskID)
      ) {
        shownTaskFailureIDs.current.add(taskID);
        setTaskFailure({
          taskID,
          positionName: String(item.name || "当前岗位"),
          message: errorMessage,
        });
      }
    } catch (error) {
      if (!options.silent)
        notify(
          error instanceof Error ? error.message : "日志读取失败",
          "error",
        );
    } finally {
      if (!options.silent) setLogLoadingPositionID("");
    }
  }
  /** loadLatestTaskStats 读取各岗位最近一次本地任务统计。 */
  async function loadLatestTaskStats(positionItems: any[]) {
    if (!agentBase) return;
    const next: Record<string, PositionTaskStats> = {};
    await Promise.all(
      positionItems
        .filter((item) => isCurrentUserPosition(item, user?.email))
        .map(async (item) => {
          try {
            const task = await localRequest(
              agentBase,
              `/api/v1/local/positions/${encodeURIComponent(item.id)}/status`,
            );
            next[item.id] = normalizePositionTaskStats(task);
          } catch {
            // 没有本地任务记录时保留零值，不能影响岗位列表加载。
          }
        }),
    );
    setLatestTaskStats(next);
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
      await localRequest(
        agentBase,
        `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs`,
        {
          method: "DELETE",
        },
      );
      setLogs((current) => ({ ...current, [item.id]: [] }));
      if (allLogPosition?.id === item.id) setAllLogs([]);
      notify("岗位日志已经清空，我把小本本翻到新的一页了。", "success");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "日志没清空成功，我们再试一次。",
        "error",
      );
    }
  }
  /** loadAllPositionLogs 读取指定岗位保留的全部本地日志并打开弹框。 */
  async function loadAllPositionLogs(item: any) {
    if (!agentBase) return notify("本地程序还没连上", "warning");
    setAllLogPosition(item);
    setAllLogLoading(true);
    try {
      const data = await localRequest(
        agentBase,
        `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs?limit=${ALL_LOG_LIMIT}`,
      );
      setAllLogs(data.logs || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "全部日志读取失败",
        "error",
      );
    } finally {
      setAllLogLoading(false);
    }
  }
  /** copyPositionDiagnosticLink 先同步本地日志，再复制七天有效的只读排障链接。 */
  async function copyPositionDiagnosticLink(item: any) {
    setLogSharePositionID(item.id);
    try {
      if (agentBase) {
        await localRequest(
          agentBase,
          `/api/v1/local/positions/${encodeURIComponent(item.id)}/logs/upload`,
          {
            method: "POST",
            body: { token: getToken() },
            timeoutMS: 30000,
          },
        );
      }
      const result = await cloudRequest(
        `/api/positions/${encodeURIComponent(item.id)}/logs/share`,
        { method: "POST" },
      );
      const token = String(result.token || "");
      if (!/^[a-f0-9]{64}$/.test(token))
        throw new Error("排障链接没有生成成功，请再试一次");
      await navigator.clipboard.writeText(
        `${window.location.origin}/diagnostics/${token}`,
      );
      notify(
        `排障链接已复制，发给作者就能查看这 ${Number(result.count || 0)} 条日志；7 天内有效。`,
        "success",
      );
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "排障链接没生成成功，我们再试一次。",
        "error",
      );
    } finally {
      setLogSharePositionID("");
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
    if (value === "ai" && !aiMembership) return requireMembership();
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
    if (value === "ai" && !aiMembership) return requireMembership();
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
        : defaultCreateDetailMode(value, aiMembership),
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
      <PositionFloatingStatus
        pipWindow={floatingStatusWindow}
        status={floatingPositionTask?.status || "stopped"}
        currentStep={floatingPositionTask?.currentStep || ""}
        analysis={floatingPositionTask?.analysis || null}
        scannedCount={floatingPositionTask?.scannedCount || 0}
        greetedCount={floatingPositionTask?.greetedCount || 0}
        skippedCount={floatingPositionTask?.skippedCount || 0}
        onClosed={() => {
          setFloatingStatusWindow(null);
          setFloatingPositionTask(null);
        }}
      />
      <PageHeader
        title="岗位管理"
        description="岗位模板决定首次筛选、详情识别和最终打招呼判断。"
        actions={
          <>
            <Button
              disabled={loading}
              onClick={openCreate}
              type={"primary"}
              icon={<PlusOutlined aria-hidden="true" />}
            >
              新建岗位
            </Button>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      {items.length ? (
        <Flex className={styles.s1}>
          {items.map((item) => (
            <Card
              key={item.id}
              styles={{
                body: {
                  padding: 0,
                  height: "100%",
                  flex: 1,
                  display: "flex",
                  flexDirection: "column",
                },
              }}
              className={styles.s2}
            >
              <Flex className={styles.s3}>
                <PlatformLogo platformID={item.platform_id} size={42} />
                <Flex className={styles.s4}>
                  <div className={styles.s5}>
                    <Typography.Paragraph
                      title={String(item.name || "")}
                      className={`${styles.text} ${styles.s6}`}
                    >
                      {truncatePositionCardText(item.name)}
                    </Typography.Paragraph>
                    <Typography.Paragraph
                      className={`${styles.text} ${styles.s7}`}
                    >
                      {platformLabel(item.platform_id)}·{" "}
                      {item.common_config?.mode_default === "ai"
                        ? "AI 筛选"
                        : "关键词筛选"}{" "}
                      · 详情：
                      {detailModeLabel(item.common_config?.detail_mode)}·
                      关键词：
                      <span title={(item.keywords || []).join(" / ") || "无"}>
                        {truncatePositionCardText(
                          (item.keywords || []).join(" / ") || "无",
                        )}
                      </span>
                    </Typography.Paragraph>
                  </div>
                  {isCurrentUserPosition(item, user?.email) ? (
                    <Flex className={styles.s8}>
                      {item.status === "running" ? (
                        <Button
                          disabled={busyPositionID === item.id}
                          onClick={() => void stopPosition(item)}
                          type={"primary"}
                          danger
                          icon={<StopOutlined aria-hidden="true" />}
                        >
                          停止
                        </Button>
                      ) : (
                        <Button
                          disabled={busyPositionID === item.id}
                          onClick={() => openStartPosition(item)}
                          type={"primary"}
                          icon={<PlayCircleOutlined aria-hidden="true" />}
                        >
                          开始
                        </Button>
                      )}
                      <Button
                        onClick={() => void togglePositionLogs(item)}
                        type={"text"}
                      >
                        {expandedLogPositionID === item.id
                          ? "收起日志"
                          : "日志"}
                      </Button>
                      <Button
                        onClick={() => void openEdit(item)}
                        type={"text"}
                        icon={<EditOutlined aria-hidden="true" />}
                      >
                        编辑
                      </Button>
                      <Button
                        onClick={() => void remove(item)}
                        type={"text"}
                        danger
                        icon={<DeleteOutlined aria-hidden="true" />}
                      >
                        删除
                      </Button>
                    </Flex>
                  ) : (
                    <Typography.Paragraph
                      className={`${styles.text} ${styles.s9}`}
                    >
                      团队岗位，仅查看
                    </Typography.Paragraph>
                  )}
                </Flex>
              </Flex>
              <div className={styles.s10}>
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s11}`}
                >
                  今日 {item.today_greeted_count || 0}
                  {isCurrentUserPosition(item, user?.email) ? (
                    <>
                      {" "}
                      · 本次（扫描{" "}
                      {latestTaskStats[item.id]?.scanned_count || 0} · 打招呼{" "}
                      {latestTaskStats[item.id]?.greeted_count || 0} · 跳过{" "}
                      {latestTaskStats[item.id]?.skipped_count || 0}）
                    </>
                  ) : null}
                </Typography.Paragraph>
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s12}`}
                >
                  创建人：{item.creator_email || "当前账号"}· 创建于：
                  {formatDate(item.created_at) || "--"}
                </Typography.Paragraph>
              </div>
              <>
                {isCurrentUserPosition(item, user?.email) &&
                expandedLogPositionID === item.id ? (
                  <>
                    <PositionLogPanel
                      logs={logs[item.id] || []}
                      loading={logLoadingPositionID === item.id}
                      sharing={logSharePositionID === item.id}
                      onRefresh={() => void loadPositionLogs(item)}
                      onViewAll={() => void loadAllPositionLogs(item)}
                      onShare={() => void copyPositionDiagnosticLink(item)}
                      onClear={() => void clearPositionLogs(item)}
                    />
                  </>
                ) : null}
              </>
            </Card>
          ))}
        </Flex>
      ) : (
        <SectionPanel>
          <EmptyState text="暂无岗位模板" />
        </SectionPanel>
      )}
      <AdminDialog
        open={Boolean(startPositionItem)}
        title={startError ? "岗位还没启动成功" : "开始招聘岗位"}
        confirmText={
          startError
            ? "我知道了"
            : startRequiresUpdate
              ? "立即更新"
              : "我已筛选好，立即开始"
        }
        showCancel={!startError}
        loading={startLoading}
        loadingText="启动中"
        onClose={closeStartDialog}
        onConfirm={() =>
          startError ? closeStartDialog() : void confirmStartPosition()
        }
      >
        {startError ? (
          <Alert
            type={"error"}
            showIcon
            description={
              <>
                {" "}
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s13}`}
                >
                  {startError}
                </Typography.Paragraph>
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s14}`}
                >
                  岗位没有偷偷开跑。按上面的提示处理好后，再点一次开始就行。
                </Typography.Paragraph>{" "}
              </>
            }
          />
        ) : (
          <Flex className={styles.s15}>
            <Typography.Paragraph className={styles.text}>
              确认开始“{startPositionItem?.name || ""}”吗？
            </Typography.Paragraph>
            <Alert
              type={"warning"}
              showIcon
              description={
                <>
                  {" "}
                  <Typography.Paragraph
                    className={`${styles.text} ${styles.s16}`}
                  >
                    强烈建议您先点击下方“打开平台，并筛选条件”，在招聘平台中设置好年龄、学历、地区等基础筛选条件，再回来开始任务。基础筛选会直接影响候选人结果，此步骤非常重要。
                  </Typography.Paragraph>{" "}
                </>
              }
            />
            <Button
              size="large"
              disabled={startLoading || startOpeningPlatform}
              onClick={() => void openStartPlatformForFiltering()}
              type={"default"}
              block
              icon={
                startOpeningPlatform ? (
                  <LoadingOutlined
                    aria-hidden="true"
                    spin
                    className={styles.s18}
                  />
                ) : (
                  <ExportOutlined aria-hidden="true" />
                )
              }
              className={styles.s19}
            >
              {startOpeningPlatform
                ? "正在打开招聘平台"
                : "打开平台，并筛选条件"}
            </Button>
            <div className={styles.s20}>
              {startStatus ? (
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s21}`}
                  style={
                    {
                      "--heavy-1": isPositionStartErrorStatus(startStatus)
                        ? "#cf1322"
                        : "var(--goodhr-text-secondary)",
                    } as CSSProperties
                  }
                >
                  {startStatus}
                </Typography.Paragraph>
              ) : null}
            </div>
          </Flex>
        )}
      </AdminDialog>
      <AdminDialog
        open={Boolean(taskFailure)}
        title="岗位已经停下来了"
        confirmText="我知道了"
        showCancel={false}
        onClose={() => setTaskFailure(null)}
        onConfirm={() => setTaskFailure(null)}
      >
        <Alert
          type={"error"}
          showIcon
          description={
            <>
              {" "}
              <Typography.Paragraph className={`${styles.text} ${styles.s22}`}>
                “{taskFailure?.positionName || "当前岗位"}
                ”运行时遇到了问题，已经安全停止。
              </Typography.Paragraph>
              <Typography.Paragraph className={`${styles.text} ${styles.s23}`}>
                {taskFailure?.message || "错误原因已经记到岗位日志里。"}
              </Typography.Paragraph>{" "}
            </>
          }
        />
      </AdminDialog>
      <AdminDialog
        open={Boolean(allLogPosition)}
        title="查看全部岗位日志"
        description={`读取当前岗位已保留的全部日志，单次最多 ${ALL_LOG_LIMIT} 条，可复制后发给作者排查。`}
        confirmText="复制全部"
        cancelText="关闭"
        loading={allLogLoading}
        maxWidth="lg"
        onClose={() => {
          setAllLogPosition(null);
          setAllLogs([]);
        }}
        onConfirm={() => void copyAllPositionLogs()}
      >
        <PositionLogList logs={allLogs} maxHeight="60vh" />
      </AdminDialog>
      <AdminDialog
        open={dialogOpen}
        title={form.id ? "编辑岗位模板" : "新建岗位模板"}
        description="按运行顺序填写。只有当前选择模式需要的字段会显示。"
        maxWidth="md"
        confirmText={form.id ? "保存修改" : "创建岗位"}
        loading={loading}
        confirmDisabled={!form.name.trim()}
        onClose={() => setDialogOpen(false)}
        onConfirm={() => void save()}
      >
        <Flex className={styles.s24}>
          <Alert
            type={"info"}
            showIcon
            description={
              <>
                {" "}
                运行时先读取候选人基础信息，完成第一次筛选并决定是否打开详情；读取详情后再进行第二次分析，决定是否打招呼。请按这个顺序配置下面的内容。
              </>
            }
          />
          <div>
            <Typography.Title
              level={3}
              className={`${styles.title} ${styles.s25}`}
            >
              基础信息
            </Typography.Title>
            <Form.Item
              layout="vertical"
              htmlFor="positions-field-1"
              label={"岗位名称"}
              extra={
                <>
                  {" "}
                  <Typography.Text type="danger">
                    {
                      "岗位名称必须和平台岗位岗位名称保持一致。(请前往招聘平台复制岗位名称)"
                    }
                  </Typography.Text>{" "}
                </>
              }
              style={{ marginBottom: 0 }}
            >
              <Input
                value={form.name}
                onChange={(event) =>
                  setForm({ ...form, name: event.target.value })
                }
                placeholder="例如：服装带货主播"
                id="positions-field-1"
                className={styles.s27}
              />
            </Form.Item>
            <Flex className={styles.s28}>
              <Form.Item
                layout="vertical"
                htmlFor="positions-field-2"
                label={"每次打招呼上限"}
                className={styles.s29}
                style={{ marginBottom: 0 }}
              >
                <InputNumber
                  precision={0}
                  value={form.match_limit}
                  onChange={(value) =>
                    setForm({
                      ...form,
                      match_limit: Number(String(value ?? "") || 0),
                    })
                  }
                  id="positions-field-2"
                  min={1}
                  className={styles.s30}
                />
              </Form.Item>
              <PositionSwitchOption
                checked={form.enable_sound}
                label="打招呼成功提示音"
                description="开启后，每次成功打招呼会播放提示音，不用一直盯着页面。"
                onChange={(checked) =>
                  setForm({ ...form, enable_sound: checked })
                }
              />
              <PositionSwitchOption
                checked={form.enable_thinking}
                label="思考模式"
                description="开启后 AI 会进行更深入的分析，判断通常更准确；但处理时间更久，AI 消耗也更高。"
                onChange={(checked) =>
                  setForm({ ...form, enable_thinking: checked })
                }
              />
            </Flex>
          </div>
          <ChoiceCards
            label={form.id ? "招聘平台（编辑岗位时不可更换）" : "招聘平台"}
            value={form.platform_id}
            columns={3}
            autoWidth
            onChange={(value) => selectPlatform(String(value))}
            options={[
              {
                value: "boss",
                label: "Boss直聘",
                disabled:
                  Boolean(form.id) || !isPlatformOpen(platformConfigs, "boss"),
                description: isPlatformOpen(platformConfigs, "boss")
                  ? "支持 OCR 和 AI 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("boss"),
              },
              {
                value: "zhaopin",
                label: "智联招聘",
                disabled:
                  Boolean(form.id) ||
                  !isPlatformOpen(platformConfigs, "zhaopin"),
                description: isPlatformOpen(platformConfigs, "zhaopin")
                  ? "支持 DOM 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("zhaopin"),
              },
              {
                value: "hliepin",
                label: "猎聘猎头端",
                disabled:
                  Boolean(form.id) ||
                  !isPlatformOpen(platformConfigs, "hliepin"),
                description: isPlatformOpen(platformConfigs, "hliepin")
                  ? "支持 DOM 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("hliepin"),
              },
              {
                value: "liepin",
                label: "猎聘企业端",
                disabled:
                  Boolean(form.id) ||
                  !isPlatformOpen(platformConfigs, "liepin"),
                description: isPlatformOpen(platformConfigs, "liepin")
                  ? "支持 DOM 详情识别。"
                  : "暂未开放",
                iconSrc: platformIconSrc("liepin"),
              },
            ]}
          />
          <Card
            styles={{
              body: {
                padding: 0,
                height: "100%",
                flex: 1,
                display: "flex",
                flexDirection: "column",
              },
            }}
            className={styles.s31}
          >
            <Typography.Paragraph className={`${styles.text} ${styles.s32}`}>
              平台提示（不用选择）
            </Typography.Paragraph>
            <div
              className={styles.s33}
              style={
                {
                  "--heavy-2": form.platform_id === "boss" ? "1fr" : "1fr",
                  "--heavy-3": form.platform_id === "boss" ? "1fr" : "1fr",
                  "--heavy-4":
                    form.platform_id === "boss"
                      ? "minmax(0, .9fr) minmax(0, 1.1fr)"
                      : "1fr",
                  "--heavy-5":
                    form.platform_id === "boss"
                      ? "minmax(0, .9fr) minmax(0, 1.1fr)"
                      : "1fr",
                  "--heavy-6":
                    form.platform_id === "boss"
                      ? "minmax(0, .9fr) minmax(0, 1.1fr)"
                      : "1fr",
                } as CSSProperties
              }
            >
              <PlatformTipCard
                iconSrc={CHROMIUM_ICON_SRC}
                title="浏览器图标"
                text="岗位打开后，点击任务栏里的橙色狐狸浏览器图标，完成对应平台登录。"
              />
              {form.platform_id === "boss" ? (
                <PlatformTipCard
                  imageSrc={BOSS_NOTICE_IMAGE_SRC}
                  title="BOSS 插件、外挂 提示"
                  text="很多账号会提示插件、外挂等招聘辅助工具，这是平台通用公告，不等于封号。点“我已知晓”即可，别高频操作。"
                />
              ) : null}
            </div>
          </Card>
          <ChoiceCards
            label="基础信息筛选模式   (决定是否打开查看详情)"
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
              <Typography.Paragraph className={`${styles.text} ${styles.s34}`}>
                选择哪种详情方式就只使用哪一种：DOM 最快，OCR
                在本地识别截图文字，AI 能理解完整页面但耗时更长。
              </Typography.Paragraph>
              <ChoiceCards
                label="详情信息筛选模式  (决定是否打招呼)"
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
              <div>
                <Typography.Title
                  level={3}
                  className={`${styles.title} ${styles.s35}`}
                >
                  关键词筛选
                </Typography.Title>
                <Flex className={styles.s36}>
                  <ChoiceCards
                    label="匹配方式"
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
                  <Typography.Paragraph
                    className={`${styles.text} ${styles.s37}`}
                  >
                    设置关键词或排除词后，会读取详情再判断，避免漏掉简历里的信息。满足任一关键词更宽松，必须同时满足则更严格。
                  </Typography.Paragraph>
                  <div className={styles.s38}>
                    <Form.Item
                      layout="vertical"
                      htmlFor="positions-field-3"
                      label={"关键词"}
                      extra={
                        <> {"支持空格、中文逗号、英文逗号或换行分隔。"} </>
                      }
                      style={{ marginBottom: 0 }}
                    >
                      <Input.TextArea
                        value={form.keywords}
                        onChange={(event) =>
                          setForm({ ...form, keywords: event.target.value })
                        }
                        id="positions-field-3"
                        autoSize={{ minRows: 3, maxRows: 16 }}
                        className={styles.s40}
                      />
                    </Form.Item>
                    <Form.Item
                      layout="vertical"
                      htmlFor="positions-field-4"
                      label={"排除词"}
                      extra={<> {"命中排除词后直接跳过。"} </>}
                      style={{ marginBottom: 0 }}
                    >
                      <Input.TextArea
                        value={form.exclude_keywords}
                        onChange={(event) =>
                          setForm({
                            ...form,
                            exclude_keywords: event.target.value,
                          })
                        }
                        id="positions-field-4"
                        autoSize={{ minRows: 3, maxRows: 16 }}
                        className={styles.s42}
                      />
                    </Form.Item>
                  </div>
                  {form.platform_id === "hliepin" ? (
                    <Flex className={styles.s43}>
                      <HLiepinHiddenCandidateFilters
                        hideViewed={form.hliepin_hide_viewed}
                        hideContacted={form.hliepin_hide_contacted}
                        hideContactObtained={form.hliepin_hide_contact_obtained}
                        onChange={(field, checked) =>
                          setForm({ ...form, [field]: checked })
                        }
                      />
                      <Form.Item
                        layout="vertical"
                        htmlFor="positions-field-5"
                        label={"猎聘快捷搜索名"}
                        extra={
                          <>
                            {" "}
                            {
                              "填写后，岗位运行会直接选择猎聘页面中完全同名的快捷搜索，不再输入搜索关键词；如果不填，则使用正在发布的岗位进行匹配。"
                            }{" "}
                          </>
                        }
                        style={{ marginBottom: 0 }}
                      >
                        <Input
                          value={form.hliepin_shortcut_search_name}
                          onChange={(event) =>
                            setForm({
                              ...form,
                              hliepin_shortcut_search_name: event.target.value,
                            })
                          }
                          placeholder="请填写猎聘搜索页已保存的快捷搜索名称"
                          id="positions-field-5"
                          className={styles.s45}
                        />
                      </Form.Item>
                      <HLiepinShortcutSearchGuide
                        visible={Boolean(
                          form.hliepin_shortcut_search_name.trim(),
                        )}
                      />
                    </Flex>
                  ) : null}
                </Flex>
              </div>
            </>
          ) : null}
          {form.mode_default === "ai" ? (
            <>
              <Divider />
              <div>
                <Flex className={styles.s46}>
                  <div>
                    <Typography.Title
                      level={3}
                      className={`${styles.title} ${styles.s47}`}
                    >
                      AI 配置
                    </Typography.Title>
                    <Typography.Paragraph
                      className={`${styles.text} ${styles.s48}`}
                    >
                      请将JD岗位要求复制到“岗位要求”中，点击“AI
                      优化岗位要求”按钮，AI会自动优化。
                    </Typography.Paragraph>
                  </div>
                  <Button
                    disabled={optimizing || !form.position_requirement.trim()}
                    onClick={() => void optimizeRequirement()}
                    type={"text"}
                    icon={
                      optimizing ? (
                        <LoadingOutlined
                          aria-hidden="true"
                          spin
                          className={styles.s50}
                        />
                      ) : (
                        <BulbOutlined aria-hidden="true" />
                      )
                    }
                  >
                    {optimizing ? "分析中..." : "AI 优化岗位要求"}
                  </Button>
                </Flex>
                <Flex className={styles.s51}>
                  <Form.Item
                    layout="vertical"
                    htmlFor="positions-field-6"
                    label={"岗位要求"}
                    extra={
                      <>
                        {" "}
                        {
                          "建议写清学历、经验、技能、行业、城市、到岗状态和明确的淘汰条件；不要填写“有上进心”等无法从简历判断的内容。"
                        }{" "}
                      </>
                    }
                    style={{ marginBottom: 0 }}
                  >
                    <Input.TextArea
                      value={form.position_requirement}
                      onChange={(event) =>
                        setForm({
                          ...form,
                          position_requirement: event.target.value,
                        })
                      }
                      placeholder="必须有3年以上教学经验，必须有教师资格证，学历年龄 等基础条件可以在平台提前筛选好，更不要写跟岗位要求无关的 比如 岗位福利，工作环境等。"
                      id="positions-field-6"
                      autoSize={{ minRows: 7, maxRows: 16 }}
                      className={styles.s53}
                    />
                  </Form.Item>
                  {form.platform_id === "hliepin" ? (
                    <Flex className={styles.s54}>
                      <HLiepinHiddenCandidateFilters
                        hideViewed={form.hliepin_hide_viewed}
                        hideContacted={form.hliepin_hide_contacted}
                        hideContactObtained={form.hliepin_hide_contact_obtained}
                        onChange={(field, checked) =>
                          setForm({ ...form, [field]: checked })
                        }
                      />
                      <Form.Item
                        layout="vertical"
                        htmlFor="positions-field-7"
                        label={"猎聘快捷搜索名"}
                        extra={
                          <>
                            {" "}
                            {
                              "填写后，岗位运行会直接选择猎聘页面中完全同名的快捷搜索，不再输入搜索关键词；如果不填，则使用正在发布的岗位进行匹配。它不参与本地简历的 AI 判断。"
                            }{" "}
                          </>
                        }
                        style={{ marginBottom: 0 }}
                      >
                        <Input
                          value={form.hliepin_shortcut_search_name}
                          onChange={(event) =>
                            setForm({
                              ...form,
                              hliepin_shortcut_search_name: event.target.value,
                            })
                          }
                          placeholder="请填写猎聘搜索页已保存的快捷搜索名称"
                          id="positions-field-7"
                          className={styles.s56}
                        />
                      </Form.Item>
                      <HLiepinShortcutSearchGuide
                        visible={Boolean(
                          form.hliepin_shortcut_search_name.trim(),
                        )}
                      />
                    </Flex>
                  ) : null}
                  <div className={styles.s57}>
                    <Typography.Paragraph
                      className={`${styles.text} ${styles.s58}`}
                    >
                      强烈建议 先看右上角的视频教程，了解各项参数的意义
                    </Typography.Paragraph>
                    <Typography.Paragraph
                      className={`${styles.text} ${styles.s59}`}
                    >
                      求职意向必须匹配目标岗位；具备 3
                      年以上相关经验；拥有岗位要求的证书或技能；当前状态满足到岗要求。薪资越高或岗位越重要，条件应写得越明确。
                    </Typography.Paragraph>
                  </div>
                  <Collapse
                    activeKey={advancedOpen ? ["advanced"] : []}
                    onChange={(keys) =>
                      setAdvancedOpen(keys.includes("advanced"))
                    }
                    expandIconPlacement="end"
                    items={[
                      {
                        key: "advanced",
                        label: (
                          <Flex vertical gap={4}>
                            <Typography.Text strong>高级设置</Typography.Text>
                            <Typography.Paragraph
                              className={`${styles.text} ${styles.s65}`}
                            >
                              这里是增加 AI
                              准确率的各项设置。如果不理解，先别改它，问题不大。也可以点右上角“视频教程”，看完再回来慢慢调。
                            </Typography.Paragraph>
                          </Flex>
                        ),
                        children: (
                          <Flex vertical gap={16}>
                            <PromptField
                              label="打开详情提示词（一般不需要修改）"
                              value={form.open_detail_prompt}
                              defaultValue={defaults.open_detail_prompt}
                              description="只用于第一次分析，判断候选人是否值得打开详情。普通岗位可以宽松一些，高级岗位可以更严格。"
                              onChange={(value) =>
                                setForm({ ...form, open_detail_prompt: value })
                              }
                            />
                            <Form.Item
                              layout="vertical"
                              htmlFor="positions-field-8"
                              label={"看详情阈值分"}
                              extra={
                                <>
                                  {" "}
                                  {
                                    "首次评分大于等于该值时打开候选人详情。"
                                  }{" "}
                                </>
                              }
                              style={{ marginBottom: 0 }}
                            >
                              <InputNumber
                                precision={0}
                                value={form.detail_score_threshold}
                                onChange={(value) =>
                                  setForm({
                                    ...form,
                                    detail_score_threshold: Number(
                                      String(value ?? ""),
                                    ),
                                  })
                                }
                                id="positions-field-8"
                                min={0}
                                max={100}
                                className={styles.s68}
                              />
                            </Form.Item>
                            <ChoiceCards
                              label="是否生成简历"
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
                                  description:
                                    "AI消耗多，会把信息放到简历库里。",
                                },
                              ]}
                            />
                            <PromptField
                              label="打招呼提示词（一般不需要修改）"
                              value={form.filter_prompt}
                              defaultValue={defaults.filter_prompt}
                              description="用于详情分析并决定候选人的最终分数，直接影响是否执行打招呼。"
                              onChange={(value) =>
                                setForm({ ...form, filter_prompt: value })
                              }
                            />
                            <Form.Item
                              layout="vertical"
                              htmlFor="positions-field-9"
                              label={"打招呼阈值分"}
                              extra={
                                <> {"详情评分大于等于该值时执行打招呼。"} </>
                              }
                              style={{ marginBottom: 0 }}
                            >
                              <InputNumber
                                precision={0}
                                value={form.greet_score_threshold}
                                onChange={(value) => {
                                  const nextThreshold = Number(
                                    String(value ?? ""),
                                  );
                                  setForm({
                                    ...form,
                                    greet_score_threshold: nextThreshold,
                                    request_score_threshold:
                                      form.request_score_threshold ===
                                      form.greet_score_threshold
                                        ? nextThreshold
                                        : form.request_score_threshold,
                                  });
                                }}
                                id="positions-field-9"
                                min={0}
                                max={100}
                                className={styles.s70}
                              />
                            </Form.Item>
                            <PromptField
                              label="复核提示词（可选）（一般不需要修改）"
                              value={form.review_prompt}
                              defaultValue=""
                              defaultActionLabel="清空"
                              emptyPlaceholder="可留空，不填写则不会触发复核"
                              description="当详情分数接近打招呼阈值时执行二次复核；留空则不会触发复核。"
                              onChange={(value) =>
                                setForm({ ...form, review_prompt: value })
                              }
                            />
                          </Flex>
                        ),
                      },
                    ]}
                  />
                </Flex>
              </div>
            </>
          ) : null}
          <Divider />
          <div>
            <Typography.Title
              level={3}
              className={`${styles.title} ${styles.s71}`}
            >
              可选信息
            </Typography.Title>
            <div className={styles.s72}>
              <div className={styles.s73}>
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s74}`}
                >
                  打招呼后自动索要
                </Typography.Paragraph>
                <Flex className={styles.s75}>
                  <Checkbox
                    checked={form.request_phone}
                    onChange={(event) =>
                      setForm({ ...form, request_phone: event.target.checked })
                    }
                  >
                    {"索要电话"}
                  </Checkbox>
                  <Checkbox
                    checked={form.request_wechat}
                    onChange={(event) =>
                      setForm({ ...form, request_wechat: event.target.checked })
                    }
                  >
                    {"索要微信"}
                  </Checkbox>
                  <Checkbox
                    checked={form.request_resume}
                    onChange={(event) =>
                      setForm({ ...form, request_resume: event.target.checked })
                    }
                  >
                    {"索要简历"}
                  </Checkbox>
                </Flex>
                <Typography.Paragraph
                  className={`${styles.text} ${styles.s76}`}
                >
                  当前智联招聘和猎聘猎头端已实现；只有最终 AI
                  评分严格大于索要分数时，才会执行已勾选的索要项。
                </Typography.Paragraph>
                <Form.Item
                  layout="vertical"
                  htmlFor="positions-field-10"
                  label={"索要分数"}
                  extra={
                    <>
                      {" "}
                      {
                        "候选人最终 AI 评分必须严格大于该值才执行索要；没有 AI 评分时不会索要。默认与打招呼阈值分相同。"
                      }{" "}
                    </>
                  }
                  className={styles.s77}
                  style={{ marginBottom: 0 }}
                >
                  <InputNumber
                    precision={0}
                    value={form.request_score_threshold}
                    onChange={(value) =>
                      setForm({
                        ...form,
                        request_score_threshold: Number(String(value ?? "")),
                      })
                    }
                    id="positions-field-10"
                    min={0}
                    max={100}
                    className={styles.s78}
                  />
                </Form.Item>
              </div>
              <Form.Item
                layout="vertical"
                htmlFor="positions-field-11"
                label={"首次打招呼语（可选）"}
                style={{ marginBottom: 0 }}
              >
                <Input.TextArea
                  value={form.greet_message}
                  onChange={(event) =>
                    setForm({ ...form, greet_message: event.target.value })
                  }
                  id="positions-field-11"
                  autoSize={{ minRows: 3, maxRows: 16 }}
                  className={styles.s80}
                />
              </Form.Item>
              <Form.Item
                layout="vertical"
                htmlFor="positions-field-12"
                label={"岗位描述 暂时不填"}
                style={{ marginBottom: 0 }}
              >
                <Input.TextArea
                  value={form.description}
                  onChange={(event) =>
                    setForm({ ...form, description: event.target.value })
                  }
                  id="positions-field-12"
                  autoSize={{ minRows: 3, maxRows: 16 }}
                  className={styles.s82}
                />
              </Form.Item>
            </div>
          </div>
          {!aiMembership &&
          (form.mode_default === "ai" ||
            (form.id && form.detail_mode === "ai")) ? (
            <Alert
              type={"warning"}
              showIcon
              description={
                <>
                  {" "}
                  当前会员已到期，AI 选项无法保存。可以改为关键词筛选和 OCR
                  识别。
                </>
              }
            />
          ) : null}
        </Flex>
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
  const fieldID = useId();
  return (
    <div>
      <Flex className={styles.s83}>
        <Typography.Paragraph className={`${styles.text} ${styles.s84}`}>
          {label}
        </Typography.Paragraph>
        <Button
          size="small"
          onClick={() => onChange(defaultValue)}
          type={"text"}
          icon={<ReloadOutlined aria-hidden="true" />}
        >
          {defaultActionLabel}
        </Button>
      </Flex>
      <Form.Item
        layout="vertical"
        htmlFor={fieldID}
        style={{ marginBottom: 0 }}
      >
        <Input.TextArea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={defaultValue ? "已加载系统默认提示词" : emptyPlaceholder}
          id={fieldID}
          aria-label={label}
          autoSize={{ minRows: 6, maxRows: 16 }}
          className={styles.s86}
        />
      </Form.Item>
      <Typography.Paragraph className={`${styles.text} ${styles.s87}`}>
        {description}
      </Typography.Paragraph>
    </div>
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
    <div
      className={styles.s88}
      style={
        {
          "--heavy-11": imageSrc
            ? "minmax(92px, 130px) minmax(0, 1fr)"
            : "40px minmax(0, 1fr)",
        } as CSSProperties
      }
    >
      {imageSrc ? (
        <Image
          preview={false}
          src={imageSrc}
          alt={title}
          className={styles.s89}
        />
      ) : (
        <Image
          preview={false}
          src={iconSrc}
          alt={title}
          className={styles.s90}
        />
      )}
      <div className={styles.s91}>
        <Typography.Paragraph className={`${styles.text} ${styles.s92}`}>
          {title}
        </Typography.Paragraph>
        <Typography.Paragraph className={`${styles.text} ${styles.s93}`}>
          {text}
        </Typography.Paragraph>
      </div>
    </div>
  );
}
/** HLiepinHiddenCandidateFilters 配置猎聘搜索页需要自动勾选的候选人隐藏条件。 */
function HLiepinHiddenCandidateFilters({
  hideViewed,
  hideContacted,
  hideContactObtained,
  onChange,
}: {
  hideViewed: boolean;
  hideContacted: boolean;
  hideContactObtained: boolean;
  onChange: (
    field:
      | "hliepin_hide_viewed"
      | "hliepin_hide_contacted"
      | "hliepin_hide_contact_obtained",
    checked: boolean,
  ) => void;
}) {
  return (
    <div className={styles.s94}>
      <Typography.Paragraph className={`${styles.text} ${styles.s95}`}>
        猎聘候选人隐藏条件
      </Typography.Paragraph>
      <Flex className={styles.s96}>
        <Checkbox
          checked={hideViewed}
          onChange={(event) =>
            onChange("hliepin_hide_viewed", event.target.checked)
          }
        >
          {"隐藏已查看"}
        </Checkbox>
        <Checkbox
          checked={hideContacted}
          onChange={(event) =>
            onChange("hliepin_hide_contacted", event.target.checked)
          }
        >
          {"隐藏已沟通"}
        </Checkbox>
        <Checkbox
          checked={hideContactObtained}
          onChange={(event) =>
            onChange("hliepin_hide_contact_obtained", event.target.checked)
          }
        >
          {"隐藏已获取联系方式"}
        </Checkbox>
      </Flex>
      <Typography.Paragraph className={`${styles.text} ${styles.s97}`}>
        岗位运行时只会勾选这里启用的条件；旧岗位默认三项全部启用。
      </Typography.Paragraph>
    </div>
  );
}
/** HLiepinShortcutSearchGuide 在填写猎聘快捷搜索名后展示配置提醒和可放大教程图。 */
function HLiepinShortcutSearchGuide({ visible }: { visible: boolean }) {
  return (
    <>
      {visible ? (
        <>
          <Card
            styles={{
              body: {
                padding: 0,
                height: "100%",
                flex: 1,
                display: "flex",
                flexDirection: "column",
              },
            }}
            className={styles.s98}
          >
            <Typography.Paragraph className={`${styles.text} ${styles.s99}`}>
              请先在猎聘创建并保存快捷搜索
            </Typography.Paragraph>
            <Alert
              type={"warning"}
              showIcon
              description={
                <>
                  {" "}
                  <Typography.Paragraph
                    className={`${styles.text} ${styles.s100}`}
                  >
                    请先在猎聘搜索页面配置关键词和全部筛选条件，点击“保存条件”创建快捷搜索，再把保存后的名称完整填写到上方“猎聘快捷搜索名”。岗位运行会直接使用该快捷搜索包含的全部条件，不会再次填写搜索关键词。
                  </Typography.Paragraph>
                  <Typography.Paragraph
                    className={`${styles.text} ${styles.s101}`}
                  >
                    填写的名称必须与猎聘页面显示的快捷搜索名完全一致，否则岗位运行会停止并说明未找到。不同岗位请使用容易区分且不重复的快捷搜索名，避免选错筛选条件。
                  </Typography.Paragraph>{" "}
                </>
              }
              className={styles.s102}
            />
            <ClickableImagePreview
              src={HLIEPIN_SHORTCUT_GUIDE_IMAGE_SRC}
              alt="猎聘保存搜索条件并创建快捷搜索教程"
              hint="点击图片放大查看猎聘快捷搜索创建步骤"
            />
          </Card>
        </>
      ) : null}
    </>
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
    hliepin_hide_viewed: true,
    hliepin_hide_contacted: true,
    hliepin_hide_contact_obtained: true,
    open_detail_prompt: "",
    filter_prompt: "",
    review_prompt: "",
    detail_score_threshold: 60,
    greet_score_threshold: 70,
    request_score_threshold: 70,
    output_structured_resume: false,
    request_phone: false,
    request_wechat: false,
    request_resume: false,
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
      request_phone: Boolean(common.request_phone),
      request_wechat: Boolean(common.request_wechat),
      request_resume: Boolean(common.request_resume),
      keywords: (item.keywords || []).join(" "),
      exclude_keywords: (item.exclude_keywords || []).join(" "),
      is_and_mode: Boolean(item.is_and_mode),
      position_requirement: ai.position_requirement || "",
      hliepin_shortcut_search_name: common.hliepin_shortcut_search_name || "",
      hliepin_hide_viewed: common.hliepin_hide_viewed !== false,
      hliepin_hide_contacted: common.hliepin_hide_contacted !== false,
      hliepin_hide_contact_obtained:
        common.hliepin_hide_contact_obtained !== false,
      open_detail_prompt: normalizePrompt(ai.open_detail_prompt),
      filter_prompt: normalizePrompt(
        ai.greet_prompt || ai.filter_prompt || ai.click_prompt,
      ),
      review_prompt: normalizePrompt(ai.review_prompt),
      detail_score_threshold: Number(ai.detail_score_threshold ?? 60),
      greet_score_threshold: Number(ai.greet_score_threshold ?? 70),
      request_score_threshold: Number(
        ai.request_score_threshold ?? ai.greet_score_threshold ?? 70,
      ),
      greet_message: item.greet_message || "",
      description: item.description || "",
      match_limit: Number(item.match_limit ?? 50),
      enable_sound: Boolean(item.enable_sound),
      enable_thinking: Boolean(item.enable_thinking),
    },
    defaults,
  );
}
/** isCurrentUserPosition 判断岗位是否由当前登录账号创建。 */
function isCurrentUserPosition(item: any, currentEmail: unknown) {
  if (typeof item?.is_current_user === "boolean") return item.is_current_user;
  const creator = String(item?.creator_email || "")
    .trim()
    .toLowerCase();
  const current = String(currentEmail || "")
    .trim()
    .toLowerCase();
  return !creator || (Boolean(current) && creator === current);
}
/** sortTeamPositions 按当前账号优先、创建时间从新到旧排列团队岗位。 */
function sortTeamPositions(items: any[], currentEmail: unknown) {
  return [...items].sort((left, right) => {
    const ownDifference =
      Number(isCurrentUserPosition(right, currentEmail)) -
      Number(isCurrentUserPosition(left, currentEmail));
    if (ownDifference !== 0) return ownDifference;
    const leftCreatedAt = new Date(left?.created_at || 0).getTime();
    const rightCreatedAt = new Date(right?.created_at || 0).getTime();
    return rightCreatedAt - leftCreatedAt;
  });
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
/** normalizePositionTaskStats 把本地最近任务统计转换成安全的非负整数。 */
function normalizePositionTaskStats(task: any): PositionTaskStats {
  return {
    scanned_count: Math.max(0, Number(task?.scanned_count) || 0),
    greeted_count: Math.max(0, Number(task?.greeted_count) || 0),
    skipped_count: Math.max(0, Number(task?.skipped_count) || 0),
  };
}
/** normalizeFloatingAnalysis 校验本地程序返回的 AI 或关键词结构化状态。 */
function normalizeFloatingAnalysis(
  value: unknown,
): PositionAnalysisStatus | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  const kind =
    record.kind === "ai" || record.kind === "keyword" ? record.kind : null;
  const phase =
    record.phase === "loading" ||
    record.phase === "result" ||
    record.phase === "error"
      ? record.phase
      : null;
  if (!kind || !phase) return null;
  const status: PositionAnalysisStatus = {
    kind,
    phase,
    stage:
      record.stage === "preview" || record.stage === "final"
        ? record.stage
        : undefined,
    terminal: Boolean(record.terminal),
    candidate_name: String(record.candidate_name || "").trim(),
    reason: String(record.reason || "").trim(),
    updated_at: String(record.updated_at || "").trim(),
    keywords: floatingStringArray(record.keywords),
    matched_keywords: floatingStringArray(record.matched_keywords),
    exclude_keywords: floatingStringArray(record.exclude_keywords),
    matched_excludes: floatingStringArray(record.matched_excludes),
  };
  if (typeof record.score === "number" && Number.isFinite(record.score)) {
    status.score = record.score;
  }
  if (
    typeof record.threshold === "number" &&
    Number.isFinite(record.threshold)
  ) {
    status.threshold = record.threshold;
  }
  if (typeof record.accepted === "boolean") {
    status.accepted = record.accepted;
  }
  return status;
}
/** floatingStringArray 清理悬浮窗状态中的字符串数组。 */
function floatingStringArray(value: unknown) {
  if (!Array.isArray(value)) return [];
  return value
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter(Boolean);
}
/** nextFloatingAnalysis 保证候选人最终结果至少展示八秒，不被下一位任何状态马上顶掉。 */
function nextFloatingAnalysis(
  current: PositionAnalysisStatus | null,
  incoming: PositionAnalysisStatus | null,
) {
  if (!incoming) return current;
  if (!current?.terminal || current.phase === "loading") {
    return incoming;
  }
  const currentTime = new Date(current.updated_at).getTime();
  if (
    current.updated_at !== incoming.updated_at &&
    Number.isFinite(currentTime) &&
    Date.now() - currentTime < 8000
  ) {
    return current;
  }
  return incoming;
}
/** PositionSwitchOption 展示岗位布尔开关及面向普通用户的通俗说明。 */
function PositionSwitchOption(props: {
  checked: boolean;
  label: string;
  description: string;
  onChange: (checked: boolean) => void;
}) {
  const { checked, label, description, onChange } = props;
  return (
    <Flex className={styles.s103}>
      <Flex align="center" gap={8} className={styles.s104}>
        <Switch
          aria-label={label}
          checked={checked}
          onChange={(checked) => onChange(checked)}
        />
        <Typography.Text>{label}</Typography.Text>
      </Flex>
      <Typography.Paragraph className={`${styles.text} ${styles.s105}`}>
        {description}
      </Typography.Paragraph>
    </Flex>
  );
}
/** positionStartReason 将岗位启动错误归一为后台可筛选的失败原因。 */
function positionStartReason(message: string) {
  const value = String(message || "").toLowerCase();
  if (value.includes("会员") || value.includes("订阅"))
    return "subscription_expired";
  if (
    value.includes("ai") ||
    value.includes("模型") ||
    value.includes("余额") ||
    value.includes("key")
  )
    return "ai_config_invalid";
  if (value.includes("登录") || value.includes("cookie"))
    return "platform_not_logged_in";
  if (
    value.includes("组件") ||
    value.includes("runtime") ||
    value.includes("node")
  )
    return "runtime_missing";
  if (value.includes("版本")) return "agent_version_outdated";
  return "position_start_failed";
}
/** isPositionStartErrorStatus 判断启动弹框状态是否需要使用错误色提醒。 */
function isPositionStartErrorStatus(message: string) {
  return [
    "没登录",
    "失败",
    "没跑完",
    "余额不足",
    "版本过低",
    "订阅",
    "没开放",
    "缺少",
    "超时",
  ].some((keyword) => String(message || "").includes(keyword));
}
/** PositionLogPanel 渲染岗位最近日志和日志操作入口。 */
function PositionLogPanel(props: {
  logs: any[];
  loading: boolean;
  sharing: boolean;
  onRefresh: () => void;
  onViewAll: () => void;
  onShare: () => void;
  onClear: () => void;
}) {
  const { logs, loading, sharing, onRefresh, onViewAll, onShare, onClear } =
    props;
  return (
    <div className={styles.s106}>
      <Flex className={styles.s107}>
        <Flex className={styles.s108}>
          <Typography.Paragraph className={`${styles.text} ${styles.s109}`}>
            本地岗位日志（最近 {LOG_LIMIT}条）
          </Typography.Paragraph>
          <Button size="small" onClick={onViewAll} type={"text"}>
            查看全部日志
          </Button>
        </Flex>
        <Flex className={styles.s110}>
          <Button
            size="small"
            onClick={onShare}
            disabled={sharing}
            type={"text"}
            icon={<CopyOutlined aria-hidden="true" />}
          >
            {sharing ? "生成中" : "复制错误日志发送给作者"}
          </Button>
          <Button
            size="small"
            onClick={onRefresh}
            disabled={loading}
            type={"text"}
          >
            {loading ? "刷新中" : "刷新"}
          </Button>
          <Button size="small" onClick={onClear} type={"text"} danger>
            清空
          </Button>
        </Flex>
      </Flex>
      <PositionLogList logs={logs} maxHeight={420} />
    </div>
  );
}
/** PositionLogList 按时间从新到旧展示日志，打开后优先看到最新记录。 */
function PositionLogList(props: { logs: any[]; maxHeight: number | string }) {
  const { logs, maxHeight } = props;
  const orderedLogs = sortPositionLogsNewestFirst(logs);
  return (
    <Flex
      className={styles.s111}
      style={
        {
          "--heavy-12":
            typeof maxHeight === "number" ? maxHeight + "px" : maxHeight,
        } as CSSProperties
      }
    >
      {orderedLogs.length ? (
        orderedLogs.map((item, index) => (
          <PositionLogLine
            key={String(item.id || `${item.created_at || item.time}-${index}`)}
            item={item}
            previous={
              index < orderedLogs.length - 1 ? orderedLogs[index + 1] : null
            }
          />
        ))
      ) : (
        <Typography.Paragraph className={`${styles.text} ${styles.s112}`}>
          这里暂时空空的，开始运行后我再认真记账。
        </Typography.Paragraph>
      )}
    </Flex>
  );
}
/** PositionLogLine 渲染单条带中文等级和耗时的岗位日志。 */
function PositionLogLine(props: { item: any; previous: any | null }) {
  const { item, previous } = props;
  const appearance = positionLogAppearance(item.level, item.is_error);
  return (
    <div
      className={styles.s113}
      style={
        {
          "--heavy-13": appearance.error ? "#f0b4b4" : "var(--goodhr-border)",
          "--heavy-14": appearance.color,
        } as CSSProperties
      }
    >
      <Typography.Paragraph
        className={`${styles.text} ${styles.s114}`}
        style={{ "--heavy-15": appearance.color } as CSSProperties}
      >
        {formatPositionLogTime(item.created_at || item.time)}
      </Typography.Paragraph>
      <Typography.Paragraph
        className={`${styles.text} ${styles.s115}`}
        style={{ "--heavy-16": appearance.color } as CSSProperties}
      >
        {positionLogDelta(item, previous)}
      </Typography.Paragraph>
      <Typography.Paragraph
        className={`${styles.text} ${styles.s116}`}
        style={
          {
            "--heavy-17": appearance.color,
            "--heavy-18": appearance.weight,
          } as CSSProperties
        }
      >
        {appearance.label}
      </Typography.Paragraph>
      <Typography.Paragraph
        className={`${styles.text} ${styles.s117}`}
        style={
          {
            "--heavy-19": appearance.color,
            "--heavy-20": appearance.weight,
          } as CSSProperties
        }
      >
        {positionLogMessage(item)}
      </Typography.Paragraph>
    </div>
  );
}
/** sortPositionLogsNewestFirst 按时间从新到旧排列，让最新日志位于顶部。 */
function sortPositionLogsNewestFirst(logs: any[]) {
  return [...logs].sort(
    (left, right) =>
      positionLogTimeMs(right.created_at || right.time) -
      positionLogTimeMs(left.created_at || left.time),
  );
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
  const orderedLogs = sortPositionLogsNewestFirst(logs);
  return orderedLogs
    .map((item, index) => {
      const previous =
        index < orderedLogs.length - 1 ? orderedLogs[index + 1] : null;
      return `${formatPositionLogTime(item.created_at || item.time)} ${positionLogDelta(item, previous)} ${positionLogAppearance(item.level, item.is_error).label} ${positionLogMessage(item)}`;
    })
    .join("\n");
}
/** positionLogAppearance 把岗位日志归为运行或错误两类。 */
function positionLogAppearance(value: unknown, isErrorValue?: unknown) {
  const level = String(value || "info")
    .trim()
    .toLowerCase();
  if (isErrorValue === true || level === "error")
    return { label: "错误", color: "#cf1322", weight: 760, error: true };
  return {
    label: "运行",
    color: "var(--goodhr-text)",
    weight: 400,
    error: false,
  };
}
