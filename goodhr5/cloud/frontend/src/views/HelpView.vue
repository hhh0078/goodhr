<!-- 本文件是帮助中心菜单页面，负责展示系统指南和帮助助手。 -->
<template>
  <HelpCenter :user-email="app.user.value?.email" />
</template>

<script setup lang="ts">
import HelpCenter from "../components/HelpCenter.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
