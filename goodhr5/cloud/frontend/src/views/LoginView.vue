<!-- 本文件是后台独立登录页面，负责登录成功后返回来源页面。 -->
<template>
  <LoginForm :auth="app.auth" :allow-close="false" @close="backToConsole" />
</template>

<script setup lang="ts">
import { computed, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import LoginForm from "../components/LoginForm.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
const route = useRoute();
const router = useRouter();
const redirectTarget = computed(() => {
  const value = route.query.redirect;
  return typeof value === "string" && value.startsWith("/") ? value : "/";
});

/**
 * 返回控制台首页。
 * @returns {void} 无返回值。
 */
function backToConsole() {
  void router.replace("/");
}

watch(
  () => app.auth.user.value,
  (user) => {
    if (!user) return;
    void router.replace(redirectTarget.value);
  },
  { immediate: true },
);
</script>
