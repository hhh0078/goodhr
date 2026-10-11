/** 本文件负责 支付记录 的 Ant Design 页面。 */
"use client";
import { Flex, Table, Tag, Typography } from "antd";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
/** PaymentRecordsPage 展示全部用户支付订单。 */
export default function PaymentRecordsPage() {
  const { user, notify } = useAdmin();
  const [orders, setOrders] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  /** load 读取超级管理员支付记录。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/admin/payment/orders");
      setOrders(data.orders || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "支付记录读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    if (user?.role === "super_admin") void load();
  }, [user]);
  if (user?.role !== "super_admin")
    return (
      <SectionPanel>
        <EmptyState text="只有超级管理员可以访问此页面" />
      </SectionPanel>
    );
  return (
    <>
      <PageHeader
        title="支付记录"
        description="查看全部用户的会员和 AI 余额支付订单，实付与赠送分开记账。"
        actions={
          <RefreshButton loading={loading} onClick={() => void load()} />
        }
      />
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <Table
          rowKey="order_no"
          dataSource={orders}
          loading={loading}
          pagination={{ pageSize: 20 }}
          size="small"
          scroll={{ x: 1000 }}
          columns={[
            { title: "用户邮箱", dataIndex: "user_email", ellipsis: true },
            { title: "套餐", dataIndex: "plan_name" },
            {
              title: "实付金额",
              dataIndex: "amount_cents",
              render: (amount) => "￥" + (Number(amount || 0) / 100).toFixed(2),
            },
            {
              title: "赠送余额",
              dataIndex: "bonus_amount_cents",
              render: (amount) => Number(amount || 0) > 0
                ? "￥" + (Number(amount) / 100).toFixed(2) : "—",
            },
            {
              title: "状态",
              dataIndex: "status",
              render: (status) => (
                <Tag color={status === "paid" ? "success" : undefined}>
                  {status === "paid"
                    ? "已支付"
                    : status === "closed"
                      ? "已关闭"
                      : "待支付"}
                </Tag>
              ),
            },
            { title: "订单号", dataIndex: "order_no", ellipsis: true },
            { title: "创建时间", dataIndex: "created_at", render: formatDate },
          ]}
        />
      </SectionPanel>
    </>
  );
}
