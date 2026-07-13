<template>
  <div class="ranking-list">
    <div v-if="loading" class="ranking-item">
      <div class="ranking-info">加载中...</div>
    </div>
    <div v-else-if="error" class="ranking-item">
      <div class="ranking-info">{{ error }}</div>
    </div>
    <div v-else-if="!items.length" class="ranking-item">
      <div class="ranking-info">暂无打赏数据</div>
    </div>
    <div v-for="(item, index) in items" :key="`${item.name}-${index}`" class="ranking-item">
      <div class="ranking-number">{{ index + 1 }}</div>
      <div class="ranking-info">
        <div class="ranking-name">{{ item.name || "匿名用户" }}</div>
        <div class="ranking-desc">{{ item.describe || "无留言" }}</div>
      </div>
      <div class="ranking-price">￥{{ item.price || 0 }}</div>
    </div>
  </div>
</template>

<script setup>
defineProps({
  items: {
    type: Array,
    required: true,
  },
  loading: {
    type: Boolean,
    default: false,
  },
  error: {
    type: String,
    default: "",
  },
});
</script>
