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
        >岗位模板<select v-model="tasks.form.value.positionId">
          <option value="">不使用模板</option>
          <option v-for="pos in positions" :key="pos.id" :value="pos.id">
            {{ pos.name }}
          </option>
        </select></label
      >
      <div class="mode-field">
        <span class="field-title">筛选模式</span>
        <div class="mode-cards" role="radiogroup" aria-label="筛选模式">
          <button
            v-for="option in modeOptions"
            :key="option.value"
            type="button"
            class="mode-card"
            :class="{ active: tasks.form.value.mode === option.value }"
            role="radio"
            :aria-checked="tasks.form.value.mode === option.value"
            @click="tasks.form.value.mode = option.value"
          >
            <strong>{{ option.label }}</strong>
            <span>{{ option.description }}</span>
          </button>
        </div>
      </div>
      <label
        >匹配上限<input
          v-model="tasks.form.value.matchLimit"
          type="number"
          min="1"
      /></label>
    </div>
    <div v-if="showCreate" class="actions">
      <button
        :disabled="tasks.loading.value || !tasks.form.value.platformAccountId"
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
              {{ task.position.name }}
              |
              {{
                task.platform_account.display_name || task.platform_account_id
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
        </div>

        <div class="actions compact task-actions">
          <div class="task-actions-left">
            <button
              v-if="task.status !== 'running'"
              class="ghost primary"
              :disabled="tasks.loading.value"
              @click="tasks.execute(task.id)"
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
                <option
                  v-for="acc in accounts"
                  :key="acc.id"
                  :value="acc.id"
                >
                  {{ accountLabel(acc) }}
                </option>
              </select></label
            >
            <label
              >岗位模板<select v-model="editForm.positionId">
                <option value="">不使用模板</option>
                <option v-for="pos in positions" :key="pos.id" :value="pos.id">
                  {{ pos.name }}
                </option>
              </select></label
            >
            <div class="mode-field">
              <span class="field-title">筛选模式</span>
              <div class="mode-cards" role="radiogroup" aria-label="筛选模式">
                <button
                  v-for="option in modeOptions"
                  :key="option.value"
                  type="button"
                  class="mode-card"
                  :class="{ active: editForm.mode === option.value }"
                  role="radio"
                  :aria-checked="editForm.mode === option.value"
                  @click="editForm.mode = option.value"
                >
                  <strong>{{ option.label }}</strong>
                  <span>{{ option.description }}</span>
                </button>
              </div>
            </div>
            <label
              >匹配上限<input
                v-model="editForm.matchLimit"
                type="number"
                min="1"
            /></label>
          </div>
          <div class="actions compact">
            <button
              :disabled="tasks.loading.value || !editForm.platformAccountId"
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
          <ol
            v-else
            class="log-list"
            @scroll="onLogScroll(task.id, $event)"
          >
            <li v-for="log in tasks.taskLogs.value[task.id]" :key="log.id">
              <span
                :class="{
                  error: log.level === 'error',
                  warn: log.level === 'warn',
                }"
                >{{ log.level }}</span
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
            <li
              v-if="tasks.taskLogLoadingMore.value[task.id]"
              class="log-more"
            >
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
import { listPlatformAccounts } from "../services/cloudApi";
const props = defineProps({
  tasks: Object,
  positions: Object,
  token: String,
  agent: Object,
});
const emit = defineEmits(["open-candidates"]);
const showCreate = ref(false);
const statRange = ref("today");
const accounts = ref<any[]>([]);
const accountsError = ref("");
const modeOptions = [
  {
    value: "keyword",
    label: "关键词筛选",
    description: "按关键词和排除词快速判断，免费且稳定。",
  },
  {
    value: "ai",
    label: "AI筛选",
    description: "结合岗位要求打分，适合更细的候选人判断。",
  },
];
const editingTaskId = ref("");
const editForm = ref({
  platformId: "boss",
  platformAccountId: "",
  positionId: "",
  mode: "keyword",
  matchLimit: 20,
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
function onCreateAccountChange() {
  const account = selectedAccount(tasks.form.value.platformAccountId);
  tasks.form.value.platformId = account?.platform_id || "";
}
async function createTask() {
  onCreateAccountChange();
  if (props.tasks) await props.tasks.create();
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
    platformId: task.platform_id || "boss",
    platformAccountId: task.platform_account_id || "",
    positionId: task.position_id || "",
    mode: task.mode || "keyword",
    matchLimit: task.match_limit || 20,
    enableSound: Boolean(task.enable_sound),
  };
}
function onEditAccountChange() {
  const account = selectedAccount(editForm.value.platformAccountId);
  editForm.value.platformId = account?.platform_id || "";
}
async function saveEdit(taskId: string) {
  if (!props.tasks) return;
  onEditAccountChange();
  await props.tasks.update(taskId, editForm.value);
  editingTaskId.value = "";
}
async function toggleSound(task: any, enableSound: boolean) {
  if (!props.tasks) return;
  await props.tasks.update(task.id, {
    platformId: task.platform_id || "boss",
    platformAccountId: task.platform_account_id || "",
    positionId: task.position_id || "",
    mode: task.mode || "keyword",
    matchLimit: task.match_limit || 20,
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
function onLogScroll(taskId: string, event: Event) {
  const target = event.target as HTMLElement | null;
  if (!target) return;
  const distanceToBottom =
    target.scrollHeight - target.scrollTop - target.clientHeight;
  if (distanceToBottom <= 24) {
    void tasks.loadOlderLogs(taskId);
  }
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
  color: #0f0;
}

.sound-toggle.enabled .sound-toggle-track {
  background: rgba(0, 255, 0, 0.18);
  border-color: rgba(0, 255, 0, 0.35);
}

.sound-toggle.enabled .sound-toggle-thumb {
  transform: translateX(14px);
  background: #0f0;
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
  border: 1px solid #2f2f2f;

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
  border-left: 1px solid #2f2f2f;
}
.range-tab.active {
  color: #0f0;
  box-shadow: inset 0 -1px 0 #0f0;
}
.range-tab:hover {
  color: #ddd;
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
.mode-cards {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.mode-card {
  min-height: 74px;
  border: 1px solid #333;
  background: #050505;
  color: #ddd;
  text-align: left;
  padding: 10px 12px;
  cursor: pointer;
  font: inherit;
}
.mode-card strong {
  display: block;
  color: #fff;
  margin-bottom: 6px;
}
.mode-card span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.mode-card:hover {
  border-color: #0f0;
}
.mode-card.active {
  border-color: #0f0;
  box-shadow: inset 0 0 0 1px rgba(0, 255, 0, 0.35);
}
.mode-card.active strong {
  color: #0f0;
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
.stat-chip {
  border: 1px solid #333;
  color: #ddd;
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
  color: #0f0;
}
.text-action:disabled {
  color: #666;
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
