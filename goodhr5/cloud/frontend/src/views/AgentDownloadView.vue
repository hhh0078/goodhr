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
          <dd>{{ app.systemAppConfig.value?.local_agent_version || "--" }}</dd>
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

    <div class="help-section">
      <div class="section-title">
        <h3>下载后常见问题</h3>
        <p class="hint">遇到下面情况，按步骤处理即可。</p>
      </div>
      <div class="help-list">
        <article v-for="item in helpItems" :key="item.title" class="help-card">
          <strong>{{ item.title }}</strong>
          <p>{{ item.reason }}</p>
          <ol>
            <li v-for="step in item.steps" :key="step">{{ step }}</li>
          </ol>
        </article>
      </div>
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
const helpItems = [
  {
    title: "下载后不要直接在压缩包里双击",
    reason: "压缩包和文件夹长得很像，但在压缩包里直接打开，程序会找不到旁边的文件。",
    steps: [
      "右键下载好的压缩包，选择“全部解压”或“解压到当前文件夹”。",
      "进入解压出来的 GoodHR 文件夹。",
      "双击里面的 GoodHR.exe 启动。",
    ],
  },
  {
    title: "Windows 提示“已保护你的电脑”",
    reason: "这是 Windows 对新程序的安全提醒，不代表程序一定有问题。",
    steps: [
      "点击“更多信息”。",
      "点击“仍要运行”。",
      "如果没有“仍要运行”，请联系管理员或把 GoodHR 文件夹加入信任。",
    ],
  },
  {
    title: "浏览器提示文件不常下载或可能有风险",
    reason: "新软件刚开始下载人数少，浏览器可能会提醒。",
    steps: [
      "确认下载来源是 GoodHR 官网。",
      "选择保留文件。",
      "下载完成后先解压，再进入 GoodHR 文件夹启动。",
    ],
  },
  {
    title: "双击后没有反应或窗口一闪而过",
    reason: "常见原因是运行环境缺失，或只复制了 GoodHR.exe，没有复制整个文件夹。",
    steps: [
      "确认你打开的是解压后的整个 GoodHR 文件夹。",
      "不要只把 GoodHR.exe 单独拖到桌面运行。",
      "打开文件夹里的“启动失败.txt”，按里面步骤下载安装环境。",
    ],
  },
  {
    title: "提示需要安装运行环境",
    reason: "部分 Windows 电脑缺少微软运行环境，本地程序需要它才能打开。",
    steps: [
      "在 GoodHR 窗口点击“下载安装环境”。",
      "安装完成后重启电脑。",
      "重新打开 GoodHR.exe。",
    ],
  },
  {
    title: "没有桌面快捷方式",
    reason: "有些电脑会禁止程序自动创建快捷方式，或桌面目录在 OneDrive 里。",
    steps: [
      "先打开 GoodHR.exe。",
      "在 GoodHR 窗口点击“创建快捷方式”。",
      "如果失败，查看窗口日志里的失败原因。",
    ],
  },
  {
    title: "后台还是显示未连接",
    reason: "后台需要检测到本地程序已经启动，端口一般是 95271。",
    steps: [
      "确认 GoodHR 本地程序窗口处于运行中。",
      "回到后台点击“重新检测”。",
      "如果仍未连接，关闭 GoodHR 后重新打开。",
    ],
  },
  {
    title: "杀毒软件拦截或删除文件",
    reason: "本地程序会启动浏览器并打开本地端口，少数安全软件会误报。",
    steps: [
      "确认文件来自 GoodHR 官网。",
      "把整个 GoodHR 文件夹加入杀毒软件信任。",
      "重新解压一份完整的 GoodHR 文件夹。",
    ],
  },
  {
    title: "Mac 提示无法验证开发者",
    reason: "Mac 对未上架 App Store 的程序会有安全提醒。",
    steps: [
      "打开“系统设置”。",
      "进入“隐私与安全性”。",
      "找到 GoodHR 的拦截提示，点击“仍要打开”。",
    ],
  },
];

/**
 * 重新检测本地程序状态。
 * @returns {void} 无返回值。
 */
function redetect() {
  void app.agent.detect();
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
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 14px;
}
.status-card h3,
.download-card h3 {
  margin: 0 0 12px;
  color: var(--fg);
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
  color: var(--fg);
  overflow-wrap: anywhere;
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
.alt-download {
  margin: 10px 0 0;
  font-size: 12px;
}
.text-link {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--fg-muted);
  font-size: 12px;
  text-decoration: underline;
}
.text-link:hover {
  color: var(--fg);
}
.help-section {
  margin-top: 14px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 14px;
}
.section-title {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: baseline;
  margin-bottom: 12px;
}
.section-title h3 {
  margin: 0;
  color: var(--fg);
}
.help-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.help-card {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
}
.help-card strong {
  display: block;
  color: var(--fg);
  margin-bottom: 6px;
}
.help-card p {
  margin: 0 0 8px;
  color: var(--fg-dim);
  line-height: 1.6;
}
.help-card ol {
  margin: 0;
  padding-left: 18px;
  color: var(--fg-dim);
  line-height: 1.7;
}
@media (max-width: 900px) {
  .download-layout {
    grid-template-columns: 1fr;
  }
  .section-title {
    display: block;
  }
  .help-list {
    grid-template-columns: 1fr;
  }
}
</style>
