<!-- 本文件负责展示本地程序运行组件信息、安装状态和下载配置。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <div>
        <h2>本地组件信息</h2>
        <p class="hint">查看当前系统需要的运行组件、下载地址、安装状态和版本说明。</p>
      </div>
      <button class="ghost" :disabled="app.agent.checking.value" @click="redetect">
        {{ app.agent.checking.value ? "检测中..." : "重新检测" }}
      </button>
    </div>

    <div class="summary-grid">
      <article class="summary-card">
        <span>本地连接</span>
        <strong :class="{ success: connected, error: !connected }">
          {{ app.agent.status.value }}
        </strong>
      </article>
      <article class="summary-card">
        <span>本机系统</span>
        <strong>{{ platformLabel }}</strong>
      </article>
      <article class="summary-card">
        <span>本地程序版本</span>
        <strong>{{ app.agent.info.value?.version || "--" }}</strong>
      </article>
      <article class="summary-card">
        <span>监听端口</span>
        <strong>{{ app.agent.info.value?.port || "--" }}</strong>
      </article>
    </div>

    <div class="component-grid">
      <article v-for="item in componentItems" :key="item.key" class="component-card">
        <div class="component-head">
          <div>
            <h3>{{ item.name }}</h3>
            <p>{{ item.note || "暂无版本说明" }}</p>
          </div>
          <strong :class="{ success: item.installed, warn: !item.installed && !item.required, error: !item.installed && item.required }">
            {{ item.installed ? "已安装" : item.required ? "未安装" : "可选" }}
          </strong>
        </div>
        <dl>
          <dt>配置版本</dt>
          <dd>{{ item.configVersion || "--" }}</dd>
          <dt>本地版本</dt>
          <dd>{{ item.installedVersion || "--" }}</dd>
          <dt>下载地址</dt>
          <dd>
            <code v-if="item.url">{{ item.url }}</code>
            <span v-else>{{ item.bundled ? "随本地程序内置" : "未配置" }}</span>
          </dd>
          <dt>本地路径</dt>
          <dd><code>{{ item.path || "--" }}</code></dd>
          <dt>SHA256</dt>
          <dd><code>{{ item.sha256 || "--" }}</code></dd>
        </dl>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useAppContext } from "../composables/useAppContext";
import { detectAgentDownloadPlatform } from "../services/agentDownload";

type ComponentItem = {
  key: string;
  name: string;
  installed: boolean;
  required: boolean;
  bundled?: boolean;
  configVersion: string;
  installedVersion: string;
  url: string;
  sha256: string;
  note: string;
  path: string;
};

const app = useAppContext();
const connected = computed(() => app.agent.status.value.includes("连接"));
const currentPlatform = computed(() => detectAgentDownloadPlatform());
const platformKey = computed(() => (currentPlatform.value === "windows" ? "win" : "mac"));
const platformLabel = computed(() => (currentPlatform.value === "windows" ? "Windows" : "macOS"));
const runtime = computed(() => app.agent.info.value?.runtime || {});
const runtimeComponents = computed(() => app.onboardingConfig.value?.runtime_components || {});
const installedVersions = computed(() => runtime.value?.installed_versions || {});

const componentItems = computed<ComponentItem[]>(() => [
  buildComponentItem({
    key: "node_runtime",
    name: "Node 运行环境",
    required: true,
    installed: Boolean(runtime.value?.node_installed),
    path: runtime.value?.node_path,
  }),
  {
    key: "node_worker",
    name: "浏览器控制 Worker",
    required: true,
    bundled: true,
    installed: Boolean(runtime.value?.worker_installed),
    configVersion: app.agent.info.value?.version || "",
    installedVersion: app.agent.info.value?.version || "",
    url: "",
    sha256: "",
    note: "随本地程序安装包内置，不需要单独下载安装。",
    path: runtime.value?.worker_entry || "",
  },
  buildComponentItem({
    key: "cloakbrowser",
    name: "CloakBrowser 浏览器",
    required: true,
    installed: Boolean(runtime.value?.cloakbrowser_installed),
    path: runtime.value?.cloakbrowser_path,
  }),
  buildComponentItem({
    key: "ocr",
    name: "OCR 组件",
    required: false,
    installed: Boolean(runtime.value?.ocr_installed),
    path: runtime.value?.ocr_path,
  }),
]);

/**
 * 重新检测本地程序状态。
 * @returns {void} 无返回值。
 */
function redetect() {
  void app.agent.detect();
}

/**
 * 构建运行组件展示项。
 * @param {{key: string; name: string; required: boolean; installed: boolean; path: string}} options - 组件展示基础信息。
 * @returns {ComponentItem} 组件展示项。
 */
function buildComponentItem(options: {
  key: string;
  name: string;
  required: boolean;
  installed: boolean;
  path: string;
}): ComponentItem {
  const asset = componentAsset(options.key);
  const installed = installedVersions.value?.[options.key] || {};
  return {
    key: options.key,
    name: options.name,
    required: options.required,
    installed: options.installed,
    configVersion: stringValue(asset.version),
    installedVersion: stringValue(installed.version),
    url: stringValue(asset.url),
    sha256: stringValue(asset.sha256),
    note: stringValue(asset.note || asset.changelog || asset.description || asset.release_note),
    path: stringValue(options.path),
  };
}

/**
 * 读取当前系统对应的组件配置。
 * @param {string} key - 组件键名。
 * @returns {Record<string, any>} 组件配置。
 */
function componentAsset(key: string) {
  const component = runtimeComponents.value?.[key] || {};
  const value = component[platformKey.value] || component[currentPlatform.value] || {};
  return value && typeof value === "object" ? value : {};
}

/**
 * 安全读取字符串。
 * @param {any} value - 待读取的值。
 * @returns {string} 字符串。
 */
function stringValue(value: any) {
  return String(value || "").trim();
}
</script>

<style scoped>
.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}
.summary-card,
.component-card {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 14px;
}
.summary-card span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
  margin-bottom: 8px;
}
.summary-card strong {
  color: var(--fg);
}
.component-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-top: 14px;
}
.component-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
  margin-bottom: 12px;
}
.component-head h3 {
  margin: 0 0 6px;
  color: var(--fg);
}
.component-head p {
  margin: 0;
  color: var(--fg-dim);
  line-height: 1.6;
}
dl {
  display: grid;
  grid-template-columns: 86px minmax(0, 1fr);
  gap: 8px;
  margin: 0;
}
dt {
  color: var(--fg-dim);
}
dd {
  margin: 0;
  color: var(--fg);
  overflow-wrap: anywhere;
}
code {
  color: var(--fg);
  white-space: normal;
}
.success {
  color: var(--accent);
}
.error {
  color: #f33;
}
.warn {
  color: #fa0;
}
@media (max-width: 900px) {
  .summary-grid,
  .component-grid {
    grid-template-columns: 1fr;
  }
}
</style>
