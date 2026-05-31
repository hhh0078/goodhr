<!-- 本文件负责展示本地程序下载入口和当前本地程序连接状态。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>本地程序下载</h2>
        <p class="hint">本地程序负责浏览器控制、截图、OCR 和任务执行。</p>
      </div>
      <button class="ghost" :disabled="app.agent.checking.value" @click="redetect">
        {{ app.agent.checking.value ? "检测中..." : "重新检测" }}
      </button>
    </div>

    <div class="download-layout">
      <article class="status-card">
        <h3>当前状态</h3>
        <dl>
          <dt>连接</dt>
          <dd :class="{ success: connected, error: !connected }">
            {{ app.agent.status.value }}
          </dd>
          <dt>端口</dt>
          <dd>{{ app.agent.info.value?.port || "--" }}</dd>
          <dt>版本</dt>
          <dd>{{ app.agent.info.value?.version || "--" }}</dd>
          <dt>要求版本</dt>
          <dd>{{ app.systemAppConfig.value?.local_agent_version || "5.0.0" }}</dd>
          <dt>绑定</dt>
          <dd>{{ app.agent.bindStatus.value }}</dd>
          <dt>WS</dt>
          <dd>{{ app.agent.wsStatus.value }}</dd>
        </dl>
      </article>

      <article class="download-card">
        <h3>下载 GoodHR 本地程序</h3>
        <p>
          下载后双击启动，回到后台右上角显示“已连接”就可以开始创建平台账号和任务。
        </p>
        <button :disabled="!primaryDownload.url" @click="openDownload(primaryDownload.url)">
          {{ primaryDownload.url ? primaryDownload.label : "暂未配置下载链接" }}
        </button>
        <p v-if="secondaryDownload.url" class="alt-download">
          <button class="text-link" @click="openDownload(secondaryDownload.url)">
            {{ secondaryDownload.label }}
          </button>
        </p>
        <p v-if="!primaryDownload.url" class="warn">
          请到系统配置里的 system.onboarding_config 配置 Mac 或 Windows 下载链接。
        </p>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useAppContext } from "../composables/useAppContext";
import { buildAgentDownloadOptions } from "../services/agentDownload";

const app = useAppContext();
const downloadOptions = computed(() => buildAgentDownloadOptions(app.onboardingConfig.value));
const primaryDownload = computed(() => downloadOptions.value.primary);
const secondaryDownload = computed(() => downloadOptions.value.secondary);
const connected = computed(() => app.agent.status.value.includes("连接"));

/**
 * 重新检测本地程序状态。
 * @returns {void} 无返回值。
 */
function redetect() {
  void app.agent.detect(app.user.value, app.auth.token.value);
}

/**
 * 打开本地程序下载链接。
 * @param {string} url - 下载链接。
 * @returns {void} 无返回值。
 */
function openDownload(url: string) {
  if (!url) return;
  window.open(url, "_blank", "noopener,noreferrer");
}
</script>

<style scoped>
.download-layout {
  display: grid;
  grid-template-columns: minmax(260px, 360px) minmax(0, 1fr);
  gap: 12px;
}
.status-card,
.download-card {
  border: 1px solid #333;
  background: #050505;
  padding: 14px;
}
.status-card h3,
.download-card h3 {
  margin: 0 0 12px;
  color: #eee;
}
.download-card p {
  color: var(--fg-dim);
  line-height: 1.6;
}
dl {
  display: grid;
  grid-template-columns: 80px minmax(0, 1fr);
  gap: 8px;
  margin: 0;
}
dt {
  color: var(--fg-dim);
}
dd {
  margin: 0;
  color: #eee;
  overflow-wrap: anywhere;
}
.success {
  color: #0f0;
}
.error {
  color: #f33;
}
.warn {
  color: #fa0;
}
.alt-download {
  margin: 10px 0 0;
  font-size: 12px;
}
.text-link {
  border: 0;
  padding: 0;
  background: transparent;
  color: #aaa;
  font-size: 12px;
  text-decoration: underline;
}
.text-link:hover {
  color: #eee;
}
@media (max-width: 900px) {
  .download-layout {
    grid-template-columns: 1fr;
  }
}
</style>
