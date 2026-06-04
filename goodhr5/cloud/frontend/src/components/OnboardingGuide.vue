<!-- 本文件负责展示首页新手教学卡片，引导用户完成关键流程。 -->
<template>
  <section class="panel onboarding-panel">
    <div class="panel-header">
      <div>
        <h2>新手教学</h2>
        <p class="hint">完成这些步骤，会自动消失哦。会帮你快速上手 GoodHR。</p>
      </div>
      <span class="progress">{{ doneCount }}/{{ cards.length }}</span>
    </div>

    <div class="onboarding-grid">
      <article
        v-for="card in cards"
        :key="card.key"
        :class="[
          'guide-card',
          { done: isDone(card.key), active: card.key === activeKey },
        ]"
      >
        <div class="need">{{ isDone(card.key) ? "已完成" : "需要" }}</div>
        <div class="card-title">
          <span>{{ card.index }}</span>
          <strong>{{ card.title }}</strong>
        </div>
        <p>{{ card.description }}</p>
        <ol>
          <li v-for="item in card.tips" :key="item">{{ item }}</li>
        </ol>
        <button
          v-if="card.key === 'local_agent' && !isDone(card.key)"
          class="ghost primary"
          @click="openDownload"
        >
          {{ primaryDownload.label }}
        </button>
        <button
          v-else-if="card.menu"
          class="ghost"
          @click="$emit('go', card.menu)"
        >
          去完成
        </button>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { buildAgentDownloadOptions } from "../services/agentDownload";
import { ONBOARDING_STEPS } from "../services/onboarding";

const props = defineProps<{ progress: any; config: any }>();
defineEmits<{ (e: "go", menu: string): void }>();

const cards = [
  {
    key: "local_agent",
    index: "1",
    title: "确认本地程序已启动",
    menu: "agent",
    description:
      "本地 GoodHR 程序是核心组件，浏览器控制、截图和 OCR 都依赖它。",
    tips: [
      "先启动本地 GoodHR 程序",
      "前端检测 health 成功后会自动完成",
      "检测失败时请下载并启动本地程序",
    ],
  },
  {
    key: "platform_account",
    index: "2",
    title: "创建平台账号",
    menu: "account",
    description: "进入平台账号页，新增账号，扫码登录后保存 Cookie。",
    tips: [
      "左侧进入平台账号",
      "点击新增，再点登录并获取Cookie",
      "扫码登录完成后填写名称并保存账号",
    ],
  },
  {
    key: "position_template",
    index: "3",
    title: "创建岗位模板",
    menu: "position",
    description: "岗位模板决定筛选条件、岗位要求和后续打招呼逻辑。",
    tips: [
      "左侧进入岗位模板",
      "点击新建模板",
      "填写岗位名称、岗位要求或关键词后保存",
    ],
  },
  {
    key: "personal_config",
    index: "4",
    title: "保存个人配置",
    menu: "personal-config",
    description: "建议填入千问的 API 地址、模型和 API Key。",
    tips: [
      "API地址可填 https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
      "模型可填 qwen3.7-plus",
      "保存后 API Key 会隐藏",
    ],
  },
  {
    key: "task_started",
    index: "5",
    title: "创建并运行任务",
    menu: "task-list",
    description: "选择平台账号和岗位模板创建任务，然后点击开始。",
    tips: [
      "左侧进入任务列表",
      "点击创建任务并选择账号",
      "任务开始成功后自动完成这一步",
    ],
  },
  {
    key: "subscription_viewed",
    index: "6",
    title: "查看订阅页面",
    menu: "subscription",
    description: "订阅页可以查看会员到期时间、套餐和支付记录。",
    tips: ["左侧进入订阅", "查看当前会员状态", "需要续期时可在这里选择套餐"],
  },
];

const doneCount = computed(
  () => ONBOARDING_STEPS.filter((step) => isDone(step)).length,
);
const activeKey = computed(
  () => cards.find((card) => !isDone(card.key))?.key || "",
);
const primaryDownload = computed(() => buildAgentDownloadOptions(props.config).primary);

/**
 * 判断指定步骤是否完成。
 * @param {string} key - 步骤键。
 * @returns {boolean} 是否完成。
 */
function isDone(key: string) {
  return Boolean(props.progress?.steps?.[key]);
}

/**
 * 打开本地程序下载链接。
 * @returns {void} 无返回值。
 */
function openDownload() {
  const url = primaryDownload.value.url;
  if (url) window.open(url, "_blank", "noopener,noreferrer");
}
</script>

<style scoped>
.onboarding-panel {
  min-height: 520px;
}
.progress {
  color: var(--accent);
  font-weight: 700;
}
.onboarding-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 12px;
}
.guide-card {
  position: relative;
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 16px;
  min-height: 220px;
  transition:
    transform 0.16s ease,
    border-color 0.16s ease;
}
.guide-card.active {
  transform: scale(1.04);
  border-color: var(--accent);
  z-index: 2;
}
.guide-card.done {
  opacity: 0.72;
}
.need {
  position: absolute;
  top: 8px;
  left: 8px;
  color: #fa0;
  font-size: 12px;
}
.guide-card.done .need {
  color: var(--accent);
}
.card-title {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  margin-top: 18px;
  margin-bottom: 8px;
}
.card-title span {
  color: var(--fg-dim);
}
.guide-card p,
.guide-card li {
  color: var(--fg-dim);
  line-height: 1.55;
}
.guide-card ol {
  padding-left: 18px;
}
</style>
