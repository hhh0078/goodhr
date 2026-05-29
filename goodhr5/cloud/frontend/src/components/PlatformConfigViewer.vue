<!-- 超级管理员系统配置页：直接编辑云端保存的原始 JSON。 -->
<template>
  <section class="panel platform-config-panel">
    <div class="panel-header">
      <div>
        <h2>系统配置</h2>
        <p class="hint">超级管理员可直接编辑原始 JSON。</p>
      </div>
      <div class="actions compact">
        <button class="ghost" :disabled="loading" @click="load">
          {{ loading ? "刷新中..." : "刷新" }}
        </button>
        <button
          class="ghost"
          :disabled="!activeConfig || saving || !dirty"
          @click="resetDraft"
        >
          撤销修改
        </button>
        <button
          class="ghost primary"
          :disabled="!activeConfig || saving || hasErrors || !dirty"
          @click="save"
        >
          {{ saving ? "保存中..." : "保存" }}
        </button>
      </div>
    </div>

    <p v-if="error" class="error">{{ error }}</p>

    <div v-if="configs.length" class="config-layout">
      <div class="config-tabs" role="tablist" aria-label="系统配置列表">
        <button
          v-for="item in configs"
          :key="item.config_key"
          :class="['config-tab', { active: activeKey === item.config_key }]"
          @click="selectConfig(item.config_key)"
        >
          <strong>{{ item.config_key }}</strong>
          <span>{{ item.description || "无说明" }}</span>
        </button>
      </div>

      <div class="config-editor-wrap">
        <div v-if="activeConfig" class="config-editor-head">
          <div>
            <strong>{{ activeConfig.config_key }}</strong>
            <p class="card-meta">{{ activeConfig.description || "无说明" }}</p>
          </div>
          <span :class="['status-chip', activeConfig.enabled ? 'enabled' : 'disabled']">
            {{ activeConfig.enabled ? "启用" : "停用" }}
          </span>
        </div>

        <p v-if="hasErrors" class="warn">{{ jsonError || "JSON 语法有误，修正后才能保存。" }}</p>
        <p v-else-if="dirty" class="hint">有未保存修改。</p>

        <Codemirror
          v-model="draftText"
          class="json-code-editor"
          :extensions="editorExtensions"
          :indent-with-tab="true"
          :tab-size="2"
          :style="{ height: '520px' }"
        />
      </div>
    </div>

    <p v-else-if="loading" class="hint">加载中...</p>
    <p v-else class="hint">暂无系统配置</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Codemirror } from "vue-codemirror";
import { json } from "@codemirror/lang-json";
import { oneDark } from "@codemirror/theme-one-dark";
import {
  listAdminSystemConfigs,
  updateAdminSystemConfig,
} from "../services/api/adminApi";

const configs = ref<any[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const activeKey = ref("");
const draftText = ref("{}");
const originalText = ref("{}");
const dirty = ref(false);
const hasErrors = ref(false);
const jsonError = ref("");
const editorExtensions = [json(), oneDark];

const activeConfig = computed(() =>
  configs.value.find((item) => item.config_key === activeKey.value) || null,
);

/**
 * 加载系统配置列表，并同步当前选中配置的编辑内容。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await listAdminSystemConfigs();
    configs.value = data;
    if (!activeKey.value && data.length) {
      activeKey.value = data[0].config_key;
    }
    if (activeKey.value) {
      const current = data.find((item: any) => item.config_key === activeKey.value);
      if (current) {
        setEditorText(current.config_value || "{}");
      }
    }
  } catch (e: any) {
    error.value = e?.message || "加载系统配置失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 将服务端 JSON 内容写入编辑器，并重置修改状态。
 * @param {string} raw - 服务端返回的 JSON 字符串。
 * @returns {void} 无返回值。
 */
function setEditorText(raw: string) {
  const formatted = formatJSON(raw || "{}");
  originalText.value = formatted;
  draftText.value = formatted;
  dirty.value = false;
  validateDraft(formatted);
}

/**
 * 选择新的配置标签；存在未保存修改时禁止切换。
 * @param {string} configKey - 目标系统配置键。
 * @returns {void} 无返回值。
 */
function selectConfig(configKey: string) {
  if (configKey === activeKey.value) return;
  if (dirty.value) {
    error.value = "当前配置有未保存修改，请先保存或撤销修改";
    return;
  }
  error.value = "";
  activeKey.value = configKey;
  const current = configs.value.find((item) => item.config_key === configKey);
  if (current) {
    setEditorText(current.config_value || "{}");
  }
}

/**
 * 保存当前配置的 JSON 内容。
 * @returns {Promise<void>} 无返回值。
 */
async function save() {
  if (!activeConfig.value || hasErrors.value) return;
  saving.value = true;
  error.value = "";
  try {
    const saved = await updateAdminSystemConfig(
      activeConfig.value.config_key,
      formatJSON(draftText.value),
    );
    const index = configs.value.findIndex(
      (item) => item.config_key === saved.config_key,
    );
    if (index >= 0) {
      configs.value[index] = saved;
    }
    setEditorText(saved.config_value || "{}");
  } catch (e: any) {
    error.value = e?.message || "保存系统配置失败";
  } finally {
    saving.value = false;
  }
}

/**
 * 放弃当前配置的未保存修改，恢复为服务端版本。
 * @returns {void} 无返回值。
 */
function resetDraft() {
  setEditorText(activeConfig.value?.config_value || "{}");
  error.value = "";
}

/**
 * 校验 JSON 草稿并更新错误状态。
 * @param {string} value - 当前编辑器文本。
 * @returns {void} 无返回值。
 */
function validateDraft(value: string) {
  try {
    JSON.parse(value || "{}");
    hasErrors.value = false;
    jsonError.value = "";
  } catch (e: any) {
    hasErrors.value = true;
    jsonError.value = `JSON 语法有误：${e?.message || e}`;
  }
}

/**
 * 将 JSON 文本格式化为带缩进的代码文本。
 * @param {string} raw - 原始 JSON 文本。
 * @returns {string} 格式化后的 JSON 文本。
 */
function formatJSON(raw: string) {
  return JSON.stringify(JSON.parse(raw || "{}"), null, 2);
}

onMounted(async () => {
  await load();
  if (activeConfig.value) {
    setEditorText(activeConfig.value.config_value || "{}");
  }
});

watch(draftText, (value) => {
  dirty.value = value !== originalText.value;
  validateDraft(value);
});
</script>

<style scoped>
.platform-config-panel {
  min-height: 640px;
}
.config-layout {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 560px;
}
.config-tabs {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 4px;
}
.config-tab {
  flex: 0 0 auto;
  min-width: 180px;
  max-width: 260px;
  border: 1px solid var(--border);
  background: var(--bg);
  text-align: left;
  color: var(--fg);
  padding: 8px 10px;
}
.config-tab:hover,
.config-tab.active {
  background: #141414;
  color: var(--fg);
}
.config-tab strong {
  display: block;
  margin-bottom: 4px;
  white-space: nowrap;
}
.config-tab span {
  display: block;
  font-size: 12px;
  color: var(--fg-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.config-editor-wrap {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.config-editor-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.json-code-editor {
  border: 1px solid var(--border);
  background: #0b0b0b;
  overflow: hidden;
}
.status-chip {
  border: 1px solid var(--border);
  padding: 2px 8px;
  font-size: 12px;
}
.status-chip.enabled {
  color: #86efac;
}
.status-chip.disabled {
  color: #fca5a5;
}
:deep(.cm-editor) {
  height: 100%;
  font-size: 13px;
}
:deep(.cm-scroller) {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
}
</style>
