<!-- 本文件是平台账号菜单页面，负责展示平台账号管理功能。 -->
<template>
  <AccountManager
    :token="app.auth.token.value"
    :agent-base-url="app.agent.baseUrl.value"
  />
</template>

<script setup lang="ts">
import AccountManager from "../components/AccountManager.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
