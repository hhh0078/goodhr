<!-- 本文件负责展示超级管理员可查看的全部支付记录。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <h2>支付记录</h2>
      <button class="ghost" :disabled="loading" @click="load">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="loading" class="hint">正在读取支付记录...</p>

    <div v-if="orders.length" class="record-table">
      <div class="record-row head">
        <span>用户</span>
        <span>套餐</span>
        <span>金额</span>
        <span>状态</span>
        <span>订单号</span>
        <span>创建时间</span>
      </div>
      <div v-for="order in orders" :key="order.order_no" class="record-row">
        <span>{{ order.user_email }}</span>
        <span>{{ order.plan_name }}</span>
        <span>{{ formatMoney(order.amount_cents) }}</span>
        <span :class="['status', order.status]">{{ statusText(order.status) }}</span>
        <span class="mono">{{ order.order_no }}</span>
        <span>{{ formatDate(order.created_at) }}</span>
      </div>
    </div>
    <p v-else-if="!loading" class="hint">暂无支付记录</p>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { listAdminPaymentOrders } from "../services/api/adminApi";

const orders = ref<any[]>([]);
const loading = ref(false);
const error = ref("");

/**
 * 读取超级管理员可见的全部支付记录。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    orders.value = await listAdminPaymentOrders();
  } catch (e: any) {
    error.value = e.message || "读取支付记录失败";
  } finally {
    loading.value = false;
  }
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

onMounted(load);
</script>

<style scoped>
.record-table {
  border: 1px solid #333;
  background: #050505;
  overflow-x: auto;
}
.record-row {
  display: grid;
  grid-template-columns: 220px 120px 90px 80px 190px 180px;
  gap: 10px;
  align-items: center;
  min-width: 900px;
  padding: 10px;
  border-bottom: 1px solid #222;
}
.record-row:last-child {
  border-bottom: 0;
}
.record-row.head {
  color: var(--fg-dim);
  background: #0d0d0d;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
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
</style>
