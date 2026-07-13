<template>
  <section v-if="ui.activeView === 'logs'" class="terminal-panel">
    <header class="terminal-header">
      <span class="terminal-title">● 运行日志</span>
      <button type="button" class="terminal-clear-btn" @click="logs.length = 0">
        清空
      </button>
    </header>
    <div ref="terminalBody" class="terminal-body">
      <div
        v-for="(entry, index) in logs"
        :key="`${entry.time}-${index}`"
        class="terminal-line"
        :class="entry.type"
      >
        <span class="terminal-time">{{ entry.time }}</span>
        <span class="terminal-msg">{{ entry.message }}</span>
      </div>
      <div v-if="!logs.length" class="terminal-empty">
        暂无日志，等待操作...
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, computed } from "vue";
import { usePanelStore } from "../composables/usePanelStore";

const { ui, logs } = usePanelStore();
const terminalBody = ref<HTMLElement | null>(null);

const lastLogSignature = computed(() => {
  if (logs.length === 0) return "";
  const last = logs[logs.length - 1];
  return `${last.time}|${last.message}|${last.type}`;
});

watch([() => logs.length, lastLogSignature], async () => {
  await nextTick();
  if (terminalBody.value) {
    const el = terminalBody.value;
    const target = el.scrollHeight - el.clientHeight * 0.7;
    el.scrollTop = Math.max(0, target);
  }
});
</script>
