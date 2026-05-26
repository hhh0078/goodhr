<template>
  <section class="panel">
    <div class="panel-header">
      <h2>本地 Agent</h2>
      <button class="ghost" :disabled="agent.checking.value" @click="agent.detect(agent.user, agent.token)">重新检测</button>
    </div>
    <div class="agent-info">
      <dl>
        <dt>状态</dt>
        <dd :class="{ success: agent.status.value.includes('连接'), error: agent.status.value.includes('未检测到') }">{{ agent.status.value }}</dd>
        <dt v-if="requiresUpdate">更新</dt>
        <dd v-if="requiresUpdate" class="warn">请更新本地程序到 {{ requiredVersion }} 或更高版本</dd>
        <dt v-if="agent.info">版本</dt>
        <dd v-if="agent.info">{{ agent.info.value?.version }}</dd>
        <dt>绑定</dt>
        <dd :class="{ error: agent.bindStatus.value === '绑定失败' }">{{ agent.bindStatus.value }}</dd>
        <dt>WS</dt>
        <dd :class="{ success: agent.wsStatus.value === '已连接', error: agent.wsStatus.value.includes('失败') }">{{ agent.wsStatus.value }}</dd>
        <dt v-if="agent.info">机器码</dt>
        <dd v-if="agent.info" class="hint">{{ (agent.info.value?.machine_id || '').slice(0, 20) }}...</dd>
        <dt v-if="agent.baseUrl">地址</dt>
        <dd v-if="agent.baseUrl">{{ agent.baseUrl.value }}</dd>
      </dl>
    </div>
    <p v-if="agent.bindError.value" class="error">{{ agent.bindError.value }}</p>
    <p v-if="agent.wsError.value" class="error">{{ agent.wsError.value }}</p>
    <p v-if="!agent.info && !agent.checking.value" class="hint">
      未检测到本地程序，请先下载并启动 GoodHR 5 Local Agent。
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps({ agent: Object, appConfig: Object })

const requiredVersion = computed(() => String(props.appConfig?.local_agent_version || "5.0.0"));
const localVersion = computed(() => String(props.agent?.info?.value?.version || ""));
const requiresUpdate = computed(() => {
  if (!localVersion.value) return false;
  return compareVersions(localVersion.value, requiredVersion.value) < 0;
});

/**
 * 比较两个版本号大小。
 * @param {string} current - 当前版本号。
 * @param {string} required - 要求版本号。
 * @returns {number} 当前版本低于要求时返回 -1，相等返回 0，高于返回 1。
 */
function compareVersions(current: string, required: string) {
  const currentParts = parseVersion(current);
  const requiredParts = parseVersion(required);
  const length = Math.max(currentParts.length, requiredParts.length);
  for (let i = 0; i < length; i += 1) {
    const left = currentParts[i] || 0;
    const right = requiredParts[i] || 0;
    if (left < right) return -1;
    if (left > right) return 1;
  }
  return 0;
}

/**
 * 将版本号转换为数字数组。
 * @param {string} version - 原始版本号。
 * @returns {number[]} 数字版本片段。
 */
function parseVersion(version: string) {
  return String(version)
    .split(".")
    .map((part) => Number.parseInt(part.replace(/\D+.*/, "") || "0", 10));
}
</script>
