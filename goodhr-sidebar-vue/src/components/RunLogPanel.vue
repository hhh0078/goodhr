<template>
  <section class="run-log-panel" :class="{ expanded }">
    <header class="run-log-toolbar">
      <div class="run-log-title-wrap">
        <p class="run-log-kicker">Run Console</p>
        <h3 class="run-log-title">{{ title }}</h3>
      </div>
      <div class="run-log-actions">
        <button
          v-if="showClear"
          class="run-log-action"
          type="button"
          @click="$emit('clear')"
        >
          清空
        </button>
        <button class="run-log-action strong" type="button" @click="$emit('toggle-expand')">
          {{ expanded ? "收起" : "放大" }}
        </button>
      </div>
    </header>

    <div class="run-log-meta">
      <span>日志数 {{ logs.length }}</span>
      <span v-if="statusText">{{ statusText }}</span>
    </div>

    <div class="log-shell" :class="{ expanded }">
      <div
        v-for="(log, index) in logs"
        :key="`${log.time}-${index}`"
        class="log-entry"
        :class="log.type"
      >
        <span class="log-prefix">{{ log.prefix }}</span>
        <span class="log-message">
          <template v-if="log.time">[{{ log.time }}] </template>{{ log.message }}
        </span>
      </div>
    </div>
  </section>
</template>

<script setup>
defineProps({
  logs: {
    type: Array,
    required: true,
  },
  expanded: {
    type: Boolean,
    default: false,
  },
  title: {
    type: String,
    default: "运行日志",
  },
  statusText: {
    type: String,
    default: "",
  },
  showClear: {
    type: Boolean,
    default: true,
  },
});

defineEmits(["toggle-expand", "clear"]);
</script>
