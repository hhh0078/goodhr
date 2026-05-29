<!-- 本文件是简历库菜单页面，负责展示候选人简历列表。 -->
<template>
  <ResumeLibrary :initial-task-id="initialTaskId" />
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import ResumeLibrary from "../components/ResumeLibrary.vue";

const route = useRoute();
const initialTaskId = computed(() => String(route.query.task_id || ""));
</script>
