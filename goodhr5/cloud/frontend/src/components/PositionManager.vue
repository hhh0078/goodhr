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
      </div>

      <template v-if="positions.form.value.modeDefault === 'ai'">
        <h3>AI 模式专属</h3>
        <div class="position-form-grid">
          <label class="field field-full"
            >岗位要求<textarea
              v-model="positions.form.value.aiPositionRequirement"
              rows="2"
            />
          </label>
          <label class="field field-full"
            >AI提示词<textarea
              v-model="positions.form.value.aiClickPrompt"
              rows="2"
            />
          </label>
        </div>
      </template>

      <template v-if="positions.form.value.modeDefault === 'keyword'">
        <h3>关键词模式专属</h3>
        <div class="position-form-grid">
          <label class="field field-small"
            >AND/OR<select v-model="positions.form.value.isAndMode">
              <option :value="false">OR</option>
              <option :value="true">AND</option>
            </select></label
          >
          <label class="field field-medium"
            >关键词<input
              v-model="positions.form.value.keywords"
              placeholder="Java Spring"
          /></label>
          <label class="field field-medium"
            >排除词<input
              v-model="positions.form.value.excludeKeywords"
              placeholder="实习 应届"
          /></label>
          <label class="field field-small"
            >关键词模式详情打开概率(%)<input
              v-model="positions.form.value.keywordDetailOpenProbability"
              type="number"
              min="0"
              max="100"
          /></label>
          <label class="field field-small"
            >详情模式<select v-model="positions.form.value.keywordDetailMode">
              <option value="dom">DOM</option>
              <option value="ocr">OCR</option>
            </select></label
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
