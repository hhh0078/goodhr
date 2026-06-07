<template>
  <RouterView v-if="isFullScreenRoute" />
  <div v-else class="app-layout">
    <aside class="menu-panel">
      <div class="menu-bar">
        <span class="bar-btn bar-close"></span
        ><span class="bar-btn bar-min"></span
        ><span class="bar-btn bar-max"></span
        ><span class="bar-title">GoodHR — menu</span>
      </div>
      <div class="menu-body">
        <div
          v-for="item in menuItems"
          :key="item.id"
          :class="['menu-item', { active: activeMenu === item.id }]"
          @click="goMenu(item.id)"
        >
          <span class="prompt">&gt;</span><span>{{ item.label }}</span>
        </div>
      </div>
      <div class="menu-footer">
        <div class="menu-item" @click="goContact">
          <span class="prompt">&gt;</span><span>联系我17607080935</span>
        </div>
        <div class="menu-item impert" @click="goInvitation">
          <span class="prompt">&gt;</span><span>不想付钱?点我</span>
        </div>
        <div class="menu-item" @click="user ? auth.logout() : requestLogin()">
          <span class="prompt">&gt;</span><span>{{ user ? "登出" : "登录" }}</span>
        </div>
      </div>
    </aside>
    <main class="main-area">
      <div class="top-bar">
        <span class="prompt">$</span><span class="cmd">百度搜GoodHR</span>
        <span class="spacer"></span>
        <button
          v-if="!user"
          class="top-info top-link error"
          @click="requestLogin"
        >
          请登录
        </button>
        <span v-else class="top-info">{{ user.email }}</span
        ><span class="sep">|</span>
        <span class="top-info">{{ currentRoleLabel }}</span
        ><span class="sep">|</span>
        <button
          :class="['top-info', 'top-link', subscriptionStatusColor]"
          @click="goSubscription"
        >
          {{ subscriptionText }}
        </button>
        <span class="sep">|</span>
        <button
          :class="['top-info', 'top-link', agentStatusColor]"
          @click="goMenu('agent-download')"
        >
          {{ agent.status.value }}
        </button>
        ><span class="sep">|</span>
        <button
          class="top-info top-link"
          @click="goMenu('agent-download')"
        >
          PID {{ agent.info?.value?.port || "---" }}
        </button>
        <span class="sep">|</span>
        <button class="top-info top-link" @click="openThemeSelector">
          主题
        </button>
      </div>
      <div class="content-area">
        <RouterView />
      </div>
    </main>
    <div v-if="visibleAnnouncements.length" class="announcement-mask">
      <section class="announcement-panel">
        <div class="panel-header">
          <h2>系统公告</h2>
          <button class="ghost" @click="closeAnnouncements">关闭</button>
        </div>
        <div class="announcement-list">
          <article
            v-for="item in visibleAnnouncements"
            :key="item.id"
            class="announcement-item"
          >
            <div class="announcement-title">
              <strong>{{ item.title || "公告" }}</strong>
              <span v-if="item.created_at">{{ item.created_at }}</span>
            </div>
            <p>{{ item.content }}</p>
          </article>
        </div>
      </section>
    </div>
    <ThemeSelector
      v-if="themeSelectorVisible"
      :themes="APP_THEMES"
      :model-value="selectedTheme"
      :allow-close="hasCachedTheme"
      @select="selectTheme"
      @confirm="confirmTheme"
      @close="closeThemeSelector"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { RouterView, useRoute, useRouter } from "vue-router";
import { getSystemAppConfig } from "./services/api/systemApi";
import { getSubscriptionStatus } from "./services/api/subscriptionApi";
import { getOnboardingStatus } from "./services/api/onboardingApi";
import { isLocalConsole } from "./services/localConsole";
import { useAuth } from "./composables/useAuth";
import { useAgent } from "./composables/useAgent";
import { usePositions } from "./composables/usePositions";
import { usePersonalConfig } from "./composables/usePersonalConfig";
import { useTasks } from "./composables/useTasks";
import { provideAppContext } from "./composables/useAppContext";
import { MENU_CACHE_KEY, menuRouteMap } from "./router";
import ThemeSelector from "./components/ThemeSelector.vue";
import {
  initOnboarding,
  markOnboardingStep,
  ONBOARDING_EVENT,
  readOnboardingProgress,
} from "./services/onboarding";
import {
  APP_THEMES,
  applyTheme,
  loadCachedTheme,
  saveTheme,
  type ThemeID,
} from "./services/theme";

const auth = useAuth();
const route = useRoute();
const router = useRouter();
const agent = useAgent();
const positions = usePositions();
const personalConfig = usePersonalConfig();
const { user } = auth;
const systemAppConfig = ref({
  local_agent_version: "5.0.0",
  announcements_enabled: false,
  announcements: [],
});
const dismissedSessionAnnouncements = ref<string[]>([]);
const subscription = ref<any>(null);
const onboardingProgress = ref<any>({ completed: true, steps: {} });
const onboardingConfig = ref<any>({
  local_agent_download_url: "",
  local_agent_download_url_mac: "",
  local_agent_download_url_windows: "",
  trial_days: 3,
});
const ANNOUNCEMENT_DISMISSED_KEY = "goodhr5_dismissed_announcements";
const cachedTheme = loadCachedTheme();
const selectedTheme = ref<ThemeID>(cachedTheme || APP_THEMES[0].id);
const hasCachedTheme = ref(Boolean(cachedTheme));
const themeSelectorVisible = ref(!cachedTheme);
applyTheme(selectedTheme.value);
const tasks = useTasks(agent.baseUrl, () => {
  goMenu("subscription");
  loadSubscriptionStatus();
}, resolvePositionSnapshot);
const isSuperAdmin = computed(() => user.value?.role === "super_admin");
const currentRoleLabel = computed(() => user.value?.role_label || "游客");
const isFullScreenRoute = computed(() => Boolean(route.meta.fullScreen));
const activeMenu = computed(() => String(route.meta.menuId || "agent"));
const menuItems = computed(() => {
  const items = [
    { id: "agent", label: "控制台" },
    { id: "account", label: "平台账号" },
    { id: "position", label: "岗位模板" },
    { id: "task-list", label: "任务列表" },
    { id: "resume-library", label: "简历库" },
    { id: "tenant", label: "团队管理" },
    { id: "invitation", label: "邀请" },
    { id: "personal-config", label: "个人配置" },
    { id: "subscription", label: "订阅" },
    { id: "help", label: "常见问题" },
    { id: "agent-download", label: "本地程序下载" },
  ];
  if (isLocalConsole()) {
    items.splice(8, 0, { id: "local-data", label: "本地数据" });
  }
  if (isSuperAdmin.value) {
    items.push({ id: "user-management", label: "用户管理" });
    items.push({ id: "activation-codes", label: "激活码管理" });
    items.push({ id: "payment-records", label: "支付记录" });
    items.push({ id: "system-config", label: "系统配置" });
  }
  return items;
});
provideAppContext({
  auth,
  agent,
  positions,
  personalConfig,
  tasks,
  user,
  systemAppConfig,
  onboardingProgress,
  onboardingConfig,
  goMenu,
  requestLogin,
  loadSubscriptionStatus,
});

/**
 * 根据岗位模板 ID 返回当前前端内存中的岗位快照。
 * @param {string} positionID - 岗位模板 ID。
 * @returns {any} 岗位模板快照。
 */
function resolvePositionSnapshot(positionID: string) {
  return positions.positions.value.find((item: any) => item.id === positionID) || {};
}

const agentStatusColor = computed(() => {
  const s = agent.status.value;
  if (s.includes("连接")) return "success";
  return "error";
});
const subscriptionStatusColor = computed(() =>
  subscriptionExpired.value
    ? "error"
    : subscription.value?.active
      ? "success"
      : "warn",
);
const subscriptionText = computed(() => {
  if (!subscription.value) return "会员 --";
  const memberType = subscription.value.member_type || "plus";
  if (subscriptionExpired.value) {
    return `${memberType} 已过期 ${formatShortDate(subscription.value.expires_at)}`;
  }
  return `${memberType} 到期 ${formatShortDate(subscription.value.expires_at)}`;
});
const subscriptionExpired = computed(() =>
  isSubscriptionExpired(subscription.value),
);
const visibleAnnouncements = computed(() => {
  if (!systemAppConfig.value?.announcements_enabled) return [];
  const dismissed = loadDismissedAnnouncements();
  const sessionDismissed = dismissedSessionAnnouncements.value;
  return (systemAppConfig.value.announcements || []).filter((item: any) => {
    const id = String(item?.id || "");
    if (!id || !item?.enabled || !String(item?.content || "").trim())
      return false;
    if (sessionDismissed.includes(id)) return false;
    if (item.once && dismissed.includes(id)) return false;
    return true;
  });
});

/**
 * 实时预览主题。
 * @param themeID - 用户选择的主题标识。
 * @returns void。
 */
function selectTheme(themeID: ThemeID) {
  selectedTheme.value = themeID;
  applyTheme(themeID);
}

/**
 * 确认并缓存当前主题。
 * @returns void。
 */
function confirmTheme() {
  saveTheme(selectedTheme.value);
  hasCachedTheme.value = true;
  themeSelectorVisible.value = false;
}

/**
 * 打开主题选择弹窗。
 * @returns void。
 */
function openThemeSelector() {
  themeSelectorVisible.value = true;
}

/**
 * 关闭主题选择弹窗。
 * @returns void。
 */
function closeThemeSelector() {
  if (!hasCachedTheme.value) return;
  themeSelectorVisible.value = false;
}

watch(user, async (u) => {
  if (u) {
    initOnboarding(u);
    refreshOnboardingProgress();
    await loadOnboardingStatus();
    await loadSystemAppConfig();
    await loadSubscriptionStatus();
    agent.detect();
    positions.load();
    personalConfig.load();
    tasks.load();
  } else {
    subscription.value = null;
  }
});
watch(activeMenu, (menu) => {
  localStorage.setItem(MENU_CACHE_KEY, menu);
  if (menu === "subscription" && !user.value) {
    requestLogin();
    return;
  }
  if (menu === "subscription") {
    markOnboardingStep("subscription_viewed");
  }
});
watch(
  [user, menuItems],
  () => {
    if (!menuItems.value.some((item) => item.id === activeMenu.value)) {
      goMenu("agent");
    }
  },
  { immediate: true },
);
onMounted(async () => {
  window.addEventListener(ONBOARDING_EVENT, refreshOnboardingProgress);
  refreshOnboardingProgress();
  await loadSystemAppConfig();
  detectLocalAgent();
  await auth.loadCurrentUser();
  if (auth.user.value) {
    initOnboarding(auth.user.value);
    refreshOnboardingProgress();
    await loadOnboardingStatus();
    await loadSubscriptionStatus();
    agent.detect();
    positions.load();
    personalConfig.load();
    tasks.load();
  }
});

/**
 * 跳转到联系我页面。
 * @returns {void} 无返回值。
 */
function goContact() {
  window.open("https://goodhr.58it.cn", "_blank");
}

/**
 * 跳转到订阅页面。
 * @returns {void} 无返回值。
 */
function goSubscription() {
  if (!user.value) {
    requestLogin();
    return;
  }
  void router.push({ name: "subscription" });
}

function goInvitation() {
  if (!user.value) {
    requestLogin();
    return;
  }
  void router.push({ name: "invitations" });
}

/**
 * 跳转到独立登录页面。
 * @returns {void} 无返回值。
 */
function requestLogin() {
  if (route.name === "login") return;
  const redirect = route.fullPath && route.fullPath !== "/" ? route.fullPath : "";
  void router.push({
    name: "login",
    query: redirect ? { redirect } : {},
  });
}

/**
 * 读取服务端教学状态和教学配置。
 * @returns {Promise<void>} 无返回值。
 */
async function loadOnboardingStatus() {
  try {
    const data = await getOnboardingStatus();
    onboardingConfig.value = data.config || onboardingConfig.value;
    if (data.onboarding?.completed) {
      onboardingProgress.value = {
        ...onboardingProgress.value,
        completed: true,
      };
    }
  } catch {
    onboardingConfig.value = {
      local_agent_download_url: "",
      local_agent_download_url_mac: "",
      local_agent_download_url_windows: "",
      trial_days: 3,
    };
  }
}

/**
 * 从本地缓存刷新教学进度。
 * @returns {void} 无返回值。
 */
function refreshOnboardingProgress() {
  onboardingProgress.value = readOnboardingProgress();
}

/**
 * 跳转到指定菜单页面。
 * @param {string} menu - 菜单 ID。
 * @returns {void} 无返回值。
 */
function goMenu(menu: string) {
  if (menu === "subscription" && !user.value) {
    requestLogin();
    return;
  }
  const routeName = menuRouteMap[menu] || "dashboard";
  void router.push({ name: routeName });
  if (menu === "subscription") {
    markOnboardingStep("subscription_viewed");
  }
}

/**
 * 读取前端公共系统配置。
 * @returns {Promise<void>} 无返回值。
 */
async function loadSystemAppConfig() {
  try {
    systemAppConfig.value = {
      ...systemAppConfig.value,
      ...(await getSystemAppConfig()),
    };

    //加入缓存
    const SYSTEM_APP_CONFIG_CACHE_KEY = "system_app_config";
    localStorage.setItem(
      SYSTEM_APP_CONFIG_CACHE_KEY,
      JSON.stringify(systemAppConfig.value),
    );
  } catch {
    systemAppConfig.value = {
      local_agent_version: "5.0.0",
      announcements_enabled: false,
      announcements: [],
    };
  }
}

/**
 * 读取已关闭的一次性公告 ID。
 * @returns {string[]} 公告 ID 列表。
 */
function loadDismissedAnnouncements() {
  try {
    const parsed = JSON.parse(
      localStorage.getItem(ANNOUNCEMENT_DISMISSED_KEY) || "[]",
    );
    return Array.isArray(parsed) ? parsed.map(String) : [];
  } catch {
    return [];
  }
}

/**
 * 保存已关闭的一次性公告 ID。
 * @param {string[]} ids - 公告 ID 列表。
 * @returns {void} 无返回值。
 */
function saveDismissedAnnouncements(ids: string[]) {
  localStorage.setItem(
    ANNOUNCEMENT_DISMISSED_KEY,
    JSON.stringify(Array.from(new Set(ids))),
  );
}

/**
 * 关闭当前可见公告。
 * @returns {void} 无返回值。
 */
function closeAnnouncements() {
  const current = visibleAnnouncements.value;
  const onceIDs = current
    .filter((item: any) => item.once)
    .map((item: any) => String(item.id));
  if (onceIDs.length) {
    saveDismissedAnnouncements([...loadDismissedAnnouncements(), ...onceIDs]);
  }
  dismissedSessionAnnouncements.value = [
    ...dismissedSessionAnnouncements.value,
    ...current.map((item: any) => String(item.id)),
  ];
}

/**
 * 读取当前用户订阅状态。
 * @returns {Promise<void>} 无返回值。
 */
async function loadSubscriptionStatus() {
  if (!auth.token.value) {
    subscription.value = null;
    return;
  }
  try {
    subscription.value = await getSubscriptionStatus();
  } catch {
    subscription.value = null;
  }
}

/**
 * 格式化顶部订阅到期日期。
 * @param {string} value - ISO日期字符串。
 * @returns {string} 短日期文案。
 */
function formatShortDate(value: string) {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return date.toLocaleDateString();
}

/**
 * 判断会员是否已经过期。
 * @param {any} value - 订阅状态数据。
 * @returns {boolean} 是否已过期。
 */
function isSubscriptionExpired(value: any) {
  if (!value) return false;
  if (value.active === false) return true;
  const expiresAt = new Date(value.expires_at);
  if (Number.isNaN(expiresAt.getTime())) return false;
  return Date.now() >= expiresAt.getTime();
}

const detectLocalAgent = () => {
  agent.detect();
  setInterval(() => {
    agent.detect();
  }, 10000);
};
</script>

<style scoped>
.app-layout {
  display: flex;
  height: 100vh;
  gap: 12px;
  padding: 12px;
  padding-top: 0;
}
.menu-panel {
  width: 200px;
  min-width: 200px;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  background: var(--bg-panel);
  margin-top: 12px;
}
.menu-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-input);
}
.bar-title {
  flex: 1;
  text-align: center;
  font-size: 11px;
  color: var(--fg-dim);
  margin-right: 24px;
}
.bar-btn {
  width: 10px;
  height: 10px;
  display: inline-block;
}
.bar-close {
  background: var(--window-close);
}
.bar-min {
  background: var(--window-min);
}
.bar-max {
  background: var(--window-max);
  opacity: 0.5;
}
.menu-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}
.menu-footer {
  border-top: 1px solid var(--border);
  padding: 4px 0;
}
.menu-item {
  padding: 8px 12px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg-dim);
  border-left: 2px solid transparent;
}
.menu-item .prompt {
  color: var(--fg-muted);
  font-size: 12px;
}

.impert {
  color: var(--accent) !important;
}
.menu-item:hover {
  color: var(--accent);
  background: var(--accent-soft);
}
.menu-item.active {
  color: var(--accent);
  border-left-color: var(--accent);
  background: var(--accent-soft);
}
.menu-item.active .prompt {
  color: var(--accent);
}
.main-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  margin-top: 12px;
}
.top-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border: 1px solid var(--border);
  background: var(--bg-panel);
  font-size: 13px;
  margin-bottom: 12px;
}
.top-bar .prompt {
  color: var(--accent);
}
.top-bar .cmd {
  color: var(--success);
}
.top-bar .spacer {
  flex: 1;
}
.top-bar .sep {
  color: var(--fg-muted);
}
.top-info {
  color: var(--fg-dim);
  font-size: 12px;
}
.top-link {
  border: 0;
  background: transparent;
  padding: 0;
  cursor: pointer;
  font-family: inherit;
}
.top-link:hover {
  color: var(--accent);
}
.top-info.success {
  color: var(--success);
}
.top-info.warn {
  color: var(--fg-warn);
}
.top-info.error {
  color: var(--fg-error);
}
.content-area {
  flex: 1;
  overflow-y: auto;
}
.announcement-mask {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 72px 16px 16px;
  background: rgba(0, 0, 0, 0.72);
  z-index: 20;
}
.announcement-panel {
  width: min(620px, 100%);
  max-height: 72vh;
  overflow-y: auto;
  border: 1px solid var(--border);
  background: var(--bg-panel);
  padding: 12px;
}
.announcement-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.announcement-item {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 10px;
}
.announcement-title {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}
.announcement-title span {
  color: var(--fg-dim);
  font-size: 12px;
  white-space: nowrap;
}
.announcement-item p {
  color: var(--fg-dim);
  white-space: pre-wrap;
}
</style>
