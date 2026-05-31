<template>
  <section class="panel">
    <div class="panel-header">
      <h2>个人配置</h2>
      <button class="ghost" @click="config.load">刷新</button>
    </div>

    <h3>AI 配置</h3>
    <div class="personal-grid">
      <label class="field">
        API 地址
        <input
          v-model="config.form.value.aiBaseURL"
          placeholder="如: https://api.deepseek.com/chat/completions"
        />
        <small class="field-help"
          >用于调用 AI 服务的基础地址。推荐使用 Deepseek。地址为
          https://api.deepseek.com/chat/completions</small
        >
      </label>
      <label class="field">
        模型
        <input
          v-model="config.form.value.aiModel"
          placeholder="如: deepseek-v4-flash"
        />
        <small class="field-help"
          >任务使用的默认模型名称。deepseek-v4-flash或者deepseek-v4-pro</small
        >
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
          >如果当前已经配置过 Key，这里留空会保留原值，不会清空。deepseek
          你可以看这个文章获取 Key<a
            href="https://www.explinks.com/blog/how-to-get-deepseek-api-key-step-by-step-guide/"
            target="_blank"
            >获取秘钥教程</a
          ></small
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
        >详情查看概率(%)<input
          v-model="config.form.value.detailOpenProbability"
          type="number"
          min="0"
          max="100"
        />
        <small class="field-help"
          >关键词模式下，用这个概率决定是否打开详情再继续筛选。</small
        ></label
      >
      <label class="field"
        >点击详情前延时最小(秒)<input
          v-model="config.form.value.detailOpenDelayMin"
          type="number"
          min="0"
          step="0.1"
        />
        <small class="field-help"
          >点击候选人详情按钮前等待，系统会在最小和最大值之间随机。</small
        ></label
      >
      <label class="field"
        >点击详情前延时最大(秒)<input
          v-model="config.form.value.detailOpenDelayMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field"
        >关闭详情前延时最小(秒)<input
          v-model="config.form.value.detailCloseDelayMin"
          type="number"
          min="0"
          step="0.1"
        />
        <small class="field-help"
          >详情文本提取完成后，关闭详情页之前等待，像真人看完再关闭。</small
        ></label
      >
      <label class="field"
        >关闭详情前延时最大(秒)<input
          v-model="config.form.value.detailCloseDelayMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
      <label class="field"
        >打招呼前延时最小(秒)<input
          v-model="config.form.value.greetBeforeDelayMin"
          type="number"
          min="0"
          step="0.1"
        />
        <small class="field-help"
          >决定候选人通过筛选后，点击打招呼按钮前等待多久。</small
        ></label
      >
      <label class="field"
        >打招呼前延时最大(秒)<input
          v-model="config.form.value.greetBeforeDelayMax"
          type="number"
          min="0"
          step="0.1"
      /></label>
    </div>

    <h3>模拟休息</h3>
    <div class="personal-grid">
      <label class="field"
        >处理多少人后休息最小(人)<input
          v-model="config.form.value.restAfterCandidatesMin"
          type="number"
          min="0"
        />
        <small class="field-help"
          >系统会随机决定处理多少个候选人后休息一次，例如 40-70 人。</small
        ></label
      >
      <label class="field"
        >处理多少人后休息最大(人)<input
          v-model="config.form.value.restAfterCandidatesMax"
          type="number"
          min="0"
      /></label>
      <label class="field"
        >单次任务最多休息最小(次)<input
          v-model="config.form.value.restTimesMin"
          type="number"
          min="0"
        />
        <small class="field-help"
          >任务启动时会随机决定本次最多休息几次，到次数后不再休息。</small
        ></label
      >
      <label class="field"
        >单次任务最多休息最大(次)<input
          v-model="config.form.value.restTimesMax"
          type="number"
          min="0"
      /></label>
      <label class="field"
        >每次休息时长最小(分钟)<input
          v-model="config.form.value.restDurationMin"
          type="number"
          min="0"
          step="0.1"
        />
        <small class="field-help"
          >每次摸鱼休息会在这个分钟范围内随机，并写入任务日志。</small
        ></label
      >
      <label class="field"
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
