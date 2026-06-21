/** 本文件负责新版后台会员状态、订阅套餐、激活码和支付记录展示。 */
"use client";

import CheckRoundedIcon from "@mui/icons-material/CheckRounded";
import CreditCardRoundedIcon from "@mui/icons-material/CreditCardRounded";
import WorkspacePremiumRoundedIcon from "@mui/icons-material/WorkspacePremiumRounded";
import { Box, Button, Chip, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, FormActionRow, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** SubscriptionPage 展示会员状态并处理套餐购买和激活码。 */
export default function SubscriptionPage() {
  const { notify, subscription, refreshSession } = useAdmin();
  const [plans, setPlans] = useState<any[]>([]);
  const [orders, setOrders] = useState<any[]>([]);
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [payingPlanID, setPayingPlanID] = useState("");

  /** load 读取套餐和当前用户的支付记录。 */
  async function load() {
    setLoading(true);
    try {
      const [planData, orderData] = await Promise.all([cloudRequest("/api/subscription/plans", { auth: false }), cloudRequest("/api/payment/orders")]);
      setPlans(Array.isArray(planData.plans) ? planData.plans : []);
      setOrders(Array.isArray(orderData.orders) ? orderData.orders : []);
    } catch (error) {
      notify(error instanceof Error ? error.message : "订阅信息读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, []);

  /** redeem 兑换会员激活码并刷新当前会员状态。 */
  async function redeem() {
    if (!code.trim()) return;
    try {
      await cloudRequest("/api/activation-codes/redeem", { method: "POST", body: { code: code.trim() } });
      await refreshSession();
      setCode("");
      notify("激活成功，会员时间已增加", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "激活失败", "error");
    }
  }

  /** pay 创建套餐订单并提交至第三方支付平台。 */
  async function pay(planID: string) {
    if (!planID || payingPlanID) return;
    setPayingPlanID(planID);
    try {
      const data = await cloudRequest("/api/payment/orders", { method: "POST", body: { plan_id: planID } });
      submitPayment(data.payment);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "创建支付订单失败", "error");
    } finally {
      setPayingPlanID("");
    }
  }

  return <>
    <PageHeader title="订阅会员" description="关键词模式永久免费，升级会员可使用 AI 筛选、AI 详情识别和智能沟通。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} />

    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(300px, .75fr) minmax(0, 1.25fr)" }, gap: 2, mb: 3 }}>
      <SectionPanel sx={{ bgcolor: subscription.active ? "#15271e" : "#f7f8f7", color: subscription.active ? "#f8f1da" : "text.primary", borderColor: subscription.active ? "#4d5a48" : "divider" }}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}><Box sx={{ width: 46, height: 46, borderRadius: "8px", display: "grid", placeItems: "center", bgcolor: subscription.active ? "#c9a55d" : "#e5ebe7", color: subscription.active ? "#1b241e" : "#53645b" }}><WorkspacePremiumRoundedIcon /></Box><Box><Typography sx={{ fontSize: 13, opacity: 0.72 }}>当前会员</Typography><Typography sx={{ fontSize: 25, fontWeight: 800 }}>{subscription.member_type || "免费版"}</Typography></Box></Stack>
        <Stack direction="row" spacing={1} sx={{ mt: 2.5, alignItems: "center", flexWrap: "wrap", rowGap: 1 }}><Chip size="small" label={subscription.active ? "会员有效" : "未开通或已到期"} sx={{ bgcolor: subscription.active ? "#c9a55d" : "#e9eeeb", color: subscription.active ? "#17221b" : "#526158", fontWeight: 700 }} /><Typography sx={{ fontSize: 13, opacity: 0.76 }}>到期时间：{formatDate(subscription.expires_at)}</Typography></Stack>
      </SectionPanel>

      <SectionPanel>
        <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}><CreditCardRoundedIcon sx={{ color: "#1e6545" }} /><Box><Typography sx={{ fontWeight: 760 }}>使用会员激活码</Typography><Typography sx={{ color: "text.secondary", fontSize: 13 }}>输入从官方渠道获得的激活码，会员时间会立即增加。</Typography></Box></Stack>
        <Box sx={{ mt: 2 }}><FormActionRow field={<TextField size="small" label="会员激活码" value={code} onChange={(event) => setCode(event.target.value)} fullWidth />} action={<Button variant="contained" disabled={!code.trim()} onClick={() => void redeem()}>确认激活</Button>} maxWidth={620} /></Box>
      </SectionPanel>
    </Box>

    <Box sx={{ mb: 2.25 }}><Typography component="h2" sx={{ fontSize: 22, fontWeight: 780 }}>选择会员套餐</Typography><Typography sx={{ mt: 0.5, color: "text.secondary" }}>未到期时购买会从当前到期时间继续增加天数。</Typography></Box>
    <Box sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(min(100%, 280px), 320px))", gap: 2, alignItems: "stretch", justifyContent: { xs: "stretch", sm: "start" } }}>
      {plans.map((plan, index) => <PlanCard key={plan.id || index} plan={plan} featured={index === 1 || Boolean(plan.recommended)} paying={payingPlanID === plan.id} onPay={() => void pay(plan.id)} />)}
    </Box>

    <SectionPanel sx={{ mt: 3, bgcolor: "#f8faf8" }}>
      <Typography component="h2" sx={{ fontSize: 18, fontWeight: 760 }}>充值与退款说明</Typography>
      <Stack component="ol" spacing={0.8} sx={{ mt: 1.5, mb: 0, pl: 2.5, color: "text.secondary", lineHeight: 1.7 }}>
        <li>会员未到期时再次购买，新套餐天数会从当前到期时间继续增加。</li>
        <li>需要退款时，按套餐原价折算剩余天数，并扣除支付渠道产生的 5% 手续费。</li>
        <li>购买过永久会员或仍有未使用权益的用户，可联系作者抵扣升级；继续充值表示同意充值协议。</li>
      </Stack>
    </SectionPanel>

    <SectionPanel sx={{ mt: 2 }}>
      <Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>支付记录</Typography>
      {orders.length ? <Stack sx={{ mt: 1.5 }}>{orders.map((order) => <Stack key={order.order_no} direction={{ xs: "column", md: "row" }} spacing={1.5} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider", alignItems: { md: "center" } }}><Box sx={{ flex: 1, minWidth: 0 }}><Typography sx={{ fontWeight: 700 }}>{order.plan_name}</Typography><Typography noWrap sx={{ color: "text.secondary", fontSize: 12 }}>{order.order_no}</Typography></Box><Typography sx={{ minWidth: 80, fontWeight: 700 }}>￥{(Number(order.amount_cents || 0) / 100).toFixed(2)}</Typography><Chip size="small" color={order.status === "paid" ? "success" : "default"} label={statusText(order.status)} /><Typography sx={{ color: "text.secondary", fontSize: 13, minWidth: 168 }}>{formatDate(order.created_at)}</Typography></Stack>)}</Stack> : <EmptyState text={loading ? "正在读取支付记录" : "暂无支付记录"} />}
    </SectionPanel>
  </>;
}

/** PlanCard 展示一个固定宽度的会员套餐卡片。 */
function PlanCard({ plan, featured, paying, onPay }: { plan: any; featured: boolean; paying: boolean; onPay: () => void }) {
  const price = finalPrice(plan);
  const originalPrice = Number(plan.original_price || 0);
  return <Box component="article" sx={{ position: "relative", display: "flex", flexDirection: "column", minHeight: 390, p: 2.5, border: "1px solid", borderColor: featured ? "#b69957" : "divider", borderRadius: "8px", bgcolor: featured ? "#fffdf7" : "#fbfdfc", boxShadow: featured ? "0 12px 28px rgba(76, 61, 28, .09)" : "none" }}>
    {featured ? <Chip size="small" label="推荐" sx={{ position: "absolute", top: 14, right: 14, bgcolor: "#202a24", color: "#f5dfaa", fontWeight: 700 }} /> : null}
    <Typography component="h3" sx={{ pr: featured ? 7 : 0, fontSize: 20, fontWeight: 780 }}>{plan.name || "会员套餐"}</Typography>
    <Stack direction="row" spacing={1} sx={{ mt: 2, alignItems: "baseline" }}><Typography sx={{ fontSize: 36, lineHeight: 1, fontWeight: 850, color: featured ? "#80621f" : "text.primary" }}>￥{price}</Typography>{originalPrice > price ? <Typography sx={{ color: "text.disabled", textDecoration: "line-through" }}>￥{originalPrice}</Typography> : null}</Stack>
    <Typography sx={{ mt: 1.5, color: "text.secondary", lineHeight: 1.65, minHeight: 52 }}>{plan.description || "解锁更多智能招聘功能。"}</Typography>
    <Stack spacing={1.1} sx={{ mt: 2.25, flex: 1 }}>{(Array.isArray(plan.features) ? plan.features : []).map((feature: string) => <Stack key={feature} direction="row" spacing={1} sx={{ alignItems: "flex-start" }}><CheckRoundedIcon sx={{ mt: 0.15, color: featured ? "#9c7b32" : "#1e6545", fontSize: 19 }} /><Typography sx={{ fontSize: 14, lineHeight: 1.55 }}>{feature}</Typography></Stack>)}</Stack>
    {originalPrice > 0 ? <Button fullWidth variant={featured ? "contained" : "outlined"} disabled={paying} sx={{ mt: 2.5, bgcolor: featured ? "#202a24" : undefined, color: featured ? "#f8e6b8" : undefined, "&:hover": { bgcolor: featured ? "#2b382f" : undefined } }} onClick={onPay}>{paying ? "正在创建订单" : "立即订阅"}</Button> : <Chip label="永久免费" sx={{ mt: 2.5, alignSelf: "flex-start", bgcolor: "#eaf3ed", color: "#1e6545", fontWeight: 700 }} />}
  </Box>;
}

/** finalPrice 计算套餐折扣后的最终售价。 */
function finalPrice(plan: any) {
  return Math.max(0, Number(plan?.original_price || 0) - Number(plan?.discount_amount || 0));
}

/** statusText 将支付状态转换成中文。 */
function statusText(status: string) {
  return status === "paid" ? "已支付" : status === "closed" ? "已关闭" : "待支付";
}

/** submitPayment 创建并提交第三方支付表单。 */
function submitPayment(payment: any) {
  if (!payment?.submit_url) throw new Error("支付平台没有返回可打开的支付地址");
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
