<template>
  <div class="view-tabs">
    <button
      type="button"
      :class="['view-tab', { active: ui.activeView === 'main' }]"
      @click="ui.activeView = 'main'"
    >
      主页
    </button>
    <button
      type="button"
      :class="['view-tab', { active: ui.activeView === 'logs' }]"
      @click="ui.activeView = 'logs'"
    >
      日志
      <span v-if="logs.length" class="log-badge">{{ logs.length }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { usePanelStore } from "../composables/usePanelStore";

const { ui, logs } = usePanelStore();
</script>
