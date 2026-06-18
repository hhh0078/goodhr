/** 本文件负责新版后台订阅状态、套餐、激活码和支付记录。 */
"use client";

import CheckRoundedIcon from "@mui/icons-material/CheckRounded";
import { Box, Button, Chip, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** SubscriptionPage 展示会员状态并处理购买和激活码。 */
export default function SubscriptionPage() {
  const { notify } = useAdmin();
  const [subscription, setSubscription] = useState<any>({});
  const [plans, setPlans] = useState<any[]>([]);
  const [orders, setOrders] = useState<any[]>([]);
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);

  /** load 读取订阅、套餐和支付记录。 */
  async function load() { setLoading(true); try { const [status, planData, orderData] = await Promise.all([cloudRequest("/api/subscription/status"), cloudRequest("/api/subscription/plans", { auth: false }), cloudRequest("/api/payment/orders")]); setSubscription(status.subscription || {}); setPlans(planData.plans || []); setOrders(orderData.orders || []); } catch (error) { notify(error instanceof Error ? error.message : "订阅信息读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { void load(); }, []);

  /** redeem 兑换会员激活码。 */
  async function redeem() { if (!code.trim()) return; try { const data = await cloudRequest("/api/activation-codes/redeem", { method: "POST", body: { code: code.trim() } }); setSubscription(data.subscription || {}); setCode(""); notify("激活成功，会员时间已增加", "success"); } catch (error) { notify(error instanceof Error ? error.message : "激活失败", "error"); } }

  /** pay 创建订单并向第三方支付平台提交表单。 */
  async function pay(planID: string) { try { const data = await cloudRequest("/api/payment/orders", { method: "POST", body: { plan_id: planID } }); submitPayment(data.payment); await load(); } catch (error) { notify(error instanceof Error ? error.message : "创建支付订单失败", "error"); } }

  return <><PageHeader title="订阅会员" description="关键词模式永久免费，AI 筛选、AI 详情和智能沟通属于 Plus 会员功能。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} /><SectionPanel sx={{ mb: 2 }}><Stack direction={{ xs: "column", sm: "row" }} spacing={3} sx={{ justifyContent: "space-between" }}><Box><Typography color="text.secondary">当前会员</Typography><Stack direction="row" spacing={1} sx={{ mt: 0.5, alignItems: "center" }}><Typography sx={{ fontSize: 26, fontWeight: 780 }}>{subscription.member_type || "免费版"}</Typography><Chip color={subscription.active ? "success" : "default"} label={subscription.active ? "有效" : "未开通或已到期"} /></Stack></Box><Box><Typography color="text.secondary">到期时间</Typography><Typography sx={{ mt: 0.75, fontWeight: 700 }}>{formatDate(subscription.expires_at)}</Typography></Box></Stack><Stack direction={{ xs: "column", sm: "row" }} spacing={1.5} sx={{ mt: 3 }}><TextField label="会员激活码" value={code} onChange={(event) => setCode(event.target.value)} fullWidth /><Button variant="contained" onClick={() => void redeem()}>确认激活</Button></Stack></SectionPanel><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(3, 1fr)" }, gap: 2 }}>{plans.map((plan) => <SectionPanel key={plan.id}><Typography component="h2" sx={{ fontSize: 20, fontWeight: 760 }}>{plan.name}</Typography><Typography sx={{ mt: 1.5, fontSize: 32, fontWeight: 800 }}>￥{finalPrice(plan)}</Typography><Typography sx={{ mt: 1, color: "text.secondary", minHeight: 48 }}>{plan.description}</Typography><Stack spacing={1} sx={{ mt: 2 }}>{(plan.features || []).map((feature: string) => <Stack key={feature} direction="row" spacing={1}><CheckRoundedIcon color="primary" fontSize="small" /><Typography>{feature}</Typography></Stack>)}</Stack>{Number(plan.original_price || 0) > 0 ? <Button fullWidth variant="contained" sx={{ mt: 3 }} onClick={() => void pay(plan.id)}>立即支付</Button> : null}</SectionPanel>)}</Box><SectionPanel sx={{ mt: 2 }}><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>支付记录</Typography>{orders.length ? <Stack sx={{ mt: 1.5 }}>{orders.map((order) => <Stack key={order.order_no} direction={{ xs: "column", md: "row" }} spacing={2} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider", justifyContent: "space-between" }}><Typography sx={{ fontWeight: 700 }}>{order.plan_name}</Typography><Typography>￥{(Number(order.amount_cents || 0) / 100).toFixed(2)}</Typography><Chip size="small" label={statusText(order.status)} /><Typography color="text.secondary">{formatDate(order.created_at)}</Typography></Stack>)}</Stack> : <EmptyState text="暂无支付记录" />}</SectionPanel></>;
}

/** finalPrice 计算套餐最终售价。 */
function finalPrice(plan: any) { return Math.max(0, Number(plan.original_price || 0) - Number(plan.discount_amount || 0)); }

/** statusText 将支付状态转换成中文。 */
function statusText(status: string) { return status === "paid" ? "已支付" : status === "closed" ? "已关闭" : "待支付"; }

/** submitPayment 创建并提交第三方支付表单。 */
function submitPayment(payment: any) { if (!payment?.submit_url) throw new Error("支付平台没有返回可打开的支付地址"); const form = document.createElement("form"); form.method = payment.submit_method || "POST"; form.action = payment.submit_url; form.target = "_blank"; Object.entries(payment.submit_fields || {}).forEach(([key, value]) => { const input = document.createElement("input"); input.type = "hidden"; input.name = key; input.value = String(value ?? ""); form.appendChild(input); }); document.body.appendChild(form); form.submit(); form.remove(); }
