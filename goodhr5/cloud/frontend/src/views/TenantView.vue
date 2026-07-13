<!-- 本文件是团队管理菜单页面，负责展示团队成员管理功能。 -->
<template>
  <TenantManager
    :token="app.auth.token.value"
    :user-email="app.user.value?.email"
  />
</template>

<script setup lang="ts">
import TenantManager from "../components/TenantManager.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
