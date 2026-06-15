<!-- 本文件负责在控制台首页展示平台账号快捷入口，并支持直接打开账号浏览器。 -->
<template>
  <section class="panel account-quick-launch">
    <div class="panel-header">
      <div>
        <h2>平台账号快捷入口</h2>
        <p class="hint">在这里可以直接打开已有账号，方便开始任务前确认登录状态。</p>
      </div>
      <button class="ghost" :disabled="loading" @click="load">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <p v-if="message" :class="messageType">{{ message }}</p>
    <p v-if="!loading && accounts.length === 0" class="hint">暂无平台账号。</p>

    <div v-if="accounts.length > 0" class="quick-account-grid">
      <article v-for="account in accounts" :key="account.id" class="quick-account">
        <div>
          <strong>{{ account.display_name || "未命名账号" }}</strong>
          <p class="hint">
            {{ platformLabel(account.platform_id) }} · {{ accountStatusLabel(account.status) }}
          </p>
        </div>
        <button
          class="ghost"
          :disabled="loading || openingId === account.id"
          @click="openAccount(account)"
        >
          {{ openingId === account.id ? "打开中..." : "打开" }}
        </button>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  listPlatformAccounts,
  listPlatformConfigs,
} from "../services/api/accountApi";
import { openPage, startBrowser } from "../services/localAgentApi";
import { isLocalConsole, localAgentBase } from "../services/localConsole";
import {
  pickAuthEntryURL,
  pickPlatformAuthConfig,
  type PlatformAuthConfig,
} from "../services/platformLoginFlow";

const props = defineProps<{ agent: any }>();
const accounts = ref<any[]>([]);
const platformConfigs = ref<any[]>([]);
const loading = ref(false);
const openingId = ref("");
const message = ref("");
const messageType = ref("success");
const effectiveAgentBaseUrl = computed(() => {
  if (isLocalConsole()) return localAgentBase();
  return props.agent?.baseUrl?.value || "";
});

/**
 * 加载平台账号和平台配置。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  message.value = "";
  try {
    const [configs, list] = await Promise.all([
      listPlatformConfigs(),
      listPlatformAccounts(),
    ]);
    platformConfigs.value = configs || [];
    accounts.value = list || [];
  } catch (error: any) {
    message.value = error?.message || "平台账号加载失败";
    messageType.value = "error";
  } finally {
    loading.value = false;
  }
}

/**
 * 直接打开指定平台账号。
 * @param {any} account - 平台账号记录。
 * @returns {Promise<void>} 无返回值。
 */
async function openAccount(account: any) {
  if (!account?.id) return;
  if (!effectiveAgentBaseUrl.value) {
    message.value = "未检测到本地程序";
    messageType.value = "error";
    return;
  }
  openingId.value = account.id;
  message.value = "";
  try {
    const authConfig = platformAuthConfig(account.platform_id);
    const targetURL = pickAuthEntryURL(authConfig);
    if (!targetURL) throw new Error("平台配置缺少入口地址");
    const payload = {
      url: targetURL,
      persistent: true,
      user_data_dir: account.local_profile_id || account.id,
      headless: false,
      humanize: true,
    };
    await startBrowser(effectiveAgentBaseUrl.value, payload);
    await openPage(effectiveAgentBaseUrl.value, payload);
    message.value = `已打开账号：${account.display_name || account.id}`;
    messageType.value = "success";
  } catch (error: any) {
    message.value = error?.message || "打开账号失败";
    messageType.value = "error";
  } finally {
    openingId.value = "";
  }
}

/**
 * 返回平台登录检测配置。
 * @param {string} platformId - 平台 ID。
 * @returns {PlatformAuthConfig} 平台登录配置。
 */
function platformAuthConfig(platformId: string): PlatformAuthConfig {
  return pickPlatformAuthConfig(platformConfigs.value, platformId);
}

/**
 * 返回平台中文名称。
 * @param {string} platformId - 平台 ID。
 * @returns {string} 平台中文名称。
 */
function platformLabel(platformId: string) {
  const labels: Record<string, string> = {
    boss: "Boss直聘",
    zhaopin: "智联招聘",
    liepin: "猎聘",
  };
  return labels[String(platformId || "")] || platformId || "未知平台";
}

/**
 * 返回账号状态中文。
 * @param {string} status - 账号状态。
 * @returns {string} 状态中文。
 */
function accountStatusLabel(status: string) {
  const key = String(status || "").toLowerCase();
  if (key === "available") return "已登录";
  if (key === "expired") return "已过期";
  if (key === "in_use") return "使用中";
  return "未登录";
}

onMounted(load);
</script>

<style scoped>
.quick-account-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}
.quick-account {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 10px 12px;
}
.quick-account strong {
  color: var(--fg);
}
.quick-account .hint {
  margin: 4px 0 0;
}
</style>
