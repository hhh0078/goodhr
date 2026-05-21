<template>
  <LoginForm v-if="!user" :auth="auth" />
  <div v-else class="app-layout">
    <aside class="menu-panel">
      <div class="menu-bar">
        <span class="bar-btn bar-close"></span
        ><span class="bar-btn bar-min"></span
        ><span class="bar-btn bar-max"></span
        ><span class="bar-title">goodhr5 — menu</span>
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
        <div class="menu-item" @click="auth.logout">
          <span class="prompt">&gt;</span><span>登出</span>
        </div>
      </div>
    </aside>
    <main class="main-area">
      <div class="top-bar">
        <span class="prompt">$</span><span class="cmd">goodhr@cloud:~$</span>
        <span class="spacer"></span>
        <span class="top-info">{{ user?.email }}</span
        ><span class="sep">|</span>
        <span :class="['top-info', agentStatusColor]">{{
          agent.status.value
        }}</span
        ><span class="sep">|</span>
        <span class="top-info">PID {{ agent.info?.value?.port || "---" }}</span>
      </div>
      <div class="content-area">
        <AgentPanel
          v-if="activeMenu === 'agent'"
          :agent="agent"
          :user="user"
          :token="auth.token"
        />
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

        <TaskList
          v-else-if="activeMenu === 'task-list'"
          :tasks="tasks"
          :positions="positions.positions.value"
          :token="auth.token.value"
          :agent="agent"
        />
        <PlatformConfigViewer
          v-else-if="activeMenu === 'platform-config' && isAdmin"
        />
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
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
import PositionManager from "./components/PositionManager.vue";
import PersonalConfig from "./components/PersonalConfig.vue";
import TaskList from "./components/TaskList.vue";

const auth = useAuth();
const agent = useAgent();
const positions = usePositions();
const personalConfig = usePersonalConfig();
const tasks = useTasks(agent.baseUrl);
const { user } = auth;
const ACTIVE_MENU_KEY = "goodhr5_active_menu";
const activeMenu = ref(localStorage.getItem(ACTIVE_MENU_KEY) || "agent");
const isAdmin = computed(() => user.value?.role === "admin");
const menuItems = computed(() => {
  const items = [
    { id: "agent", label: "本地 Agent" },
    { id: "tenant", label: "团队管理" },
    { id: "account", label: "平台账号" },
    { id: "position", label: "岗位模板" },
    { id: "personal-config", label: "个人配置" },
    { id: "task-list", label: "任务列表" },
  ];
  if (isAdmin.value) {
    items.push({ id: "platform-config", label: "平台配置" });
  }
  return items;
});
const agentStatusColor = computed(() => {
  const s = agent.status.value;
  if (s.includes("连接")) return "success";
  if (s.includes("检测中")) return "warn";
  return "error";
});
watch(user, async (u) => {
  if (u) {
    agent.detect(u, auth.token.value);
    positions.load();
    personalConfig.load();
    tasks.load();
  }
});
watch(activeMenu, (menu) => {
  localStorage.setItem(ACTIVE_MENU_KEY, menu);
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
  await auth.loadCurrentUser();
  if (auth.user.value) {
    agent.detect(auth.user.value, auth.token.value);
    detectLocalAgent();
    positions.load();
    personalConfig.load();
    tasks.load();
  }
});

const detectLocalAgent = () => {
  //3秒运行一次
  setInterval(() => {
    agent.detect(auth.user.value, auth.token.value);
  }, 3000);
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
  color: var(--border);
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
</style>
