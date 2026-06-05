<!-- 本文件是订阅菜单页面，负责展示会员状态、套餐和支付记录。 -->
<template>
  <SubscriptionPanel
    :token="app.auth.token.value"
    @request-login="app.requestLogin"
  />
</template>

<script setup lang="ts">
import SubscriptionPanel from "../components/SubscriptionPanel.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
