<template>
  <LoginForm v-if="!user" :auth="auth" />
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
          @click="activeMenu = item.id"
        >
          <span class="prompt">&gt;</span><span>{{ item.label }}</span>
        </div>
      </div>
      <div class="menu-footer">
        <div class="menu-item" @click="goContact">
          <span class="prompt">&gt;</span><span>联系我17607080935</span>
        </div>
        <div class="menu-item" @click="auth.logout">
          <span class="prompt">&gt;</span><span>登出</span>
        </div>
      </div>
    </aside>
    <main class="main-area">
      <div class="top-bar">
        <span class="prompt">$</span><span class="cmd">百度搜GoodHR</span>
        <span class="spacer"></span>
        <span class="top-info">{{ user?.email }}</span
        ><span class="sep">|</span>
        <span class="top-info">{{ currentRoleLabel }}</span
        ><span class="sep">|</span>
        <span :class="['top-info', subscriptionStatusColor]">{{
          subscriptionText
        }}</span
        ><span class="sep">|</span>
        <span :class="['top-info', agentStatusColor]">{{
          agent.status.value
        }}</span
        ><span class="sep">|</span>
        <span class="top-info">PID {{ agent.info?.value?.port || "---" }}</span>
      </div>
      <div class="content-area">
        <template v-if="activeMenu === 'agent'">
          <OnboardingGuide
            v-if="!onboardingProgress.completed"
            :progress="onboardingProgress"
            :config="onboardingConfig"
            @go="goOnboardingMenu"
          />
          <GreetingDashboard :tasks="tasks" />
          <AgentPanel
            :agent="agent"
            :app-config="systemAppConfig"
            :user="user"
            :token="auth.token"
          />
        </template>
        <TenantManager
          v-else-if="activeMenu === 'tenant'"
          :token="auth.token.value"
          :user-email="user?.email"
        />

        <AccountManager
          v-else-if="activeMenu === 'account'"
          :token="auth.token.value"
          :agent-base-url="agent.baseUrl.value"
        />
        <PositionManager
          v-else-if="activeMenu === 'position'"
          :positions="positions"
        />
        <PersonalConfig
          v-else-if="activeMenu === 'personal-config'"
          :config="personalConfig"
        />
        <SubscriptionPanel v-else-if="activeMenu === 'subscription'" />
        <InvitationPanel v-else-if="activeMenu === 'invitation'" />
        <HelpCenter
          v-else-if="activeMenu === 'help'"
          :user-email="user?.email"
        />

        <TaskList
          v-else-if="activeMenu === 'task-list'"
          :tasks="tasks"
          :positions="positions.positions.value"
          :token="auth.token.value"
          :agent="agent"
        />
        <PlatformConfigViewer
          v-else-if="activeMenu === 'system-config' && isSuperAdmin"
        />
        <PaymentRecords
          v-else-if="activeMenu === 'payment-records' && isSuperAdmin"
        />
        <ActivationCodeManager
          v-else-if="activeMenu === 'activation-codes' && isSuperAdmin"
        />
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { getSystemAppConfig } from "./services/cloudApi";
import { useAuth } from "./composables/useAuth";
import { useAgent } from "./composables/useAgent";
import { usePositions } from "./composables/usePositions";
import { usePersonalConfig } from "./composables/usePersonalConfig";
import { useTasks } from "./composables/useTasks";
import LoginForm from "./components/LoginForm.vue";
import AgentPanel from "./components/AgentPanel.vue";
import TenantManager from "./components/TenantManager.vue";

import AccountManager from "./components/AccountManager.vue";
import PlatformConfigViewer from "./components/PlatformConfigViewer.vue";
import PaymentRecords from "./components/PaymentRecords.vue";
import PositionManager from "./components/PositionManager.vue";
import PersonalConfig from "./components/PersonalConfig.vue";
import SubscriptionPanel from "./components/SubscriptionPanel.vue";
import InvitationPanel from "./components/InvitationPanel.vue";
import OnboardingGuide from "./components/OnboardingGuide.vue";
import GreetingDashboard from "./components/GreetingDashboard.vue";
import HelpCenter from "./components/HelpCenter.vue";
import ActivationCodeManager from "./components/ActivationCodeManager.vue";
import TaskList from "./components/TaskList.vue";
import {
  getOnboardingStatus,
  getSubscriptionStatus,
} from "./services/cloudApi";
import {
  initOnboarding,
  markOnboardingStep,
  ONBOARDING_EVENT,
  readOnboardingProgress,
} from "./services/onboarding";

const auth = useAuth();
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
  trial_days: 3,
});
const ACTIVE_MENU_KEY = "goodhr5_active_menu";
const ANNOUNCEMENT_DISMISSED_KEY = "goodhr5_dismissed_announcements";
const savedMenu = localStorage.getItem(ACTIVE_MENU_KEY);
const activeMenu = ref(
  savedMenu === "platform-config" ? "system-config" : savedMenu || "agent",
);
const tasks = useTasks(agent.baseUrl, () => {
  activeMenu.value = "subscription";
  loadSubscriptionStatus();
});
const isSuperAdmin = computed(() => user.value?.role === "super_admin");
const currentRoleLabel = computed(() => user.value?.role_label || "成员");
const menuItems = computed(() => {
  const items = [
    { id: "agent", label: "控制台" },
    { id: "tenant", label: "团队管理" },
    { id: "account", label: "平台账号" },
    { id: "position", label: "岗位模板" },
    { id: "personal-config", label: "个人配置" },
    { id: "subscription", label: "订阅" },
    { id: "invitation", label: "邀请" },
    { id: "task-list", label: "任务列表" },
    { id: "help", label: "帮助中心" },
  ];
  if (isSuperAdmin.value) {
    items.push({ id: "system-config", label: "系统配置" });
    items.push({ id: "payment-records", label: "支付记录" });
    items.push({ id: "activation-codes", label: "激活码管理" });
  }
  return items;
});
const agentStatusColor = computed(() => {
  const s = agent.status.value;
  if (s.includes("连接")) return "success";
  if (s.includes("检测中")) return "warn";
  return "error";
});
const subscriptionStatusColor = computed(() =>
  subscription.value?.active ? "success" : "warn",
);
const subscriptionText = computed(() => {
  if (!subscription.value) return "会员 --";
  const memberType = subscription.value.member_type || "plus";
  return `${memberType} 到期 ${formatShortDate(subscription.value.expires_at)}`;
});
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
watch(user, async (u) => {
  if (u) {
    initOnboarding(u);
    refreshOnboardingProgress();
    await loadOnboardingStatus();
    await loadSystemAppConfig();
    await loadSubscriptionStatus();
    agent.detect(u, auth.token.value);
    positions.load();
    personalConfig.load();
    tasks.load();
  }
});
watch(activeMenu, (menu) => {
  localStorage.setItem(ACTIVE_MENU_KEY, menu);
  if (menu === "subscription") {
    markOnboardingStep("subscription_viewed");
  }
});
watch(
  [user, menuItems],
  () => {
    if (!menuItems.value.some((item) => item.id === activeMenu.value)) {
      activeMenu.value = "agent";
    }
  },
  { immediate: true },
);
onMounted(async () => {
  window.addEventListener(ONBOARDING_EVENT, refreshOnboardingProgress);
  await auth.loadCurrentUser();
  if (auth.user.value) {
    initOnboarding(auth.user.value);
    refreshOnboardingProgress();
    await loadOnboardingStatus();
    await loadSystemAppConfig();
    await loadSubscriptionStatus();
    agent.detect(auth.user.value, auth.token.value);
    detectLocalAgent();
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
    onboardingConfig.value = { local_agent_download_url: "", trial_days: 3 };
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
 * 跳转到教学卡片对应菜单。
 * @param {string} menu - 菜单 ID。
 * @returns {void} 无返回值。
 */
function goOnboardingMenu(menu: string) {
  activeMenu.value = menu;
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

const detectLocalAgent = () => {
  //3秒运行一次
  setInterval(() => {
    agent.detect(auth.user.value, auth.token.value);
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
  border: 1px solid #333;
  background: #050505;
  margin-top: 12px;
}
.menu-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-bottom: 1px solid #333;
  background: #0d0d0d;
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
  background: #e33;
}
.bar-min {
  background: #e83;
}
.bar-max {
  background: #3a3;
  opacity: 0.5;
}
.menu-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}
.menu-footer {
  border-top: 1px solid #333;
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
  color: var(--border);
  font-size: 12px;
}
.menu-item:hover {
  color: #0f0;
  background: #0a0a0a;
}
.menu-item.active {
  color: #0f0;
  border-left-color: #0f0;
  background: #0d0d0d;
}
.menu-item.active .prompt {
  color: #0f0;
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
  border: 1px solid #333;
  background: #0d0d0d;
  font-size: 13px;
  margin-bottom: 12px;
}
.top-bar .prompt {
  color: #0f0;
}
.top-bar .cmd {
  color: #0a0;
}
.top-bar .spacer {
  flex: 1;
}
.top-bar .sep {
  /* color: var(--border); */
  color: #fff;
}
.top-info {
  color: var(--fg-dim);
  font-size: 12px;
}
.top-info.success {
  color: #0f0;
}
.top-info.warn {
  color: #fa0;
}
.top-info.error {
  color: #f33;
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
  border: 1px solid #333;
  background: #0d0d0d;
  padding: 12px;
}
.announcement-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.announcement-item {
  border: 1px solid #333;
  background: #050505;
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
