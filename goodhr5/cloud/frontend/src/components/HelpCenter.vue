<!-- 本文件负责展示帮助中心卡片和系统 AI 助手对话框。 -->
<template>
  <section class="panel help-center">
    <div class="panel-header">
      <div>
        <h2>常见问题</h2>
        <p class="help-subtitle">
          常见功能、参数说明和报错处理都可以在这里查。
        </p>
      </div>
      <button class="ghost" :disabled="loadingGuide" @click="loadGuide">
        {{ loadingGuide ? "读取中..." : "刷新指南" }}
      </button>
    </div>

    <div class="help-card-grid">
      <article
        v-for="card in guideCards"
        :key="card.id || card.title"
        class="help-card"
        :class="{ active: activeCard?.id === card.id }"
        @click="selectCard(card)"
      >
        <span class="card-index">{{ cardIndex(card) }}</span>
        <h3>{{ card.title }}</h3>
        <p>{{ card.summary }}</p>
      </article>
    </div>

    <article v-if="activeCard" class="guide-detail">
      <div class="guide-detail-title">
        <span class="prompt">&gt;</span>
        <strong>{{ activeCard.title }}</strong>
      </div>
      <p>{{ activeCard.content }}</p>
      <button class="ghost" @click="askCard(activeCard)">问 AI 这个问题</button>
    </article>

    <div class="assistant-box">
      <div class="assistant-header">
        <div>
          <h3>AI 帮助助手</h3>
          <p class="hint">
            我的主人很忙，有问题可以先问我，如果我回答不了 再问主人。
          </p>
        </div>
        <button class="ghost" :disabled="chatLoading" @click="clearChat">
          清空对话
        </button>
      </div>

      <div ref="chatBody" class="chat-body">
        <div v-if="messages.length === 0" class="empty-chat">
          可以问：本地程序连不上怎么办、任务参数是什么意思、cookie
          解密失败怎么处理。
        </div>
        <div
          v-for="(message, index) in messages"
          :key="`${message.role}-${index}`"
          :class="['chat-message', message.role]"
        >
          <div class="chat-bubble">
            <span class="chat-role">{{
              message.role === "user" ? "我" : "助手"
            }}</span>
            <p>{{ message.content }}</p>
          </div>
        </div>
      </div>

      <form class="chat-form" @submit.prevent="sendMessage">
        <textarea
          v-model="input"
          :disabled="chatLoading"
          rows="3"
          placeholder="输入你遇到的问题，比如：为什么提示当前机器无可用 cookie 密钥？"
          @keydown.enter.exact.prevent="sendMessage"
        ></textarea>
        <button :disabled="chatLoading || !input.trim()">
          {{ chatLoading ? "回答中..." : "发送" }}
        </button>
      </form>
      <p v-if="error" class="error">{{ error }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { getSystemGuide, streamHelpChat } from "../services/api/helpApi";

const props = defineProps({
  userEmail: String,
});

type HelpCard = {
  id?: string;
  title?: string;
  summary?: string;
  content?: string;
};

type ChatMessage = {
  role: "user" | "assistant";
  content: string;
};

const guide = ref<any>({});
const loadingGuide = ref(false);
const activeCard = ref<HelpCard | null>(null);
const messages = ref<ChatMessage[]>([]);
const input = ref("");
const chatLoading = ref(false);
const error = ref("");
const chatBody = ref<HTMLElement | null>(null);

const guideCards = computed<HelpCard[]>(() => {
  const cards = guide.value?.cards;
  return Array.isArray(cards) ? cards : [];
});

/**
 * 生成当前用户的帮助对话缓存键。
 * @returns {string} localStorage 缓存键。
 */
function chatCacheKey() {
  return `goodhr5_help_chat_${props.userEmail || "guest"}`;
}

/**
 * 读取系统指南和本地聊天缓存。
 * @returns {Promise<void>} 无返回值。
 */
async function initHelpCenter() {
  loadCachedChat();
  await loadGuide();
}

/**
 * 读取帮助中心系统指南。
 * @returns {Promise<void>} 无返回值。
 */
async function loadGuide() {
  loadingGuide.value = true;
  error.value = "";
  try {
    guide.value = await getSystemGuide();
    if (!activeCard.value && guideCards.value.length) {
      activeCard.value = guideCards.value[0];
    }
  } catch (e: any) {
    error.value = e?.message || "读取系统指南失败";
  } finally {
    loadingGuide.value = false;
  }
}

/**
 * 选中一个帮助卡片。
 * @param {HelpCard} card - 用户点击的卡片。
 * @returns {void} 无返回值。
 */
function selectCard(card: HelpCard) {
  activeCard.value = card;
}

/**
 * 根据卡片内容向 AI 提问。
 * @param {HelpCard} card - 当前帮助卡片。
 * @returns {void} 无返回值。
 */
function askCard(card: HelpCard) {
  input.value = `请解释一下：${card.title || ""}。${card.summary || ""}`;
  sendMessage();
}

/**
 * 返回卡片序号文案。
 * @param {HelpCard} card - 当前卡片。
 * @returns {string} 两位数序号。
 */
function cardIndex(card: HelpCard) {
  const index = guideCards.value.findIndex((item) => item === card);
  return String(index + 1).padStart(2, "0");
}

/**
 * 发送用户问题并读取流式回答。
 * @returns {Promise<void>} 无返回值。
 */
async function sendMessage() {
  const content = input.value.trim();
  if (!content || chatLoading.value) return;
  error.value = "";
  input.value = "";
  messages.value.push({ role: "user", content });
  messages.value.push({ role: "assistant", content: "" });
  trimMessages();
  saveCachedChat();
  await scrollToBottom();

  const assistantIndex = messages.value.length - 1;
  chatLoading.value = true;
  try {
    await streamHelpChat(messages.value.slice(0, assistantIndex), (chunk) => {
      messages.value[assistantIndex].content += chunk;
      trimMessages();
      saveCachedChat();
      scrollToBottom();
    });
  } catch (e: any) {
    messages.value[assistantIndex].content =
      "我这边暂时没法连接帮助助手，请检查超级管理员 AI 配置是否完整。";
    error.value = e?.message || "帮助助手请求失败";
  } finally {
    chatLoading.value = false;
    trimMessages();
    saveCachedChat();
    await scrollToBottom();
  }
}

/**
 * 限制聊天缓存最多保留 20 轮。
 * @returns {void} 无返回值。
 */
function trimMessages() {
  if (messages.value.length > 40) {
    messages.value = messages.value.slice(messages.value.length - 40);
  }
}

/**
 * 保存聊天记录到浏览器本地缓存。
 * @returns {void} 无返回值。
 */
function saveCachedChat() {
  localStorage.setItem(chatCacheKey(), JSON.stringify(messages.value));
}

/**
 * 从浏览器本地缓存读取聊天记录。
 * @returns {void} 无返回值。
 */
function loadCachedChat() {
  try {
    const parsed = JSON.parse(localStorage.getItem(chatCacheKey()) || "[]");
    messages.value = Array.isArray(parsed)
      ? parsed
          .filter((item) => item?.role && item?.content != null)
          .map((item) => ({
            role: item.role === "assistant" ? "assistant" : "user",
            content: String(item.content || ""),
          }))
      : [];
    trimMessages();
  } catch {
    messages.value = [];
  }
}

/**
 * 清空当前用户本地聊天缓存。
 * @returns {void} 无返回值。
 */
function clearChat() {
  messages.value = [];
  localStorage.removeItem(chatCacheKey());
}

/**
 * 将聊天区域滚动到底部。
 * @returns {Promise<void>} 无返回值。
 */
async function scrollToBottom() {
  await nextTick();
  if (!chatBody.value) return;
  chatBody.value.scrollTop = chatBody.value.scrollHeight;
}

onMounted(initHelpCenter);
</script>

<style scoped>
.help-center {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.help-subtitle {
  color: var(--fg-dim);
  font-size: 12px;
  margin-top: 4px;
}

.help-card-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.help-card {
  min-height: 132px;
  border: 1px solid var(--border);
  background: var(--bg);
  padding: 10px;
  cursor: pointer;
}

.help-card:hover,
.help-card.active {
  border-color: var(--fg);
}

.card-index {
  color: var(--fg);
  font-size: 12px;
}

.help-card h3 {
  margin: 8px 0 6px;
}

.help-card p,
.guide-detail p {
  color: var(--fg-dim);
  font-size: 13px;
  line-height: 1.6;
}

.guide-detail {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
}

.guide-detail-title {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}

.guide-detail button {
  margin-top: 10px;
}

.assistant-box {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
}

.assistant-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--border);
  padding-bottom: 8px;
  margin-bottom: 10px;
}

.chat-body {
  height: 340px;
  overflow-y: auto;
  border: 1px solid #1a1a1a;
  background: var(--bg);
  padding: 10px;
}

.empty-chat {
  color: var(--fg-dim);
  font-size: 13px;
}

.chat-message {
  display: flex;
  padding: 8px 0;
}

.chat-message.assistant {
  justify-content: flex-start;
}

.chat-message.user {
  justify-content: flex-end;
}

.chat-bubble {
  max-width: min(78%, 620px);
  border: 1px solid #242424;
  background: var(--bg-input);
  padding: 8px 10px;
}

.chat-message.user .chat-bubble {
  border-color: var(--success);
  background: #071007;
}

.chat-role {
  display: block;
  color: var(--fg);
  font-size: 12px;
  margin-bottom: 4px;
}

.chat-bubble p {
  color: var(--fg-dim);
  white-space: pre-wrap;
  word-break: break-word;
}

.chat-message.user .chat-bubble p {
  color: var(--fg);
}

.chat-form {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 8px;
  margin-top: 10px;
  align-items: stretch;
}

.chat-form button {
  min-width: 92px;
}

@media (max-width: 900px) {
  .help-card-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .help-card-grid,
  .chat-form {
    grid-template-columns: 1fr;
  }
}
</style>
