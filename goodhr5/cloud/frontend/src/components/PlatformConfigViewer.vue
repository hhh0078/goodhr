<!-- 超级管理员平台配置页：直接编辑云端保存的原始 JSON。 -->
<template>
  <section class="panel platform-config-panel">
    <div class="panel-header">
      <div>
        <h2>平台配置</h2>
        <p class="hint">超级管理员可直接编辑原始 JSON。</p>
      </div>
      <div class="actions compact">
        <button class="ghost" :disabled="loading" @click="load">
          {{ loading ? "刷新中..." : "刷新" }}
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
      <aside class="config-list">
        <button
          v-for="item in configs"
          :key="item.config_key"
          :class="['config-list-item', { active: activeKey === item.config_key }]"
          @click="selectConfig(item.config_key)"
        >
          <strong>{{ item.config_key }}</strong>
          <span class="config-list-meta">{{ item.description || "无说明" }}</span>
        </button>
      </aside>

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

        <p v-if="hasErrors" class="warn">JSON 语法有误，修正后才能保存。</p>
        <p v-else-if="dirty" class="hint">有未保存修改。</p>

        <div ref="editorTarget" class="json-editor-host"></div>
      </div>
    </div>

    <p v-else-if="loading" class="hint">加载中...</p>
    <p v-else class="hint">暂无平台配置</p>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { createJSONEditor } from "vanilla-jsoneditor";
import "vanilla-jsoneditor/themes/jse-theme-dark.css";
import {
  listAdminPlatformConfigs,
  updateAdminPlatformConfig,
} from "../services/cloudApi";

const configs = ref<any[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const activeKey = ref("");
const draftText = ref("{}");
const dirty = ref(false);
const hasErrors = ref(false);
const editorTarget = ref<HTMLDivElement | null>(null);
let editor: any = null;

const activeConfig = computed(() =>
  configs.value.find((item) => item.config_key === activeKey.value) || null,
);

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await listAdminPlatformConfigs();
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
    error.value = e?.message || "加载平台配置失败";
  } finally {
    loading.value = false;
  }
}

function setEditorText(raw: string) {
  draftText.value = raw || "{}";
  dirty.value = false;
  hasErrors.value = false;
  if (editor) {
    editor.set({ text: draftText.value });
  }
}

function selectConfig(configKey: string) {
  if (configKey === activeKey.value) return;
  activeKey.value = configKey;
  const current = configs.value.find((item) => item.config_key === configKey);
  if (current) {
    setEditorText(current.config_value || "{}");
  }
}

async function save() {
  if (!activeConfig.value || hasErrors.value) return;
  saving.value = true;
  error.value = "";
  try {
    const saved = await updateAdminPlatformConfig(
      activeConfig.value.config_key,
      draftText.value,
    );
    const index = configs.value.findIndex(
      (item) => item.config_key === saved.config_key,
    );
    if (index >= 0) {
      configs.value[index] = saved;
    }
    setEditorText(saved.config_value || "{}");
  } catch (e: any) {
    error.value = e?.message || "保存平台配置失败";
  } finally {
    saving.value = false;
  }
}

function mountEditor() {
  if (!editorTarget.value) return;
  editor = createJSONEditor({
    target: editorTarget.value,
    props: {
      mode: "text",
      mainMenuBar: true,
      navigationBar: false,
      statusBar: true,
      content: { text: draftText.value },
      onChange: (content: any, _previous: any, status: any) => {
        if (typeof content?.text === "string") {
          draftText.value = content.text;
        } else if (content?.json !== undefined) {
          draftText.value = JSON.stringify(content.json, null, 2);
        }
        const current = activeConfig.value?.config_value || "{}";
        dirty.value = draftText.value !== current;
        hasErrors.value = Boolean(status?.contentErrors?.length);
      },
    },
  });
}

onMounted(async () => {
  await load();
  await nextTick();
  mountEditor();
  if (activeConfig.value) {
    setEditorText(activeConfig.value.config_value || "{}");
  }
});

onBeforeUnmount(async () => {
  if (editor?.destroy) {
    await editor.destroy();
  }
});
</script>

<style scoped>
.platform-config-panel {
  min-height: 640px;
}
.config-layout {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: 12px;
  min-height: 560px;
}
.config-list {
  border: 1px solid var(--border);
  background: var(--bg);
  overflow-y: auto;
}
.config-list-item {
  width: 100%;
  text-align: left;
  border: 0;
  border-bottom: 1px solid var(--border);
  color: var(--fg);
  padding: 10px 12px;
}
.config-list-item:hover,
.config-list-item.active {
  background: #141414;
  color: var(--fg);
}
.config-list-item strong {
  display: block;
  margin-bottom: 4px;
}
.config-list-meta {
  display: block;
  font-size: 12px;
  color: var(--fg-dim);
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
.json-editor-host {
  flex: 1;
  min-height: 500px;
  border: 1px solid var(--border);
  background: #0b0b0b;
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
:deep(.jse-theme-dark) {
  --jse-theme-color: #00ff00;
  --jse-theme-color-highlight: #7dd3fc;
  --jse-background-color: #0b0b0b;
  --jse-text-color: #00ff00;
  --jse-text-color-inverse: #0a0a0a;
  --jse-panel-background: #0d0d0d;
  --jse-menu-color: #d4d4d4;
  --jse-delimiter-color: #9ca3af;
  --jse-key-color: #7dd3fc;
  --jse-value-color: #86efac;
  --jse-string-color: #86efac;
  --jse-number-color: #f9a8d4;
  --jse-boolean-color: #fcd34d;
  --jse-null-color: #fca5a5;
}
@media (max-width: 860px) {
  .config-layout {
    grid-template-columns: 1fr;
  }
  .config-list {
    max-height: 180px;
  }
}
</style>
