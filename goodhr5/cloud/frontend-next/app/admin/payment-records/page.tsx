/** 本文件负责超级管理员查看全部支付记录。 */
"use client";

import { Chip, Stack, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** PaymentRecordsPage 展示全部用户支付订单。 */
export default function PaymentRecordsPage() {
  const { user, notify } = useAdmin();
  const [orders, setOrders] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  /** load 读取超级管理员支付记录。 */
  async function load() { setLoading(true); try { const data = await cloudRequest("/api/admin/payment/orders"); setOrders(data.orders || []); } catch (error) { notify(error instanceof Error ? error.message : "支付记录读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { if (user?.role === "super_admin") void load(); }, [user]);
  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;
  return <><PageHeader title="支付记录" description="查看全部用户的订阅支付订单。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} /><SectionPanel>{orders.length ? <Stack>{orders.map((order) => <Stack key={order.order_no} direction={{ xs: "column", lg: "row" }} spacing={2} sx={{ py: 1.75, borderBottom: "1px solid", borderColor: "divider", alignItems: { lg: "center" } }}><Typography sx={{ flex: 1, fontWeight: 700 }}>{order.user_email}</Typography><Typography>{order.plan_name}</Typography><Typography sx={{ fontWeight: 760 }}>￥{(Number(order.amount_cents || 0) / 100).toFixed(2)}</Typography><Chip size="small" color={order.status === "paid" ? "success" : "default"} label={order.status === "paid" ? "已支付" : order.status === "closed" ? "已关闭" : "待支付"} /><Typography sx={{ fontFamily: "monospace", fontSize: 12 }}>{order.order_no}</Typography><Typography color="text.secondary">{formatDate(order.created_at)}</Typography></Stack>)}</Stack> : <EmptyState text={loading ? "正在读取支付记录" : "暂无支付记录"} />}</SectionPanel></>;
}
