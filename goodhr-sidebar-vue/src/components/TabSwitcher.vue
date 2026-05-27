<template>
  <div class="tab-container">
    <div
      class="tab"
      :class="{ active: modelValue === 'free' }"
      @click="$emit('update:modelValue', 'free')"
    >
      免费版
    </div>
    <div
      class="tab"
      :class="{ active: modelValue === 'ai' }"
      @click="$emit('update:modelValue', 'ai')"
    >
      AI版(赠送1元)
    </div>
  </div>
</template>

<script setup>
defineProps({
  modelValue: {
    type: String,
    required: true,
  },
});

defineEmits(["update:modelValue"]);
</script>

