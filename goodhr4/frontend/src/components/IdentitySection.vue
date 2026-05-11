<template>
  <section class="identity-strip card" @focusout.capture="requestAutoSave">
    <div class="identity-row">
      <div class="identity-inputs">
        <input
          v-model="ui.identityInput"
          class="text-input"
          placeholder="输入邮箱或手机号，点击后直接自动注册"
          @keydown.enter.prevent="bindAccount"
        />
        <button
          class="btn btn-primary"
          type="button"
          :disabled="ui.binding"
          @click="bindAccount"
        >
          {{ ui.binding ? "绑定中..." : "绑定" }}
        </button>
      </div>

      <div>
        余额:
        <strong :style="{ color: balanceColor }">{{
          settings.aiBalanceText || "--"
        }}</strong>
        &nbsp;
        <a
          style="
            border: 1px solid #ccc;
            padding: 2px 4px;
            border-radius: 4px;
            text-decoration: none;
            color: #000;
          "
          href="https://ai.58it.cn"
          target="_blank"
          rel="noreferrer noopener"
          >ai充值(GoodAI)</a
        >

        &nbsp;&nbsp;
        <span
          style="
            cursor: pointer;
            border: 1px solid #ccc;
            padding: 2px 4px;
            border-radius: 4px;
          "
          @click="showPricingHint"
          >价格说明</span
        >
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { usePanelStore } from "../composables/usePanelStore";

const { settings, ui, bindAccount, requestAutoSave } = usePanelStore();

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
</script>
