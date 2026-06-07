<!-- 本文件负责在首页展示本地程序运行状态、组件状态和快捷维护操作。 -->
<template>
  <section class="panel local-agent-status">
    <div class="panel-header status-head">
      <div>
        <h2>本地程序</h2>
        <p class="hint">控制台、浏览器组件、OCR 和本地数据都在这里统一检查。</p>
      </div>
      <div class="status-actions">
        <button class="ghost" :disabled="loading" @click="refresh">
          {{ loading ? "检查中..." : "刷新" }}
        </button>
        <button class="ghost" :disabled="!connected" @click="openConsole">
          打开控制台
        </button>
      </div>
    </div>

    <div class="summary-row">
      <div class="summary-main">
        <span :class="['status-dot', connected ? 'ok' : 'bad']"></span>
        <div>
          <strong>{{ connected ? "已连接" : "未连接" }}</strong>
          <p>{{ connected ? agentBase : "请启动 GoodHR 本地程序" }}</p>
        </div>
      </div>
      <div class="summary-meta">
        <span>版本 {{ health?.version || "--" }}</span>
        <span>端口 {{ health?.port || "--" }}</span>
      </div>
    </div>

    <div class="status-grid">
      <div class="status-item">
        <span>Node Worker</span>
        <strong :class="runtime?.worker_installed ? 'ok-text' : 'warn-text'">
          {{ runtime?.worker_installed ? "已安装" : "未安装" }}
        </strong>
      </div>
      <div class="status-item">
        <span>CloakBrowser</span>
        <strong :class="runtime?.cloakbrowser_installed ? 'ok-text' : 'warn-text'">
          {{ runtime?.cloakbrowser_installed ? "已安装" : "未安装" }}
        </strong>
      </div>
      <div class="status-item">
        <span>OCR</span>
        <strong :class="ocrInstalled ? 'ok-text' : 'warn-text'">
          {{ ocrInstalled ? "已安装" : "未安装" }}
        </strong>
      </div>
      <div class="status-item">
        <span>控制台包</span>
        <strong :class="consoleStatus?.installed ? 'ok-text' : 'warn-text'">
          {{ consoleStatus?.installed ? "已安装" : "开发/内置" }}
        </strong>
      </div>
    </div>

    <div class="path-grid">
      <div>
        <span>数据目录</span>
        <code>{{ health?.dataDir || "--" }}</code>
      </div>
      <div>
        <span>下载目录</span>
        <code>{{ health?.downloadsDir || "--" }}</code>
      </div>
    </div>

    <p v-if="message" class="hint">{{ message }}</p>
    <p v-if="error" class="error">{{ error }}</p>

    <div class="maintenance-row">
      <button class="ghost" :disabled="!connected || updatingRuntime" @click="installRuntime">
        {{ updatingRuntime ? "更新中..." : "更新运行组件" }}
      </button>
      <button class="ghost" :disabled="!connected || updatingConsole" @click="updateConsole">
        {{ updatingConsole ? "更新中..." : "更新控制台" }}
      </button>
      <button class="ghost" :disabled="!connected || loadingDiagnostics" @click="loadDiagnostics">
        {{ loadingDiagnostics ? "读取中..." : "诊断信息" }}
      </button>
    </div>

    <div v-if="diagnostics" class="diagnostics-box">
      <div>
        <span>端口</span>
        <strong>{{ diagnostics.port || "--" }}</strong>
      </div>
      <div>
        <span>系统</span>
        <strong>{{ diagnostics.os || "--" }} / {{ diagnostics.arch || "--" }}</strong>
      </div>
      <div>
        <span>建议</span>
        <strong>{{ recommendationsText }}</strong>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  getLocalConsoleStatus,
  getLocalDiagnostics,
  installLocalRuntime,
  updateLocalConsolePackage,
} from "../services/localAgentApi";

const props = defineProps<{ agent: any }>();

const health = ref<any>(null);
const consoleStatus = ref<any>(null);
const diagnostics = ref<any>(null);
const loading = ref(false);
const loadingDiagnostics = ref(false);
const updatingRuntime = ref(false);
const updatingConsole = ref(false);
const message = ref("");
const error = ref("");

const agentBase = computed(() => String(props.agent?.baseUrl?.value || ""));
const connected = computed(() => Boolean(agentBase.value && props.agent?.info?.value));
const runtime = computed(() => health.value?.runtime || props.agent?.info?.value?.runtime || {});
const ocrInstalled = computed(() =>
  Boolean(health.value?.ocr?.installed || runtime.value?.ocr_installed),
);
const recommendationsText = computed(() => {
  const items = diagnostics.value?.recommendations || [];
  return Array.isArray(items) && items.length ? items.join("；") : "暂无异常";
});

watch(
  () => props.agent?.info?.value,
  () => {
    health.value = props.agent?.info?.value || null;
    if (connected.value) void refreshDetails();
  },
  { immediate: true },
);

/**
 * 刷新本地程序连接和组件状态。
 * @returns {Promise<void>} 无返回值。
 */
async function refresh() {
  loading.value = true;
  error.value = "";
  message.value = "";
  try {
    await props.agent?.detect?.();
    health.value = props.agent?.info?.value || null;
    await refreshDetails();
    message.value = connected.value ? "本地程序状态已刷新" : "";
  } catch (e: any) {
    error.value = e?.message || "刷新本地程序状态失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 读取本地控制台包状态。
 * @returns {Promise<void>} 无返回值。
 */
async function refreshDetails() {
  if (!agentBase.value) return;
  try {
    consoleStatus.value = await getLocalConsoleStatus(agentBase.value);
  } catch {
    consoleStatus.value = null;
  }
}

/**
 * 打开当前本地控制台地址。
 * @returns {void} 无返回值。
 */
function openConsole() {
  if (!agentBase.value) return;
  window.open(`${agentBase.value}/admin/`, "_blank", "noopener,noreferrer");
}

/**
 * 触发运行组件更新。
 * @returns {Promise<void>} 无返回值。
 */
async function installRuntime() {
  if (!agentBase.value || updatingRuntime.value) return;
  updatingRuntime.value = true;
  error.value = "";
  message.value = "";
  try {
    await installLocalRuntime(agentBase.value);
    await refresh();
    message.value = "运行组件更新完成";
  } catch (e: any) {
    error.value = e?.message || "更新运行组件失败";
  } finally {
    updatingRuntime.value = false;
  }
}

/**
 * 触发控制台前端包更新。
 * @returns {Promise<void>} 无返回值。
 */
async function updateConsole() {
  if (!agentBase.value || updatingConsole.value) return;
  updatingConsole.value = true;
  error.value = "";
  message.value = "";
  try {
    await updateLocalConsolePackage(agentBase.value);
    await refreshDetails();
    message.value = "控制台更新完成，刷新页面后生效";
  } catch (e: any) {
    error.value = e?.message || "更新控制台失败";
  } finally {
    updatingConsole.value = false;
  }
}

/**
 * 读取本地诊断信息。
 * @returns {Promise<void>} 无返回值。
 */
async function loadDiagnostics() {
  if (!agentBase.value || loadingDiagnostics.value) return;
  loadingDiagnostics.value = true;
  error.value = "";
  try {
    diagnostics.value = await getLocalDiagnostics(agentBase.value);
  } catch (e: any) {
    error.value = e?.message || "读取诊断信息失败";
  } finally {
    loadingDiagnostics.value = false;
  }
}
</script>

<style scoped>
.local-agent-status {
  min-height: 0;
}
.status-head,
.status-actions,
.summary-row,
.summary-main,
.summary-meta,
.maintenance-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.status-head,
.summary-row {
  justify-content: space-between;
}
.status-actions,
.maintenance-row {
  flex-wrap: wrap;
}
.summary-row {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
  margin-bottom: 12px;
}
.summary-main strong {
  display: block;
}
.summary-main p,
.summary-meta,
.status-item span,
.path-grid span,
.diagnostics-box span {
  color: var(--fg-dim);
  font-size: 12px;
}
.summary-meta {
  flex-wrap: wrap;
  justify-content: flex-end;
}
.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #7f1d1d;
  flex: 0 0 10px;
}
.status-dot.ok {
  background: var(--accent);
}
.status-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(120px, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}
.status-item,
.path-grid div,
.diagnostics-box div {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 10px;
}
.status-item strong,
.path-grid code,
.diagnostics-box strong {
  display: block;
  margin-top: 4px;
}
.ok-text {
  color: var(--accent);
}
.warn-text {
  color: #f59e0b;
}
.path-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin-bottom: 12px;
}
.path-grid code {
  color: var(--fg);
  word-break: break-all;
  font-size: 12px;
}
.diagnostics-box {
  display: grid;
  grid-template-columns: 100px 180px 1fr;
  gap: 10px;
  margin-top: 12px;
}
@media (max-width: 980px) {
  .status-head,
  .summary-row {
    align-items: flex-start;
    flex-direction: column;
  }
  .status-grid,
  .path-grid,
  .diagnostics-box {
    grid-template-columns: 1fr;
  }
}
</style>
