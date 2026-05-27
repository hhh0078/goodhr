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
    <div style="margin-bottom: 20px">
      <p>
        充值后，您在任何时间都可以退费。我们将按原价/套餐天数作为单价*剩余天数=退费金额。还会收取5%的退费手续费，因为我们使用的充值渠道是有百分之5%的手续费。继续充值视为您同意充值协议。
      </p>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="message" class="success">{{ message }}</p>
    <p v-if="loading" class="hint">正在读取订阅信息...</p>

    <div class="activation-box">
      <label>
        激活码
        <input v-model="activationCode" placeholder="输入会员激活码" />
      </label>
      <button
        class="ghost primary"
        :disabled="activating || !activationCode.trim()"
        @click="redeemCode"
      >
        {{ activating ? "激活中..." : "确认激活" }}
      </button>
    </div>

    <div class="plan-grid">
      <article v-for="plan in plans" :key="plan.id" class="plan-card">
        <div class="plan-title">
          <strong>{{ plan.name }}</strong>
          <span>{{ plan.member_type || "plus" }}</span>
        </div>
        <div class="plan-price">
          <span class="final-price">￥{{ finalPrice(plan) }}</span>
          <span class="original-price"
            >原价 ￥{{ Number(plan.original_price || 0) }}</span
          >
        </div>
        <p class="hint">{{ plan.description }}</p>
        <ul>
          <li v-for="feature in plan.features || []" :key="feature">
            {{ feature }}
          </li>
        </ul>
        <button
          class="ghost primary"
          :disabled="payingPlanId === plan.id"
          @click="pay(plan)"
        >
          {{ payingPlanId === plan.id ? "下单中..." : "立即支付" }}
        </button>
      </article>
    </div>

    <div class="records-head">
      <h3>支付记录</h3>
      <button class="ghost" :disabled="loading" @click="loadOrders">
        刷新记录
      </button>
    </div>
    <div v-if="orders.length" class="record-list">
      <div v-for="order in orders" :key="order.order_no" class="record-row">
        <div>
          <strong>{{ order.plan_name }}</strong>
          <p class="hint">{{ order.order_no }}</p>
        </div>
        <span>{{ formatMoney(order.amount_cents) }}</span>
        <span :class="['status', order.status]">{{
          statusText(order.status)
        }}</span>
        <span class="hint">{{ formatDate(order.created_at) }}</span>
      </div>
    </div>
    <p v-else class="hint">暂无支付记录</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  createPaymentOrder,
  getSubscriptionStatus,
  listPaymentOrders,
  listSubscriptionPlans,
  redeemActivationCode,
} from "../services/cloudApi";

const subscription = ref<any>(null);
const plans = ref<any[]>([]);
const orders = ref<any[]>([]);
const loading = ref(false);
const error = ref("");
const message = ref("");
const payingPlanId = ref("");
const activationCode = ref("");
const activating = ref(false);
const memberLabel = computed(
  () => `${subscription.value?.member_type || "plus"} 会员`,
);

/**
 * 读取订阅状态和套餐列表。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  message.value = "";
  try {
    const [nextSubscription, nextPlans, nextOrders] = await Promise.all([
      getSubscriptionStatus(),
      listSubscriptionPlans(),
      listPaymentOrders(),
    ]);
    subscription.value = nextSubscription;
    plans.value = nextPlans;
    orders.value = nextOrders;
  } catch (e: any) {
    error.value = e.message || "读取订阅信息失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 兑换会员激活码。
 * @returns {Promise<void>} 无返回值。
 */
async function redeemCode() {
  const code = activationCode.value.trim();
  if (!code) return;
  activating.value = true;
  error.value = "";
  message.value = "";
  try {
    subscription.value = await redeemActivationCode(code);
    activationCode.value = "";
    message.value = "激活成功，会员时间已增加";
  } catch (e: any) {
    error.value = e.message || "激活码兑换失败";
  } finally {
    activating.value = false;
  }
}

/**
 * 读取当前用户支付记录。
 * @returns {Promise<void>} 无返回值。
 */
async function loadOrders() {
  error.value = "";
  try {
    orders.value = await listPaymentOrders();
  } catch (e: any) {
    error.value = e.message || "读取支付记录失败";
  }
}

/**
 * 创建订阅订单，并打开好收米支付页面。
 * @param {any} plan - 订阅套餐配置。
 * @returns {Promise<void>} 无返回值。
 */
async function pay(plan: any) {
  if (!plan?.id) return;
  payingPlanId.value = plan.id;
  error.value = "";
  try {
    const data = await createPaymentOrder(plan.id);
    await loadOrders();
    submitPaymentForm(data.payment);
  } catch (e: any) {
    error.value = e.message || "创建支付订单失败";
  } finally {
    payingPlanId.value = "";
  }
}

/**
 * 计算套餐实付价格。
 * @param {any} plan - 套餐配置。
 * @returns {number} 实付价格。
 */
function finalPrice(plan: any) {
  return Math.max(
    0,
    Number(plan?.original_price || 0) - Number(plan?.discount_amount || 0),
  );
}

/**
 * 提交第三方支付表单。
 * @param {any} payment - 后端返回的支付提交参数。
 * @returns {void} 无返回值。
 */
function submitPaymentForm(payment: any) {
  if (!payment?.submit_url) {
    error.value = "支付平台没有返回可打开的支付地址";
    return;
  }
  const form = document.createElement("form");
  form.method = payment.submit_method || "POST";
  form.action = payment.submit_url;
  form.target = "_blank";
  Object.entries(payment.submit_fields || {}).forEach(([key, value]) => {
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = key;
    input.value = String(value ?? "");
    form.appendChild(input);
  });
  document.body.appendChild(form);
  form.submit();
  form.remove();
}

/**
 * 格式化金额分为人民币文案。
 * @param {number} cents - 金额，单位分。
 * @returns {string} 金额文案。
 */
function formatMoney(cents: number) {
  return `￥${(Number(cents || 0) / 100).toFixed(2)}`;
}

/**
 * 转换订单状态为中文。
 * @param {string} status - 订单状态。
 * @returns {string} 中文状态。
 */
function statusText(status: string) {
  if (status === "paid") return "已支付";
  if (status === "closed") return "已关闭";
  return "待支付";
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
.activation-box {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: end;
  border: 1px solid #333;
  background: #050505;
  padding: 10px;
  margin-bottom: 12px;
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
.records-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin: 18px 0 8px;
}
.record-list {
  border: 1px solid #333;
  background: #050505;
}
.record-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 90px 80px 170px;
  gap: 10px;
  align-items: center;
  padding: 10px;
  border-bottom: 1px solid #222;
}
.record-row:last-child {
  border-bottom: 0;
}
.status.paid {
  color: #0f0;
}
.status.pending {
  color: #fa0;
}
.status.closed {
  color: #f33;
}

@media (max-width: 640px) {
  .activation-box,
  .record-row {
    grid-template-columns: 1fr;
  }
}
</style>
