<!-- 本文件负责展示团队简历库和指定任务下的候选人简历。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>{{ taskId ? "任务候选人" : "简历库" }}</h2>
        <p class="sub-title">
          {{ taskId ? `当前任务：${taskId}` : "当前团队和当前用户的全部简历" }}
        </p>
      </div>
      <div class="header-actions">
        <button v-if="taskId" class="ghost" @click="showAll">查看全部简历</button>
        <button class="ghost" :disabled="loading" @click="load">
          {{ loading ? "刷新中..." : "刷新" }}
        </button>
      </div>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="!loading && candidates.length === 0" class="hint">
      暂无简历数据
    </p>

    <div v-else class="resume-layout">
      <div class="resume-list">
        <article
          v-for="item in candidates"
          :key="item.id"
          class="resume-card"
          :class="{ active: selected?.id === item.id }"
          @click="selectCandidate(item)"
        >
          <div class="resume-card-main">
            <strong>{{ candidateName(item) }}</strong>
            <span>{{ platformLabel(item.platform_id) }}</span>
          </div>
          <p class="resume-meta">
            {{ compactInfo(item) }}
          </p>
          <div class="score-row">
            <span>详情 {{ scoreText(item.ai_detail_score) }}</span>
            <span>打招呼 {{ scoreText(item.ai_greet_score) }}</span>
            <span v-if="item.greeted_at">已打招呼</span>
          </div>
        </article>
      </div>

      <aside v-if="selected" class="resume-detail">
        <div class="detail-header">
          <div>
            <h3>{{ candidateName(selected) }}</h3>
            <p>{{ compactInfo(selected) }}</p>
          </div>
          <span class="platform-tag">{{ platformLabel(selected.platform_id) }}</span>
        </div>

        <div class="detail-grid">
          <div><span>所属账号</span><strong>{{ selected.user_email || "--" }}</strong></div>
          <div><span>任务ID</span><strong>{{ selected.task_id || "--" }}</strong></div>
          <div><span>期望岗位</span><strong>{{ selected.expected_position || "--" }}</strong></div>
          <div><span>在线状态</span><strong>{{ selected.online_status || "--" }}</strong></div>
          <div><span>期望薪资</span><strong>{{ salaryText(selected) }}</strong></div>
          <div><span>创建时间</span><strong>{{ formatDate(selected.created_at) }}</strong></div>
        </div>

        <div class="score-panel">
          <div>
            <span>详情评分</span>
            <strong>{{ scoreText(selected.ai_detail_score) }}</strong>
            <p>{{ selected.ai_detail_reason || "无原因" }}</p>
          </div>
          <div>
            <span>打招呼评分</span>
            <strong>{{ scoreText(selected.ai_greet_score) }}</strong>
            <p>{{ selected.ai_greet_reason || "无原因" }}</p>
          </div>
          <div>
            <span>复核评分</span>
            <strong>{{ scoreText(selected.ai_review_score) }}</strong>
            <p>{{ selected.ai_review_reason || "无原因" }}</p>
          </div>
        </div>

        <section class="detail-section">
          <h4>基础信息</h4>
          <p>{{ selected.basic_info || selected.personal_description || "暂无基础信息" }}</p>
        </section>

        <section class="detail-section">
          <h4>候选人文本</h4>
          <pre>{{ selected.resume_text || selected.raw_text || selected.filter_text || "暂无文本" }}</pre>
        </section>
      </aside>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { listCandidates } from "../services/cloudApi";

const props = defineProps({
  initialTaskId: String,
});

const taskId = ref(props.initialTaskId || "");
const candidates = ref<any[]>([]);
const selected = ref<any>(null);
const loading = ref(false);
const error = ref("");

watch(
  () => props.initialTaskId,
  (value) => {
    taskId.value = value || "";
    void load();
  },
);

/**
 * 读取简历库数据。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    candidates.value = await listCandidates({ taskId: taskId.value, limit: 300 });
    selected.value = candidates.value[0] || null;
  } catch (e: any) {
    error.value = e?.message || "读取简历库失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 选择当前查看的候选人。
 * @param {any} item - 候选人简历对象。
 * @returns {void} 无返回值。
 */
function selectCandidate(item: any) {
  selected.value = item;
}

/**
 * 切换到全部简历视图。
 * @returns {void} 无返回值。
 */
function showAll() {
  taskId.value = "";
  const url = new URL(window.location.href);
  url.searchParams.set("menu", "resume-library");
  url.searchParams.delete("task_id");
  window.history.replaceState({}, "", url.toString());
  void load();
}

/**
 * 返回候选人展示姓名。
 * @param {any} item - 候选人简历对象。
 * @returns {string} 展示姓名。
 */
function candidateName(item: any) {
  return item?.candidate_name || "未知候选人";
}

/**
 * 返回平台中文名称。
 * @param {string} platformId - 平台 ID。
 * @returns {string} 平台名称。
 */
function platformLabel(platformId: string) {
  if (platformId === "boss") return "Boss直聘";
  if (platformId === "zhaopin") return "智联招聘";
  if (platformId === "liepin") return "猎聘";
  return platformId || "未知平台";
}

/**
 * 拼接候选人摘要信息。
 * @param {any} item - 候选人简历对象。
 * @returns {string} 摘要文案。
 */
function compactInfo(item: any) {
  return [
    item?.work_region,
    item?.work_years,
    item?.education_level,
    item?.expected_position,
  ]
    .filter(Boolean)
    .join(" / ") || "暂无摘要";
}

/**
 * 格式化评分展示。
 * @param {number | null | undefined} score - AI 分数。
 * @returns {string} 分数字符串。
 */
function scoreText(score: number | null | undefined) {
  if (score === null || score === undefined || score === "") return "--";
  const value = Number(score);
  if (Number.isNaN(value)) return "--";
  return value.toFixed(1);
}

/**
 * 格式化薪资展示。
 * @param {any} item - 候选人简历对象。
 * @returns {string} 薪资文案。
 */
function salaryText(item: any) {
  const min = item?.expected_salary_min;
  const max = item?.expected_salary_max;
  if (min && max) return `${min}-${max}`;
  if (min) return `${min}起`;
  if (max) return `${max}以内`;
  return "--";
}

/**
 * 格式化日期展示。
 * @param {string} value - 日期字符串。
 * @returns {string} 本地日期时间。
 */
function formatDate(value: string) {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return date.toLocaleString();
}

onMounted(load);
</script>

<style scoped>
.sub-title {
  margin: 4px 0 0;
  color: var(--fg-dim);
  font-size: 13px;
}
.header-actions {
  display: flex;
  gap: 8px;
}
.resume-layout {
  display: grid;
  grid-template-columns: minmax(280px, 360px) minmax(0, 1fr);
  gap: 12px;
  min-height: 0;
}
.resume-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: calc(100vh - 170px);
  overflow: auto;
}
.resume-card {
  border: 1px solid #333;
  background: #060606;
  padding: 10px;
  cursor: pointer;
}
.resume-card:hover,
.resume-card.active {
  border-color: #0f0;
}
.resume-card-main {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  color: #eee;
}
.resume-card-main span,
.resume-meta,
.score-row {
  color: var(--fg-dim);
  font-size: 12px;
}
.resume-meta {
  margin: 6px 0;
}
.score-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.resume-detail {
  border: 1px solid #333;
  background: #050505;
  padding: 14px;
  max-height: calc(100vh - 170px);
  overflow: auto;
}
.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid #333;
  padding-bottom: 12px;
}
.detail-header h3 {
  margin: 0;
}
.detail-header p {
  color: var(--fg-dim);
  margin: 6px 0 0;
}
.platform-tag {
  color: #0f0;
  white-space: nowrap;
}
.detail-grid,
.score-panel {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin-top: 12px;
}
.detail-grid div,
.score-panel div {
  border: 1px solid #2d2d2d;
  padding: 10px;
  min-width: 0;
}
.detail-grid span,
.score-panel span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
  margin-bottom: 6px;
}
.detail-grid strong,
.score-panel strong {
  color: #eee;
  overflow-wrap: anywhere;
}
.score-panel p,
.detail-section p {
  margin: 6px 0 0;
  color: var(--fg-dim);
  line-height: 1.6;
}
.detail-section {
  margin-top: 14px;
}
.detail-section h4 {
  margin: 0 0 8px;
  color: #eee;
}
.detail-section pre {
  white-space: pre-wrap;
  word-break: break-word;
  border: 1px solid #2d2d2d;
  background: #030303;
  padding: 10px;
  color: var(--fg-dim);
  line-height: 1.6;
  max-height: 320px;
  overflow: auto;
}
@media (max-width: 980px) {
  .resume-layout,
  .detail-grid,
  .score-panel {
    grid-template-columns: 1fr;
  }
  .resume-list,
  .resume-detail {
    max-height: none;
  }
}
</style>
