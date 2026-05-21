/** 任务和候选人管理 */
import { ref } from "vue";
import { cloudApiBase, getAccessToken } from "../services/apiClient";
import {
  createTask,
  deleteTask,
  listTasks,
  listTaskLogs,
  updateTask,
} from "../services/cloudApi";
import {
  initLocalTask,
  listLocalCandidates,
  deleteLocalCandidate,
  startTaskWS,
  stopTaskWS,
} from "../services/localAgentApi";

export function useTasks(agentBaseUrl: Ref<string>) {
  const tasks = ref<any[]>([]);
  const loading = ref(false);
  const error = ref("");
  const form = ref({
    platformId: "boss",
    platformAccountId: "",
    positionId: "",
    mode: "keyword",
    matchLimit: 20,
    enableSound: false,
  });
  const expandedTaskId = ref("");
  const taskLogs = ref<Record<string, any[]>>({});
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
        platform_id: form.value.platformId,
        platform_account_id: form.value.platformAccountId,
        position_id: form.value.positionId || "",
        mode: form.value.mode,
        match_limit: Number(form.value.matchLimit || 0),
        enable_sound: Boolean(form.value.enableSound),
      });
      await load();
      form.value = { ...form.value, positionId: "", enableSound: false };
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
      await load();
    } catch (e: any) {
      console.error("[goodhr5][task-start] frontend failed", { taskId, error: e });
      error.value = e.message;
    } finally {
      loading.value = false;
    }
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

  async function refreshLogs(taskId: string) {
    try {
      const logs = await listTaskLogs(taskId);
      taskLogs.value = { ...taskLogs.value, [taskId]: logs };
      console.info("[goodhr5][task-logs] refreshed", { taskId, count: logs.length });
    } catch (e) {
      console.error("[goodhr5][task-logs] refresh failed", { taskId, error: e });
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
    refreshLogs,
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
