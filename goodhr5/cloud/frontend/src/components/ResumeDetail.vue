<!-- 本文件负责展示单个候选人的独立简历详情页。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>{{ candidateName(candidate) }}</h2>
        <p class="sub-title">{{ compactInfo(candidate) }}</p>
      </div>
      <button class="ghost" @click="closePage">关闭页面</button>
    </div>

    <p v-if="loading" class="hint">正在读取候选人详情...</p>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="candidate && !loading">
      <div class="detail-grid">
        <div><span>平台</span><strong>{{ platformLabel(candidate.platform_id) }}</strong></div>
        <div><span>岗位模板</span><strong>{{ candidate.position_name || "--" }}</strong></div>
        <div><span>任务ID</span><strong>{{ candidate.task_id || "--" }}</strong></div>
        <div><span>所属账号</span><strong>{{ candidate.user_email || "--" }}</strong></div>
        <div><span>期望岗位</span><strong>{{ candidate.expected_position || "--" }}</strong></div>
        <div><span>期望薪资</span><strong>{{ salaryText(candidate) }}</strong></div>
        <div><span>地区</span><strong>{{ candidate.work_region || "--" }}</strong></div>
        <div><span>年限</span><strong>{{ candidate.work_years || "--" }}</strong></div>
        <div><span>学历</span><strong>{{ candidate.education_level || "--" }}</strong></div>
        <div><span>在线状态</span><strong>{{ candidate.online_status || "--" }}</strong></div>
        <div><span>打招呼时间</span><strong>{{ formatDate(candidate.greeted_at) }}</strong></div>
        <div><span>创建时间</span><strong>{{ formatDate(candidate.created_at) }}</strong></div>
      </div>

      <div class="score-panel">
        <div>
          <span>详情评分</span>
          <strong>{{ scoreText(candidate.ai_detail_score) }}</strong>
          <p>{{ candidate.ai_detail_reason || "无原因" }}</p>
        </div>
        <div>
          <span>打招呼评分</span>
          <strong>{{ scoreText(candidate.ai_greet_score) }}</strong>
          <p>{{ candidate.ai_greet_reason || "无原因" }}</p>
        </div>
        <div>
          <span>复核评分</span>
          <strong>{{ scoreText(candidate.ai_review_score) }}</strong>
          <p>{{ candidate.ai_review_reason || "无原因" }}</p>
        </div>
      </div>

      <section class="detail-section">
        <h3>基础信息</h3>
        <p>{{ candidate.basic_info || candidate.personal_description || "暂无基础信息" }}</p>
      </section>

      <section class="detail-section">
        <h3>工作经历</h3>
        <div v-if="candidate.work_experiences?.length" class="sub-list">
          <article v-for="(item, index) in candidate.work_experiences" :key="index">
            <strong>{{ item.company_name || "未填写公司" }} · {{ item.position_name || "未填写职位" }}</strong>
            <p>{{ item.start_ym || "--" }} 至 {{ item.end_ym || "至今" }}</p>
            <p>{{ item.content || "暂无描述" }}</p>
          </article>
        </div>
        <p v-else>暂无工作经历</p>
      </section>

      <section class="detail-section">
        <h3>教育经历</h3>
        <div v-if="candidate.educations?.length" class="sub-list">
          <article v-for="(item, index) in candidate.educations" :key="index">
            <strong>{{ item.school_name || "未填写学校" }} · {{ item.major_name || "未填写专业" }}</strong>
            <p>{{ item.education_level || "--" }} · {{ item.start_ym || "--" }} 至 {{ item.end_ym || "--" }}</p>
          </article>
        </div>
        <p v-else>暂无教育经历</p>
      </section>

      <section class="detail-section">
        <h3>候选人文本</h3>
        <pre>{{ candidate.resume_text || candidate.raw_text || candidate.filter_text || "暂无文本" }}</pre>
      </section>

      <section class="detail-section">
        <h3>事件流水</h3>
        <div v-if="candidate.events?.length" class="event-list">
          <article v-for="event in candidate.events" :key="event.id">
            <div class="event-head">
              <strong>{{ eventTypeLabel(event.event_type) }}</strong>
              <span>{{ formatDate(event.created_at) }}</span>
            </div>
            <p v-if="event.score !== null && event.score !== undefined">评分：{{ scoreText(event.score) }}</p>
            <p v-if="event.reason">原因：{{ event.reason }}</p>
            <p v-if="event.message_text">消息：{{ event.message_text }}</p>
          </article>
        </div>
        <p v-else>暂无事件记录</p>
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { getCandidate } from "../services/cloudApi";

const props = defineProps({
  candidateId: String,
});

const candidate = ref<any>(null);
const loading = ref(false);
const error = ref("");

/**
 * 读取候选人详情。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  if (!props.candidateId) {
    error.value = "缺少候选人ID";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    candidate.value = await getCandidate(props.candidateId);
  } catch (e: any) {
    error.value = e?.message || "读取候选人详情失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 返回候选人展示姓名。
 * @param {any} item - 候选人详情对象。
 * @returns {string} 展示姓名。
 */
function candidateName(item: any) {
  return item?.candidate_name || "候选人详情";
}

/**
 * 返回候选人摘要。
 * @param {any} item - 候选人详情对象。
 * @returns {string} 摘要文案。
 */
function compactInfo(item: any) {
  if (!item) return "简历详情";
  return [item.work_region, item.work_years, item.education_level, item.expected_position]
    .filter(Boolean)
    .join(" / ") || "暂无摘要";
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
 * 返回事件类型中文名称。
 * @param {string} eventType - 事件类型。
 * @returns {string} 展示名称。
 */
function eventTypeLabel(eventType: string) {
  const labels: Record<string, string> = {
    detail_analysis: "详情分析",
    detail_fetched: "详情读取",
    greet_analysis: "打招呼分析",
    review_analysis: "复核分析",
    candidate_skipped: "候选人跳过",
    greet_success: "打招呼成功",
  };
  return labels[eventType] || eventType || "未知事件";
}

/**
 * 格式化薪资展示。
 * @param {any} item - 候选人详情对象。
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

onMounted(load);

/**
 * 关闭当前详情页。
 * @returns {void} 无返回值。
 */
function closePage() {
  window.close();
}
</script>

<style scoped>
.sub-title {
  margin: 4px 0 0;
  color: var(--fg-dim);
  font-size: 13px;
}
.detail-grid,
.score-panel {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.detail-grid div,
.score-panel div,
.sub-list article {
  border: 1px solid #333;
  background: #050505;
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
.score-panel {
  margin-top: 12px;
}
.score-panel p,
.detail-section p {
  margin: 6px 0 0;
  color: var(--fg-dim);
  line-height: 1.6;
}
.detail-section {
  margin-top: 16px;
}
.detail-section h3 {
  margin: 0 0 8px;
  color: #eee;
  font-size: 16px;
}
.sub-list {
  display: grid;
  gap: 8px;
}
.event-list {
  display: grid;
  gap: 8px;
}
.event-list article {
  border: 1px solid #333;
  background: #050505;
  padding: 10px;
}
.event-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: #eee;
}
.event-head span {
  color: var(--fg-dim);
  font-size: 12px;
}
.detail-section pre {
  white-space: pre-wrap;
  word-break: break-word;
  border: 1px solid #333;
  background: #030303;
  padding: 12px;
  color: var(--fg-dim);
  line-height: 1.6;
}
@media (max-width: 980px) {
  .detail-grid,
  .score-panel {
    grid-template-columns: 1fr;
  }
}
</style>
