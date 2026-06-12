<!-- 本文件负责展示单个候选人的独立简历详情页。 -->
<template>
  <section class="panel resume-detail-page">
    <div class="panel-header">
      <div>
        <h2>简历详情</h2>
        <p class="sub-title">{{ pageSubTitle }}</p>
      </div>
      <button class="ghost" @click="backToLibrary">返回简历库</button>
    </div>

    <p v-if="loading" class="hint">正在读取候选人详情...</p>
    <p v-if="error" class="error">{{ error }}</p>

    <article v-if="candidate && !loading" class="resume-paper">
      <header class="resume-hero">
        <div class="avatar-wrap">
          <img v-if="avatarURL" :src="avatarURL" alt="候选人头像" />
          <span v-else>{{ avatarText }}</span>
        </div>
        <div class="hero-main">
          <h1>{{ candidateName(candidate) }}</h1>
          <div v-if="baseMeta.length" class="meta-line">
            <span v-for="item in baseMeta" :key="item">{{ item }}</span>
          </div>
          <p v-if="introText" class="intro-text">{{ introText }}</p>
        </div>
      </header>

      <section v-if="expectationMeta.length" class="resume-section expectation-section">
        <h3>期望职位</h3>
        <div class="pipe-line">
          <span v-for="item in expectationMeta" :key="item">{{ item }}</span>
        </div>
      </section>

      <section v-if="workExperiences.length" class="resume-section">
        <h3>工作经历</h3>
        <article v-for="(item, index) in workExperiences" :key="index" class="experience-item">
          <div class="experience-head">
            <div>
              <strong>{{ experienceTitle(item) }}</strong>
              <span v-if="experienceRole(item)">{{ experienceRole(item) }}</span>
            </div>
            <time v-if="experienceTime(item)">{{ experienceTime(item) }}</time>
          </div>
          <div v-if="experienceTextBlocks(item).length" class="content-blocks">
            <p v-for="(line, lineIndex) in experienceTextBlocks(item)" :key="lineIndex">
              {{ line }}
            </p>
          </div>
          <div v-if="experienceTags(item).length" class="tag-row">
            <span v-for="tag in experienceTags(item)" :key="tag">{{ tag }}</span>
          </div>
        </article>
      </section>

      <section v-if="projectExperiences.length" class="resume-section">
        <h3>项目经历</h3>
        <article v-for="(item, index) in projectExperiences" :key="index" class="experience-item">
          <div class="experience-head">
            <div>
              <strong>{{ projectTitle(item) }}</strong>
              <span v-if="projectRole(item)">{{ projectRole(item) }}</span>
            </div>
            <time v-if="experienceTime(item)">{{ experienceTime(item) }}</time>
          </div>
          <div v-if="experienceTextBlocks(item).length" class="content-blocks">
            <p v-for="(line, lineIndex) in experienceTextBlocks(item)" :key="lineIndex">
              {{ line }}
            </p>
          </div>
        </article>
      </section>

      <section v-if="educations.length" class="resume-section">
        <h3>教育经历</h3>
        <article v-for="(item, index) in educations" :key="index" class="simple-item">
          <strong>{{ educationTitle(item) }}</strong>
          <span>{{ educationMeta(item) }}</span>
        </article>
      </section>

      <section v-if="otherSections.length" class="resume-section">
        <h3>更多信息</h3>
        <div class="other-grid">
          <div v-for="section in otherSections" :key="section.label">
            <strong>{{ section.label }}</strong>
            <p>{{ section.value }}</p>
          </div>
        </div>
      </section>

      <section v-if="aiSections.length" class="resume-section ai-section">
        <h3>AI 分析</h3>
        <div class="ai-list">
          <article v-for="item in aiSections" :key="item.label">
            <strong>
              {{ item.label }}
              <span v-if="hasValue(item.score)">{{ scoreText(item.score) }}分</span>
            </strong>
            <p v-if="item.reason">{{ item.reason }}</p>
          </article>
        </div>
      </section>

      <section v-if="events.length" class="resume-section debug-section">
        <details>
          <summary>事件流水 {{ events.length }} 条</summary>
          <article v-for="event in events" :key="event.id" class="event-item">
            <div class="event-head">
              <strong>{{ eventTypeLabel(event.event_type) }}</strong>
              <time>{{ formatDate(event.created_at) }}</time>
            </div>
            <p v-if="hasValue(event.score)">评分：{{ scoreText(event.score) }}</p>
            <p v-if="event.reason">原因：{{ event.reason }}</p>
            <p v-if="event.message_text">消息：{{ event.message_text }}</p>
          </article>
        </details>
      </section>

      <section v-if="hasJSONContent(localCandidateJSON) || hasJSONContent(candidate.ext)" class="resume-section debug-section">
        <details>
          <summary>原始 JSON</summary>
          <details v-if="hasJSONContent(localCandidateJSON)" class="json-card" open>
            <summary>本地回传 JSON</summary>
            <JsonTree :value="localCandidateJSON" />
          </details>
          <details v-if="hasJSONContent(candidate.ext)" class="json-card">
            <summary>完整 ext 扩展字段</summary>
            <JsonTree :value="candidate.ext" />
          </details>
        </details>
      </section>
    </article>
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
const avatarURL = computed(() => pickText(["avatar_url", "avatar", "photo_url", "head_url", "head_img"]));
const avatarText = computed(() => candidateName(candidate.value).slice(0, 1));
const pageSubTitle = computed(() => [candidateName(candidate.value), pickText(["position_name", "expected_position"])].filter(hasValue).join(" / "));
const baseMeta = computed(() =>
  uniqueTexts([
    pickText(["age", "candidate_age"]),
    candidate.value?.education_level,
    candidate.value?.work_years,
    candidate.value?.work_status,
    candidate.value?.online_status,
    candidate.value?.work_region,
  ]),
);
const introText = computed(() =>
  firstNonEmpty([
    candidate.value?.personal_description,
    candidate.value?.basic_info,
    pickText(["summary", "description", "intro", "advantage"]),
  ]),
);
const expectationMeta = computed(() =>
  uniqueTexts([
    candidate.value?.work_region,
    candidate.value?.expected_position,
    pickText(["industry", "expected_industry"]),
    salaryText(candidate.value),
  ]),
);
const workExperiences = computed(() => normalizeArray(candidate.value?.work_experiences, ["work_experiences", "works", "work_list"]));
const projectExperiences = computed(() => normalizeArray(candidate.value?.project_experiences, ["project_experiences", "projects", "project_list"]));
const educations = computed(() => normalizeArray(candidate.value?.educations, ["educations", "education_experiences", "education_list"]));
const events = computed(() => (Array.isArray(candidate.value?.events) ? candidate.value.events : []));
const otherSections = computed(() =>
  [
    { label: "证书", value: joinItems(normalizeArray(candidate.value?.certificates, ["certificates", "certificate_list"])) },
    { label: "荣誉", value: joinItems(normalizeArray(candidate.value?.honors, ["honors", "honor_list"])) },
    { label: "沟通记录", value: joinItems(normalizeArray(candidate.value?.communications, ["communications", "communication_list"])) },
    { label: "简历附件文本", value: candidate.value?.resume_text },
    { label: "筛选文本", value: candidate.value?.filter_text },
    { label: "原始文本", value: candidate.value?.raw_text },
  ].filter((item) => hasValue(item.value)),
);
const aiSections = computed(() =>
  [
    { label: "基础筛选", score: candidate.value?.ai_detail_score, reason: candidate.value?.ai_detail_reason },
    { label: "打招呼判断", score: candidate.value?.ai_greet_score, reason: candidate.value?.ai_greet_reason },
    { label: "复核判断", score: candidate.value?.ai_review_score, reason: candidate.value?.ai_review_reason },
  ].filter((item) => hasValue(item.score) || hasValue(item.reason)),
);

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
  return item?.candidate_name || pickText(["candidate_name", "name", "real_name"]) || "候选人";
}

/**
 * 从候选人标准字段和原始 JSON 中读取第一个有值文本。
 * @param {string[]} keys - 需要查找的字段名。
 * @returns {string} 字段文本。
 */
function pickText(keys: string[]) {
  const sources = [candidate.value, localCandidateJSON.value].filter(Boolean);
  for (const key of keys) {
    for (const source of sources) {
      const value = readPath(source, key);
      if (hasValue(value)) return String(value).trim();
    }
  }
  return "";
}

/**
 * 从对象中按字段路径读取值。
 * @param {any} source - 数据源。
 * @param {string} path - 字段名或点分路径。
 * @returns {any} 字段值。
 */
function readPath(source: any, path: string) {
  if (!source || !path) return undefined;
  return path.split(".").reduce((value, key) => (value ? value[key] : undefined), source);
}

/**
 * 返回第一个有值文本。
 * @param {any[]} values - 候选值数组。
 * @returns {string} 文本。
 */
function firstNonEmpty(values: any[]) {
  const found = values.find(hasValue);
  return found ? String(found).trim() : "";
}

/**
 * 判断字段是否有可展示内容。
 * @param {any} value - 字段值。
 * @returns {boolean} 有内容返回 true。
 */
function hasValue(value: any) {
  if (value === null || value === undefined) return false;
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === "object") return Object.keys(value).length > 0;
  return String(value).trim() !== "";
}

/**
 * 去重并清理文本数组。
 * @param {any[]} values - 原始文本数组。
 * @returns {string[]} 去重后的文本数组。
 */
function uniqueTexts(values: any[]) {
  return Array.from(new Set(values.filter(hasValue).map((item) => String(item).trim())));
}

/**
 * 规范化数组字段。
 * @param {any} value - 标准字段值。
 * @param {string[]} fallbackKeys - 原始 JSON 兜底字段名。
 * @returns {any[]} 数组数据。
 */
function normalizeArray(value: any, fallbackKeys: string[] = []) {
  if (Array.isArray(value) && value.length) return value;
  for (const key of fallbackKeys) {
    const fallback = readPath(localCandidateJSON.value, key);
    if (Array.isArray(fallback) && fallback.length) return fallback;
  }
  return [];
}

/**
 * 把数组或对象转为可读文本。
 * @param {any[]} items - 数组内容。
 * @returns {string} 展示文本。
 */
function joinItems(items: any[]) {
  return items
    .map((item) => {
      if (typeof item === "string") return item;
      if (typeof item === "number") return String(item);
      if (item && typeof item === "object") return uniqueTexts(Object.values(item)).join(" / ");
      return "";
    })
    .filter(hasValue)
    .join("；");
}

/**
 * 返回经历主标题。
 * @param {any} item - 经历对象。
 * @returns {string} 标题文本。
 */
function experienceTitle(item: any) {
  return firstNonEmpty([item.company_name, item.company, item.name, item.organization, item.project_name, "经历"]);
}

/**
 * 返回经历职位信息。
 * @param {any} item - 经历对象。
 * @returns {string} 职位文本。
 */
function experienceRole(item: any) {
  return uniqueTexts([item.position_name, item.position, item.title, item.role, item.department]).join(" / ");
}

/**
 * 返回项目标题。
 * @param {any} item - 项目经历对象。
 * @returns {string} 项目标题。
 */
function projectTitle(item: any) {
  return firstNonEmpty([item.project_name, item.name, item.company_name, item.company, "项目"]);
}

/**
 * 返回项目角色。
 * @param {any} item - 项目经历对象。
 * @returns {string} 角色文本。
 */
function projectRole(item: any) {
  return uniqueTexts([item.role, item.position_name, item.position, item.title]).join(" / ");
}

/**
 * 返回经历时间。
 * @param {any} item - 经历对象。
 * @returns {string} 时间文本。
 */
function experienceTime(item: any) {
  const start = firstNonEmpty([item.start_ym, item.start_date, item.start_time, item.begin_time]);
  const end = firstNonEmpty([item.end_ym, item.end_date, item.end_time, item.finish_time]) || (start ? "至今" : "");
  return uniqueTexts([start, end]).join(" - ");
}

/**
 * 返回经历正文段落。
 * @param {any} item - 经历对象。
 * @returns {string[]} 段落文本。
 */
function experienceTextBlocks(item: any) {
  return uniqueTexts([
    item.performance,
    item.achievement,
    item.achievements,
    item.content,
    item.description,
    item.duty,
    item.responsibility,
    item.detail,
  ]).flatMap(splitLines);
}

/**
 * 返回经历标签。
 * @param {any} item - 经历对象。
 * @returns {string[]} 标签数组。
 */
function experienceTags(item: any) {
  const raw = item.tags || item.skills || item.highlights || [];
  if (Array.isArray(raw)) return uniqueTexts(raw);
  if (typeof raw === "string") return splitLines(raw).slice(0, 8);
  return [];
}

/**
 * 返回教育经历标题。
 * @param {any} item - 教育对象。
 * @returns {string} 标题文本。
 */
function educationTitle(item: any) {
  return uniqueTexts([item.school_name, item.school, item.name, item.major_name, item.major]).join(" / ");
}

/**
 * 返回教育经历摘要。
 * @param {any} item - 教育对象。
 * @returns {string} 摘要文本。
 */
function educationMeta(item: any) {
  return uniqueTexts([item.education_level, item.degree, experienceTime(item)]).join(" / ");
}

/**
 * 按换行拆分文本。
 * @param {any} value - 原始文本。
 * @returns {string[]} 文本行。
 */
function splitLines(value: any) {
  if (!hasValue(value)) return [];
  return String(value)
    .split(/\n+/)
    .map((line) => line.trim())
    .filter(Boolean);
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
  return platformId || "";
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
  const fallback = pickText(["salary", "expected_salary"]);
  if (min && max) return `${min}-${max}`;
  if (min) return `${min}起`;
  if (max) return `${max}以内`;
  return fallback;
}

/**
 * 格式化评分展示。
 * @param {number | null | undefined} score - AI 分数。
 * @returns {string} 分数字符串。
 */
function scoreText(score: number | null | undefined) {
  if (!hasValue(score)) return "";
  const value = Number(score);
  if (Number.isNaN(value)) return "";
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
 * 判断 JSON 字段是否有内容。
 * @param {any} value - JSON 字段值。
 * @returns {boolean} 有内容时返回 true。
 */
function hasJSONContent(value: any) {
  return hasValue(value);
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
  max-width: 1180px;
}
.sub-title {
  margin: 4px 0 0;
  color: var(--fg-dim);
  font-size: 13px;
}
.resume-paper {
  background: color-mix(in srgb, var(--bg-panel) 88%, white 12%);
  border: 1px solid var(--border);
  padding: 36px 44px;
  color: var(--fg);
}
.resume-hero {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}
.avatar-wrap {
  width: 74px;
  height: 74px;
  border-radius: 50%;
  overflow: hidden;
  background: var(--bg-input);
  border: 1px solid var(--border);
  display: grid;
  place-items: center;
  color: var(--accent);
  font-size: 28px;
  font-weight: 700;
}
.avatar-wrap img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.hero-main h1 {
  margin: 0;
  font-size: 30px;
  line-height: 1.15;
  color: var(--fg);
}
.meta-line,
.pipe-line {
  display: flex;
  flex-wrap: wrap;
  gap: 0;
  margin-top: 10px;
  color: var(--fg-dim);
  font-size: 15px;
}
.meta-line span:not(:last-child)::after,
.pipe-line span:not(:last-child)::after {
  content: "|";
  margin: 0 12px;
  color: var(--border);
}
.intro-text {
  margin: 26px 0 0;
  color: var(--fg);
  font-size: 15px;
  line-height: 1.9;
  white-space: pre-wrap;
}
.resume-section {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr);
  gap: 28px 40px;
  margin-top: 34px;
}
.resume-section h3 {
  margin: 0;
  color: var(--fg);
  font-size: 17px;
  line-height: 1.6;
}
.expectation-section {
  align-items: center;
}
.experience-item,
.simple-item,
.ai-list,
.other-grid,
.debug-section > details {
  min-width: 0;
}
.experience-item + .experience-item,
.simple-item + .simple-item {
  margin-top: 30px;
}
.experience-head {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  align-items: baseline;
}
.experience-head strong,
.simple-item strong {
  color: var(--fg);
  font-size: 17px;
}
.experience-head span,
.simple-item span,
.experience-head time {
  color: var(--fg-dim);
  font-size: 14px;
}
.experience-head div {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.experience-head div span::before {
  content: "|";
  margin-right: 10px;
  color: var(--border);
}
.content-blocks {
  margin-top: 16px;
  color: var(--fg);
  line-height: 1.9;
}
.content-blocks p {
  margin: 0 0 8px;
  white-space: pre-wrap;
}
.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.tag-row span {
  background: var(--bg-input);
  color: var(--fg-dim);
  border: 1px solid var(--border);
  padding: 4px 10px;
  font-size: 13px;
}
.simple-item {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.other-grid {
  display: grid;
  gap: 16px;
}
.other-grid strong,
.ai-list strong {
  display: block;
  color: var(--fg);
  margin-bottom: 6px;
}
.other-grid p,
.ai-list p,
.event-item p {
  margin: 0;
  color: var(--fg-dim);
  line-height: 1.8;
  white-space: pre-wrap;
}
.ai-list {
  display: grid;
  gap: 14px;
}
.ai-list strong span {
  margin-left: 10px;
  color: var(--accent);
}
.debug-section details {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
}
.debug-section summary,
.json-card summary {
  cursor: pointer;
  color: var(--fg);
}
.json-card {
  margin-top: 10px;
}
.event-item {
  margin-top: 12px;
  border-top: 1px solid var(--border);
  padding-top: 12px;
}
.event-head {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 6px;
}
.event-head time {
  color: var(--fg-dim);
  font-size: 12px;
}
@media (max-width: 980px) {
  .resume-paper {
    padding: 22px;
  }
  .resume-hero,
  .resume-section {
    grid-template-columns: 1fr;
    gap: 14px;
  }
  .experience-head {
    display: block;
  }
  .experience-head time {
    display: block;
    margin-top: 6px;
  }
}
</style>
