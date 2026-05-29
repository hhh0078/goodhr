<!-- 本文件是岗位模板菜单页面，负责展示岗位模板管理功能。 -->
<template>
  <PositionManager :positions="app.positions" />
</template>

<script setup lang="ts">
import PositionManager from "../components/PositionManager.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
</script>
