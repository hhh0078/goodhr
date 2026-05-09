<template>
  <div class="tab-content active">
    <div class="scroll-area">
      <div class="scroll-tip">↓ 可以往下滑动查看更多设置 ↓</div>
      <PhoneBindSection
        title="账号绑定(新用户赠送1元)"
        :phone="state.phone"
        :binding="ui.bindingPhone"
        @update:phone="(value) => emit('update:phone', value)"
        @bind="emit('bind-phone')"
      />
      <div class="filter-group">
        <div class="ai-status">
          <div class="ai-status-indicator"></div>
          <span>{{ ui.aiStatusText }}</span>
          <span>{{ ui.aiBalanceText }}</span>
          <a href="https://siliconflow.a.58it.cn" target="_blank">余额充值</a>
        </div>
        <slot name="status" />
      </div>
      <PositionManager
        :draft="ui.aiPositionDraft"
        :positions="state.positions"
        :current-position-name="state.currentPositionName"
        placeholder="添加岗位(回车键添加)"
        @update:draft="(value) => emit('update:position-draft', value)"
        @add="emit('add-position')"
        @select="(value) => emit('select-position', value)"
        @remove="(value) => emit('remove-position', value)"
      />
      <slot name="rest" />
    </div>
  </div>
</template>

<script setup>
import PhoneBindSection from "./PhoneBindSection.vue";
import PositionManager from "./PositionManager.vue";

defineProps({
  state: Object,
  ui: Object,
});

const emit = defineEmits([
  "update:phone",
  "bind-phone",
  "update:position-draft",
  "add-position",
  "select-position",
  "remove-position",
]);
</script>
