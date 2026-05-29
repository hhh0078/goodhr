<!-- 本文件负责展示简历库卡片列表、搜索筛选和分页。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>{{ filters.taskId ? "任务候选人" : "简历库" }}</h2>
        <p class="sub-title">
          {{
            filters.taskId ? "按任务筛选候选人" : "当前团队和当前用户的全部简历"
          }}
        </p>
      </div>
      <button class="ghost" :disabled="loading" @click="load">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <div class="filter-panel">
      <label>
        搜索
        <input
          v-model.trim="filters.keyword"
          placeholder="姓名、手机号、邮箱、地区、简历内容"
          @keyup.enter="applyFilters"
        />
      </label>
      <label>
        任务
        <select v-model="filters.taskId">
          <option value="">全部任务</option>
          <option v-for="task in tasks" :key="task.id" :value="task.id">
            {{ taskLabel(task) }}
          </option>
        </select>
      </label>
      <label>
        岗位模板
        <select v-model="filters.positionId">
          <option value="">全部岗位</option>
          <option
            v-for="position in positions"
            :key="position.id"
            :value="position.id"
          >
            {{ position.name }}
          </option>
        </select>
      </label>
      <div class="filter-actions">
        <button :disabled="loading" @click="applyFilters">查询</button>
        <button class="ghost" :disabled="loading" @click="resetFilters">
          重置
        </button>
      </div>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="!loading && candidates.length === 0" class="hint">暂无简历数据</p>

    <div v-else class="resume-grid">
      <article
        v-for="item in candidates"
        :key="item.id"
        class="resume-card"
        @click="openDetail(item)"
      >
        <div class="resume-card-head">
          <strong>{{ candidateName(item) }}</strong>
          <span
            >{{
              item.position_name || item.expected_position || "未关联岗位"
            }}
            | {{ platformLabel(item.platform_id) }}</span
          >
        </div>
        <p class="resume-meta">{{ compactInfo(item) }}</p>

        <div class="score-row"></div>
        <p class="resume-time">
          {{ formatDate(item.created_at) }} |
          <span :class="item.greeted_at ? 'ok' : 'muted'">
            {{ item.greeted_at ? "已打招呼" : "未打招呼" }}
          </span>

          <span> | 打招呼分 {{ scoreText(item.ai_greet_score) }}</span>
          <span> | 详情分 {{ scoreText(item.ai_detail_score) }}</span>
        </p>
      </article>
    </div>

    <div class="pager">
      <span>共 {{ total }} 条，第 {{ page }} / {{ totalPages }} 页</span>
      <button
        class="ghost"
        :disabled="loading || page <= 1"
        @click="goPage(page - 1)"
      >
        上一页
      </button>
      <button
        class="ghost"
        :disabled="loading || page >= totalPages"
        @click="goPage(page + 1)"
      >
        下一页
      </button>
      <select
        class="page-size-select"
        v-model.number="pageSize"
        @change="applyFilters"
      >
        <option :value="12">12条/页</option>
        <option :value="24">24条/页</option>
        <option :value="48">48条/页</option>
      </select>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { listCandidates } from "../services/api/candidateApi";
import { listPositions } from "../services/api/positionApi";
import { listTasks } from "../services/api/taskApi";

const props = defineProps({
  initialTaskId: String,
});
const router = useRouter();

const filters = ref({
  keyword: "",
  taskId: props.initialTaskId || "",
  positionId: "",
});
const candidates = ref<any[]>([]);
const tasks = ref<any[]>([]);
const positions = ref<any[]>([]);
const loading = ref(false);
const error = ref("");
const page = ref(1);
const pageSize = ref(12);
const total = ref(0);
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);

watch(
  () => props.initialTaskId,
  (value) => {
    filters.value.taskId = value || "";
    page.value = 1;
    void load();
  },
);

/**
 * 初始化筛选项和简历列表。
 * @returns {Promise<void>} 无返回值。
 */
async function init() {
  await Promise.all([loadFilterOptions(), load()]);
}

/**
 * 读取任务和岗位筛选项。
 * @returns {Promise<void>} 无返回值。
 */
async function loadFilterOptions() {
  try {
    const [taskItems, positionItems] = await Promise.all([
      listTasks(),
      listPositions(),
    ]);
    tasks.value = Array.isArray(taskItems) ? taskItems : [];
    positions.value = Array.isArray(positionItems) ? positionItems : [];
  } catch {
    tasks.value = [];
    positions.value = [];
  }
}

/**
 * 读取简历库分页数据。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await listCandidates({
      keyword: filters.value.keyword,
      taskId: filters.value.taskId,
      positionId: filters.value.positionId,
      page: page.value,
      pageSize: pageSize.value,
    });
    candidates.value = data.items || [];
    total.value = data.total || 0;
  } catch (e: any) {
    error.value = e?.message || "读取简历库失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 应用当前筛选条件。
 * @returns {void} 无返回值。
 */
function applyFilters() {
  page.value = 1;
  void load();
}

/**
 * 重置筛选条件。
 * @returns {void} 无返回值。
 */
function resetFilters() {
  filters.value = { keyword: "", taskId: "", positionId: "" };
  page.value = 1;
  void load();
}

/**
 * 跳转到指定分页。
 * @param {number} nextPage - 目标页码。
 * @returns {void} 无返回值。
 */
function goPage(nextPage: number) {
  page.value = Math.min(Math.max(1, nextPage), totalPages.value);
  void load();
}

/**
 * 新开页面查看候选人详情。
 * @param {any} item - 候选人简历对象。
 * @returns {void} 无返回值。
 */
function openDetail(item: any) {
  if (!item?.id) return;
  const route = router.resolve({
    name: "resume-detail",
    query: { candidate_id: item.id },
  });
  window.open(route.href, "_blank");
}

/**
 * 返回任务筛选展示名。
 * @param {any} task - 任务对象。
 * @returns {string} 展示文案。
 */
function taskLabel(task: any) {
  const name = task?.position_name || task?.position?.name || "未命名岗位";
  const account =
    task?.platform_account_name ||
    task?.platform_account?.display_name ||
    task?.platform_id ||
    "";
  return `${name} · ${account}`;
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
  return (
    [item?.work_region, item?.work_years, item?.education_level]
      .filter(Boolean)
      .join(" / ") || "暂无摘要"
  );
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

onMounted(init);
</script>

<style scoped>
.sub-title {
  margin: 4px 0 0;
  color: var(--fg-dim);
  font-size: 13px;
}
.filter-panel {
  display: grid;
  grid-template-columns: 1.4fr 1fr 1fr auto;
  gap: 10px;
  align-items: end;
  border: 1px solid #333;
  padding: 12px;
  margin-bottom: 12px;
  background: #050505;
}
.filter-actions {
  display: flex;
  gap: 8px;
}
.resume-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 10px;
}
.resume-card {
  border: 1px solid #333;
  background: #060606;
  padding: 12px;
  cursor: pointer;
  min-height: 118px;
}
.resume-card:hover {
  border-color: #0f0;
}
.resume-card-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  color: #eee;
}
.resume-card-head span,
.resume-meta,
.resume-position,
.score-row,
.resume-time {
  color: var(--fg-dim);
  font-size: 12px;
}
.resume-meta,
.resume-position,
.resume-time {
  margin: 8px 0 0;
}
.score-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}
.score-row .ok {
  color: #0f0;
}
.score-row .muted {
  color: #777;
}
.pager {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 14px;
  color: var(--fg-dim);
  font-size: 13px;
}
.page-size-select {
  width: 92px;
  min-width: 92px;
  flex: 0 0 92px;
}
@media (max-width: 980px) {
  .filter-panel {
    grid-template-columns: 1fr;
  }
  .pager {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
}
</style>
