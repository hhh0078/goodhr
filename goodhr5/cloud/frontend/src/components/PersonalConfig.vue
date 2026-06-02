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
          placeholder="https://api.minimaxi.com/v1/chat/completions"
        />
        <small class="field-help"
          >用于调用 MiniMax 多模态和对话模型。默认地址为
          https://api.minimaxi.com/v1/chat/completions</small
        >
      </label>
      <label class="field">
        模型
        <input
          v-model="config.form.value.aiModel"
          placeholder="MiniMax-M3"
        />
        <small class="field-help">任务使用的默认模型名称。默认 MiniMax-M3</small>
      </label>
      <label class="field">
        API Key
        <input
          v-model="config.form.value.aiAPIKey"
          type="text"
          :placeholder="
            config.form.value.aiAPIKeySet
              ? `当前已设置：${config.form.value.aiAPIKeyMasked || '已隐藏'}；留空则保持不变`
              : '输入你的 AI Key'
          "
        />
        <small class="field-help"
          >如果当前已经配置过 Key，这里留空会保留原值，不会清空。MiniMax
          需要先购买 token 套餐后再使用。<a
            href="https://platform.minimaxi.com/subscribe/token-plan"
            target="_blank"
            rel="noreferrer"
            >前往购买</a
          ></small
        >
      </label>
    </div>

    <h3>模拟延迟</h3>
    <div class="settings-list">
      <label class="field-row">
        <span class="field-title">点击频率(%)</span>
        <input
          v-model="config.form.value.clickFrequency"
          class="number-input"
          type="number"
          min="0"
          max="100"
        />
      </label>
      <label class="field-row">
        <span class="field-title">详情查看概率(%)</span>
        <span class="field-main">
          <input
            v-model="config.form.value.detailOpenProbability"
            class="number-input"
            type="number"
            min="0"
            max="100"
          />
        </span>
        <small class="field-help"
          >关键词模式下，用这个概率决定是否打开详情再继续筛选。</small
        >
      </label>
      <label class="field-row">
        <span class="field-title">点击详情前延时</span>
        <span class="field-main">
          <input
            v-model="config.form.value.detailOpenDelayMin"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="点击详情前延时最小秒数"
          />
          <span class="range-separator">到</span>
          <input
            v-model="config.form.value.detailOpenDelayMax"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="点击详情前延时最大秒数"
          />
          <span class="unit">秒</span>
        </span>
        <small class="field-help"
          >点击候选人详情按钮前等待，系统会在最小和最大值之间随机。</small
        >
      </label>
      <label class="field-row">
        <span class="field-title">关闭详情前延时</span>
        <span class="field-main">
          <input
            v-model="config.form.value.detailCloseDelayMin"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="关闭详情前延时最小秒数"
          />
          <span class="range-separator">到</span>
          <input
            v-model="config.form.value.detailCloseDelayMax"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="关闭详情前延时最大秒数"
          />
          <span class="unit">秒</span>
        </span>
        <small class="field-help"
          >详情文本提取完成后，关闭详情页之前等待，像真人看完再关闭。</small
        >
      </label>
      <label class="field-row">
        <span class="field-title">打招呼前延时</span>
        <span class="field-main">
          <input
            v-model="config.form.value.greetBeforeDelayMin"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="打招呼前延时最小秒数"
          />
          <span class="range-separator">到</span>
          <input
            v-model="config.form.value.greetBeforeDelayMax"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="打招呼前延时最大秒数"
          />
          <span class="unit">秒</span>
        </span>
        <small class="field-help"
          >决定候选人通过筛选后，点击打招呼按钮前等待多久。</small
        >
      </label>
    </div>

    <h3>模拟休息</h3>
    <div class="settings-list">
      <label class="field-row">
        <span class="field-title">处理多少人后休息</span>
        <span class="field-main">
          <input
            v-model="config.form.value.restAfterCandidatesMin"
            class="number-input"
            type="number"
            min="0"
            aria-label="处理多少人后休息最小人数"
          />
          <span class="range-separator">到</span>
          <input
            v-model="config.form.value.restAfterCandidatesMax"
            class="number-input"
            type="number"
            min="0"
            aria-label="处理多少人后休息最大人数"
          />
          <span class="unit">人</span>
        </span>
        <small class="field-help"
          >系统会随机决定处理多少个候选人后休息一次，例如 40-70 人。</small
        >
      </label>
      <label class="field-row">
        <span class="field-title">单次任务最多休息</span>
        <span class="field-main">
          <input
            v-model="config.form.value.restTimesMin"
            class="number-input"
            type="number"
            min="0"
            aria-label="单次任务最多休息最小次数"
          />
          <span class="range-separator">到</span>
          <input
            v-model="config.form.value.restTimesMax"
            class="number-input"
            type="number"
            min="0"
            aria-label="单次任务最多休息最大次数"
          />
          <span class="unit">次</span>
        </span>
        <small class="field-help"
          >任务启动时会随机决定本次最多休息几次，到次数后不再休息。</small
        >
      </label>
      <label class="field-row">
        <span class="field-title">每次休息时长</span>
        <span class="field-main">
          <input
            v-model="config.form.value.restDurationMin"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="每次休息最小分钟数"
          />
          <span class="range-separator">到</span>
          <input
            v-model="config.form.value.restDurationMax"
            class="number-input"
            type="number"
            min="0"
            step="0.1"
            aria-label="每次休息最大分钟数"
          />
          <span class="unit">分钟</span>
        </span>
        <small class="field-help"
          >每次摸鱼休息会在这个分钟范围内随机，并写入任务日志。</small
        >
      </label>
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

.settings-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.field-row {
  display: grid;
  grid-template-columns: minmax(150px, 190px) minmax(0, 1fr);
  align-items: start;
  gap: 8px 14px;
}

.field-title {
  color: var(--fg);
  font-weight: 600;
  line-height: 36px;
}

.field-main {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.number-input {
  width: 120px;
  max-width: 100%;
}

.range-separator,
.unit {
  color: var(--fg-dim);
  font-size: 13px;
  line-height: 36px;
}

.field-help {
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
  grid-column: 2;
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

  .field-row {
    grid-template-columns: 1fr;
  }

  .field-title {
    line-height: 1.4;
  }

  .field-help {
    grid-column: 1;
  }
}
</style>
