<!-- 本文件负责在必要运行组件缺失时展示不可关闭的强制安装弹框。 -->
<template>
  <div v-if="visible" class="runtime-mask">
    <section class="runtime-panel">
      <div class="runtime-head">
        <h2>安装必要组件</h2>
        <strong>{{ runtimeInstalling ? "安装中" : "必须完成" }}</strong>
      </div>

      <p class="runtime-desc">
        本地程序需要下面组件才能控制浏览器。安装成功前不能继续使用后台。
      </p>

      <div class="component-list">
        <div
          v-for="item in requiredComponents"
          :key="item.key"
          :class="['component-item', { done: item.installed }]"
        >
          <span>{{ item.label }}</span>
          <strong>{{ item.installed ? "已可用" : "未安装" }}</strong>
        </div>
      </div>

      <div v-if="runtimeProgressVisible" class="runtime-progress">
        <div class="progress-head">
          <strong>{{ runtimeProgressTitle }}</strong>
          <span>{{ runtimeProgressPercent }}%</span>
        </div>
        <div class="progress-bar" aria-label="必要组件安装进度">
          <span :style="{ width: `${runtimeProgressPercent}%` }"></span>
        </div>
        <p>{{ runtimeProgressMessage }}</p>
        <small v-if="runtimeProgressBytes">{{ runtimeProgressBytes }}</small>
      </div>

      <p v-if="error || runtimeInstallError" class="error">
        {{ error || runtimeInstallError }}
      </p>

      <button
        class="ghost primary runtime-action"
        :disabled="runtimeInstalling"
        @click="installRuntime"
      >
        {{ runtimeInstalling ? "正在安装..." : "安装必要组件" }}
      </button>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  getLocalRuntimeStatus,
  installLocalRuntime,
} from "../services/localAgentApi";
import { buildRuntimeInstallPayload } from "../services/runtimeInstallConfig";

const props = defineProps<{ agent: any }>();

const health = ref<any>(null);
const updatingRuntime = ref(false);
const error = ref("");
let runtimeInstallStarted = false;
let runtimePollingTimer: number | null = null;

const agentBase = computed(() => String(props.agent?.baseUrl?.value || ""));
const connected = computed(() => Boolean(agentBase.value && props.agent?.info?.value));
const runtime = computed(() => health.value?.runtime || props.agent?.info?.value?.runtime || {});
const runtimeProgress = computed(() => runtime.value?.install_progress || {});
const requiredComponents = computed(() => [
  { key: "node", label: "Node 运行环境", installed: Boolean(runtime.value?.node_installed) },
  { key: "cloakbrowser", label: "CloakBrowser 浏览器", installed: Boolean(runtime.value?.cloakbrowser_installed) },
]);
const runtimeRequiredMissing = computed(() =>
  requiredComponents.value.some((item) => !item.installed),
);
const visible = computed(() => Boolean(connected.value && runtimeRequiredMissing.value));
const runtimeInstalling = computed(() =>
  Boolean(updatingRuntime.value || runtimeProgress.value?.running),
);
const runtimeProgressVisible = computed(() =>
  Boolean(runtimeInstalling.value || runtimeProgress.value?.message),
);
const runtimeProgressPercent = computed(() => {
  const value = Number(runtimeProgress.value?.percent || 0);
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, Math.round(value)));
});
const runtimeProgressTitle = computed(() => {
  const component = componentName(runtimeProgress.value?.component);
  const stage = stageName(runtimeProgress.value?.stage);
  return [component, stage].filter(Boolean).join(" / ") || "必要组件安装";
});
const runtimeProgressMessage = computed(() =>
  String(runtimeProgress.value?.message || (runtimeInstalling.value ? "正在安装必要组件" : "等待安装")),
);
const runtimeProgressBytes = computed(() => {
  const received = Number(runtimeProgress.value?.received || 0);
  const total = Number(runtimeProgress.value?.total || 0);
  if (!received && !total) return "";
  if (total > 0) return `${formatBytes(received)} / ${formatBytes(total)}`;
  return `已下载 ${formatBytes(received)}`;
});
const runtimeInstallError = computed(() => {
  if (runtimeProgress.value?.stage !== "failed") return "";
  return String(runtimeProgress.value?.message || "必要组件安装失败");
});

watch(
  () => props.agent?.info?.value,
  () => {
    health.value = props.agent?.info?.value || null;
    if (visible.value) startRuntimePolling();
  },
  { immediate: true },
);

watch(visible, (nextVisible) => {
  if (nextVisible) {
    startRuntimePolling();
  } else {
    stopRuntimePolling();
  }
});

onBeforeUnmount(() => {
  stopRuntimePolling();
});

/**
 * 触发必要运行组件安装。
 * @returns {Promise<void>} 无返回值。
 */
async function installRuntime() {
  if (!agentBase.value || runtimeInstalling.value) return;
  updatingRuntime.value = true;
  error.value = "";
  startRuntimePolling();
  try {
    await installLocalRuntime(agentBase.value, buildRuntimeInstallPayload());
    runtimeInstallStarted = true;
    await pollRuntimeStatus();
    updatingRuntime.value = false;
  } catch (e: any) {
    error.value = e?.message || "安装必要组件失败";
    updatingRuntime.value = false;
    stopRuntimePollingIfIdle();
  }
}

/**
 * 开始轮询运行组件安装状态。
 * @returns {void} 无返回值。
 */
function startRuntimePolling() {
  if (runtimePollingTimer != null) return;
  void pollRuntimeStatus();
  runtimePollingTimer = window.setInterval(() => {
    void pollRuntimeStatus();
  }, 1000);
}

/**
 * 停止轮询运行组件安装状态。
 * @returns {void} 无返回值。
 */
function stopRuntimePolling() {
  if (runtimePollingTimer == null) return;
  window.clearInterval(runtimePollingTimer);
  runtimePollingTimer = null;
}

/**
 * 在安装空闲时停止轮询。
 * @returns {void} 无返回值。
 */
function stopRuntimePollingIfIdle() {
  if (runtimeProgress.value?.running || updatingRuntime.value || visible.value) return;
  stopRuntimePolling();
}

/**
 * 轮询本地运行组件状态。
 * @returns {Promise<void>} 无返回值。
 */
async function pollRuntimeStatus() {
  if (!agentBase.value) return;
  try {
    const status = await getLocalRuntimeStatus(agentBase.value);
    health.value = {
      ...(health.value || props.agent?.info?.value || {}),
      runtime: status,
    };
    if (!status?.install_progress?.running && !updatingRuntime.value) {
      if (runtimeInstallStarted) {
        runtimeInstallStarted = false;
        if (status?.install_progress?.stage === "failed") {
          error.value = status?.install_progress?.message || "必要组件安装失败";
        } else {
          await props.agent?.detect?.();
        }
      }
      stopRuntimePollingIfIdle();
    }
  } catch (e: any) {
    if (!updatingRuntime.value) {
      error.value = e?.message || "读取必要组件状态失败";
      stopRuntimePollingIfIdle();
    }
  }
}

/**
 * 转换组件键名为中文名称。
 * @param {string} value - 组件键名。
 * @returns {string} 中文名称。
 */
function componentName(value: string) {
  const names: Record<string, string> = {
    node_runtime: "Node 运行环境",
    node_worker: "Node Worker",
    cloakbrowser: "CloakBrowser",
    ocr: "OCR 组件",
  };
  return names[value] || "";
}

/**
 * 转换安装阶段为中文名称。
 * @param {string} value - 阶段键名。
 * @returns {string} 中文名称。
 */
function stageName(value: string) {
  const names: Record<string, string> = {
    queued: "已开始",
    manifest: "读取清单",
    download: "下载中",
    verify: "校验中",
    extract: "解压中",
    installed: "安装完成",
    skipped: "已跳过",
    failed: "失败",
    idle: "空闲",
  };
  return names[value] || "";
}

/**
 * 格式化字节大小。
 * @param {number} bytes - 字节数。
 * @returns {string} 便于阅读的大小。
 */
function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}
</script>

<style scoped>
.runtime-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(0, 0, 0, 0.72);
}
.runtime-panel {
  width: min(520px, 100%);
  border: 1px solid var(--border);
  background: var(--bg-panel);
  padding: 14px;
}
.runtime-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}
.runtime-head h2 {
  margin: 0;
}
.runtime-head strong,
.runtime-desc,
.runtime-progress small {
  color: var(--fg-dim);
  font-size: 13px;
}
.component-list {
  display: grid;
  gap: 8px;
  margin: 12px 0;
}
.component-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 8px 10px;
}
.component-item strong {
  color: var(--fg-warn);
}
.component-item.done strong {
  color: var(--accent);
}
.runtime-progress {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 8px;
  margin-bottom: 8px;
}
.progress-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 6px;
}
.progress-bar {
  height: 6px;
  overflow: hidden;
  background: var(--border);
  margin-bottom: 6px;
}
.progress-bar span {
  display: block;
  height: 100%;
  background: var(--accent);
  transition: width 0.2s ease;
}
.runtime-progress p {
  margin: 0 0 4px;
  color: var(--fg);
  font-size: 12px;
}
.runtime-action {
  width: 100%;
  margin-top: 10px;
}
</style>
