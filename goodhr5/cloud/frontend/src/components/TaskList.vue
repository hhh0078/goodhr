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
    <div class="task-range-tabs" role="tablist" aria-label="任务统计范围">
      <button
        type="button"
        class="range-tab"
        :class="{ active: statRange === 'today' }"
        role="tab"
        :aria-selected="statRange === 'today'"
        @click="statRange = 'today'"
      >
        仅看今天
      </button>
      <button
        type="button"
        class="range-tab"
        :class="{ active: statRange === 'all' }"
        role="tab"
        :aria-selected="statRange === 'all'"
        @click="statRange = 'all'"
      >
        全部时间
      </button>
    </div>
    <div v-if="tasks.localTaskMode?.()" class="run-options-panel">
      <label class="run-option-check">
        <input v-model="tasks.runOptions.value.enableGreet" type="checkbox" />
        <span>真实打招呼</span>
      </label>
      <label
        >扫描轮数<input
          v-model.number="tasks.runOptions.value.scanRounds"
          type="number"
          min="1"
          max="20"
      /></label>
      <label
        >每轮数量<input
          v-model.number="tasks.runOptions.value.maxItems"
          type="number"
          min="1"
          max="100"
      /></label>
      <label
        >滚动距离<input
          v-model.number="tasks.runOptions.value.scrollDistance"
          type="number"
          min="120"
          max="3000"
      /></label>
      <label
        >等待最小秒<input
          v-model.number="tasks.runOptions.value.greetDelayMin"
          type="number"
          min="0"
          step="0.5"
      /></label>
      <label
        >等待最大秒<input
          v-model.number="tasks.runOptions.value.greetDelayMax"
          type="number"
          min="0"
          step="0.5"
      /></label>
      <label
        >失败重试<input
          v-model.number="tasks.runOptions.value.greetRetries"
          type="number"
          min="0"
          max="5"
      /></label>
    </div>
    <p v-if="tasks.localTaskMode?.()" class="hint run-options-hint">
      不勾选真实打招呼时，只会扫描、过滤和 AI 评分，不会点击平台按钮。
    </p>

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
        >岗位模板<select
          v-model="tasks.form.value.positionId"
          @change="onCreatePositionChange"
        >
          <option value="">请选择岗位模板</option>
          <option v-for="pos in positions" :key="pos.id" :value="pos.id">
            {{ pos.name }}
          </option>
        </select></label
      >
      <label
        >任务名称<input
          v-model.trim="tasks.form.value.name"
          @input="createNameEdited = true"
          placeholder="不填则自动使用岗位模板名称+默认模式"
      /></label>
      <p class="hint field-wide">
        默认模式：{{
          positionModeLabel(selectedCreatePosition())
        }}，来自岗位模板配置。
      </p>
      <label
        >匹配上限<input
          v-model="tasks.form.value.matchLimit"
          type="number"
          min="1"
      /></label>
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
              {{
                task.platform_account?.display_name || task.platform_account_id
              }}
              | {{ task.platform_id }} |
              {{ task.mode === "keyword" ? "关键词筛选" : "AI筛选" }} |
              {{ task.match_limit }}
            </div>

            <div>状态 {{ taskStatusLabel(task.status) }}</div>
          </div>
          <div class="task-stats">
            <span class="stat-chip"
              >扫描 {{ displayTaskCount(task, "scanned") }}</span
            >
            <span class="stat-chip"
              >打招呼 {{ displayTaskCount(task, "greeted") }}</span
            >
            <span class="stat-chip"
              >跳过 {{ displayTaskCount(task, "skipped") }}</span
            >
            <span class="stat-chip"
              >失败 {{ displayTaskCount(task, "failed") }}</span
            >
          </div>
          <div
            v-if="taskProgress(task)"
            class="task-progress"
            :class="{ running: taskProgress(task)?.stage === 'running' || task.status === 'running' }"
          >
            <div class="task-progress-meta">
              <span>{{ taskProgress(task)?.message || taskStatusLabel(task.status) }}</span>
              <span v-if="taskProgress(task)?.total_rounds">
                {{ taskProgress(task)?.round || 0 }}/{{ taskProgress(task)?.total_rounds }}
              </span>
            </div>
            <div class="task-progress-bar">
              <span :style="{ width: taskProgressPercent(task) + '%' }"></span>
            </div>
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
              >岗位模板<select
                v-model="editForm.positionId"
                @change="onEditPositionChange"
              >
                <option value="">请选择岗位模板</option>
                <option v-for="pos in positions" :key="pos.id" :value="pos.id">
                  {{ pos.name }}
                </option>
              </select></label
            >
            <label
              >任务名称<input
                v-model.trim="editForm.name"
                placeholder="不填则自动使用岗位模板名称+默认模式"
            /></label>
            <p class="hint field-wide">
              默认模式：{{
                positionModeLabel(selectedEditPosition())
              }}，来自岗位模板配置。
            </p>
            <label
              >匹配上限<input
                v-model="editForm.matchLimit"
                type="number"
                min="1"
            /></label>
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
const statRange = ref("today");
const createNameEdited = ref(false);
const accounts = ref<any[]>([]);
const accountsError = ref("");
const editingTaskId = ref("");
const editForm = ref({
  name: "",
  platformId: "boss",
  platformAccountId: "",
  positionId: "",
  mode: "ai",
  matchLimit: 50,
  enableSound: false,
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
  return `${account?.display_name || account?.id || "未命名账号"} · ${platform}`;
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
  return position?.common_config?.mode_default === "keyword" ? "keyword" : "ai";
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
    matchLimit: task.match_limit || 50,
    enableSound: Boolean(task.enable_sound),
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
    matchLimit: task.match_limit || 50,
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
function displayTaskCount(task: any, key: string) {
  if (statRange.value === "today") {
    return Number(task?.[`today_${key}_count`] || 0);
  }
  return Number(task?.[`${key}_count`] || 0);
}
/**
 * 返回任务运行进度。
 * @param {any} task - 当前任务。
 * @returns {any} 进度对象。
 */
function taskProgress(task: any) {
  return props.tasks?.taskProgress?.value?.[task.id] || null;
}
/**
 * 返回任务进度百分比。
 * @param {any} task - 当前任务。
 * @returns {number} 百分比。
 */
function taskProgressPercent(task: any) {
  const progress = taskProgress(task);
  const total = Number(progress?.total_rounds || 0);
  const round = Number(progress?.round || 0);
  if (!total) {
    if (task.status === "completed") return 100;
    if (task.status === "running") return 8;
    return 0;
  }
  return Math.max(0, Math.min(100, Math.round((round / total) * 100)));
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
.task-range-tabs {
  display: inline-flex;
  align-items: center;
  gap: 0;
  border: 1px solid var(--border);

  margin-bottom: 12px;
  background: transparent;
}
.range-tab {
  border: none;
  background: transparent;
  color: var(--fg-dim);
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
  line-height: 1.2;
}
.range-tab + .range-tab {
  border-left: 1px solid var(--border);
}
.range-tab.active {
  color: var(--accent);
  box-shadow: inset 0 -1px 0 var(--accent);
}
.range-tab:hover {
  color: var(--fg-dim);
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
.task-stats {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 8px;
  margin-bottom: 8px;
  /* justify-content: flex-end; */
}
.task-progress {
  margin: 8px 0 10px;
  max-width: 520px;
}
.task-progress-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.4;
  margin-bottom: 5px;
}
.task-progress-bar {
  height: 6px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  overflow: hidden;
}
.task-progress-bar span {
  display: block;
  height: 100%;
  min-width: 0;
  background: var(--accent);
  transition: width 0.2s ease;
}
.task-progress.running .task-progress-bar span {
  opacity: 0.85;
}
.stat-chip {
  border: 1px solid var(--border);
  color: var(--fg-dim);
  padding: 4px 10px;
  font-size: 14px;
  font-weight: bold;
  /* line-height: 1.3; */
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
  .task-stats {
    justify-content: flex-start;
  }
  .task-actions {
    flex-wrap: wrap;
    justify-content: space-between;
  }
}
</style>
