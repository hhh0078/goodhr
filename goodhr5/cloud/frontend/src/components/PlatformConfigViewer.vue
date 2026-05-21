<!-- 平台配置查看页，仅供管理员浏览云端保存的原始平台 JSON。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>平台配置</h2>
        <p class="hint">仅管理员可见，用于查看云端保存的平台配置 JSON。</p>
      </div>
      <button class="ghost" :disabled="loading" @click="load">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="loading && configs.length === 0" class="hint">加载中...</p>
    <p v-else-if="configs.length === 0" class="hint">暂无平台配置</p>

    <div v-else class="card-list">
      <article v-for="item in configs" :key="item.config_key" class="card config-card">
        <div class="config-head">
          <div>
            <strong>{{ item.config_key }}</strong>
            <p class="card-meta">{{ item.description || "无说明" }}</p>
          </div>
          <span :class="['status-chip', item.enabled ? 'enabled' : 'disabled']">
            {{ item.enabled ? "启用" : "停用" }}
          </span>
        </div>
        <JsonTree :value="parseConfigValue(item.config_value)" />
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { listAdminPlatformConfigs } from "../services/cloudApi";
import JsonTree from "./JsonTree.vue";

const configs = ref<any[]>([]);
const loading = ref(false);
const error = ref("");

async function load() {
  loading.value = true;
  error.value = "";
  try {
    configs.value = await listAdminPlatformConfigs();
  } catch (e: any) {
    error.value = e?.message || "加载平台配置失败";
  } finally {
    loading.value = false;
  }
}

function parseConfigValue(raw: string) {
  try {
    return JSON.parse(raw || "{}");
  } catch {
    return raw;
  }
}

onMounted(load);
</script>

<style scoped>
.config-card {
  gap: 12px;
}
.config-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
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
</style>
