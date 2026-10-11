/** 本文件负责 AI 余额流水展示 的 Ant Design 页面。 */
"use client";
import { Flex, Pagination, Table, Tag, Typography } from "antd";
import styles from "./page.module.css";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AIWalletChange from "@/components/payment/AIWalletChange";
import { cloudRequest, formatDate } from "@/lib/admin-api";
const pageSize = 20;
/** AIRecordsPage 展示当前用户或超管指定用户的 AI 使用记录。 */
export default function AIRecordsPage() {
  const { user, notify } = useAdmin();
  const searchParams = useSearchParams();
  const requestedEmail = String(searchParams.get("email") || "").trim();
  const canViewTarget = user?.role === "super_admin" && requestedEmail;
  const [records, setRecords] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const query = useMemo(() => {
    const params = new URLSearchParams({
      page: String(page),
      page_size: String(pageSize),
    });
    if (canViewTarget) params.set("email", requestedEmail);
    return params.toString();
  }, [canViewTarget, page, requestedEmail]);
  /** load 读取 AI 余额流水记录。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest(`/api/ai-wallet/records?${query}`);
      setRecords(data.records || []);
      setTotal(Number(data.total || 0));
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "AI 使用记录读取失败，我再试也得先缓缓。",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, [query]);
  const title = canViewTarget ? "AI 使用记录" : "我的 AI 使用记录";
  const description = canViewTarget
    ? `正在查看 ${requestedEmail} 的内置 AI 余额流水。`
    : "这里记录内置 AI 的充值、赠送、调整和调用扣费。";
  return (
    <>
      <PageHeader
        title={title}
        description={description}
        actions={
          <RefreshButton loading={loading} onClick={() => void load()} />
        }
      />
      <SectionPanel>
        {records.length ? (
          <>
            <div className={styles.layout2}>
              <Table
                rowKey={(record) =>
                  record.id || record.created_at + record.reason
                }
                dataSource={records}
                loading={loading}
                pagination={false}
                size="small"
                scroll={{ x: 880 }}
                columns={[
                  {
                    key: "category",
                    title: "类型",
                    render: (_, record) => (
                      <CategoryChip category={record.category} />
                    ),
                  },
                  {
                    key: "amount",
                    title: "余额变动",
                    render: (_, record) => (
                      <AIWalletChange record={record} />
                    ),
                  },
                  {
                    key: "balance",
                    title: "余额",
                    render: (_, record) =>
                      "￥" + (record.balance_after || "0.00"),
                  },
                  {
                    key: "token",
                    title: "模型 / Token",
                    render: (_, record) => <TokenInfo record={record} />,
                  },
                  {
                    key: "reason",
                    title: "说明",
                    render: (_, record) => <ReasonText record={record} />,
                  },
                  {
                    key: "date",
                    title: "时间",
                    render: (_, record) => formatDate(record.created_at),
                  },
                ]}
              />
            </div>
            <Flex className={styles.layout3} vertical={true} gap={1.25 * 8}>
              {records.map((record) => (
                <RecordCard
                  key={record.id || `${record.created_at}-${record.reason}`}
                  record={record}
                />
              ))}
            </Flex>
            <Pagination
              current={page}
              total={total}
              pageSize={pageSize}
              onChange={(value) => setPage(value)}
              showSizeChanger={false}
              style={{
                marginTop: 16,
                display: "flex",
                justifyContent: "center",
              }}
            />
          </>
        ) : (
          <EmptyState
            text={
              loading
                ? "正在读取 AI 使用记录"
                : "这里暂时空空的，等你开始使用内置 AI 后我再认真记账。"
            }
          />
        )}
      </SectionPanel>
    </>
  );
}
/** RecordRow 展示桌面端单条 AI 记录。 */
/** RecordCard 展示移动端单条 AI 记录。 */
function RecordCard({ record }: { record: any }) {
  return (
    <div
      style={{
        padding: 12,
        border: "1px solid",
        borderColor: "var(--goodhr-border)",
        borderRadius: "8px",
        backgroundColor: "var(--goodhr-hover)",
      }}
    >
      <Flex style={{ justifyContent: "space-between", gap: 8 }}>
        <CategoryChip category={record.category} />
        <AIWalletChange record={record} />
      </Flex>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginTop: 8,
          color: "var(--goodhr-text-secondary)",
          fontSize: 13,
        }}
      >
        <ReasonText record={record} />
      </Typography.Text>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginTop: 6,
          color: "var(--goodhr-text-secondary)",
          fontSize: 12,
        }}
      >
        <TokenInfo record={record} />· 余额 ￥{record.balance_after || "0.00"} ·{" "}
        {formatDate(record.created_at)}
      </Typography.Text>
    </div>
  );
}
/** CategoryChip 展示流水类别。 */
function CategoryChip({ category }: { category: string }) {
  const labelMap: Record<string, string> = {
    ai_usage: "AI调用",
    recharge: "充值",
    refund: "退款",
    refund_release: "退款退回",
    signup_bonus: "注册赠送",
    admin_adjust: "后台调整",
  };
  const color =
    category === "ai_usage"
      ? "warning"
      : category === "admin_adjust"
        ? "processing"
        : "success";
  return (
    <Tag color={color as any}>{labelMap[category] || category || "记录"}</Tag>
  );
}
/** TokenInfo 展示模型和 token 用量。 */
function TokenInfo({ record }: { record: any }) {
  const tokens = Number(record.total_tokens || 0);
  return (
    <>
      {record.model_id || "无模型"}
      {tokens ? ` / ${tokens} tokens` : ""}
    </>
  );
}
/** ReasonText 展示记录说明和关联订单号。 */
function ReasonText({ record }: { record: any }) {
  return (
    <>
      {record.reason || "暂无说明"}
      {record.related_order_no ? `（订单 ${record.related_order_no}）` : ""}
    </>
  );
}
