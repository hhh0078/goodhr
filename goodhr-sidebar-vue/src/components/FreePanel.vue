<template>
  <div class="tab-content active">
    <div class="scroll-area">
      <div class="scroll-tip">↓ 可以往下滑动查看更多设置 ↓</div>
      <PhoneBindSection
        title="账号绑定"
        :phone="state.phone"
        :binding="ui.bindingPhone"
        help="* 绑定手机号后可以从服务器同步你的配置(关键词等等)(无需验证)"
        @update:phone="(value) => emit('update:phone', value)"
        @bind="emit('bind-phone')"
      />
      <PositionManager
        :draft="ui.positionDraft"
        :positions="state.positions"
        :current-position-name="state.currentPositionName"
        placeholder="添加岗位(关键词分组,回车键添加)"
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

