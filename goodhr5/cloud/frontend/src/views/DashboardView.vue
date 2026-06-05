<!-- 本文件是后台控制台页面，负责组合新手引导、概览和本地程序面板。 -->
<template>
  <OnboardingGuide
    v-if="!app.onboardingProgress.value.completed"
    :progress="app.onboardingProgress.value"
    :config="app.onboardingConfig.value"
    @go="app.goMenu"
  />
  <GreetingDashboard :tasks="app.tasks" />
  <AgentPanel
    :agent="app.agent"
    :app-config="app.systemAppConfig.value"
  />
</template>

<script setup lang="ts">
import AgentPanel from "../components/AgentPanel.vue";
import GreetingDashboard from "../components/GreetingDashboard.vue";
import OnboardingGuide from "../components/OnboardingGuide.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
