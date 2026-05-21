<template>
  <section class="panel">
    <div class="panel-header">
      <h2>个人配置</h2>
      <button class="ghost" @click="config.load">刷新</button>
    </div>

    <h3>AI 默认配置</h3>
    <div class="personal-grid">
      <label class="field">
        API 地址
        <input
          v-model="config.form.value.aiBaseURL"
          placeholder="如: https://api.siliconflow.cn/v1"
        />
        <small class="field-help">用于调用 AI 服务的基础地址。</small>
      </label>
      <label class="field">
        模型
        <input
          v-model="config.form.value.aiModel"
          placeholder="如: gpt-5.1-chat"
        />
        <small class="field-help">任务使用的默认模型名称。</small>
      </label>
      <label class="field">
        API Key
        <input
          v-model="config.form.value.aiAPIKey"
          :placeholder="
            config.form.value.aiAPIKeySet
              ? `当前已设置：${config.form.value.aiAPIKeyMasked || '已隐藏'}；留空则保持不变`
              : '输入你的 AI Key'
          "
        />
        <small class="field-help"
          >如果当前已经配置过 Key，这里留空会保留原值，不会清空。</small
        >
      </label>
    </div>

    <h3>模拟延迟</h3>
    <div class="personal-grid">
      <label class="field"
        >点击频率(%)<input
          v-model="config.form.value.clickFrequency"
          type="number"
          min="0"
          max="100"
      /></label>
      <label class="field field-small"
        >滚动延迟最小(秒)<input
          v-model="config.form.value.scrollDelayMin"
          type="number"
          min="0"
      /></label>
      <label class="field field-small"
        >滚动延迟最大(秒)<input
          v-model="config.form.value.scrollDelayMax"
          type="number"
          min="0"
      /></label>
      <label class="field field-small"
        >列表查看最小(秒)<input
          v-model="config.form.value.listViewDelayMin"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field field-small"
        >列表查看最大(秒)<input
          v-model="config.form.value.listViewDelayMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field field-small"
        >详情查看最小(秒)<input
          v-model="config.form.value.detailViewDelayMin"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field field-small"
        >详情查看最大(秒)<input
          v-model="config.form.value.detailViewDelayMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field field-small"
        >打招呼延迟最小(秒)<input
          v-model="config.form.value.greetDelayMin"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field field-small"
        >打招呼延迟最大(秒)<input
          v-model="config.form.value.greetDelayMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
    </div>

    <h3>模拟休息</h3>
    <div class="personal-grid">
      <label class="field field-small"
        >处理后休息阈值最小(人)<input
          v-model="config.form.value.restAfterCandidatesMin"
          type="number"
          min="0"
      /></label>
      <label class="field field-small"
        >处理后休息阈值最大(人)<input
          v-model="config.form.value.restAfterCandidatesMax"
          type="number"
          min="0"
      /></label>
      <label class="field field-small"
        >单次任务休息次数最小<input
          v-model="config.form.value.restTimesMin"
          type="number"
          min="0"
      /></label>
      <label class="field field-small"
        >单次任务休息次数最大<input
          v-model="config.form.value.restTimesMax"
          type="number"
          min="0"
      /></label>
      <label class="field field-small"
        >每次休息时长最小(分钟)<input
          v-model="config.form.value.restDurationMin"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field field-small"
        >每次休息时长最大(分钟)<input
          v-model="config.form.value.restDurationMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
    </div>

    <p v-if="config.error.value" class="error">{{ config.error.value }}</p>
    <p v-if="config.message.value" class="success">
      {{ config.message.value }}
    </p>

    <div class="actions">
      <button :disabled="config.loading.value" @click="config.save">
        {{ config.loading.value ? "保存中..." : "保存配置" }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
defineProps({
  config: Object,
});
</script>

<style scoped>
.personal-grid {
  display: grid;
  grid-template-columns: repeat(1, minmax(0, 1fr));
  gap: 12px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-small {
  grid-column: span 3;
}

.field-medium {
  grid-column: span 6;
}

.field input {
  width: 100%;
  box-sizing: border-box;
}

.field-help {
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 900px) {
  .field-small,
  .field-medium {
    grid-column: span 6;
  }
}

@media (max-width: 640px) {
  .field-small,
  .field-medium {
    grid-column: 1 / -1;
  }
}
</style>
