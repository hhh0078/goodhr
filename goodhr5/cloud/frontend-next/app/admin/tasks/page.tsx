/** 本文件负责新版后台任务的创建、编辑、运行、停止、日志和删除。 */
"use client";

import AddRoundedIcon from "@mui/icons-material/AddRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import EditRoundedIcon from "@mui/icons-material/EditRounded";
import PlayArrowRoundedIcon from "@mui/icons-material/PlayArrowRounded";
import StopRoundedIcon from "@mui/icons-material/StopRounded";
import {
  Box,
  Button,
  Chip,
  FormControlLabel,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import {
  CLOUD_API_BASE,
  cloudRequest,
  getToken,
  localRequest,
} from "@/lib/admin-api";
import { markOnboardingStep } from "@/lib/onboarding";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AdminDialog from "@/components/admin/AdminDialog";

const emptyForm = {
  id: "",
  name: "",
  position_id: "",
  match_limit: 50,
  enable_sound: false,
  enable_thinking: false,
};
const LOG_REFRESH_MS = 3000;
const LOG_LIMIT = 50;
const ALL_LOG_LIMIT = 5000;

/** TasksPage 管理招聘任务完整生命周期。 */
export default function TasksPage() {
  const { agentBase, user, notify, confirm } = useAdmin();
  const [tasks, setTasks] = useState<any[]>([]);
  const [positions, setPositions] = useState<any[]>([]);
  const [logs, setLogs] = useState<Record<string, any[]>>({});
  const [expandedLogTaskID, setExpandedLogTaskID] = useState("");
  const [logLoadingTaskID, setLogLoadingTaskID] = useState("");
  const [allLogs, setAllLogs] = useState<any[]>([]);
  const [allLogTask, setAllLogTask] = useState<any | null>(null);
  const [allLogLoading, setAllLogLoading] = useState(false);
  const [runningTaskIDs, setRunningTaskIDs] = useState<Record<string, boolean>>(
    {},
  );
  const [localTaskStats, setLocalTaskStats] = useState<Record<string, any>>({});
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ ...emptyForm });

  /** load 读取任务及创建任务需要的岗位。 */
  async function load() {
    setLoading(true);
    try {
      const [taskData, positionData] = await Promise.all([
        cloudRequest("/api/tasks"),
        cloudRequest("/api/positions"),
      ]);
      setTasks(taskData.tasks || []);
      setPositions(positionData.positions || []);
    } catch (error) {
      notify(error instanceof Error ? error.message : "任务读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    const taskIDs = tasks.map((task) => String(task.id || "")).filter(Boolean);
    if (!agentBase || taskIDs.length === 0) return;
    void syncLocalRunningStates(taskIDs);
  }, [agentBase, tasks]);

  useEffect(() => {
    const ids = Object.keys(runningTaskIDs).filter(
      (taskID) => runningTaskIDs[taskID],
    );
    if (!agentBase || ids.length === 0) return;
    const timer = window.setInterval(() => {
      ids.forEach((taskID) => void refreshLocalTaskStatus(taskID));
    }, 3500);
    return () => window.clearInterval(timer);
  }, [agentBase, runningTaskIDs]);

  const taskList = useMemo(
    () =>
      tasks.map((task) => ({
        ...task,
        ...(localTaskStats[task.id] || {}),
        status: runningTaskIDs[task.id] ? "running" : task.status,
      })),
    [localTaskStats, runningTaskIDs, tasks],
  );

  /** syncLocalRunningStates 批量同步本地任务真实运行状态。 */
  async function syncLocalRunningStates(taskIDs: string[]) {
    if (!agentBase) return;
    const results = await Promise.allSettled(
      taskIDs.map(async (taskID) => {
        const data = await localRequest(
          agentBase,
          `/api/v1/local/tasks/${encodeURIComponent(taskID)}/status`,
        );
        return {
          taskID,
          running: data.running === true,
          task: data.task || {},
        };
      }),
    );
    setRunningTaskIDs((current) => {
      const next = { ...current };
      results.forEach((result) => {
        if (result.status === "fulfilled") {
          next[result.value.taskID] = result.value.running;
        }
      });
      return next;
    });
    setLocalTaskStats((current) => {
      const next = { ...current };
      results.forEach((result) => {
        if (result.status === "fulfilled") {
          next[result.value.taskID] = pickLocalTaskStats(result.value.task);
        }
      });
      return next;
    });
  }

  useEffect(() => {
    if (!agentBase || !expandedLogTaskID) return;
    const currentTask = taskList.find((task) => task.id === expandedLogTaskID);
    if (currentTask?.status !== "running") return;
    const timer = window.setInterval(() => {
      void loadLogs(expandedLogTaskID, { silent: true });
    }, LOG_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [agentBase, expandedLogTaskID, taskList]);

  /** save 创建或更新带岗位快照的招聘任务。 */
  async function save() {
    const position = positions.find((item) => item.id === form.position_id);
    if (!position) return notify("请选择岗位模板", "warning");
    setLoading(true);
    try {
      const payload = {
        name: form.name.trim() || `${position.name}招聘任务`,
        platform_id: position.platform_id,
        platform_account_id: "",
        position_id: position.id,
        mode: position.common_config?.mode_default || "keyword",
        match_limit: Math.max(1, Number(form.match_limit || 50)),
        enable_sound: form.enable_sound,
        enable_thinking: form.enable_thinking,
        position_snapshot: position,
      };
      await cloudRequest(
        form.id ? `/api/tasks/${encodeURIComponent(form.id)}` : "/api/tasks",
        { method: form.id ? "PUT" : "POST", body: payload },
      );
      notify(form.id ? "任务已更新" : "任务已创建", "success");
      setForm({ ...emptyForm });
      setShowForm(false);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存任务失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** openCreate 重置表单并打开任务弹框。 */
  function openCreate() {
    setForm({ ...emptyForm });
    setShowForm(true);
  }

  /** openEdit 将任务数据填入编辑弹框。 */
  function openEdit(task: any) {
    setForm({
      id: task.id || "",
      name: task.name || "",
      position_id: task.position_id || task.position?.id || "",
      match_limit: Number(task.match_limit || 50),
      enable_sound: Boolean(task.enable_sound),
      enable_thinking: Boolean(task.enable_thinking),
    });
    setShowForm(true);
  }

  /** run 在校验登录、会员和本地程序后启动任务。 */
  async function run(task: any) {
    if (!agentBase) return notify("请先启动本地程序", "error");
    if (!(await confirm("开始招聘任务", `确认开始“${task.name}”吗？`))) return;
    try {
      const subscriptionData = await cloudRequest("/api/subscription/status");
      const active = Boolean(subscriptionData.subscription?.active);
      const position =
        positions.find((item) => item.id === task.position_id) ||
        task.position_snapshot ||
        {};
      const usesAI =
        task.mode === "ai" ||
        position.common_config?.mode_default === "ai" ||
        position.common_config?.detail_mode === "ai";
      if (usesAI && !active)
        return notify("当前任务使用会员 AI 功能，请订阅后再开始", "warning");
      if (!active)
        notify("当前为免费版，今日打招呼数量受系统免费额度限制", "info");
      await localRequest(
        agentBase,
        `/api/v1/local/tasks/${encodeURIComponent(task.id)}/run`,
        {
          method: "POST",
          body: { cloud_api_base: CLOUD_API_BASE, token: getToken() },
        },
      );
      setRunningTaskIDs((current) => ({ ...current, [task.id]: true }));
      await markOnboardingStep(String(user?.email || ""), "task_started");
      notify("任务已开始", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "任务启动失败", "error");
    }
  }

  /** stop 停止任务但保持浏览器打开。 */
  async function stop(task: any) {
    if (!agentBase) return notify("本地程序未连接", "error");
    try {
      await localRequest(
        agentBase,
        `/api/v1/local/tasks/${encodeURIComponent(task.id)}/stop`,
        {
          method: "POST",
          body: { cloud_api_base: CLOUD_API_BASE, token: getToken() },
        },
      );
      setRunningTaskIDs((current) => ({ ...current, [task.id]: false }));
      notify("任务已停止，浏览器保持打开", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "停止任务失败", "error");
    }
  }

  /** refreshLocalTaskStatus 同步本地任务真实运行状态，避免按钮状态长期停留。 */
  async function refreshLocalTaskStatus(taskID: string) {
    if (!agentBase) return;
    try {
      const data = await localRequest(
        agentBase,
        `/api/v1/local/tasks/${encodeURIComponent(taskID)}/status`,
      );
      if (data.running === true) {
        setRunningTaskIDs((current) => ({ ...current, [taskID]: true }));
        setLocalTaskStats((current) => ({
          ...current,
          [taskID]: pickLocalTaskStats(data.task || {}),
        }));
        return;
      }
      if (data.running === false) {
        setRunningTaskIDs((current) => ({ ...current, [taskID]: false }));
        setLocalTaskStats((current) => ({
          ...current,
          [taskID]: pickLocalTaskStats(data.task || {}),
        }));
        await load();
        if (expandedLogTaskID === taskID) await loadLogs(taskID);
      }
    } catch {
      // 本地状态偶发读取失败时保留当前按钮状态，避免闪烁。
    }
  }

  /** remove 删除指定招聘任务。 */
  async function remove(task: any) {
    if (!(await confirm("删除任务", `确认删除“${task.name}”吗？`))) return;
    try {
      await cloudRequest(`/api/tasks/${encodeURIComponent(task.id)}`, {
        method: "DELETE",
      });
      notify("任务已删除", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "删除失败", "error");
    }
  }

  /**
   * loadLogs 从本地程序读取指定任务日志。
   * @param taskID - 要读取日志的任务 ID。
   * @param options - silent 为 true 时不显示按钮加载状态和错误弹窗。
   */
  async function loadLogs(taskID: string, options: { silent?: boolean } = {}) {
    if (!agentBase) return;
    if (!options.silent) setLogLoadingTaskID(taskID);
    try {
      const data = await localRequest(
        agentBase,
        `/api/v1/local/tasks/${encodeURIComponent(taskID)}/logs?limit=${LOG_LIMIT}`,
      );
      setLogs((value) => ({ ...value, [taskID]: data.logs || [] }));
    } catch (error) {
      if (!options.silent) {
        notify(
          error instanceof Error ? error.message : "日志读取失败",
          "error",
        );
      }
    } finally {
      if (!options.silent) setLogLoadingTaskID("");
    }
  }

  /** toggleLogs 展开或收起当前任务日志，只有展开时才读取日志。 */
  function toggleLogs(taskID: string) {
    if (expandedLogTaskID === taskID) {
      setExpandedLogTaskID("");
      return;
    }
    setExpandedLogTaskID(taskID);
    void loadLogs(taskID);
  }

  /** clearLogs 清空指定任务的本地日志。 */
  async function clearLogs(taskID: string) {
    if (
      !agentBase ||
      !(await confirm("清空任务日志", "确认清空该任务的本地日志吗？"))
    )
      return;
    try {
      await localRequest(
        agentBase,
        `/api/v1/local/tasks/${encodeURIComponent(taskID)}/logs`,
        { method: "DELETE" },
      );
      setLogs((current) => ({ ...current, [taskID]: [] }));
      notify("任务日志已清空", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "清空日志失败", "error");
    }
  }

  /**
   * loadAllLogs 拉取指定任务的完整日志并打开查看弹框。
   * @param task - 当前任务对象。
   */
  async function loadAllLogs(task: any) {
    if (!agentBase) return notify("本地程序未连接", "error");
    setAllLogTask(task);
    setAllLogLoading(true);
    try {
      const data = await localRequest(
        agentBase,
        `/api/v1/local/tasks/${encodeURIComponent(task.id)}/logs?limit=${ALL_LOG_LIMIT}`,
      );
      setAllLogs(data.logs || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "完整日志读取失败",
        "error",
      );
    } finally {
      setAllLogLoading(false);
    }
  }

  /** copyAllLogs 复制完整日志文本，方便用户反馈异常。 */
  async function copyAllLogs() {
    try {
      await navigator.clipboard.writeText(buildLogText(allLogs));
      notify("完整日志已复制", "success");
    } catch {
      notify("复制失败，请手动选中日志内容复制", "warning");
    }
  }

  return (
    <>
      <PageHeader
        title='任务列表'
        description='创建任务后由本地程序运行，云端保存任务和岗位配置。'
        actions={
          <>
            <Button
              variant='contained'
              startIcon={<AddRoundedIcon />}
              onClick={openCreate}
            >
              创建任务
            </Button>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      <SectionPanel>
        {taskList.length ? (
          <Stack spacing={1.5}>
            {taskList.map((task) => (
              <Box
                key={task.id}
                sx={{
                  borderBottom: "1px solid",
                  borderColor: "divider",
                  pb: 1.5,
                }}
              >
                <Stack
                  direction={{ xs: "column", md: "row" }}
                  spacing={2}
                  sx={{ alignItems: { md: "center" } }}
                >
                  <Box sx={{ flex: 1 }}>
                    <Stack
                      direction='row'
                      spacing={1}
                      sx={{ alignItems: "center" }}
                    >
                      <Typography sx={{ fontWeight: 760 }}>
                        {task.name || "未命名任务"}
                      </Typography>
                      <Chip
                        size='small'
                        color={
                          task.status === "running" ? "success" : "default"
                        }
                        label={statusLabel(task.status)}
                      />
                    </Stack>
                    <Typography
                      sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}
                    >
                      {task.position?.name || task.platform_id || "未选择岗位"}{" "}
                      · 本次上限 {Number(task.match_limit || 50)} · 总计{" "}
                      {Number(task.greeted_count || 0)} · 今日{" "}
                      {Math.max(
                        Number(task.today_greeted_count || 0),
                        Number(task.current_run_greeted_count || 0),
                      )}{" "}
                      · 本次 {Number(task.current_run_greeted_count || 0)} ·
                      提示音 {task.enable_sound ? "开" : "关"}
                    </Typography>
                  </Box>
                  <Stack
                    direction='row'
                    spacing={0.5}
                    sx={{ flexWrap: "wrap" }}
                  >
                    {task.status === "running" ? (
                      <Button
                        color='warning'
                        startIcon={<StopRoundedIcon />}
                        onClick={() => void stop(task)}
                      >
                        停止
                      </Button>
                    ) : (
                      <Button
                        variant='contained'
                        startIcon={<PlayArrowRoundedIcon />}
                        onClick={() => void run(task)}
                      >
                        开始
                      </Button>
                    )}
                    <Button
                      startIcon={<EditRoundedIcon />}
                      disabled={task.status === "running"}
                      onClick={() => openEdit(task)}
                    >
                      编辑
                    </Button>
                    <Button
                      component={Link}
                      href={`/admin/resumes?task_id=${encodeURIComponent(task.id)}`}
                    >
                      简历
                    </Button>
                    <Button
                      variant={
                        expandedLogTaskID === task.id ? "contained" : "text"
                      }
                      onClick={() => toggleLogs(task.id)}
                    >
                      {expandedLogTaskID === task.id ? "收起日志" : "日志"}
                    </Button>
                    <Button
                      color='error'
                      startIcon={<DeleteOutlineRoundedIcon />}
                      onClick={() => void remove(task)}
                    >
                      删除
                    </Button>
                  </Stack>
                </Stack>
                {expandedLogTaskID === task.id ? (
                  <TaskLogPanel
                    logs={logs[task.id] || []}
                    loading={logLoadingTaskID === task.id}
                    onRefresh={() => void loadLogs(task.id)}
                    onViewAll={() => void loadAllLogs(task)}
                    onClear={() => void clearLogs(task.id)}
                  />
                ) : null}
              </Box>
            ))}
          </Stack>
        ) : (
          <EmptyState text='暂无招聘任务' />
        )}
      </SectionPanel>
      <AdminDialog
        open={showForm}
        title={form.id ? "编辑招聘任务" : "创建招聘任务"}
        description='账号决定招聘平台，岗位模板决定筛选和详情识别方式。'
        confirmText={form.id ? "保存修改" : "创建任务"}
        loading={loading}
        maxWidth='md'
        onClose={() => setShowForm(false)}
        onConfirm={() => void save()}
      >
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" },
            gap: 2,
          }}
        >
          <TextField
            select
            label='岗位模板'
            value={form.position_id}
            onChange={(event) =>
              setForm({ ...form, position_id: event.target.value })
            }
          >
            {positions.map((item) => (
              <MenuItem key={item.id} value={item.id}>
                {item.name} · {item.platform_id || "未设置平台"}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            label='任务名称'
            value={form.name}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
            placeholder='不填写则按岗位自动生成'
          />
          <TextField
            label='本次打招呼上限'
            type='number'
            value={form.match_limit}
            onChange={(event) =>
              setForm({ ...form, match_limit: Number(event.target.value) })
            }
            helperText='本次运行达到该数量后自动停止，默认 50。'
          />
          <FormControlLabel
            control={
              <Switch
                checked={form.enable_sound}
                onChange={(event) =>
                  setForm({ ...form, enable_sound: event.target.checked })
                }
              />
            }
            label='打招呼成功后播放提示音'
          />
          <Box>
            <Typography sx={{ mb: 1, fontWeight: 800 }}>思考模式</Typography>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                gap: 1,
              }}
            >
              {[
                {
                  value: false,
                  title: "普通模式",
                  desc: "速度快，费用低，适合日常批量打招呼。准度正常，我会努力不添乱。",
                },
                {
                  value: true,
                  title: "思考模式",
                  desc: "准度更高，会多想一会儿；缺点是速度更慢，费用也更贵一点。",
                },
              ].map((option) => {
                const selected = form.enable_thinking === option.value;
                return (
                  <Box
                    key={option.title}
                    role='button'
                    tabIndex={0}
                    onClick={() =>
                      setForm({ ...form, enable_thinking: option.value })
                    }
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ")
                        setForm({ ...form, enable_thinking: option.value });
                    }}
                    sx={{
                      border: "1px solid",
                      borderColor: selected ? "#16724c" : "divider",
                      borderRadius: "8px",
                      bgcolor: selected ? "#edf7f1" : "#fff",
                      cursor: "pointer",
                      minHeight: 112,
                      p: 1.5,
                      transition:
                        "border-color .15s ease, background-color .15s ease",
                      "&:hover": {
                        bgcolor: selected ? "#e3f1e9" : "#fafbfa",
                        borderColor: selected ? "#16724c" : "#9bb8aa",
                      },
                    }}
                  >
                    <Stack
                      direction='row'
                      spacing={1}
                      sx={{ alignItems: "center", mb: 0.75 }}
                    >
                      <Box
                        sx={{
                          width: 12,
                          height: 12,
                          borderRadius: "50%",
                          border: "2px solid",
                          borderColor: selected ? "#16724c" : "#aeb8b2",
                          bgcolor: selected ? "#16724c" : "transparent",
                          flex: "0 0 auto",
                        }}
                      />
                      <Typography sx={{ fontWeight: 900, lineHeight: 1.2 }}>
                        {option.title}
                      </Typography>
                    </Stack>
                    <Typography
                      sx={{
                        mt: 0.5,
                        color: "text.secondary",
                        fontSize: 13,
                        lineHeight: 1.6,
                      }}
                    >
                      {option.desc}
                    </Typography>
                  </Box>
                );
              })}
            </Box>
          </Box>
        </Box>
      </AdminDialog>
      <AdminDialog
        open={Boolean(allLogTask)}
        title='查看全部任务日志'
        description='如果任务有异常，请点击复制全部，把完整日志发给作者。'
        confirmText='复制全部'
        cancelText='关闭'
        loading={allLogLoading}
        maxWidth='lg'
        onClose={() => {
          setAllLogTask(null);
          setAllLogs([]);
        }}
        onConfirm={() => void copyAllLogs()}
      >
        <Box
          component='pre'
          sx={{
            m: 0,
            maxHeight: 560,
            overflow: "auto",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            fontSize: 12,
            lineHeight: 1.7,
            bgcolor: "#f4f7f5",
            borderRadius: "8px",
            p: 1.5,
          }}
        >
          {allLogs.length ? buildLogText(allLogs) : "暂无日志"}
        </Box>
      </AdminDialog>
    </>
  );
}

/**
 * TaskLogPanel 渲染当前展开任务的本地日志面板。
 * @param props - 日志数据、加载状态和操作回调。
 * @returns 当前任务日志展示区域。
 */
function TaskLogPanel(props: {
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
        mt: 1.25,
        border: "1px solid",
        borderColor: "divider",
        borderRadius: 2.5,
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
        <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
          <Typography sx={{ fontSize: 13, fontWeight: 760 }}>
            本地任务日志（最近 {LOG_LIMIT} 条）
          </Typography>
          <Button size="small" onClick={onViewAll}>
            查看全部
          </Button>
          <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
            有异常请复制全部信息给作者
          </Typography>
        </Stack>
        <Stack direction="row" spacing={1}>
          <Button size="small" onClick={onRefresh} disabled={loading}>
            {loading ? "刷新中" : "刷新"}
          </Button>
          <Button color="error" size="small" onClick={onClear}>
            清空
          </Button>
        </Stack>
      </Stack>
      <Stack
        spacing={0.75}
        sx={{
          p: 1,
          maxHeight: 420,
          overflow: "auto",
        }}
      >
        {logs.length ? (
          logs.map((item, index) => (
            <LogLine
              key={String(
                item.id || `${item.created_at || item.time}-${index}`,
              )}
              item={item}
              previous={index > 0 ? logs[index - 1] : null}
            />
          ))
        ) : (
          <Box
            sx={{
              py: 4,
              textAlign: "center",
              color: "text.secondary",
              fontSize: 13,
            }}
          >
            暂无日志
          </Box>
        )}
      </Stack>
    </Box>
  );
}

/**
 * LogLine 渲染一条简单任务日志。
 * @param props - 当前日志和上一条日志。
 * @returns 单条日志展示内容。
 */
function LogLine(props: { item: any; previous: any | null }) {
  const { item, previous } = props;
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: {
          xs: "1fr",
          md: "190px 82px 82px minmax(0, 1fr)",
        },
        gap: 1,
        py: 0.75,
        borderBottom: "1px solid",
        borderColor: "divider",
      }}
    >
      <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
        {formatLogTime(item.created_at || item.time)}
      </Typography>
      <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
        {getLogDelta(item, previous)}
      </Typography>
      <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
        {getLogLevelLabel(item.level)}
      </Typography>
      <Typography
        sx={{
          fontSize: 13,
          lineHeight: 1.65,
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
        }}
      >
        {getLogMessage(item)}
      </Typography>
    </Box>
  );
}

/**
 * getLogMessage 提取日志正文。
 * @param item - 日志对象。
 * @returns 日志正文文本。
 */
function getLogMessage(item: any) {
  return String(item.message || item.msg || item.detail || "");
}

/**
 * getLogTimeMs 将日志时间转换为毫秒时间戳。
 * @param value - 原始时间。
 * @returns 毫秒时间戳，解析失败返回 0。
 */
function getLogTimeMs(value: unknown) {
  const time = new Date(String(value || "")).getTime();
  return Number.isNaN(time) ? 0 : time;
}

/**
 * formatLogTime 格式化日志时间，精确到毫秒。
 * @param value - 原始时间。
 * @returns 本地时间字符串。
 */
function formatLogTime(value: unknown) {
  const date = new Date(String(value || ""));
  if (Number.isNaN(date.getTime())) return "--";
  const pad = (input: number, size = 2) => String(input).padStart(size, "0");
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`;
}

/**
 * getLogDelta 计算当前日志距离上一条日志的耗时。
 * @param item - 当前日志。
 * @param previous - 上一条日志。
 * @returns 间隔毫秒文案。
 */
function getLogDelta(item: any, previous: any | null) {
  if (!previous) return "+0ms";
  const currentMs = getLogTimeMs(item.created_at || item.time);
  const previousMs = getLogTimeMs(previous.created_at || previous.time);
  if (!currentMs || !previousMs) return "+--ms";
  return `+${Math.max(0, currentMs - previousMs)}ms`;
}

/**
 * buildLogText 构建可复制的完整日志文本。
 * @param logs - 日志列表。
 * @returns 多行日志文本。
 */
function buildLogText(logs: any[]) {
  return logs
    .map((item, index) => {
      const previous = index > 0 ? logs[index - 1] : null;
      return `${formatLogTime(item.created_at || item.time)} ${getLogDelta(item, previous)} ${getLogLevelLabel(item.level)} ${getLogMessage(item)}`;
    })
    .join("\n");
}

/**
 * getLogLevelLabel 将日志等级转换成中文。
 * @param level - 原始日志等级。
 * @returns 中文日志等级。
 */
function getLogLevelLabel(level: string) {
  const value = String(level || "info").toLowerCase();
  return (
    (
      {
        error: "错误",
        warning: "警告",
        warn: "警告",
        info: "信息",
        debug: "调试",
      } as Record<string, string>
    )[value] || "信息"
  );
}

/**
 * pickLocalTaskStats 提取本地程序返回的任务统计。
 * @param task - 本地程序任务状态对象。
 * @returns 可合并到任务卡片的统计字段。
 */
function pickLocalTaskStats(task: any) {
  const result: Record<string, number> = {};
  [
    "scanned_count",
    "greeted_count",
    "skipped_count",
    "failed_count",
    "current_run_greeted_count",
  ].forEach((key) => {
    if (task?.[key] !== undefined && task?.[key] !== null) {
      result[key] = Number(task[key] || 0);
    }
  });
  return result;
}

/** statusLabel 将任务状态转换为中文。 */
function statusLabel(status: string) {
  return (
    (
      {
        created: "待运行",
        running: "运行中",
        done: "已停止",
        stopped: "已停止",
        failed: "失败",
      } as Record<string, string>
    )[String(status || "").toLowerCase()] ||
    status ||
    "未知"
  );
}
