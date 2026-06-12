<!-- 本文件负责展示单个候选人的独立简历详情页。 -->
<template>
  <section class="panel resume-detail-page">
    <div class="panel-header">
      <div>
        <h2>{{ candidateName(candidate) }}</h2>
        <p class="sub-title">{{ compactInfo(candidate) }}</p>
      </div>
      <button class="ghost" @click="backToLibrary">返回简历库</button>
    </div>

    <p v-if="loading" class="hint">正在读取候选人详情...</p>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="candidate && !loading">
      <div class="summary-row">
        <span>{{ platformLabel(candidate.platform_id) }}</span>
        <span>{{ candidate.position_name || candidate.expected_position || "未关联岗位" }}</span>
        <span>{{ candidate.work_status || "工作状态未知" }}</span>
        <span>{{ candidate.greeted_at ? "已打招呼" : "未打招呼" }}</span>
      </div>

      <section class="detail-block">
        <h3>简历主体字段</h3>
        <div class="field-grid">
          <div v-for="field in profileFields" :key="field.label" class="field-item">
            <span>{{ field.label }}</span>
            <strong>{{ displayValue(field.value) }}</strong>
          </div>
        </div>
      </section>

      <section class="detail-block">
        <h3>任务和平台字段</h3>
        <div class="field-grid">
          <div v-for="field in engagementFields" :key="field.label" class="field-item">
            <span>{{ field.label }}</span>
            <strong>{{ displayValue(field.value) }}</strong>
          </div>
        </div>
      </section>

      <section class="detail-block">
        <h3>AI 分析结果</h3>
        <div class="score-panel">
          <article v-for="score in scoreFields" :key="score.label">
            <span>{{ score.label }}</span>
            <strong>{{ scoreText(score.score) }}</strong>
            <p>{{ score.reason || "暂无原因" }}</p>
          </article>
        </div>
      </section>

      <section class="detail-block">
        <h3>文本内容</h3>
        <div class="text-list">
          <article v-for="item in textSections" :key="item.label">
            <h4>{{ item.label }}</h4>
            <pre>{{ item.value || "暂无内容" }}</pre>
          </article>
        </div>
      </section>

      <section class="detail-block">
        <h3>结构化简历 JSON</h3>
        <div class="json-section-list">
          <details
            v-for="section in structuredSections"
            :key="section.label"
            class="json-card"
            :open="hasJSONContent(section.value)"
          >
            <summary>
              <strong>{{ section.label }}</strong>
              <span>{{ jsonSummary(section.value) }}</span>
            </summary>
            <JsonTree v-if="hasJSONContent(section.value)" :value="section.value" />
            <p v-else class="muted-text">暂无数据</p>
          </details>
        </div>
      </section>

      <section class="detail-block">
        <h3>本地回传原始 JSON</h3>
        <details class="json-card" open>
          <summary>
            <strong>ext.local_candidate_json</strong>
            <span>{{ jsonSummary(localCandidateJSON) }}</span>
          </summary>
          <JsonTree v-if="hasJSONContent(localCandidateJSON)" :value="localCandidateJSON" />
          <p v-else class="muted-text">暂无本地原始 JSON</p>
        </details>
        <details class="json-card">
          <summary>
            <strong>完整 ext 扩展字段</strong>
            <span>{{ jsonSummary(candidate.ext) }}</span>
          </summary>
          <JsonTree v-if="hasJSONContent(candidate.ext)" :value="candidate.ext" />
          <p v-else class="muted-text">暂无扩展字段</p>
        </details>
      </section>

      <section class="detail-block">
        <h3>事件流水</h3>
        <div v-if="candidate.events?.length" class="event-list">
          <article v-for="event in candidate.events" :key="event.id">
            <div class="event-head">
              <strong>{{ eventTypeLabel(event.event_type) }}</strong>
              <span>{{ formatDate(event.created_at) }}</span>
            </div>
            <div class="event-grid">
              <p>评分：{{ scoreText(event.score) }}</p>
              <p>模型：{{ displayValue(event.model) }}</p>
              <p>Token：{{ displayValue(event.token_usage) }}</p>
              <p>平台：{{ platformLabel(event.platform_id) }}</p>
            </div>
            <p v-if="event.reason">原因：{{ event.reason }}</p>
            <p v-if="event.message_text">消息：{{ event.message_text }}</p>
            <details v-if="event.input_text || event.output_text || hasJSONContent(event.metadata)" class="event-extra">
              <summary>查看输入输出</summary>
              <pre v-if="event.input_text">输入：{{ event.input_text }}</pre>
              <pre v-if="event.output_text">输出：{{ event.output_text }}</pre>
              <JsonTree v-if="hasJSONContent(event.metadata)" :value="event.metadata" />
            </details>
          </article>
        </div>
        <p v-else class="muted-text">暂无事件记录</p>
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import JsonTree from "./JsonTree.vue";
import { getCandidate } from "../services/api/candidateApi";

const props = defineProps({
  candidateId: String,
  engagementId: String,
  taskId: String,
});

const router = useRouter();
const candidate = ref<any>(null);
const loading = ref(false);
const error = ref("");

const localCandidateJSON = computed(() => candidate.value?.ext?.local_candidate_json || null);
const profileFields = computed(() => [
  { label: "候选人ID", value: candidate.value?.id },
  { label: "来源平台候选人ID", value: candidate.value?.platform_candidate_id },
  { label: "姓名", value: candidate.value?.candidate_name },
  { label: "出生年月", value: candidate.value?.birth_ym },
  { label: "手机号", value: candidate.value?.phone },
  { label: "邮箱", value: candidate.value?.email },
  { label: "工作地区", value: candidate.value?.work_region },
  { label: "工作年限", value: candidate.value?.work_years },
  { label: "工作状态", value: candidate.value?.work_status },
  { label: "学历", value: candidate.value?.education_level },
  { label: "期望岗位", value: candidate.value?.expected_position },
  { label: "期望薪资", value: salaryText(candidate.value) },
  { label: "在线状态", value: candidate.value?.online_status },
  { label: "简历附件", value: candidate.value?.resume_url },
  { label: "首次发现", value: formatDate(candidate.value?.first_seen_at) },
  { label: "创建时间", value: formatDate(candidate.value?.created_at) },
  { label: "更新时间", value: formatDate(candidate.value?.updated_at) },
]);
const engagementFields = computed(() => [
  { label: "触达ID", value: candidate.value?.engagement_id },
  { label: "触达状态", value: candidate.value?.engagement_status },
  { label: "任务ID", value: candidate.value?.task_id },
  { label: "岗位ID", value: candidate.value?.position_id },
  { label: "岗位名称", value: candidate.value?.position_name },
  { label: "平台账号ID", value: candidate.value?.platform_account_id },
  { label: "所属用户", value: candidate.value?.user_email },
  { label: "平台", value: platformLabel(candidate.value?.platform_id) },
  { label: "详情抓取时间", value: formatDate(candidate.value?.detail_fetched_at) },
  { label: "打招呼时间", value: formatDate(candidate.value?.greeted_at) },
]);
const scoreFields = computed(() => [
  { label: "基础筛选评分", score: candidate.value?.ai_detail_score, reason: candidate.value?.ai_detail_reason },
  { label: "打招呼评分", score: candidate.value?.ai_greet_score, reason: candidate.value?.ai_greet_reason },
  { label: "复核评分", score: candidate.value?.ai_review_score, reason: candidate.value?.ai_review_reason },
]);
const textSections = computed(() => [
  { label: "基础信息", value: candidate.value?.basic_info },
  { label: "个人描述", value: candidate.value?.personal_description },
  { label: "原始文本", value: candidate.value?.raw_text },
  { label: "筛选文本", value: candidate.value?.filter_text },
  { label: "简历附件提取文本", value: candidate.value?.resume_text },
]);
const structuredSections = computed(() => [
  { label: "工作经历 work_experiences", value: candidate.value?.work_experiences },
  { label: "教育经历 educations", value: candidate.value?.educations },
  { label: "证书 certificates", value: candidate.value?.certificates },
  { label: "荣誉 honors", value: candidate.value?.honors },
  { label: "项目经历 project_experiences", value: candidate.value?.project_experiences },
  { label: "沟通记录 communications", value: candidate.value?.communications },
]);

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
    candidate.value = await getCandidate(
      props.candidateId,
      props.engagementId || "",
      props.taskId || "",
    );
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
  return (
    [
      item.work_region,
      item.work_years,
      item.education_level,
      item.expected_position,
    ]
      .filter(Boolean)
      .join(" / ") || "暂无摘要"
  );
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
    detail_analysis: "基础筛选分析",
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
  return "";
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
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString();
}

/**
 * 格式化普通字段展示。
 * @param {any} value - 原始字段值。
 * @returns {string} 可展示的字段文本。
 */
function displayValue(value: any) {
  if (value === null || value === undefined || value === "") return "--";
  if (typeof value === "number") return String(value);
  if (typeof value === "boolean") return value ? "是" : "否";
  return String(value);
}

/**
 * 判断 JSON 字段是否有内容。
 * @param {any} value - JSON 字段值。
 * @returns {boolean} 有内容时返回 true。
 */
function hasJSONContent(value: any) {
  if (!value) return false;
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === "object") return Object.keys(value).length > 0;
  return String(value).trim() !== "";
}

/**
 * 返回 JSON 字段摘要。
 * @param {any} value - JSON 字段值。
 * @returns {string} 摘要文本。
 */
function jsonSummary(value: any) {
  if (!hasJSONContent(value)) return "暂无数据";
  if (Array.isArray(value)) return `${value.length} 项`;
  if (typeof value === "object") return `${Object.keys(value).length} 个字段`;
  return "1 项";
}

/**
 * 返回简历库页面。
 * @returns {void} 无返回值。
 */
function backToLibrary() {
  void router.push({
    name: "resumes",
    query: props.taskId ? { task_id: props.taskId } : {},
  });
}

onMounted(load);
watch(
  () => [props.candidateId, props.engagementId, props.taskId],
  () => {
    void load();
  },
);
</script>

<style scoped>
.resume-detail-page {
  max-width: 1280px;
}
.sub-title {
  margin: 4px 0 0;
  color: var(--fg-dim);
  font-size: 13px;
}
.summary-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}
.summary-row span {
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--fg);
  padding: 6px 10px;
  font-size: 12px;
}
.detail-block {
  margin-top: 16px;
}
.detail-block h3 {
  margin: 0 0 8px;
  color: var(--fg);
  font-size: 16px;
}
.field-grid,
.score-panel,
.event-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.field-item,
.score-panel article,
.text-list article,
.json-card,
.event-list article {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 10px;
  min-width: 0;
}
.field-item span,
.score-panel span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
  margin-bottom: 6px;
}
.field-item strong,
.score-panel strong {
  color: var(--fg);
  overflow-wrap: anywhere;
}
.score-panel p,
.event-list p,
.muted-text {
  margin: 6px 0 0;
  color: var(--fg-dim);
  line-height: 1.6;
}
.text-list,
.json-section-list,
.event-list {
  display: grid;
  gap: 10px;
}
.text-list h4 {
  margin: 0 0 8px;
  color: var(--fg);
  font-size: 14px;
}
pre {
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
  color: var(--fg-dim);
  line-height: 1.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.json-card summary {
  cursor: pointer;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--fg);
}
.json-card summary span {
  color: var(--fg-dim);
  font-size: 12px;
}
.event-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--fg);
}
.event-head span {
  color: var(--fg-dim);
  font-size: 12px;
}
.event-grid {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin-top: 8px;
}
.event-extra {
  margin-top: 8px;
}
.event-extra summary {
  cursor: pointer;
  color: var(--fg);
}
@media (max-width: 980px) {
  .field-grid,
  .score-panel,
  .event-grid {
    grid-template-columns: 1fr;
  }
}
</style>
