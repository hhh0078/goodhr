<!-- 本文件是个人配置菜单页面，负责展示个人 AI 和操作偏好配置。 -->
<template>
  <PersonalConfig :config="app.personalConfig" />
</template>

<script setup lang="ts">
import PersonalConfig from "../components/PersonalConfig.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
