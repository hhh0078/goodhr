<template>
  <div class="theme-mask">
    <section class="theme-panel">
      <div class="theme-header">
        <h2>选择后台主题</h2>
        <button v-if="allowClose" class="ghost" @click="$emit('close')">关闭</button>
      </div>
      <div class="theme-grid">
        <button
          v-for="theme in themes"
          :key="theme.id"
          :class="['theme-option', { active: theme.id === modelValue }]"
          @click="$emit('select', theme.id)"
        >
          <span class="theme-name">{{ theme.name }}</span>
          <span class="theme-summary">{{ theme.summary }}</span>
          <span class="theme-swatches">
            <span
              v-for="color in theme.colors"
              :key="color"
              class="theme-swatch"
              :style="{ backgroundColor: color }"
            ></span>
          </span>
        </button>
      </div>
      <div class="theme-actions">
        <button class="ghost primary" @click="$emit('confirm')">使用此主题</button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
/** 后台主题选择弹窗。 */
import type { AppTheme, ThemeID } from "../services/theme";

defineProps<{
  themes: AppTheme[];
  modelValue: ThemeID;
  allowClose?: boolean;
}>();

defineEmits<{
  select: [themeID: ThemeID];
  confirm: [];
  close: [];
}>();
</script>

<style scoped>
.theme-mask {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(0, 0, 0, 0.68);
}
.theme-panel {
  width: min(720px, 100%);
  border: 1px solid var(--border);
  background: var(--bg-panel);
  padding: 14px;
}
.theme-header,
.theme-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.theme-header {
  margin-bottom: 12px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}
.theme-header h2 {
  margin: 0;
}
.theme-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 10px;
}
.theme-option {
  display: flex;
  min-height: 150px;
  flex-direction: column;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  border-color: var(--border);
  background: var(--bg-input);
  color: var(--fg-dim);
  padding: 12px;
  text-align: left;
}
.theme-option:hover,
.theme-option.active {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--fg);
}
.theme-name {
  color: var(--fg);
  font-size: 16px;
}
.theme-summary {
  color: var(--fg-dim);
  font-size: 13px;
  line-height: 1.5;
}
.theme-swatches {
  display: flex;
  gap: 6px;
}
.theme-swatch {
  width: 22px;
  height: 22px;
  border: 1px solid var(--border);
}
.theme-actions {
  justify-content: flex-end;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
@media (max-width: 720px) {
  .theme-grid {
    grid-template-columns: 1fr;
  }
}
</style>
