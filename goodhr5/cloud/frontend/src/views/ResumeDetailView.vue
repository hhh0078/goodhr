<!-- 本文件是候选人详情页面，负责展示单个候选人的完整简历。 -->
<template>
  <ResumeDetail :candidate-id="candidateId" :engagement-id="engagementId" />
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import ResumeDetail from "../components/ResumeDetail.vue";

const route = useRoute();
const candidateId = computed(() => String(route.query.candidate_id || ""));
const engagementId = computed(() => String(route.query.engagement_id || ""));
</script>
