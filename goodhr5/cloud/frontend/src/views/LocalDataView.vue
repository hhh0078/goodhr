<!-- 本文件负责展示 Local Agent 本地数据、下载记录、截图记录和规则包状态。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>本地数据</h2>
        <p class="hint">查看本机保存的下载、截图和规则包状态。</p>
      </div>
      <button class="ghost" :disabled="loading" @click="loadAll">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="message" class="success">{{ message }}</p>

    <div class="local-grid">
      <article class="local-section">
        <div class="section-head">
          <h3>规则包</h3>
          <button class="ghost" :disabled="loading" @click="updateRules">更新规则</button>
        </div>
        <div v-if="rules.length" class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>平台</th>
                <th>版本</th>
                <th>状态</th>
                <th>更新时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in rules" :key="item.platform_id">
                <td>{{ item.platform_id }}</td>
                <td>{{ item.version || "--" }}</td>
                <td>{{ item.status || "--" }}</td>
                <td>{{ formatTime(item.updated_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="empty">暂无规则包记录。</p>
      </article>

      <article class="local-section">
        <h3>下载记录</h3>
        <div v-if="downloads.length" class="record-list">
          <div v-for="item in downloads" :key="item.id || item.file_path" class="record-item">
            <strong>{{ item.file_name || filename(item.file_path) }}</strong>
            <span>{{ item.status || "--" }} · {{ formatSize(item.size) }}</span>
            <small>{{ item.url || item.file_path }}</small>
          </div>
        </div>
        <p v-else class="empty">暂无下载记录。</p>
      </article>

      <article class="local-section">
        <h3>截图记录</h3>
        <div v-if="screenshots.length" class="record-list">
          <div v-for="item in screenshots" :key="item.id || item.file_path" class="record-item">
            <strong>{{ item.label || filename(item.file_path) }}</strong>
            <span>{{ item.task_id || "未关联任务" }} · {{ formatTime(item.created_at) }}</span>
            <small>{{ item.file_path }}</small>
          </div>
        </div>
        <p v-else class="empty">暂无截图记录。</p>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  getLocalRulesStatus,
  listLocalDownloads,
  listLocalScreenshotRecords,
  updateLocalRules,
} from "../services/localAgentApi";

const loading = ref(false);
const error = ref("");
const message = ref("");
const downloads = ref<any[]>([]);
const screenshots = ref<any[]>([]);
const rules = ref<any[]>([]);

onMounted(() => {
  void loadAll();
});

/**
 * 加载本地数据总览。
 * @returns {Promise<void>} 无返回值。
 */
async function loadAll() {
  loading.value = true;
  error.value = "";
  message.value = "";
  try {
    const base = localAgentBase();
    const [downloadRows, screenshotRows, ruleStatus] = await Promise.all([
      listLocalDownloads(base),
      listLocalScreenshotRecords(base),
      getLocalRulesStatus(base),
    ]);
    downloads.value = downloadRows;
    screenshots.value = screenshotRows;
    rules.value = ruleStatus.rules || [];
  } catch (e: any) {
    error.value = e.message || "读取本地数据失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 更新本地规则包。
 * @returns {Promise<void>} 无返回值。
 */
async function updateRules() {
  loading.value = true;
  error.value = "";
  message.value = "";
  try {
    const result = await updateLocalRules(localAgentBase());
    message.value = `规则更新完成：更新 ${result.updated?.length || 0} 个，跳过 ${result.skipped?.length || 0} 个`;
    await loadAll();
  } catch (e: any) {
    error.value = e.message || "规则更新失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 返回 Local Agent 基础地址。
 * @returns {string} Local Agent 地址。
 */
function localAgentBase() {
  return window.location.origin;
}

/**
 * 格式化文件大小。
 * @param {number} value - 字节数。
 * @returns {string} 文件大小。
 */
function formatSize(value: number) {
  const size = Number(value || 0);
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

/**
 * 格式化时间。
 * @param {string} value - 时间字符串。
 * @returns {string} 展示时间。
 */
function formatTime(value: string) {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

/**
 * 从路径中提取文件名。
 * @param {string} value - 文件路径。
 * @returns {string} 文件名。
 */
function filename(value: string) {
  return String(value || "").split(/[\\/]/).filter(Boolean).pop() || "--";
}
</script>

<style scoped>
.local-grid {
  display: grid;
  gap: 16px;
}

.local-section {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
  background: var(--bg-panel);
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 10px 8px;
  border-bottom: 1px solid var(--border);
  text-align: left;
}

.record-list {
  display: grid;
  gap: 10px;
}

.record-item {
  display: grid;
  gap: 4px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
}

.record-item small {
  color: var(--fg-dim);
  word-break: break-all;
}

.empty {
  color: var(--fg-dim);
}
</style>
