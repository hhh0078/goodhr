<template>
  <div class="filter-group">
    <label class="title">{{ title }}</label>
    <div class="position-input-group">
      <input
        :value="phone"
        class="keyword-input"
        :placeholder="placeholder"
        @input="$emit('update:phone', $event.target.value)"
        @keydown.enter.prevent="$emit('bind')"
      />
      <button class="keyword-btn" type="button" @click="$emit('bind')">
        {{ binding ? "绑定中..." : "绑定" }}
      </button>
    </div>
    <div v-if="help" class="section-help">{{ help }}</div>
  </div>
</template>

<script setup>
defineProps({
  title: String,
  phone: String,
  binding: Boolean,
  placeholder: {
    type: String,
    default: "输入手机号以同步配置",
  },
  help: {
    type: String,
    default: "",
  },
});

defineEmits(["update:phone", "bind"]);
</script>

