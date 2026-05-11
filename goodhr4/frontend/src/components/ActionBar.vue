<template>
  <footer class="action-bar floating-action-bar">
    <div class="site-links footer-links">
      <a :href="ui.systemConfig.contact_url" target="_blank" rel="noreferrer"
        >联系我</a
      >
      <a :href="ui.systemConfig.donate_url" target="_blank" rel="noreferrer"
        >打赏我</a
      >
      <a :href="ui.systemConfig.website_url" target="_blank" rel="noreferrer"
        >前往官网</a
      >
    </div>
    <div class="header-actions footer-actions">
      <button
        v-if="!ui.running"
        class="btn btn-primary btn-large"
        type="button"
        @click="startRun"
      >
        开始运行
      </button>
      <button
        v-else
        class="btn btn-danger btn-large"
        type="button"
        @click="stopRun"
      >
        停止运行
      </button>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { usePanelStore } from "../composables/usePanelStore";

const { ui, startRun, stopRun } = usePanelStore();
</script>
