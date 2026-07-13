<!-- 本文件负责展示 HR 视角的今日打招呼控制台。 -->
<template>
  <section class="panel greeting-dashboard">
    <div class="panel-header">
      <div>
        <h2>今日打招呼</h2>
        <p class="hint">{{ summaryText }}</p>
      </div>
      <button
        class="ghost"
        :disabled="tasks?.loading?.value"
        @click="tasks?.load?.()"
      >
        刷新
      </button>
    </div>

    <div class="metric-grid">
      <div class="metric-card primary">
        <span>今日已打招呼</span>
        <strong>{{ totals.greeted }}</strong>
      </div>
      <div class="metric-card">
        <span>运行中任务</span>
        <strong>{{ runningTasks.length }}</strong>
      </div>
      <div class="metric-card">
        <span>任务总数</span>
        <strong>{{ taskItems.length }}</strong>
      </div>
    </div>

    <div class="dashboard-layout">
      <div class="dashboard-block">
        <div class="block-title">
          <strong>正在进行</strong>
          <span>{{ runningTasks.length }} 个任务</span>
        </div>
        <div v-if="runningTasks.length" class="mini-list">
          <article v-for="task in runningTasks" :key="task.id" class="mini-row">
            <div>
              <strong>{{ taskName(task) }}</strong>
              <p class="hint">
                {{ accountName(task) }} · {{ modeLabel(task.mode) }}
              </p>
            </div>
            <span>已打 {{ todayCount(task, "greeted") }}</span>
          </article>
        </div>
        <p v-else class="hint">当前没有正在运行的任务。</p>
      </div>

      <div class="dashboard-block">
        <div class="block-title">
          <strong>今日排行</strong>
          <span>按今日打招呼排序</span>
        </div>
        <div v-if="topTasks.length" class="mini-list">
          <article v-for="task in topTasks" :key="task.id" class="mini-row">
            <div>
              <strong>{{ taskName(task) }}</strong>
              <p class="hint">
                {{ accountName(task) }} · {{ modeLabel(task.mode) }}
              </p>
            </div>
            <span>{{ todayCount(task, "greeted") }} 人</span>
          </article>
        </div>
        <p v-else class="hint">今天还没有打招呼数据。</p>
      </div>
    </div>

    <div class="dashboard-block">
      <div class="block-title">
        <strong>需要关注</strong>
        <span>{{ alerts.length }} 条</span>
      </div>
      <div v-if="alerts.length" class="alert-list">
        <p v-for="item in alerts" :key="item" class="warn">{{ item }}</p>
      </div>
      <p v-else class="success">今日打招呼数据正常。</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{ tasks: any }>();

const taskItems = computed(() => props.tasks?.tasks?.value || []);
const totals = computed(() => {
  return taskItems.value.reduce(
    (sum: any, task: any) => {
      sum.greeted += todayCount(task, "greeted");
      return sum;
    },
    { greeted: 0 },
  );
});
const runningTasks = computed(() =>
  taskItems.value.filter((task: any) => task.status === "running"),
);
const topTasks = computed(() =>
  [...taskItems.value]
    .sort(
      (a: any, b: any) => todayCount(b, "greeted") - todayCount(a, "greeted"),
    )
    .filter(
      (task: any) =>
        todayCount(task, "greeted") > 0,
    )
    .slice(0, 5),
);
const alerts = computed(() => {
  const result: string[] = [];
  if (!taskItems.value.length) {
    result.push("还没有任务。先创建任务后，控制台才会有打招呼数据。");
    return result;
  }
  if (!runningTasks.value.length && totals.value.greeted === 0) {
    result.push("当前没有运行中的任务，也没有今日打招呼结果。");
  }
  return result;
});
const summaryText = computed(() => {
  if (runningTasks.value.length) {
    return `当前有 ${runningTasks.value.length} 个任务运行中，今天已打招呼 ${totals.value.greeted} 人。`;
  }
  if (totals.value.greeted > 0) {
    return `今天已打招呼 ${totals.value.greeted} 人，建议查看后续回复。`;
  }
  return "今天还没有打招呼结果。";
});

/**
 * 读取任务今日计数。
 * @param {any} task - 任务对象。
 * @param {string} key - 计数字段名称。
 * @returns {number} 今日计数。
 */
function todayCount(task: any, key: string) {
  return Number(task?.[`today_${key}_count`] || 0);
}

/**
 * 返回任务展示名称。
 * @param {any} task - 任务对象。
 * @returns {string} 任务名称。
 */
function taskName(task: any) {
  return task?.position?.name || task?.position_name || "未命名岗位";
}

/**
 * 返回平台账号展示名称。
 * @param {any} task - 任务对象。
 * @returns {string} 平台账号名称。
 */
function accountName(task: any) {
  return (
    task?.platform_account?.display_name ||
    task?.platform_account_id ||
    "未选择账号"
  );
}

/**
 * 返回筛选模式中文。
 * @param {string} mode - 任务筛选模式。
 * @returns {string} 中文模式。
 */
function modeLabel(mode: string) {
  return mode === "ai" ? "AI筛选" : "关键词筛选";
}
</script>

<style scoped>
.greeting-dashboard {
  border-color: #2f4f2f;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}
.metric-card {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
}
.metric-card span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
}
.metric-card strong {
  display: block;
  margin-top: 4px;
  font-size: 28px;
  line-height: 1.1;
}
.metric-card.primary {
  border-color: var(--accent);
}
.metric-card.danger strong {
  color: var(--fg-error);
}
.dashboard-layout {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.dashboard-block {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
  margin-bottom: 12px;
}
.block-title,
.mini-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}
.block-title {
  margin-bottom: 8px;
}
.block-title span,
.mini-row span {
  color: var(--fg-dim);
  white-space: nowrap;
}
.mini-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.mini-row {
  border-top: 1px solid var(--border);
  padding-top: 8px;
}
.alert-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
@media (max-width: 760px) {
  .metric-grid,
  .dashboard-layout {
    grid-template-columns: 1fr;
  }
}
</style>
