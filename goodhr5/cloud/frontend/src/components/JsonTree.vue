<template>
  <div class="json-tree">
    <template v-if="isObject(value)">
      <details :open="depth < 2" class="json-node">
        <summary>
          <span v-if="label" class="json-key">{{ label }}</span>
          <span class="json-brace">{{ Array.isArray(value) ? "[" : "{" }}</span>
          <span class="json-meta">{{ Array.isArray(value) ? `${value.length} 项` : `${objectEntries(value).length} 个字段` }}</span>
          <span class="json-brace">{{ Array.isArray(value) ? "]" : "}" }}</span>
        </summary>
        <div class="json-children">
          <JsonTree
            v-for="(entryValue, entryKey) in iterableEntries(value)"
            :key="String(entryKey)"
            :label="String(entryKey)"
            :value="entryValue"
            :depth="depth + 1"
          />
        </div>
      </details>
    </template>
    <div v-else class="json-leaf">
      <span v-if="label" class="json-key">{{ label }}</span>
      <span class="json-colon">:</span>
      <span :class="leafClass">{{ formatValue(value) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";

defineOptions({ name: "JsonTree" });

const props = withDefaults(
  defineProps<{
    label?: string;
    value: any;
    depth?: number;
  }>(),
  {
    label: "",
    depth: 0,
  },
);

function isObject(value: unknown) {
  return value !== null && typeof value === "object";
}

function objectEntries(value: Record<string, any> | any[]) {
  return Array.isArray(value) ? value : Object.entries(value || {});
}

function iterableEntries(value: Record<string, any> | any[]) {
  if (Array.isArray(value)) {
    return value.map((item, index) => [index, item] as const);
  }
  return Object.entries(value || {});
}

function formatValue(value: any) {
  if (value === null) return "null";
  if (typeof value === "string") return `"${value}"`;
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") return String(value);
  return String(value);
}

const leafClass = computed(() => {
  const value = props.value;
  if (value === null) return "json-null";
  switch (typeof value) {
    case "string":
      return "json-string";
    case "number":
      return "json-number";
    case "boolean":
      return "json-boolean";
    default:
      return "json-string";
  }
});
</script>

<style scoped>
.json-tree {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
}
.json-node {
  margin-left: 8px;
}
.json-node > summary {
  cursor: pointer;
  color: var(--fg);
  list-style: none;
}
.json-node > summary::-webkit-details-marker {
  display: none;
}
.json-children {
  margin-left: 16px;
  border-left: 1px solid var(--border);
  padding-left: 12px;
}
.json-leaf {
  margin-left: 24px;
}
.json-key {
  color: #7dd3fc;
  margin-right: 4px;
}
.json-colon {
  color: var(--fg-dim);
  margin-right: 6px;
}
.json-brace {
  color: #93c5fd;
}
.json-meta {
  color: var(--fg-dim);
  margin: 0 6px;
}
.json-string {
  color: #86efac;
}
.json-number {
  color: #f9a8d4;
}
.json-boolean {
  color: #fcd34d;
}
.json-null {
  color: #fca5a5;
}
</style>
