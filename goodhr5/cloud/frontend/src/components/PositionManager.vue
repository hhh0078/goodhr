<template>
  <section class="panel">
    <div class="panel-header">
      <h2>岗位模板</h2>
      <div style="display: flex; gap: 8px">
        <button v-if="!showForm" class="ghost" @click="showForm = true">
          + 新建模板
        </button>
        <button v-else class="ghost" @click="showForm = false">收起</button>
        <button class="ghost" @click="positions.load">刷新</button>
      </div>
    </div>

    <template v-if="showForm">
      <h3>基础信息</h3>
      <div class="position-form-grid">
        <label class="field field-medium"
          >岗位名称<input
            v-model="positions.form.value.name"
            placeholder="如: Java高级开发"
        /></label>
        <label class="field field-small"
          >默认模式
          <select v-model="positions.form.value.modeDefault">
            <option value="ai">AI筛选</option>
            <option value="keyword">关键词筛选</option>
          </select>
        </label>
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
            <small class="field-help"
              >1 年龄、学历等基础条件 请尽量用平台自带的筛选提前筛选好。</small
            >
            <small class="field-help"
              >2
              尽量写清楚这个岗位最看重的经验、行业、年限、技术栈或学历要求。</small
            >
            <small class="field-help"
              >3 哪些条件是不值得继续查看详情和沟通的也最好写清楚</small
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
            <small class="field-help"
              >先用候选人基础信息让 AI 判断“这次值不值得打开详情”，要求 AI
              返回是否查看和简短原因。</small
            >
          </label>
          <label class="field field-small"
            >看详情阈值分<input
              v-model="positions.form.value.detailScoreThreshold"
              type="number"
              min="0"
              max="100"
              step="1"
            />
            <small class="field-help"
              >看详情评分大于等于该值时，才会打开详情。</small
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
              >详情文本拿到后，再给 AI
              的最终筛选补充规则。这里和“打开详情提示词”是两套不同提示词。</small
            >
          </label>
          <label class="field field-small"
            >打招呼阈值分<input
              v-model="positions.form.value.greetScoreThreshold"
              type="number"
              min="0"
              max="100"
              step="1"
            />
            <small class="field-help"
              >最终评分大于等于该值时，才会执行打招呼。</small
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
              >该提示词仅用于打招呼前二次筛选得分。打招呼评分与阈值差值不超过10分时触发复核，并以复核分数作为最终打招呼依据。</small
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

@media (max-width: 900px) {
  .field-small,
  .field-medium {
    grid-column: span 6;
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
