<!-- 本文件是后台控制台页面，负责组合新手引导、概览和本地程序面板。 -->
<template>
  <LocalAgentStatusCard :agent="app.agent" />
  <OnboardingGuide
    v-if="!app.onboardingProgress.value.completed"
    :progress="app.onboardingProgress.value"
    :config="app.onboardingConfig.value"
    @go="app.goMenu"
  />
  <GreetingDashboard :tasks="app.tasks" />
</template>

<script setup lang="ts">
import GreetingDashboard from "../components/GreetingDashboard.vue";
import LocalAgentStatusCard from "../components/LocalAgentStatusCard.vue";
import OnboardingGuide from "../components/OnboardingGuide.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
