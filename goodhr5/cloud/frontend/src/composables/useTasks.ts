/** 任务和候选人管理 */
import { ref, type Ref } from "vue";
import { cloudApiBase, getAccessToken } from "../services/apiClient";
import { updateCookieStatus } from "../services/api/accountApi";
import { getSubscriptionStatus } from "../services/api/subscriptionApi";
import {
  clearTaskLogs,
  createTask,
  deleteTask,
  listTasks,
  listTaskLogs,
  updateTask,
} from "../services/api/taskApi";
import { getUserAIConfig } from "../services/api/personalConfigApi";
import {
  initLocalTask,
  listLocalCandidates,
  deleteLocalCandidate,
  startTaskWS,
  stopTaskWS,
} from "../services/localAgentApi";
import {
  detectCookieExpiredByURL,
  loadPlatformAuthConfig,
} from "../services/platformLoginFlow";
import { markOnboardingStep } from "../services/onboarding";

export function useTasks(agentBaseUrl: Ref<string>, onSubscriptionExpired?: () => void) {
  const tasks = ref<any[]>([]);
  const loading = ref(false);
  const error = ref("");
  const form = ref({
    name: "",
    platformId: "boss",
    platformAccountId: "",
    positionId: "",
    mode: "ai",
    matchLimit: 20,
    enableSound: false,
  });
  const expandedTaskId = ref("");
  const taskLogs = ref<Record<string, any[]>>({});
  const taskLogHasMore = ref<Record<string, boolean>>({});
  const taskLogLoadingMore = ref<Record<string, boolean>>({});
  const taskLogClearedAt = ref<Record<string, string>>({});
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
      await createTask({
        name: form.value.name,
        platform_id: form.value.platformId,
        platform_account_id: form.value.platformAccountId,
        position_id: form.value.positionId || "",
        mode: form.value.mode,
        match_limit: Number(form.value.matchLimit || 0),
        enable_sound: Boolean(form.value.enableSound),
      });
      await load();
      form.value = { ...form.value, name: "", positionId: "", enableSound: false };
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
      await updateTask(taskId, {
        name: payload.name,
        platform_id: payload.platformId,
        platform_account_id: payload.platformAccountId,
        position_id: payload.positionId || "",
        mode: payload.mode,
        match_limit: Number(payload.matchLimit || 0),
        enable_sound: Boolean(payload.enableSound),
      });
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
      //弹框确认
      if (!confirm("确认开始任务吗？")) return;
      const subscription = await getSubscriptionStatus();
      if (!subscription?.active) {
        onSubscriptionExpired?.();
        throw new Error("会员已到期，请先订阅后再开始任务");
      }

      const task = tasks.value.find((item: any) => item.id === taskId);
      await ensureTaskAIConfigReady(task);
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
      if (task?.platform_account_id && task?.platform_id) {
        const authConfig = await loadPlatformAuthConfig(task.platform_id);
        const status = await detectCookieExpiredByURL(
          agentBaseUrl.value,
          authConfig,
          (statusMessage) => {
            message.value = statusMessage;
          },
        );
        if (status.expired) {
          await updateCookieStatus(task.platform_account_id, "expired");
          await stopTaskWS(agentBaseUrl.value, taskId, {
            cloud_api_base: cloudApiBase(),
            token: getAccessToken(),
          });
          throw new Error("平台账号登录已过期，已停止任务，请重新登录账号");
        }
      }
      console.info("[goodhr5][task-start] frontend success", { taskId, data });
      message.value = data.message || "任务开始，请关注日志";
      await markOnboardingStep("task_started");
      await load();
    } catch (e: any) {
      console.error("[goodhr5][task-start] frontend failed", { taskId, error: e });
      error.value = e.message;
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
   * @returns {boolean} Boss 平台或 AI 模式返回 true。
   */
  function taskNeedsAIConfig(task: any) {
    const platformID = String(task?.platform_id || "").trim().toLowerCase();
    const mode = String(task?.mode || "").trim().toLowerCase();
    return platformID === "boss" || mode === "ai";
  }

  async function stop(taskId: string) {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      //弹框确认
      if (!confirm("确认停止任务吗？")) return;
      if (!agentBaseUrl.value) throw new Error("未检测到本地程序");
      console.info("[goodhr5][task-stop] frontend requested", { taskId, agentBaseUrl: agentBaseUrl.value });
      const data = await stopTaskWS(agentBaseUrl.value, taskId, {
        cloud_api_base: cloudApiBase(),
        token: getAccessToken(),
      });
      console.info("[goodhr5][task-stop] frontend success", { taskId, data });
      message.value = data.message || "任务已停止";
      await load();
      await refreshLogs(taskId);
      stopTaskLogPolling();
    } catch (e: any) {
      console.error("[goodhr5][task-stop] frontend failed", { taskId, error: e });
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function remove(taskId: string) {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      if (!confirm("确认删除任务吗？")) return;
      await deleteTask(taskId);
      message.value = "任务已删除";
      await load();
    } catch (e: any) {
      error.value = e.message;
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
      if (!confirm("确认清空该任务日志吗？")) return;
      await clearTaskLogs(taskId);
      const clearedAt = new Date().toISOString();
      taskLogs.value = { ...taskLogs.value, [taskId]: [] };
      taskLogHasMore.value = { ...taskLogHasMore.value, [taskId]: false };
      taskLogClearedAt.value = { ...taskLogClearedAt.value, [taskId]: clearedAt };
      message.value = "日志已清空";
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function refreshLogs(taskId: string) {
    try {
      const existing = taskLogs.value[taskId] || [];
      const since = latestTaskLogTime(existing) || taskLogClearedAt.value[taskId] || "";
      const data = await listTaskLogs(taskId, {
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
    } catch (e) {
      console.error("[goodhr5][task-logs] refresh failed", { taskId, error: e });
    }
  }

  async function loadOlderLogs(taskId: string) {
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
      await refreshLogs(taskId);
      await refreshTasksQuietly();
      const task = tasks.value.find((item: any) => item.id === taskId);
      if (!task || task.status !== "running") {
        stopTaskLogPolling();
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
      await initLocalTask(agentBaseUrl.value, {
        task_id: localId,
        cloud_user_id: "",
        platform_id: task.platform_id || "boss",
        platform_account_id: task.platform_account_id || "",
        position_snapshot: {},
      });
      const data = await listLocalCandidates(agentBaseUrl.value, localId);
      taskCandidates.value = { ...taskCandidates.value, [localId]: data };
    } catch (e: any) {
      candidateError.value = e.message;
    } finally {
      candidateLoadingTaskId.value = "";
    }
  }

  async function removeCandidate(task: any, candidate: any) {
    const localId = candidateExpandedTaskId.value;
    try {
      await deleteLocalCandidate(agentBaseUrl.value, localId, candidate.id);
      const items = (taskCandidates.value[localId]?.items || []).filter(
        (c: any) => c.id !== candidate.id,
      );
      taskCandidates.value = {
        ...taskCandidates.value,
        [localId]: { ...taskCandidates.value[localId], items },
      };
    } catch {}
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
    const base = cloudApiBase();
    const wsBase = base.replace(/^https:/, "wss:").replace(/^http:/, "ws:");
    return {
      cloud_api_base: base,
      cloud_ws_url: `${wsBase}/api/agents/ws`,
      token: getAccessToken(),
    };
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
  };
}
