<!-- 本文件是任务列表菜单页面，负责展示任务管理和候选人入口。 -->
<template>
  <TaskList
    :tasks="app.tasks"
    :positions="app.positions.positions.value"
    :token="app.auth.token.value"
    :agent="app.agent"
    @open-candidates="openTaskCandidates"
    @request-login="app.requestLogin"
  />
</template>

<script setup lang="ts">
import { useRouter } from "vue-router";
import TaskList from "../components/TaskList.vue";
import { useAppContext } from "../composables/useAppContext";

const app = useAppContext();
const router = useRouter();

/**
 * 新开页面查看指定任务的候选人。
 * @param {string} taskId - 云端任务 ID。
 * @returns {void} 无返回值。
 */
function openTaskCandidates(taskId: string) {
  if (!taskId) return;
  const route = router.resolve({ name: "resumes", query: { task_id: taskId } });
  window.open(route.href, "_blank");
}
</script>
