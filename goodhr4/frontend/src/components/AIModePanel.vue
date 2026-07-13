<template>
  <section class="mode-panel">
    <div class="balance-bar">
      余额:
      <strong :style="{ color: balanceColor }">{{
        settings.aiBalanceText || "--"
      }}</strong>
      &nbsp;
      <a
        class="recharge-link"
        href="https://ai.58it.cn"
        target="_blank"
        rel="noreferrer noopener"
        >充值(GoodAI)</a
      >
      &nbsp;&nbsp;
      <span class="pricing-link" @click="showPricingHint">价格说明</span>
    </div>

    <div class="content-grid" @focusout.capture="requestAutoSave">
      <section :class="ui.running ? 'span-8' : 'span-12'">
        <div class="position-toolbar">
          <input
            v-model="ui.positionDraft"
            class="text-input"
            placeholder="新增岗位名称,例如：销售顾问"
            @keydown.enter.prevent="addPosition"
          />
          <button class="btn btn-secondary" type="button" @click="addPosition">
            新增岗位
          </button>
        </div>

        <div class="position-list" style="margin-top: 5px; margin-bottom: 10px">
          <button
            v-for="position in settings.positions"
            :key="position.name"
            class="position-item"
            :class="{ active: settings.currentPositionName === position.name }"
            type="button"
            @click="settings.currentPositionName = position.name"
          >
            <span>{{ position.name }}</span>
            <span
              class="position-remove"
              @click.stop="confirmRemovePosition(position.name)"
              >x</span
            >
          </button>
        </div>

        <div v-if="currentPosition" class="field-group">
          <div
            style="
              display: flex;
              align-items: center;
              justify-content: space-between;
            "
          >
            <label style="margin: 0">岗位说明</label>
            <button
              type="button"
              :disabled="ui.optimizing"
              @click.stop.prevent="optimizeJobDescription"
              style="
                display: inline-flex;
                align-items: center;
                gap: 4px;
                padding: 3px 10px;
                border: 1px solid var(--line-strong);
                border-radius: 8px;
                background: var(--surface);
                color: var(--text);
                font-size: 12px;
                font-weight: 500;
                cursor: pointer;
                white-space: nowrap;
                transition: opacity 0.2s;
              "
              :style="{
                opacity: ui.optimizing ? 0.6 : 1,
                cursor: ui.optimizing ? 'not-allowed' : 'pointer',
              }"
            >
              <svg
                v-if="!ui.optimizing"
                style="width: 12px; height: 12px"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
              </svg>
              {{ ui.optimizing ? "AI优化中..." : "AI优化" }}
            </button>
          </div>
          <textarea
            v-model="currentPosition.description"
            class="text-area ai-compact"
            placeholder="请详细描述岗位要求，比如课程顾问：&#10;1. 3年以上销售经验&#10;2. 有过学科销售经验&#10;3 . 同事没有联系过。最近活跃的。&#10;AI将根据这些信息智能判断候选人是否合适&#10;重点：&#10;1. 学历、年龄、性别等请在平台提前筛选号。AI优化会自动去除，如果你坚持 可以在AI优化完后再加上。&#10;2. 尽量不要写 候选人简历上没有的信息。比如能加班、人品好、爱微笑。这会给AI带来困惑&#10;"
          />
        </div>
      </section>

      <section v-if="ui.running" class="card span-4">
        <div class="section-heading">
          <div>
            <span class="section-tag">日志</span>
            <h2>AI版日志</h2>
          </div>
        </div>
        <div class="log-list short">
          <div
            v-for="(entry, index) in logs.slice().reverse()"
            :key="`${entry.time}-${index}`"
            class="log-item compact"
          >
            <span class="log-time">{{ entry.time }}</span>
            <span class="log-level" :class="entry.type">{{ entry.type }}</span>
            <span class="log-text">{{ entry.message }}</span>
          </div>
        </div>
      </section>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { usePanelStore } from "../composables/usePanelStore";

const {
  settings,
  ui,
  logs,
  currentPosition,
  addPosition,
  optimizeJobDescription,
  requestAutoSave,
} = usePanelStore();

const balanceColor = computed(() => {
  const balance = Number(settings.aiBalance);
  if (!Number.isFinite(balance)) {
    return "#9ca3af";
  }
  if (balance < 0.1) {
    return "#ef4444";
  }
  if (balance > 3) {
    return "#22c55e";
  }
  return "#f59e0b";
});

function showPricingHint() {
  globalThis.alert(
    "价格跟当前使用的模型有非常大的关系。模型越好，价格就越贵，效果就越好，反之一样。\n\n不同的模型都是根据token消耗量计算价格。如果你不了解，可以直接运行。每筛选一个候选人都会显示消耗的金额。",
  );
}

function confirmRemovePosition(name: string) {
  if (!globalThis.confirm(`确认删除岗位"${name}"吗？`)) return;
  const { removePosition } = usePanelStore();
  removePosition(name);
}
</script>

<style scoped>
.balance-bar {
  display: flex;
  align-items: center;
  padding: 8px 0;
  margin-bottom: 8px;
  font-size: 13px;
}

.recharge-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 12px;
  border-radius: 8px;
  background: linear-gradient(135deg, #6366f1, #8b5cf6);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  text-decoration: none;
  white-space: nowrap;
  transition: opacity 0.2s;
}

.recharge-link:hover {
  opacity: 0.85;
}

.pricing-link {
  cursor: pointer;
  border: 1px solid #ccc;
  padding: 2px 4px;
  border-radius: 4px;
  font-size: 12px;
}

.pricing-link:hover {
  background: #f5f5f5;
}
</style>
