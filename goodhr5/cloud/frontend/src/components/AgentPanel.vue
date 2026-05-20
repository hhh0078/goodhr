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
defineProps({ agent: Object })
</script>
