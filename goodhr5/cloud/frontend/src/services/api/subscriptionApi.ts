// 本文件负责订阅、套餐、支付订单和激活码兑换接口。
import { api } from "../apiClient";

/**
 * 读取当前用户订阅状态。
 * @returns {Promise<any>} 返回会员类型、到期时间和有效状态。
 */
export async function getSubscriptionStatus() {
  const data = await api("/api/subscription/status");
  return data.subscription;
}

/**
 * 读取系统订阅套餐列表。
 * @returns {Promise<any[]>} 返回订阅套餐数组。
 */
export async function listSubscriptionPlans() {
  const data = await api("/api/subscription/plans", { auth: false });
  return data.plans || [];
}

/**
 * 兑换会员激活码。
 * @param {string} code - 用户输入的激活码。
 * @returns {Promise<any>} 返回新的订阅状态。
 */
export async function redeemActivationCode(code: string) {
  const data = await api("/api/activation-codes/redeem", { method: "POST", body: { code } });
  return data.subscription;
}

/**
 * 创建订阅支付订单。
 * @param {string} planID - 订阅套餐 ID。
 * @returns {Promise<any>} 返回支付订单和支付平台提交参数。
 */
export async function createPaymentOrder(planID: string) {
  return api("/api/payment/orders", { method: "POST", body: { plan_id: planID } });
}

/**
 * 读取当前用户支付记录。
 * @returns {Promise<any[]>} 返回当前用户支付记录数组。
 */
export async function listPaymentOrders() {
  const data = await api("/api/payment/orders");
  return data.orders || [];
}
