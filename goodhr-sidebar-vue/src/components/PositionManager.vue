<template>
  <div class="filter-group">
    <label class="title">岗位管理</label>
    <div class="position-input-group">
      <input
        :value="draft"
        class="keyword-input"
        :placeholder="placeholder"
        @input="$emit('update:draft', $event.target.value)"
        @keydown.enter.prevent="$emit('add')"
      />
      <button class="keyword-btn" type="button" @click="$emit('add')">添加</button>
    </div>
    <div class="position-tags">
      <div
        v-for="position in positions"
        :key="position.name"
        class="position-tag"
        :class="{ active: currentPositionName === position.name }"
        @click="$emit('select', position.name)"
      >
        {{ position.name }}
        <button class="remove-btn" type="button" @click.stop="$emit('remove', position.name)">
          &times;
        </button>
      </div>
      <div v-if="!positions.length" class="empty-state">请添加职位...</div>
    </div>
  </div>
</template>

<script setup>
defineProps({
  draft: String,
  positions: {
    type: Array,
    required: true,
  },
  currentPositionName: {
    type: String,
    default: "",
  },
  placeholder: String,
});

defineEmits(["update:draft", "add", "select", "remove"]);
</script>

