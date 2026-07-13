<template>
  <section class="panel">
    <div class="panel-header">
      <h2>任务列表</h2>

      <div>
        <div
          class="panel-header"
          style="margin-bottom: 8px; border: none; padding-bottom: 0"
        >
          <button class="ghost" @click="showCreate = !showCreate">
            {{ showCreate ? "收起创建" : "+ 创建任务" }}
          </button>
        </div>
      </div>
      <button class="ghost" @click="tasks.load">刷新</button>
    </div>
    <!-- 创建任务折叠 -->

    <div v-if="showCreate" class="form-grid" style="margin-bottom: 12px">
      <label
        >账号<select
          v-model="tasks.form.value.platformAccountId"
          @change="onCreateAccountChange"
        >
          <option value="">请选择账号</option>
          <option v-for="acc in accounts" :key="acc.id" :value="acc.id">
            {{ accountLabel(acc) }}
          </option>
        </select></label
      >
      <label
        >岗位<select
          v-model="tasks.form.value.positionId"
          @change="onCreatePositionChange"
        >
          <option value="">请选择岗位</option>
          <option v-for="pos in positions" :key="pos.id" :value="pos.id">
            {{ pos.name }}
          </option>
        </select></label
      >
      <label
        >任务名称<input
          v-model.trim="tasks.form.value.name"
          @input="createNameEdited = true"
          placeholder="不填则自动使用岗位名称+默认模式"
      /></label>
      <p class="hint field-wide">
        默认模式：{{
          positionModeLabel(selectedCreatePosition())
        }}，来自岗位配置。
      </p>
      <label
        >本次打招呼上限<input
          v-model="tasks.form.value.matchLimit"
          type="number"
          min="1"
      /></label>
      <p class="hint field-wide">
        每次启动任务最多打招呼的人数，默认 50
        个；停止后下次启动会重新按这个数量计算。
      </p>
    </div>
    <div v-if="showCreate" class="mode-field" style="margin-bottom: 12px">
      <span class="field-title">思考模式</span>
      <div class="mode-cards">
        <button
          type="button"
          class="mode-card"
          :class="{ active: !tasks.form.value.enableThinking }"
          @click="tasks.form.value.enableThinking = false"
        >
          <strong>关闭</strong>
          <span>速度快，AI消耗更小<br />适合常见岗位、常见条件</span>
        </button>
        <button
          type="button"
          class="mode-card"
          :class="{ active: tasks.form.value.enableThinking }"
          @click="tasks.form.value.enableThinking = true"
        >
          <strong>开启</strong>
          <span>速度慢，更精准<br />AI消耗较多</span>
        </button>
      </div>
    </div>
    <div v-if="showCreate" class="actions">
      <button
        :disabled="
          tasks.loading.value ||
          !tasks.form.value.platformAccountId ||
          !tasks.form.value.positionId
        "
        @click="createTask"
      >
        {{ tasks.loading.value ? "创建中..." : "创建任务" }}
      </button>
    </div>

    <p v-if="tasks.tasks.value.length === 0" class="hint">暂无任务</p>
    <p v-if="tasks.message.value" class="success">{{ tasks.message.value }}</p>
    <p v-if="tasks.error.value" class="error">{{ tasks.error.value }}</p>

    <div v-else class="card-list">
      <article
        v-for="task in tasks.tasks.value"
        :key="task.id"
        class="card task-card"
      >
        <div class="task-main">
          <div class="task-title">
            <div>
              {{ task.name || task.position?.name || "未命名任务" }}
              |
              {{ taskAccountName(task) }}
              | {{ task.platform_id }} |
              {{ task.mode === "keyword" ? "关键词筛选" : "AI筛选" }}
            </div>

            <div>状态 {{ taskStatusLabel(task.status) }}</div>
          </div>
        </div>

        <div class="actions compact task-actions">
          <div class="task-actions-left">
            <button
              v-if="task.status !== 'running'"
              class="ghost primary"
              :disabled="tasks.loading.value"
              @click="executeTask(task.id)"
            >
              开始
            </button>
            <button
              v-else
              class="ghost danger"
              :disabled="tasks.loading.value"
              @click="tasks.stop(task.id)"
            >
              停止
            </button>
          </div>
          <div class="task-actions-right">
            <div class="task-metrics" aria-label="任务打招呼统计">
              <span>总计 {{ displayTotalGreetedCount(task) }}</span>
              <span>今天 {{ displayTodayGreetedCount(task) }}</span>
              <span>本次 {{ displayCurrentRunGreetedCount(task) }}</span>
              <span>本次上限 {{ displayRunGreetLimit(task) }}</span>
            </div>
            <label
              :class="[
                'sound-toggle',
                {
                  enabled: Boolean(task.enable_sound),
                  disabled: tasks.loading.value || task.status === 'running',
                },
              ]"
            >
              <div style="margin-right: 10px">
                <span class="sound-toggle-label">提示音</span>
                <input
                  type="checkbox"
                  :checked="Boolean(task.enable_sound)"
                  :disabled="tasks.loading.value || task.status === 'running'"
                  @change="onSoundToggle(task, $event)"
                />
              </div>
            </label>
            <button class="text-action" @click="tasks.toggleLogs(task.id)">
              {{
                tasks.expandedTaskId.value === task.id ? "收起日志" : "展开日志"
              }}
            </button>
            <button class="text-action" @click="openCandidates(task)">
              查看候选人
            </button>
            <button
              class="text-action"
              :disabled="tasks.loading.value || task.status === 'running'"
              @click="startEdit(task)"
            >
              {{ editingTaskId === task.id ? "取消编辑" : "编辑" }}
            </button>
            <button
              class="text-action danger-text"
              :disabled="tasks.loading.value || task.status === 'running'"
              @click="tasks.remove(task.id)"
            >
              删除
            </button>
          </div>
        </div>

        <div v-if="editingTaskId === task.id" class="log-panel">
          <div class="form-grid">
            <label
              >账号<select
                v-model="editForm.platformAccountId"
                @change="onEditAccountChange"
              >
                <option value="">请选择账号</option>
                <option v-for="acc in accounts" :key="acc.id" :value="acc.id">
                  {{ accountLabel(acc) }}
                </option>
              </select></label
            >
            <label
              >岗位<select
                v-model="editForm.positionId"
                @change="onEditPositionChange"
              >
                <option value="">请选择岗位</option>
                <option v-for="pos in positions" :key="pos.id" :value="pos.id">
                  {{ pos.name }}
                </option>
              </select></label
            >
            <label
              >任务名称<input
                v-model.trim="editForm.name"
                placeholder="不填则自动使用岗位名称+默认模式"
            /></label>
            <p class="hint field-wide">
              默认模式：{{
                positionModeLabel(selectedEditPosition())
              }}，来自岗位配置。
            </p>
            <label
              >本次打招呼上限<input
                v-model="editForm.matchLimit"
                type="number"
                min="1"
            /></label>
            <p class="hint field-wide">
              每次启动任务最多打招呼的人数，默认 50
              个；停止后下次启动会重新按这个数量计算。
            </p>
          </div>
          <div class="mode-field">
            <span class="field-title">思考模式</span>
            <div class="mode-cards">
              <button
                type="button"
                class="mode-card"
                :class="{ active: !editForm.enableThinking }"
                @click="editForm.enableThinking = false"
              >
                <strong>关闭</strong>
                <span>速度快，AI消耗更小<br />适合常见岗位、常见条件</span>
              </button>
              <button
                type="button"
                class="mode-card"
                :class="{ active: editForm.enableThinking }"
                @click="editForm.enableThinking = true"
              >
                <strong>开启</strong>
                <span>速度慢，更精准<br />AI消耗较多</span>
              </button>
            </div>
          </div>
          <div class="actions compact">
            <button
              :disabled="
                tasks.loading.value ||
                !editForm.platformAccountId ||
                !editForm.positionId
              "
              @click="saveEdit(task.id)"
            >
              保存参数
            </button>
          </div>
        </div>

        <!-- 日志面板 -->
        <div v-if="tasks.expandedTaskId.value === task.id" class="log-panel">
          <div class="log-panel-header">
            <button
              class="text-action danger-text"
              :disabled="tasks.loading.value"
              @click="tasks.clearLogs(task.id)"
            >
              清空日志
            </button>
          </div>
          <p
            v-if="
              !tasks.taskLogs.value[task.id] ||
              tasks.taskLogs.value[task.id].length === 0
            "
            class="hint"
          >
            暂无日志
          </p>
          <ol v-else class="log-list" @scroll="onLogScroll(task.id, $event)">
            <li
              v-for="(log, index) in tasks.taskLogs.value[task.id]"
              :key="log.id"
            >
              <span
                :class="{
                  error: log.level === 'error',
                  warn: log.level === 'warn',
                }"
                >{{ log.level }}</span
              >
              <time class="log-time">{{ formatLogTime(log.created_at) }}</time>
              <em
                v-if="formatLogGap(tasks.taskLogs.value[task.id], index)"
                class="log-gap"
                >{{ formatLogGap(tasks.taskLogs.value[task.id], index) }}</em
              >
              <strong
                :style="{
                  color:
                    log.level === 'error'
                      ? 'red'
                      : log.level === 'warn'
                        ? 'orange'
                        : 'inherit',
                }"
                >{{ log.message }}</strong
              >
            </li>
            <li v-if="tasks.taskLogLoadingMore.value[task.id]" class="log-more">
              正在加载更多日志...
            </li>
            <li
              v-else-if="tasks.taskLogHasMore.value[task.id]"
              class="log-more"
            >
              继续向下滚动加载更多
            </li>
          </ol>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { listPlatformAccounts } from "../services/api/accountApi";
const props = defineProps({
  tasks: Object,
  positions: Object,
  token: String,
  agent: Object,
});
const emit = defineEmits(["open-candidates", "request-login"]);
const showCreate = ref(false);
const createNameEdited = ref(false);
const DEFAULT_RUN_GREET_LIMIT = 50;
const accounts = ref<any[]>([]);
const accountsError = ref("");
const editingTaskId = ref("");
const editForm = ref({
  name: "",
  platformId: "boss",
  platformAccountId: "",
  positionId: "",
  mode: "keyword",
  matchLimit: 50,
  enableSound: false,
  enableThinking: false,
});
async function loadAccounts() {
  accountsError.value = "";
  try {
    accounts.value = await listPlatformAccounts();
  } catch (e: any) {
    accountsError.value = e.message;
  }
}

/**
 * 开始任务，未登录时先请求登录。
 * @param {string} taskId - 任务 ID。
 * @returns {void} 无返回值。
 */
function executeTask(taskId: string) {
  if (!props.token) {
    emit("request-login");
    return;
  }
  props.tasks.execute(taskId);
}
function accountLabel(account: any) {
  const platform = platformLabel(account?.platform_id);
  return `${account?.display_name || "未命名账号"} · ${platform}`;
}

/**
 * 返回任务卡片展示的账号名称。
 * @param {any} task - 当前任务。
 * @returns {string} 账号名称。
 */
function taskAccountName(task: any) {
  return task?.platform_account?.display_name || "未命名账号";
}
function platformLabel(platformId: string) {
  if (platformId === "boss") return "Boss直聘";
  if (platformId === "zhaopin") return "智联招聘";
  if (platformId === "liepin") return "猎聘";
  return platformId || "未知平台";
}
function selectedAccount(accountId: string) {
  return accounts.value.find((account: any) => account.id === accountId);
}
/**
 * 按 ID 查找岗位模板。
 * @param {string} positionId - 岗位模板 ID。
 * @returns {any} 岗位模板对象。
 */
function selectedPosition(positionId: string) {
  const items = Array.isArray(props.positions) ? props.positions : [];
  return items.find((position: any) => position.id === positionId);
}
/**
 * 返回新建任务当前选择的岗位模板。
 * @returns {any} 岗位模板对象。
 */
function selectedCreatePosition() {
  return selectedPosition(props.tasks?.form?.value?.positionId || "");
}
/**
 * 返回编辑任务当前选择的岗位模板。
 * @returns {any} 岗位模板对象。
 */
function selectedEditPosition() {
  return selectedPosition(editForm.value.positionId);
}
/**
 * 返回岗位模板默认模式。
 * @param {any} position - 岗位模板对象。
 * @returns {string} 默认模式。
 */
function positionMode(position: any) {
  if (!position) return "keyword";
  return position?.common_config?.mode_default === "ai" ? "ai" : "keyword";
}
/**
 * 返回岗位模板默认模式中文名。
 * @param {any} position - 岗位模板对象。
 * @returns {string} 默认模式中文名。
 */
function positionModeLabel(position: any) {
  return positionMode(position) === "keyword" ? "关键词筛选" : "AI筛选";
}
/**
 * 生成任务默认名称。
 * @param {any} position - 岗位模板对象。
 * @returns {string} 默认任务名称。
 */
function defaultTaskName(position: any) {
  if (!position) return "";
  return `${position.name || "未命名岗位"} ${positionModeLabel(position)}`;
}
function onCreateAccountChange() {
  if (!props.tasks?.form?.value) return;
  const account = selectedAccount(props.tasks.form.value.platformAccountId);
  props.tasks.form.value.platformId = account?.platform_id || "";
}
/**
 * 新建任务切换岗位模板时补齐默认模式和默认名称。
 * @returns {void} 无返回值。
 */
function onCreatePositionChange() {
  if (!props.tasks?.form?.value) return;
  const position = selectedCreatePosition();
  props.tasks.form.value.mode = positionMode(position);
  if (!createNameEdited.value) {
    props.tasks.form.value.name = defaultTaskName(position);
  }
}
async function createTask() {
  onCreateAccountChange();
  onCreatePositionChange();
  if (props.tasks) await props.tasks.create();
  createNameEdited.value = false;
  showCreate.value = false;
  await loadAccounts();
}
function startEdit(task: any) {
  if (editingTaskId.value === task.id) {
    editingTaskId.value = "";
    return;
  }
  editingTaskId.value = task.id;
  editForm.value = {
    name: task.name || "",
    platformId: task.platform_id || "boss",
    platformAccountId: task.platform_account_id || "",
    positionId: task.position_id || "",
    mode: task.mode || "keyword",
    matchLimit: displayRunGreetLimit(task),
    enableSound: Boolean(task.enable_sound),
    enableThinking: Boolean(task.enable_thinking),
  };
}
function onEditAccountChange() {
  const account = selectedAccount(editForm.value.platformAccountId);
  editForm.value.platformId = account?.platform_id || "";
}
/**
 * 编辑任务切换岗位模板时同步默认模式，并在名称为空时补齐默认名称。
 * @returns {void} 无返回值。
 */
function onEditPositionChange() {
  const position = selectedEditPosition();
  editForm.value.mode = positionMode(position);
  if (!editForm.value.name) {
    editForm.value.name = defaultTaskName(position);
  }
}
async function saveEdit(taskId: string) {
  if (!props.tasks) return;
  onEditAccountChange();
  onEditPositionChange();
  await props.tasks.update(taskId, editForm.value);
  editingTaskId.value = "";
}
async function toggleSound(task: any, enableSound: boolean) {
  if (!props.tasks) return;
  await props.tasks.update(task.id, {
    name: task.name || "",
    platformId: task.platform_id || "boss",
    platformAccountId: task.platform_account_id || "",
    positionId: task.position_id || "",
    mode: task.mode || "keyword",
    matchLimit: displayRunGreetLimit(task),
    enableSound,
  });
}
function onSoundToggle(task: any, event: Event) {
  const target = event.target as HTMLInputElement | null;
  void toggleSound(task, Boolean(target?.checked));
}
/**
 * 打开指定任务的候选人简历页面。
 * @param {any} task - 当前任务对象。
 * @returns {void} 无返回值。
 */
function openCandidates(task: any) {
  emit("open-candidates", task?.id || "");
}
function taskStatusLabel(status: string) {
  const key = String(status || "").toLowerCase();
  if (key === "created") return "待运行";
  if (key === "running") return "运行中";
  if (key === "done") return "已停止";
  if (key === "failed") return "失败";
  if (key === "stopped") return "已停止";
  return status || "未知";
}
/**
 * 返回任务累计打招呼数量。
 * @param {any} task - 当前任务。
 * @returns {number} 累计打招呼数量。
 */
function displayTotalGreetedCount(task: any) {
  return Number(task?.greeted_count || 0);
}

/**
 * 返回任务今日打招呼数量。
 * @param {any} task - 当前任务。
 * @returns {number} 今日打招呼数量。
 */
function displayTodayGreetedCount(task: any) {
  return Number(task?.today_greeted_count || 0);
}

/**
 * 返回任务本次运行打招呼数量。
 * @param {any} task - 当前任务。
 * @returns {number} 本次运行打招呼数量。
 */
function displayCurrentRunGreetedCount(task: any) {
  return Number(task?.current_run_greeted_count || 0);
}

/**
 * 返回任务本次打招呼上限。
 * @param {any} task - 当前任务。
 * @returns {number} 有效上限，空值默认 50。
 */
function displayRunGreetLimit(task: any) {
  const limit = Number(task?.match_limit || 0);
  if (!Number.isFinite(limit) || limit <= 0) return DEFAULT_RUN_GREET_LIMIT;
  return Math.floor(limit);
}
function onLogScroll(taskId: string, event: Event) {
  const target = event.target as HTMLElement | null;
  if (!target) return;
  const distanceToBottom =
    target.scrollHeight - target.scrollTop - target.clientHeight;
  if (distanceToBottom <= 24) {
    void props.tasks?.loadOlderLogs(taskId);
  }
}
/**
 * 格式化日志创建时间，精确到毫秒。
 * @param {string} value - 后端日志创建时间。
 * @returns {string} 本地时间文本。
 */
function formatLogTime(value: string) {
  const time = parseLogTime(value);
  if (!time) return "--";
  const date = new Date(time);
  const pad = (num: number, size = 2) => String(num).padStart(size, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`;
}

/**
 * 计算当前日志和下一条日志之间的时间间隔。
 * @param {any[]} logs - 当前任务的日志列表。
 * @param {number} index - 当前日志下标。
 * @returns {string} 间隔文本。
 */
function formatLogGap(logs: any[], index: number) {
  const current = parseLogTime(logs?.[index]?.created_at);
  const previous = parseLogTime(logs?.[index + 1]?.created_at);
  if (!current || !previous) return "";
  const gap = Math.abs(current - previous);
  if (gap < 1000) return `+${gap}ms`;
  if (gap < 60000) return `+${(gap / 1000).toFixed(2)}s`;
  const minutes = Math.floor(gap / 60000);
  const seconds = ((gap % 60000) / 1000).toFixed(1).padStart(4, "0");
  return `+${minutes}m${seconds}s`;
}

/**
 * 解析日志时间，兼容后端缺少时区的时间字符串。
 * @param {string} value - 后端日志创建时间。
 * @returns {number} 时间戳毫秒值。
 */
function parseLogTime(value: string) {
  if (!value) return 0;
  const source = String(value);
  const normalized = /(?:Z|[+-]\d{2}:?\d{2})$/.test(source)
    ? source
    : `${source}Z`;
  const time = Date.parse(normalized);
  return Number.isNaN(time) ? 0 : time;
}
onMounted(loadAccounts);
</script>

<style scoped>
.sound-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--fg-dim);
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
}

.sound-toggle input {
  position: absolute;
  opacity: 0;
  pointer-events: none;
}

.sound-toggle-label {
  line-height: 1;
}

.sound-toggle-track {
  position: relative;
  width: 32px;
  height: 18px;
  border-radius: 999px;
  background: #2b2b2b;
  border: 1px solid #3a3a3a;
  transition: background-color 0.2s ease;
  flex: 0 0 auto;
}

.sound-toggle-thumb {
  position: absolute;
  top: 1px;
  left: 1px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #d0d5dd;
  transition:
    transform 0.2s ease,
    background-color 0.2s ease;
}

.sound-toggle.enabled {
  color: var(--accent);
}

.sound-toggle.enabled .sound-toggle-track {
  background: rgba(0, 255, 0, 0.18);
  border-color: rgba(0, 255, 0, 0.35);
}

.sound-toggle.enabled .sound-toggle-thumb {
  transform: translateX(14px);
  background: var(--accent);
}

.sound-toggle.disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
</style>

<style scoped>
.task-card {
  display: block;
}
.run-options-panel {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
  gap: 8px;
  align-items: end;
  margin: 0 0 8px;
  padding: 10px;
  border: 1px solid var(--border);
  background: transparent;
}
.run-options-panel label {
  min-width: 0;
  color: var(--fg-dim);
  font-size: 12px;
}
.run-options-panel input[type="number"] {
  width: 100%;
  margin-top: 4px;
}
.run-option-check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  cursor: pointer;
}
.run-option-check input {
  width: 16px;
  height: 16px;
  margin: 0;
}
.run-options-hint {
  margin: 0 0 12px;
}
.mode-field {
  grid-column: 1 / -1;
}
.field-title {
  display: block;
  color: var(--fg-dim);
  font-size: 13px;
  margin-bottom: 6px;
}
.field-wide {
  grid-column: 1 / -1;
  margin: 0;
}
.mode-cards {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.mode-card {
  min-height: 74px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--fg-dim);
  text-align: left;
  padding: 10px 12px;
  cursor: pointer;
  font: inherit;
}
.mode-card strong {
  display: block;
  color: var(--fg);
  margin-bottom: 6px;
}
.mode-card span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.mode-card:hover {
  border-color: var(--accent);
}
.mode-card.active {
  border-color: var(--accent);
  box-shadow: inset 0 0 0 1px rgba(0, 255, 0, 0.35);
}
.mode-card.active strong {
  color: var(--accent);
}
.task-main {
  /* display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px; */
}
.task-title {
  display: flex;
  justify-content: space-between;
  min-width: 0;
}
.task-actions {
  margin-top: 8px;
  justify-content: space-between;
  width: 100%;
}
.task-actions-left,
.task-actions-right {
  display: flex;
  gap: 8px;
  align-items: center;
}
.task-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  color: var(--fg-dim);
}
.task-metrics span {
  white-space: nowrap;
}
.edit-actions {
  margin-top: 8px;
}
.text-action {
  background: transparent;
  border: none;
  padding: 0;
  color: var(--fg-dim);
  cursor: pointer;
  font-size: 13px;
}
.text-action:hover {
  color: var(--accent);
}
.text-action:disabled {
  color: var(--fg-muted);
  cursor: not-allowed;
}
.danger-text:hover {
  color: #f97066;
}
.log-panel-header {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 8px;
}
.log-list {
  max-height: 360px;
  overflow-y: auto;
  padding-right: 8px;
}
.log-more {
  color: var(--fg-dim);
  text-align: center;
}
.log-time,
.log-gap {
  display: inline-block;
  color: var(--fg-dim);
  font-size: 12px;
  font-style: normal;
  margin-right: 8px;
  white-space: nowrap;
}
.log-time {
  min-width: 86px;
}
.log-gap {
  min-width: 54px;
  color: #8fd18f;
}
@media (max-width: 900px) {
  .mode-cards {
    grid-template-columns: 1fr;
  }
  .task-main {
    flex-direction: column;
    align-items: flex-start;
  }
  .task-actions {
    flex-wrap: wrap;
    justify-content: space-between;
  }
}
</style>
