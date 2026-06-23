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
  formatDate,
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
  platform_account_id: "",
  position_id: "",
  match_limit: 50,
  enable_sound: false,
  enable_thinking: false,
};
const LOG_REFRESH_MS = 3000;
const LOG_LIMIT = 50;

/** TasksPage 管理招聘任务完整生命周期。 */
export default function TasksPage() {
  const { agentBase, user, notify, confirm } = useAdmin();
  const [tasks, setTasks] = useState<any[]>([]);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [positions, setPositions] = useState<any[]>([]);
  const [logs, setLogs] = useState<Record<string, any[]>>({});
  const [expandedLogTaskID, setExpandedLogTaskID] = useState("");
  const [expandedLogRows, setExpandedLogRows] = useState<
    Record<string, boolean>
  >({});
  const [logLoadingTaskID, setLogLoadingTaskID] = useState("");
  const [runningTaskIDs, setRunningTaskIDs] = useState<Record<string, boolean>>(
    {},
  );
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ ...emptyForm });

  /** load 读取任务及创建任务需要的账号和岗位。 */
  async function load() {
    setLoading(true);
    try {
      const [taskData, accountData, positionData] = await Promise.all([
        cloudRequest("/api/tasks"),
        cloudRequest("/api/platform-accounts"),
        cloudRequest("/api/positions"),
      ]);
      setTasks(taskData.tasks || []);
      setAccounts(accountData.accounts || []);
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
        status: runningTaskIDs[task.id] ? "running" : task.status,
      })),
    [runningTaskIDs, tasks],
  );

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
    const account = accounts.find(
      (item) => item.id === form.platform_account_id,
    );
    const position = positions.find((item) => item.id === form.position_id);
    if (!account || !position)
      return notify("请选择平台账号和岗位模板", "warning");
    setLoading(true);
    try {
      const payload = {
        name: form.name.trim() || `${position.name}招聘任务`,
        platform_id: account.platform_id,
        platform_account_id: account.id,
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
      platform_account_id:
        task.platform_account_id || task.platform_account?.id || "",
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
      if (data.running === false) {
        setRunningTaskIDs((current) => ({ ...current, [taskID]: false }));
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
  async function loadLogs(
    taskID: string,
    options: { silent?: boolean } = {},
  ) {
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
        notify(error instanceof Error ? error.message : "日志读取失败", "error");
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

  /** toggleLogRow 展开或收起单条长日志内容。 */
  function toggleLogRow(rowID: string) {
    setExpandedLogRows((current) => ({
      ...current,
      [rowID]: !current[rowID],
    }));
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
                      {task.platform_account?.display_name || "未选择账号"} ·
                      本次上限 {Number(task.match_limit || 50)} · 总计{" "}
                      {Number(task.greeted_count || 0)} · 今日{" "}
                      {Number(task.today_greeted_count || 0)} · 本次{" "}
                      {Number(task.current_run_greeted_count || 0)} · 提示音{" "}
                      {task.enable_sound ? "开" : "关"}
                    </Typography>
                    <Typography
                      sx={{ mt: 0.5, color: "text.secondary", fontSize: 12 }}
                    >
                      {/* 更新时间：{formatDate(task.updated_at)} */}
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
                      variant={expandedLogTaskID === task.id ? "contained" : "text"}
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
                    expandedRows={expandedLogRows}
                    onRefresh={() => void loadLogs(task.id)}
                    onClear={() => void clearLogs(task.id)}
                    onToggleRow={toggleLogRow}
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
            label='任务名称'
            value={form.name}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
            placeholder='不填写则按岗位自动生成'
          />
          <TextField
            select
            label='平台账号'
            value={form.platform_account_id}
            onChange={(event) => {
              const account = accounts.find(
                (item) => item.id === event.target.value,
              );
              const validPosition = positions.find(
                (item) =>
                  item.id === form.position_id &&
                  item.platform_id === account?.platform_id,
              );
              setForm({
                ...form,
                platform_account_id: event.target.value,
                position_id: validPosition ? form.position_id : "",
              });
            }}
          >
            {accounts.map((item) => (
              <MenuItem key={item.id} value={item.id}>
                {item.display_name} · {item.platform_id}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            label='岗位模板'
            value={form.position_id}
            onChange={(event) =>
              setForm({ ...form, position_id: event.target.value })
            }
          >
            {positions
              .filter((item) => {
                const account = accounts.find(
                  (current) => current.id === form.platform_account_id,
                );
                return (
                  !account ||
                  !item.platform_id ||
                  item.platform_id === account.platform_id
                );
              })
              .map((item) => (
                <MenuItem key={item.id} value={item.id}>
                  {item.name}
                </MenuItem>
              ))}
          </TextField>
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
          <FormControlLabel
            control={
              <Switch
                checked={form.enable_thinking}
                onChange={(event) =>
                  setForm({ ...form, enable_thinking: event.target.checked })
                }
              />
            }
            label='在浏览器显示 AI 思考过程'
          />
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
  expandedRows: Record<string, boolean>;
  onRefresh: () => void;
  onClear: () => void;
  onToggleRow: (rowID: string) => void;
}) {
  const { logs, loading, expandedRows, onRefresh, onClear, onToggleRow } = props;
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
        <Typography sx={{ fontSize: 13, fontWeight: 760 }}>
          本地任务日志（最近 {LOG_LIMIT} 条）
        </Typography>
        <Stack direction='row' spacing={1}>
          <Button size='small' onClick={onRefresh} disabled={loading}>
            {loading ? "刷新中" : "刷新"}
          </Button>
          <Button color='error' size='small' onClick={onClear}>
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
          logs.map((item, index) => {
            const rowID = getLogRowID(item, index);
            const message = getLogMessage(item);
            const expanded = Boolean(expandedRows[rowID]);
            const canExpand = message.length > 150 || message.includes("\n");
            return (
              <Box
                key={rowID}
                sx={{
                  display: "grid",
                  gridTemplateColumns: {
                    xs: "1fr",
                    md: "112px 72px minmax(0, 1fr)",
                  },
                  gap: 1,
                  px: 1,
                  py: 0.9,
                  borderRadius: 2,
                  bgcolor: "#fff",
                  border: "1px solid",
                  borderColor: "rgba(30, 41, 59, 0.08)",
                }}
              >
                <Typography
                  sx={{ color: "text.secondary", fontSize: 12, pt: 0.25 }}
                >
                  {formatDate(item.created_at || item.time)}
                </Typography>
                <Chip
                  size='small'
                  color={getLogLevelColor(item.level)}
                  label={getLogLevelLabel(item.level)}
                  sx={{ width: "fit-content", height: 24 }}
                />
                <Box sx={{ minWidth: 0 }}>
                  <Typography
                    sx={{
                      color: "text.primary",
                      fontSize: 13,
                      lineHeight: 1.65,
                      whiteSpace: "pre-wrap",
                      wordBreak: "break-word",
                      maxHeight: expanded || !canExpand ? "none" : 44,
                      overflow: "hidden",
                    }}
                  >
                    {message}
                  </Typography>
                  {canExpand ? (
                    <Button
                      size='small'
                      sx={{ mt: 0.25, minWidth: 0, px: 0 }}
                      onClick={() => onToggleRow(rowID)}
                    >
                      {expanded ? "收起" : "展开"}
                    </Button>
                  ) : null}
                </Box>
              </Box>
            );
          })
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
 * getLogRowID 生成日志行的稳定展示 ID。
 * @param item - 日志对象。
 * @param index - 当前列表下标。
 * @returns 日志行 ID。
 */
function getLogRowID(item: any, index: number) {
  return String(item.id || `${item.created_at || item.time || "log"}-${index}`);
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
 * getLogLevelColor 将日志等级转换成 MUI 颜色。
 * @param level - 原始日志等级。
 * @returns 标签颜色。
 */
function getLogLevelColor(level: string) {
  const value = String(level || "info").toLowerCase();
  if (value === "error") return "error";
  if (value === "warning" || value === "warn") return "warning";
  if (value === "debug") return "default";
  return "info";
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
