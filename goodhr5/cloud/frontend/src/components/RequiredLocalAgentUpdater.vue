<!-- 本文件负责在本地程序版本不一致时展示不可关闭的强制更新弹框。 -->
<template>
  <div v-if="visible" class="agent-update-mask">
    <section class="agent-update-panel">
      <div class="agent-update-head">
        <h2>更新本地程序</h2>
        <strong>{{ updateRunning ? "更新中" : "必须更新" }}</strong>
      </div>

      <p class="agent-update-desc">
        当前本地程序版本和后台要求版本不一致。更新完成前不能继续使用后台。
      </p>

      <div v-if="releaseNote" class="release-note">
        <span>本次更新</span>
        <p>{{ releaseNote }}</p>
      </div>

      <div class="version-grid">
        <div>
          <span>当前版本</span>
          <strong>{{ currentVersion || "--" }}</strong>
        </div>
        <div>
          <span>要求版本</span>
          <strong>{{ requiredVersion || "--" }}</strong>
        </div>
      </div>

      <div v-if="progressVisible" class="agent-update-progress">
        <div class="progress-head">
          <strong>{{ progressTitle }}</strong>
          <span>{{ progressPercent }}%</span>
        </div>
        <div class="progress-bar" aria-label="本地程序更新进度">
          <span :style="{ width: `${progressPercent}%` }"></span>
        </div>
        <p>{{ progressMessage }}</p>
        <small v-if="progressBytes">{{ progressBytes }}</small>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <button class="ghost primary agent-update-action" :disabled="updateRunning" @click="startUpdate">
        {{ updateRunning ? "正在更新..." : "立即更新" }}
      </button>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { detectAgentDownloadPlatform, getAgentDownloadURL } from "../services/agentDownload";
import { latestLocalAgentRelease, localAgentRequiredVersion } from "../services/localAgentRelease";
import {
  getLocalAppUpdateStatus,
  startLocalAppUpdate,
} from "../services/localAgentApi";

const props = defineProps<{ agent: any; appConfig?: any; onboardingConfig?: any }>();

const progress = ref<any>({});
const updating = ref(false);
const error = ref("");
let pollingTimer: number | null = null;

const agentBase = computed(() => String(props.agent?.baseUrl?.value || ""));
const health = computed(() => props.agent?.info?.value || null);
const currentVersion = computed(() => String(health.value?.version || ""));
const requiredVersion = computed(() =>
  localAgentRequiredVersion(props.onboardingConfig || readCachedOnboardingConfig()),
);
const downloadURL = computed(() =>
  getAgentDownloadURL(props.onboardingConfig || readCachedOnboardingConfig(), detectAgentDownloadPlatform()),
);
const releaseNote = computed(() =>
  firstText(
    localAgentReleaseNote(props.appConfig || readCachedAppConfig(), props.onboardingConfig || readCachedOnboardingConfig()),
    progress.value?.release_note,
  ),
);
const versionMismatch = computed(() =>
  Boolean(agentBase.value && currentVersion.value && requiredVersion.value && isVersionLower(currentVersion.value, requiredVersion.value)),
);
const visible = computed(() => versionMismatch.value);
const updateRunning = computed(() => Boolean(updating.value || progress.value?.running || progress.value?.stage === "install"));
const progressVisible = computed(() => Boolean(updateRunning.value || progress.value?.message));
const progressPercent = computed(() => {
  const value = Number(progress.value?.percent || 0);
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, Math.round(value)));
});
const progressTitle = computed(() => stageName(progress.value?.stage) || "本地程序更新");
const progressMessage = computed(() =>
  String(progress.value?.message || (updateRunning.value ? "正在更新本地程序" : "等待更新")),
);
const progressBytes = computed(() => {
  const received = Number(progress.value?.received || 0);
  const total = Number(progress.value?.total || 0);
  if (!received && !total) return "";
  if (total > 0) return `${formatBytes(received)} / ${formatBytes(total)}`;
  return `已下载 ${formatBytes(received)}`;
});

watch(
  visible,
  (nextVisible) => {
    if (nextVisible) {
      startPolling();
    } else {
      stopPolling();
    }
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  stopPolling();
});

/**
 * 开始本地程序更新。
 * @returns {Promise<void>} 无返回值。
 */
async function startUpdate() {
  if (!agentBase.value || updateRunning.value) return;
  const url = String(downloadURL.value || "").trim();
  if (!url) {
    error.value = "没有配置当前系统的本地程序下载地址";
    return;
  }
  updating.value = true;
  error.value = "";
  startPolling();
  try {
    const status = await startLocalAppUpdate(agentBase.value, {
      url,
      target_version: requiredVersion.value,
      release_note: releaseNote.value,
    });
    progress.value = status || {};
  } catch (e: any) {
    error.value = e?.message || "启动本地程序更新失败";
    updating.value = false;
  }
}

/**
 * 开始轮询本地程序更新进度。
 * @returns {void} 无返回值。
 */
function startPolling() {
  if (pollingTimer != null) return;
  void pollUpdateStatus();
  pollingTimer = window.setInterval(() => {
    void pollUpdateStatus();
  }, 1000);
}

/**
 * 停止轮询本地程序更新进度。
 * @returns {void} 无返回值。
 */
function stopPolling() {
  if (pollingTimer == null) return;
  window.clearInterval(pollingTimer);
  pollingTimer = null;
}

/**
 * 轮询本地程序更新进度。
 * @returns {Promise<void>} 无返回值。
 */
async function pollUpdateStatus() {
  if (!agentBase.value) return;
  try {
    progress.value = await getLocalAppUpdateStatus(agentBase.value);
    if (progress.value?.stage === "failed") {
      error.value = progress.value?.message || "本地程序更新失败";
      updating.value = false;
    }
    if (progress.value?.stage === "install") {
      error.value = "安装器已启动，本地程序会自动重启。请稍等后台重新连接。";
      window.setTimeout(() => props.agent?.detect?.(), 2500);
    }
  } catch {
    if (updating.value) {
      error.value = "本地程序正在重启，请稍等后台重新连接。";
      window.setTimeout(() => props.agent?.detect?.(), 2500);
    }
  }
}

/**
 * 读取缓存中的系统应用配置。
 * @returns {any} 系统应用配置。
 */
function readCachedAppConfig() {
  try {
    return JSON.parse(localStorage.getItem("system_app_config") || "{}");
  } catch {
    return {};
  }
}

/**
 * 读取缓存中的新手引导配置。
 * @returns {any} 新手引导配置。
 */
function readCachedOnboardingConfig() {
  try {
    return JSON.parse(localStorage.getItem("system_onboarding_config") || "{}");
  } catch {
    return {};
  }
}

/**
 * 读取本地程序更新说明。
 * @param {any} appConfig - 系统应用配置。
 * @param {any} onboardingConfig - 新手引导配置。
 * @returns {string} 更新说明文本。
 */
function localAgentReleaseNote(appConfig: any, onboardingConfig: any) {
  const release = latestLocalAgentRelease(onboardingConfig, detectAgentDownloadPlatform());
  return firstText(
    appConfig?.local_agent_update_note,
    appConfig?.local_agent_changelog,
    appConfig?.local_agent_release_note,
    onboardingConfig?.local_agent_update_note,
    onboardingConfig?.local_agent_changelog,
    onboardingConfig?.local_agent_release_note,
    release.note,
  );
}

/**
 * 返回第一个非空文本。
 * @param {...any} values - 候选文本。
 * @returns {string} 非空文本。
 */
function firstText(...values: any[]) {
  for (const value of values) {
    const text = String(value || "").trim();
    if (text) return text;
  }
  return "";
}

/**
 * 判断当前版本是否低于目标版本。
 * @param {string} current - 当前版本号。
 * @param {string} target - 目标版本号。
 * @returns {boolean} 当前版本低于目标版本时返回 true。
 */
function isVersionLower(current: string, target: string) {
  return compareVersion(target, current) > 0;
}

/**
 * 按点分数字比较版本号。
 * @param {string} left - 左侧版本号。
 * @param {string} right - 右侧版本号。
 * @returns {number} left 更高返回 1，right 更高返回 -1，相等返回 0。
 */
function compareVersion(left: string, right: string) {
  const leftParts = parseVersionParts(left);
  const rightParts = parseVersionParts(right);
  const maxLen = Math.max(leftParts.length, rightParts.length);
  for (let index = 0; index < maxLen; index += 1) {
    const leftValue = leftParts[index] || 0;
    const rightValue = rightParts[index] || 0;
    if (leftValue > rightValue) return 1;
    if (leftValue < rightValue) return -1;
  }
  return 0;
}

/**
 * 将版本号拆成数字片段。
 * @param {string} value - 原始版本号。
 * @returns {number[]} 数字片段列表。
 */
function parseVersionParts(value: string) {
  return String(value || "").trim().replace(/^v/i, "").split(".").map((part) => {
    const match = part.trim().match(/^\d+/);
    return match ? Number(match[0]) : 0;
  });
}

/**
 * 转换更新阶段为中文名称。
 * @param {string} value - 阶段键名。
 * @returns {string} 中文名称。
 */
function stageName(value: string) {
  const names: Record<string, string> = {
    idle: "等待更新",
    queued: "准备下载",
    download: "下载中",
    install: "安装中",
    failed: "更新失败",
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
.agent-update-mask {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(0, 0, 0, 0.72);
}
.agent-update-panel {
  width: min(520px, 100%);
  border: 1px solid var(--border);
  background: var(--bg-panel);
  padding: 14px;
}
.agent-update-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}
.agent-update-head h2 {
  margin: 0;
}
.agent-update-head strong,
.agent-update-desc,
.agent-update-progress small {
  color: var(--fg-dim);
  font-size: 13px;
}
.version-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 12px 0;
}
.version-grid div {
  display: grid;
  gap: 4px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 8px 10px;
}
.version-grid span {
  color: var(--fg-dim);
  font-size: 12px;
}
.version-grid strong {
  overflow-wrap: anywhere;
}
.release-note {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 8px 10px;
  margin: 12px 0;
}
.release-note span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
  margin-bottom: 4px;
}
.release-note p {
  margin: 0;
  color: var(--fg);
  font-size: 13px;
  line-height: 1.7;
  white-space: pre-wrap;
}
.agent-update-progress {
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
.agent-update-progress p {
  margin: 0 0 4px;
  color: var(--fg);
  font-size: 12px;
}
.agent-update-action {
  width: 100%;
  margin-top: 10px;
}
@media (max-width: 560px) {
  .version-grid {
    grid-template-columns: 1fr;
  }
}
</style>
