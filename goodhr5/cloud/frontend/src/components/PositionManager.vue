<template>
  <section class="panel">
    <div class="panel-header">
      <!-- 底部对齐 -->
      <div style="display: flex; gap: 10px; align-items: center">
        <h2>岗位模板</h2>
      </div>
      <div style="display: flex; gap: 8px">
        <button v-if="!showForm" class="ghost" @click="showForm = true">
          + 新建模板
        </button>
        <button v-else class="ghost" @click="showForm = false">收起</button>
        <button class="ghost" @click="positions.load">刷新</button>
      </div>
    </div>

    <template v-if="showForm">
      <div style="display: flex; gap: 10px; align-items: center">
        <h3>基础信息</h3>
        <div style="font-size: 12px; color: red">
          虽然下面的内容看起来又臭又长、但是算我求你了，一定要认真看完下面的提示。非常重要😭
        </div>
      </div>
      <div style="font-size: 12px; margin-bottom: 12px" class="hint">
        运行逻辑:
        拿候选人基础信息后，再结合岗位要求，详情提示词,让ai进行打分。如果大于你设置的分数那就
        会执行点击候选人详情 拿到候选人的详细信息。
        再根据你岗位要求、打招呼提示词，让ai进行打分，如果大于你设置的分数那就
        会执行点击打招呼按钮。
      </div>
      <div class="position-form-grid">
        <label class="field field-medium"
          >岗位名称<input
            v-model="positions.form.value.name"
            placeholder="如: Java高级开发"
        /></label>
        <div class="field field-medium">
          <span class="field-label">默认模式</span>
          <div class="mode-cards" role="radiogroup" aria-label="默认模式">
            <button
              v-for="option in modeOptions"
              :key="option.value"
              type="button"
              class="mode-card"
              :class="{ active: positions.form.value.modeDefault === option.value }"
              role="radio"
              :aria-checked="positions.form.value.modeDefault === option.value"
              @click="positions.form.value.modeDefault = option.value"
            >
              <strong>{{ option.label }}</strong>
              <span>{{ option.description }}</span>
            </button>
          </div>
        </div>
        <label class="field field-full"
          >问候语<textarea
            v-model="positions.form.value.greetMessage"
            rows="2"
          />
        </label>
        <label class="field field-full"
          >描述<textarea v-model="positions.form.value.description" rows="2" />
        </label>
      </div>

      <h3>公共参数</h3>
      <div class="position-form-grid">
        <p class="hint field field-full">
          运行节奏、模型等参数已移到“个人配置”，这里仅保留岗位本身的筛选规则。
        </p>
        <label class="field field-small"
          >详情模式
          <select v-model="positions.form.value.detailMode">
            <option value="dom">页面解析</option>
            <option value="ocr">图片识别</option>
          </select>
          <small class="field-help"
            >页面解析更快；图片识别适合页面文字不稳定、需要读截图内容时使用。</small
          >
        </label>
      </div>

      <template v-if="positions.form.value.modeDefault === 'ai'">
        <h3>AI 模式专属</h3>
        <div class="position-form-grid">
          <label class="field field-full"
            >岗位要求<textarea
              v-model="positions.form.value.aiPositionRequirement"
              rows="4"
            />
            <small class="field-help" style="color: red"
              >1 年龄、学历、工作城市等基础条件
              请尽量用平台自带的筛选提前筛选好。尽量不要包含以下内容
              例如:要有上进心、要有团队合作精神等
              因为该条件在候选人信息中不能体现。所以ai无法判断。(也就是尽量写候选人简历上大概率会出现的内容)</small
            >
            <small class="field-help"
              >2
              尽量写清楚这个岗位最看重的经验、行业、年限、技术栈或学历要求。</small
            >
            <small class="field-help"
              >3 哪些条件是不值得继续查看详情和沟通的也最好写清楚</small
            >
            <small class="field-help"> 4 正确的示范: </small>
            <div class="" style="margin-left: 12px">
              <p class="field-help">1 求职意向必须是数学老师岗位</p>
              <p class="field-help">2 必须有3年以上数学教学经验</p>
              <p class="field-help">3 必须有教师资格证</p>
              <p class="field-help">4 必须是离职状态</p>
              <p class="field-help">
                聪明的你应该懂了。我写了这么多 你应该知道重要性了吧😄
              </p>
            </div>
            <small class="field-help"
              >5
              如果您实在不清楚怎么写。建议把你的岗位jd。还有上面的文字一起发给AI.让他给您写。虽然也不一定正确。哈哈哈哈😄</small
            >
          </label>
          <label class="field field-full"
            ><span class="field-title"
              >打开详情提示词<button
                type="button"
                class="ghost tiny"
                :disabled="positions.loading.value"
                @click="positions.resetOpenDetailPrompt"
              >
                重置为系统默认
              </button></span
            ><textarea
              v-model="positions.form.value.aiOpenDetailPrompt"
              rows="4"
            />
            <small class="field-help" style="color: red"
              >该提示词的作用
              仅用于判断是否值得打开详情，各行各业都有不同的判断标准。普通岗位建议宽度点。高级岗位建议严苛点。如果效果不佳，请自行调整。这个没有唯一答案。就像写作文一样的难。你也可以让ai帮你写。</small
            >
          </label>
          <label class="field field-full"
            >看详情阈值分<input
              v-model="positions.form.value.detailScoreThreshold"
              type="number"
              min="0"
              max="100"
              step="1"
            />
            <small class="field-help"
              >看详情评分大于等于该值时，才会打开详情。说直白点，候选人薪资低就设置地点。高就高点。至于高多少，请根据提示词里的内容自行调整。</small
            >
          </label>
          <label class="field field-full"
            ><span class="field-title"
              >最终筛选提示词<button
                type="button"
                class="ghost tiny"
                :disabled="positions.loading.value"
                @click="positions.resetFilterPrompt"
              >
                重置为系统默认
              </button></span
            ><textarea v-model="positions.form.value.aiFilterPrompt" rows="4" />
            <small class="field-help"
              >这个提示词
              的作用就决定了候选人的分数高低。跟上面的提示词逻辑一样。只是作用不同。</small
            >
          </label>
          <label class="field field-full"
            >打招呼阈值分<input
              v-model="positions.form.value.greetScoreThreshold"
              type="number"
              min="0"
              max="100"
              step="1"
            />
            <small class="field-help"
              >候选人打招呼评分大于等于该值时，才会执行打招呼。跟上面逻辑一样。薪资低就低点。高就高点。至于高多少，请根据提示词里的内容自行调整。</small
            >
          </label>
          <label class="field field-full"
            ><span class="field-title"
              >复核提示词<button
                type="button"
                class="ghost tiny"
                :disabled="positions.loading.value"
                @click="positions.resetReviewPrompt"
              >
                设置默认值
              </button></span
            ><textarea v-model="positions.form.value.aiReviewPrompt" rows="2" />
            <small class="field-help"
              >作用是
              当候选人打招呼评分与阈值差值不超过10分时，触发复核，并以复核分数作为最终打招呼依据。如果你不填
              就不会执行这一步。这一步加上会更加保险</small
            >
          </label>
        </div>
      </template>

      <template v-if="positions.form.value.modeDefault === 'keyword'">
        <h3>关键词模式专属</h3>
        <div class="position-form-grid">
          <p class="hint field field-full">
            关键词模式是否打开详情，已改到“个人配置”的详情查看概率里控制。
          </p>
          <label class="field field-small"
            >匹配方式<select v-model="positions.form.value.isAndMode">
              <option :value="false">满足任一关键词</option>
              <option :value="true">必须同时满足</option>
            </select>
            <small class="field-help"
              >满足任一关键词更宽松；必须同时满足更严格。</small
            ></label
          >
          <label class="field field-medium"
            >关键词<input
              v-model="positions.form.value.keywords"
              placeholder="Java Spring，支持空格、逗号或换行分隔"
            />
            <small class="field-help"
              >可用空格、中文逗号、英文逗号或换行分隔多个关键词。</small
            ></label
          >
          <label class="field field-medium"
            >排除词<input
              v-model="positions.form.value.excludeKeywords"
              placeholder="实习 应届，支持空格、逗号或换行分隔"
            />
            <small class="field-help"
              >命中这些词会被排除，适合过滤实习、应届、转行等不匹配人群。</small
            ></label
          >
        </div>
      </template>
      <template v-else>
        <p class="hint" style="margin-top: 8px">
          当前默认模式为 AI，已隐藏关键词专属参数。
        </p>
      </template>

      <p v-if="positions.error.value" class="error">
        {{ positions.error.value }}
      </p>
      <div class="actions">
        <button
          :disabled="positions.loading.value || !positions.form.value.name"
          @click="positions.save"
        >
          {{
            positions.loading.value
              ? "保存中..."
              : positions.form.value.id
                ? "更新"
                : "保存"
          }}
        </button>
        <button
          class="ghost"
          :disabled="positions.loading.value"
          @click="positions.resetForm"
        >
          清空
        </button>
      </div>
    </template>

    <p v-if="positions.positions.value.length === 0" class="hint">
      暂无岗位模板
    </p>
    <div v-else class="card-list" style="margin-top: 12px">
      <article
        v-for="pos in positions.positions.value"
        :key="pos.id"
        class="card"
      >
        <div>
          <strong>{{ pos.name }}</strong>
          <p class="card-meta">
            默认模式:
            {{
              pos.common_config?.mode_default === "keyword" ? "关键词" : "AI"
            }}
            | 关键词:{{ (pos.keywords || []).join(" / ") || "无" }} | 排除:{{
              (pos.exclude_keywords || []).join(" / ") || "无"
            }}
          </p>
        </div>
        <div class="card-actions">
          <button class="ghost" @click="edit(pos)">编辑</button>
          <button
            class="ghost danger"
            :disabled="positions.loading.value"
            @click="positions.remove(pos.id)"
          >
            删除
          </button>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from "vue";
const props = defineProps({ positions: Object });
const showForm = ref(false);
const modeOptions = [
  {
    value: "ai",
    label: "AI筛选",
    description: "先看详情评分，再做打招呼评分，适合精细判断。",
  },
  {
    value: "keyword",
    label: "关键词筛选",
    description: "按关键词和排除词判断，永久免费且速度快。",
  },
];
function edit(pos: any) {
  showForm.value = true;
  props.positions.edit(pos);
}
</script>

<style scoped>
.field-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.tiny {
  padding: 4px 8px;
  font-size: 12px;
}
</style>

<style scoped>
.position-form-grid {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 12px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  color: var(--fg-dim);
  font-size: 13px;
}

.field-small {
  grid-column: span 3;
}

.field-medium {
  grid-column: span 6;
}

.field-full {
  grid-column: 1 / -1;
}

.field input,
.field select,
.field textarea {
  width: 100%;
  box-sizing: border-box;
}

.field-help {
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
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

@media (max-width: 900px) {
  .field-small,
  .field-medium {
    grid-column: span 6;
  }
  .mode-cards {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .field-small,
  .field-medium,
  .field-full {
    grid-column: 1 / -1;
  }
}
</style>
