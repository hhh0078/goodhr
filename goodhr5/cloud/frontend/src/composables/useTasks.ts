/** 任务和候选人管理 */
import { ref, type Ref } from "vue";
import { cloudApiBase, getAccessToken } from "../services/apiClient";
import { getSubscriptionStatus } from "../services/api/subscriptionApi";
import {
  clearTaskLogs,
  createTask,
  deleteTask,
  listTasks,
  listTaskLogs,
  updateTask,
} from "../services/api/taskApi";
import { listCandidates } from "../services/api/candidateApi";
import { getUserAIConfig } from "../services/api/personalConfigApi";
import {
  clearLocalTaskLogs,
  listLocalTaskLogs,
  runLocalTask,
  getLocalTaskStatus,
  startTaskWS,
  stopTaskWS,
  stopLocalTask,
} from "../services/localAgentApi";
import { isLocalConsole, localAgentBase } from "../services/localConsole";
import { markOnboardingStep } from "../services/onboarding";
import { alertError, confirmDialog, notifySuccess } from "../services/notify";
import { freeDailyGreetLimit, isSubscriptionActive } from "../services/subscriptionLimits";

const DEFAULT_RUN_GREET_LIMIT = 50;

export function useTasks(
  agentBaseUrl: Ref<string>,
  onSubscriptionExpired?: () => void,
  positionSnapshotResolver?: (positionID: string) => any,
  subscriptionRef?: Ref<any>,
  appConfigRef?: Ref<any>,
) {
  const tasks = ref<any[]>([]);
  const loading = ref(false);
  const error = ref("");
  const form = ref({
    name: "",
    platformId: "boss",
    platformAccountId: "",
    positionId: "",
    mode: "keyword",
    matchLimit: DEFAULT_RUN_GREET_LIMIT,
    enableSound: false,
    enableThinking: false,
  });
  const expandedTaskId = ref("");
  const taskLogs = ref<Record<string, any[]>>({});
  const taskLogHasMore = ref<Record<string, boolean>>({});
  const taskLogLoadingMore = ref<Record<string, boolean>>({});
  const taskLogClearedAt = ref<Record<string, string>>({});
  const taskProgress = ref<Record<string, any>>({});
  const candidateExpandedTaskId = ref("");
  const taskCandidates = ref<Record<string, any>>({});
  const candidateLoadingTaskId = ref("");
  const candidateError = ref("");
  const message = ref("");
  let taskLogPollTimer: number | null = null;

  async function load() {
    loading.value = true;
    error.value = "";
    try {
      tasks.value = await listTasks();
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function refreshTasksQuietly() {
    try {
      tasks.value = await listTasks();
    } catch (e) {
      console.error("[goodhr5][tasks] quiet refresh failed", e);
    }
  }

  async function create() {
    if (!form.value.platformAccountId) return;
    loading.value = true;
    error.value = "";
    try {
      const payload = {
        name: form.value.name,
        platform_id: form.value.platformId,
        platform_account_id: form.value.platformAccountId,
        position_id: form.value.positionId || "",
        mode: form.value.mode,
        match_limit: normalizeRunGreetLimit(form.value.matchLimit),
        enable_sound: Boolean(form.value.enableSound),
        enable_thinking: Boolean(form.value.enableThinking),
        position_snapshot: resolvePositionSnapshot(form.value.positionId),
      };
      await createTask(payload);
      await load();
      form.value = { ...form.value, name: "", positionId: "", enableSound: false, enableThinking: false };
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function update(taskId: string, payload: any) {
    loading.value = true;
    error.value = "";
    try {
      const taskPayload = {
        name: payload.name,
        platform_id: payload.platformId,
        platform_account_id: payload.platformAccountId,
        position_id: payload.positionId || "",
        mode: payload.mode,
        match_limit: normalizeRunGreetLimit(payload.matchLimit),
        enable_sound: Boolean(payload.enableSound),
        enable_thinking: Boolean(payload.enableThinking),
      };
      await updateTask(taskId, taskPayload);
      await load();
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function execute(taskId: string) {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      if (!(await confirmDialog("确认开始任务吗？", {
        title: "开始任务",
        kind: "success",
        confirmText: "开始",
      }))) return;
      const token = getAccessToken();
      if (!token) throw new Error("请先登录后再开始任务");

      const task = tasks.value.find((item: any) => item.id === taskId);
      const subscription = await getSubscriptionStatus();
      if (subscriptionRef) subscriptionRef.value = subscription;
      await ensureFreePlanCanRun(task, subscription);
      await ensureTaskAIConfigReady(task);
      if (shouldUseLocalTasks()) {
        expandedTaskId.value = taskId;
        await refreshLogs(taskId);
        startTaskLogPolling(taskId);
        const data = await runLocalTask(localTaskBase(), taskId, taskLocalPayload(token));
        if (data?.progress) {
          taskProgress.value = { ...taskProgress.value, [taskId]: data.progress };
        }
        message.value = data.message || "本地任务已进入后台运行";
        notifySuccess(message.value);
        await markOnboardingStep("task_started");
        await load();
        return;
      }
      if (!agentBaseUrl.value) throw new Error("未检测到本地程序");
      console.info("[goodhr5][task-start] frontend requested", {
        taskId,
        agentBaseUrl: agentBaseUrl.value,
        payload: taskWSPayload(),
      });
      expandedTaskId.value = taskId;
      await refreshLogs(taskId);
      startTaskLogPolling(taskId);
      const data = await startTaskWS(
        agentBaseUrl.value,
        taskId,
        taskWSPayload(),
      );
      console.info("[goodhr5][task-start] frontend success", { taskId, data });
      message.value = data.message || "任务开始，请关注日志";
      notifySuccess(message.value);
      await markOnboardingStep("task_started");
      await load();
    } catch (e: any) {
      console.error("[goodhr5][task-start] frontend failed", { taskId, error: e });
      error.value = e.message;
      if (!e?.notified) await alertError(error.value || "任务开始失败");
    } finally {
      loading.value = false;
    }
  }

  /**
   * 开始任务前确认当前任务是否必须配置 AI Key。
   * @param {any} task - 当前任务对象。
   * @returns {Promise<void>} 已配置时返回；缺失时抛出错误。
   */
  async function ensureTaskAIConfigReady(task: any) {
    if (!taskNeedsAIConfig(task)) return;
    const aiConfig = await getUserAIConfig();
    if (!aiConfig?.api_key_set || aiConfig?.enabled === false) {
      throw new Error("当前任务需要 AI，请先在个人配置中填写并保存 AI Key");
    }
  }

  /**
   * 判断任务是否必须使用 AI 配置。
   * @param {any} task - 当前任务对象。
   * @returns {boolean} AI 模式返回 true。
   */
  function taskNeedsAIConfig(task: any) {
    const mode = String(task?.mode || "").trim().toLowerCase();
    const detailMode = String(
      task?.position_snapshot?.common_config?.detail_mode ||
        task?.position?.common_config?.detail_mode ||
        "",
    ).trim().toLowerCase();
    return mode === "ai" || detailMode === "ai";
  }

  /**
   * 确认免费版是否允许启动当前任务。
   * @param {any} task - 当前任务。
   * @param {any} subscription - 最新会员状态。
   * @returns {Promise<void>} 允许启动时返回。
   */
  async function ensureFreePlanCanRun(task: any, subscription: any) {
    if (isSubscriptionActive(subscription)) return;
    const limit = freeDailyGreetLimit(appConfigRef?.value || {});
    if (taskNeedsAIConfig(task)) {
      await promptSubscriptionRequired("该任务使用了 AI 筛选或 AI 详情识别，该功能需要订阅会员，是否前往订阅页面？");
      throw notifiedError("该功能需要订阅会员");
    }
    const todayTotal = totalTodayGreetedCount();
    const remaining = Math.max(0, limit - todayTotal);
    const runLimit = normalizeRunGreetLimit(task?.match_limit);
    if (remaining <= 0) {
      await promptSubscriptionRequired(`当前是免费版，一天最多只能打 ${limit} 个招呼，订阅会员后无限制。`);
      throw notifiedError("免费版今日打招呼数量已达上限");
    }
    if (runLimit > remaining) {
      await confirmDialog(`当前是免费版，一天最多只能打 ${limit} 个招呼，今天还剩 ${remaining} 个。请把本次打招呼上限调到 ${remaining} 以内，订阅会员后无限制。`, {
        title: "免费版限制",
        confirmText: "知道了",
        showCancel: false,
      });
      throw notifiedError("本次打招呼上限超过免费版今日剩余额度");
    }
    await confirmDialog(`当前是免费版，一天最多只能打 ${limit} 个招呼，订阅会员后无限制。今天还剩 ${remaining} 个。`, {
      title: "免费版限制",
      confirmText: "知道了",
      showCancel: false,
    });
  }

  /**
   * 引导用户前往订阅页面。
   * @param {string} messageText - 弹框提示内容。
   * @returns {Promise<void>} 无返回值。
   */
  async function promptSubscriptionRequired(messageText: string) {
    const confirmed = await confirmDialog(messageText, {
      title: "订阅会员",
      confirmText: "前往订阅",
      cancelText: "取消",
    });
    if (confirmed) onSubscriptionExpired?.();
  }

  /**
   * 生成已经弹过窗的错误对象，避免外层重复弹窗。
   * @param {string} messageText - 错误内容。
   * @returns {Error} 已标记提醒状态的错误。
   */
  function notifiedError(messageText: string) {
    const err = new Error(messageText) as Error & { notified?: boolean };
    err.notified = true;
    return err;
  }

  /**
   * 统计今天所有任务已经打招呼数量。
   * @returns {number} 今日打招呼总数。
   */
  function totalTodayGreetedCount() {
    return tasks.value.reduce((sum: number, item: any) => {
      const count = Number(item?.today_greeted_count || 0);
      return sum + (Number.isFinite(count) && count > 0 ? count : 0);
    }, 0);
  }

  async function stop(taskId: string) {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      if (!(await confirmDialog("确认停止任务吗？", {
        title: "停止任务",
        confirmText: "停止",
      }))) return;
      if (shouldUseLocalTasks()) {
        const data = await stopLocalTask(localTaskBase(), taskId, taskLocalPayload());
        taskProgress.value = {
          ...taskProgress.value,
          [taskId]: { stage: "stopped", message: "任务已停止" },
        };
        message.value = data.message || "任务已停止";
        notifySuccess(message.value);
        await load();
        await refreshLogs(taskId);
        stopTaskLogPolling();
        return;
      }
      if (!agentBaseUrl.value) throw new Error("未检测到本地程序");
      console.info("[goodhr5][task-stop] frontend requested", { taskId, agentBaseUrl: agentBaseUrl.value });
      const data = await stopTaskWS(agentBaseUrl.value, taskId, {
        cloud_api_base: cloudApiBase(),
        token: getAccessToken(),
      });
      console.info("[goodhr5][task-stop] frontend success", { taskId, data });
      message.value = data.message || "任务已停止";
      notifySuccess(message.value);
      await load();
      await refreshLogs(taskId);
      stopTaskLogPolling();
    } catch (e: any) {
      console.error("[goodhr5][task-stop] frontend failed", { taskId, error: e });
      error.value = e.message;
      if (!e?.notified) await alertError(error.value || "停止任务失败");
    } finally {
      loading.value = false;
    }
  }

  async function remove(taskId: string) {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      if (!(await confirmDialog("确认删除任务吗？", {
        title: "删除任务",
        confirmText: "删除",
      }))) return;
      await deleteTask(taskId);
      message.value = "任务已删除";
      notifySuccess(message.value);
      await load();
    } catch (e: any) {
      error.value = e.message;
      if (!e?.notified) await alertError(error.value || "删除任务失败");
    } finally {
      loading.value = false;
    }
  }

  async function toggleLogs(taskId: string) {
    if (expandedTaskId.value === taskId) {
      expandedTaskId.value = "";
      stopTaskLogPolling();
      return;
    }
    expandedTaskId.value = taskId;
    await refreshLogs(taskId);
    startTaskLogPolling(taskId);
  }

  /**
   * 清空指定任务的云端日志，并同步刷新当前面板数据。
   * @param {string} taskId - 任务 ID。
   * @returns {Promise<void>} 无返回值。
   */
  async function clearLogs(taskId: string) {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      if (!(await confirmDialog("确认清空该任务日志吗？", {
        title: "清空日志",
        confirmText: "清空",
      }))) return;
      const cleared = shouldUseLocalTasks()
        ? await clearLocalTaskLogs(localTaskBase(), taskId)
        : await clearTaskLogs(taskId);
      const clearedAt = cleared?.cleared_at || new Date().toISOString();
      taskLogs.value = { ...taskLogs.value, [taskId]: [] };
      taskLogHasMore.value = { ...taskLogHasMore.value, [taskId]: false };
      taskLogClearedAt.value = { ...taskLogClearedAt.value, [taskId]: clearedAt };
      message.value = "日志已清空";
      notifySuccess(message.value);
    } catch (e: any) {
      error.value = e.message;
      if (!e?.notified) await alertError(error.value || "清空日志失败");
    } finally {
      loading.value = false;
    }
  }

  async function refreshLogs(taskId: string) {
    try {
      let localRunning: boolean | undefined;
      if (shouldUseLocalTasks()) {
        localRunning = await refreshLocalTaskStatus(taskId);
      }
      const existing = taskLogs.value[taskId] || [];
      const since = latestTaskLogTime(existing) || taskLogClearedAt.value[taskId] || "";
      const data = shouldUseLocalTasks()
        ? await listLocalTaskLogs(localTaskBase(), taskId, { limit: 100 })
        : await listTaskLogs(taskId, {
            since: since || undefined,
            limit: 100,
          });
      const logs = data.logs || [];
      const merged = mergeTaskLogs(existing, logs, Boolean(since));
      taskLogs.value = { ...taskLogs.value, [taskId]: merged };
      if (!since) {
        taskLogHasMore.value = { ...taskLogHasMore.value, [taskId]: Boolean(data.has_more) };
      }
      console.info("[goodhr5][task-logs] refreshed", {
        taskId,
        incremental: Boolean(since),
        added: logs.length,
        total: merged.length,
      });
      return localRunning;
    } catch (e) {
      console.error("[goodhr5][task-logs] refresh failed", { taskId, error: e });
      return undefined;
    }
  }

  /**
   * 刷新本地任务状态和进度。
   * @param {string} taskId - 任务 ID。
   * @returns {Promise<boolean>} 返回任务是否仍在运行。
   */
  async function refreshLocalTaskStatus(taskId: string) {
    const data = await getLocalTaskStatus(localTaskBase(), taskId);
    if (data?.task) {
      tasks.value = tasks.value.map((item: any) =>
        item.id === taskId ? { ...item, ...data.task } : item,
      );
    }
    if (data?.progress) {
      taskProgress.value = { ...taskProgress.value, [taskId]: data.progress };
    }
    if (Array.isArray(data?.logs) && data.logs.length > 0) {
      taskLogs.value = {
        ...taskLogs.value,
        [taskId]: mergeTaskLogs(taskLogs.value[taskId] || [], data.logs, true),
      };
    }
    return Boolean(data?.running);
  }

  async function loadOlderLogs(taskId: string) {
    if (shouldUseLocalTasks()) {
      taskLogHasMore.value = { ...taskLogHasMore.value, [taskId]: false };
      return;
    }
    if (taskLogLoadingMore.value[taskId] || taskLogHasMore.value[taskId] === false) return;
    const existing = taskLogs.value[taskId] || [];
    const before = oldestTaskLogTime(existing);
    if (!before) return;
    taskLogLoadingMore.value = { ...taskLogLoadingMore.value, [taskId]: true };
    try {
      const data = await listTaskLogs(taskId, { before, limit: 100 });
      const logs = data.logs || [];
      const merged = mergeTaskLogs(existing, logs, true);
      taskLogs.value = { ...taskLogs.value, [taskId]: merged };
      taskLogHasMore.value = { ...taskLogHasMore.value, [taskId]: Boolean(data.has_more) };
    } catch (e) {
      console.error("[goodhr5][task-logs] load older failed", { taskId, error: e });
    } finally {
      taskLogLoadingMore.value = { ...taskLogLoadingMore.value, [taskId]: false };
    }
  }

  function startTaskLogPolling(taskId: string) {
    stopTaskLogPolling();
    taskLogPollTimer = window.setInterval(async () => {
      if (expandedTaskId.value !== taskId) {
        stopTaskLogPolling();
        return;
      }
      if (shouldUseLocalTasks()) {
        const running = await refreshLogs(taskId);
        if (!running) {
          stopTaskLogPolling();
          await refreshTasksQuietly();
        }
      } else {
        await refreshLogs(taskId);
        await refreshTasksQuietly();
        const task = tasks.value.find((item: any) => item.id === taskId);
        if (!task || task.status !== "running") {
          stopTaskLogPolling();
        }
      }
    }, 1500);
  }

  function stopTaskLogPolling() {
    if (taskLogPollTimer != null) {
      window.clearInterval(taskLogPollTimer);
      taskLogPollTimer = null;
    }
  }

  function latestTaskLogTime(logs: any[]) {
    if (!logs || logs.length === 0) return "";
    return logs.reduce((latest: string, item: any) => {
      const createdAt = String(item?.created_at || "");
      if (!createdAt) return latest;
      return !latest || createdAt > latest ? createdAt : latest;
    }, "");
  }

  function oldestTaskLogTime(logs: any[]) {
    if (!logs || logs.length === 0) return "";
    return logs.reduce((oldest: string, item: any) => {
      const createdAt = String(item?.created_at || "");
      if (!createdAt) return oldest;
      return !oldest || createdAt < oldest ? createdAt : oldest;
    }, "");
  }

  function mergeTaskLogs(existing: any[], incoming: any[], incremental: boolean) {
    if (!incremental) {
      return sortTaskLogs(incoming || []);
    }
    const merged = [...existing];
    const seen = new Set(existing.map((item: any) => String(item.id)));
    for (const item of incoming || []) {
      const id = String(item?.id || "");
      if (id && seen.has(id)) continue;
      if (id) seen.add(id);
      merged.push(item);
    }
    return sortTaskLogs(merged);
  }

  function sortTaskLogs(logs: any[]) {
    return [...logs].sort((a: any, b: any) => {
      const aTime = Date.parse(String(a?.created_at || ""));
      const bTime = Date.parse(String(b?.created_at || ""));
      if (aTime === bTime) {
        return String(b?.id || "").localeCompare(String(a?.id || ""));
      }
      if (!Number.isNaN(aTime) && !Number.isNaN(bTime)) {
        return bTime - aTime;
      }
      const aRaw = String(a?.created_at || "");
      const bRaw = String(b?.created_at || "");
      return bRaw.localeCompare(aRaw);
    });
  }

  async function toggleCandidates(task: any) {
    const localId = task.local_task_id || task.id;
    if (candidateExpandedTaskId.value === localId) {
      candidateExpandedTaskId.value = "";
      return;
    }
    candidateExpandedTaskId.value = localId;
    await loadCandidates(task, localId);
  }

  async function loadCandidates(task: any, localId: string) {
    candidateLoadingTaskId.value = localId;
    candidateError.value = "";
    try {
      const data = await listCandidates({
        taskId: localId,
        positionId: task.position_id || "",
        page: 1,
        pageSize: 200,
      });
      taskCandidates.value = { ...taskCandidates.value, [localId]: data };
    } catch (e: any) {
      candidateError.value = e.message;
    } finally {
      candidateLoadingTaskId.value = "";
    }
  }

  async function removeCandidate(_task: any, _candidate: any) {
    await alertError("云端暂未提供单个候选人删除接口，请先在简历库中统一管理");
  }

  function localTaskID(task: any) {
    return task.local_task_id || task.id;
  }
  function taskPositionSnapshot(task: any) {
    return taskCandidates.value[localTaskID(task)]?.position_snapshot || {};
  }
  function candidateItems(task: any) {
    return taskCandidates.value[localTaskID(task)]?.items || [];
  }
  function candidateTitle(c: any) {
    return c.name || c.id || "";
  }
  function candidateSubtitle(c: any) {
    return c.raw_text || c.skills || "";
  }
  function candidateDetail(c: any) {
    return c.detail_text || "";
  }

  /**
   * 生成本地 Agent 启动任务所需的云端连接参数。
   * @returns {any} 返回云端 HTTP 地址、WebSocket 地址和 token。
   */
  function taskWSPayload() {
    const payload = taskCloudPayload();
    const wsBase = payload.cloud_api_base.replace(/^https:/, "wss:").replace(/^http:/, "ws:");
    return {
      ...payload,
      cloud_ws_url: `${wsBase}/api/agents/ws`,
    };
  }

  /**
   * 生成任务需要的云端 HTTP 和 token 参数。
   * @returns {any} 返回云端 HTTP 地址和 token。
   */
  function taskCloudPayload() {
    const base = cloudApiBase();
    return {
      cloud_api_base: base,
      token: getAccessToken(),
    };
  }

  /**
   * 生成本地任务启动需要的云端参数。
   * @param {string} token - 当前登录令牌。
   * @returns {any} 返回云端 HTTP 地址和 token。
   */
  function taskLocalPayload(token = getAccessToken()) {
    return {
      cloud_api_base: cloudApiBase(),
      token,
    };
  }

  /**
   * 返回岗位模板快照。
   * @param {string} positionID - 岗位模板 ID。
   * @returns {any} 岗位模板快照。
   */
  function resolvePositionSnapshot(positionID: string) {
    if (!positionSnapshotResolver) return {};
    const snapshot = positionSnapshotResolver(positionID);
    return snapshot && typeof snapshot === "object" ? snapshot : {};
  }

  /**
   * 判断当前页面是否是本地控制台。
   * @returns {boolean} 本地控制台返回 true。
   */
  function shouldUseLocalTasks() {
    return isLocalConsole();
  }

  /**
   * 返回本地任务接口基础地址。
   * @returns {string} Local Agent HTTP 地址。
   */
  function localTaskBase() {
    if (shouldUseLocalTasks()) return localAgentBase();
    return agentBaseUrl.value;
  }

  return {
    tasks,
    loading,
    error,
    message,
    form,
    expandedTaskId,
    taskLogs,
    taskLogHasMore,
    taskLogLoadingMore,
    taskProgress,
    candidateExpandedTaskId,
    taskCandidates,
    candidateLoadingTaskId,
    candidateError,
    load,
    create,
    update,
    execute,
    stop,
    remove,
    toggleLogs,
    clearLogs,
    refreshLogs,
    loadOlderLogs,
    toggleCandidates,
    loadCandidates,
    removeCandidate,
    localTaskID,
    taskPositionSnapshot,
    candidateItems,
    candidateTitle,
    candidateSubtitle,
    candidateDetail,
    localTaskMode: shouldUseLocalTasks,
  };
}

/**
 * 规范化本次打招呼上限。
 * @param value - 表单输入值。
 * @returns 有效上限，空值或非法值默认返回 50。
 */
function normalizeRunGreetLimit(value: any) {
  const limit = Number(value || 0);
  if (!Number.isFinite(limit) || limit <= 0) return DEFAULT_RUN_GREET_LIMIT;
  return Math.floor(limit);
}
