<!-- 本文件负责展示当前会员状态和订阅套餐。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <h2>订阅</h2>
      <button class="ghost" @click="load">刷新</button>
    </div>

    <div class="subscription-summary">
      <div>
        <strong>{{ memberLabel }}</strong>
        <p :class="subscription?.active ? 'success' : 'error'">
          {{ subscription?.active ? "会员有效" : "会员已到期" }}
        </p>
      </div>
      <div>
        <span class="hint">到期时间</span>
        <p>{{ formatDate(subscription?.expires_at) }}</p>
      </div>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="loading" class="hint">正在读取订阅信息...</p>

    <div class="plan-grid">
      <article v-for="plan in plans" :key="plan.id" class="plan-card">
        <div class="plan-title">
          <strong>{{ plan.name }}</strong>
          <span>{{ plan.member_type || "plus" }}</span>
        </div>
        <div class="plan-price">
          <span class="final-price">￥{{ finalPrice(plan) }}</span>
          <span class="original-price">原价 ￥{{ Number(plan.original_price || 0) }}</span>
        </div>
        <p class="hint">{{ plan.description }}</p>
        <ul>
          <li v-for="feature in plan.features || []" :key="feature">{{ feature }}</li>
        </ul>
        <button class="ghost primary" disabled>暂未开放支付</button>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { getSubscriptionStatus, listSubscriptionPlans } from "../services/cloudApi";

const subscription = ref<any>(null);
const plans = ref<any[]>([]);
const loading = ref(false);
const error = ref("");
const memberLabel = computed(() => `${subscription.value?.member_type || "plus"} 会员`);

/**
 * 读取订阅状态和套餐列表。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [nextSubscription, nextPlans] = await Promise.all([
      getSubscriptionStatus(),
      listSubscriptionPlans(),
    ]);
    subscription.value = nextSubscription;
    plans.value = nextPlans;
  } catch (e: any) {
    error.value = e.message || "读取订阅信息失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 计算套餐实付价格。
 * @param {any} plan - 套餐配置。
 * @returns {number} 实付价格。
 */
function finalPrice(plan: any) {
  return Math.max(0, Number(plan?.original_price || 0) - Number(plan?.discount_amount || 0));
}

/**
 * 格式化日期时间。
 * @param {string} value - ISO日期字符串。
 * @returns {string} 展示文案。
 */
function formatDate(value: string) {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return date.toLocaleString();
}

defineExpose({ load });
onMounted(load);
</script>

<style scoped>
.subscription-summary {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid #333;
  background: #050505;
  padding: 10px;
  margin-bottom: 12px;
}
.plan-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
.plan-card {
  border: 1px solid #333;
  background: #050505;
  padding: 12px;
}
.plan-title {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}
.plan-title span {
  color: var(--fg-dim);
}
.plan-price {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-bottom: 8px;
}
.final-price {
  font-size: 22px;
  color: var(--fg);
}
.original-price {
  color: var(--fg-dim);
  text-decoration: line-through;
}
ul {
  margin: 10px 0;
  padding-left: 18px;
  color: var(--fg-dim);
}
</style>
