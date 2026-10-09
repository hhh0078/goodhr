/** 本文件负责 团队招聘统计 的 Ant Design 页面。 */
"use client";
import dayjs from "dayjs";
import {
  Button,
  Card,
  DatePicker,
  Flex,
  Form,
  Input,
  Select,
  Statistic,
  Table,
  Typography,
} from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import { useEffect, useMemo, useState } from "react";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest } from "@/lib/admin-api";
const periodOptions = [
  { value: "month", label: "本月" },
  { value: "today", label: "今天" },
  { value: "week", label: "本周" },
  { value: "last_month", label: "上月" },
  { value: "custom", label: "自定义" },
];
const metricFields = [
  ["greeted_count", "打招呼"],
  ["resume_count", "新增简历"],
  ["scanned_count", "扫描人数"],
  ["detail_count", "获取详情"],
  ["skipped_count", "跳过"],
  ["failed_count", "失败"],
  ["position_count", "岗位运行数"],
];
/** TeamStatsPage 展示团队招聘统计。 */
export default function TeamStatsPage() {
  const { notify } = useAdmin();
  const [period, setPeriod] = useState("month");
  const [startDate, setStartDate] = useState("");
  const [endDate, setEndDate] = useState("");
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any>({ totals: {}, members: [] });
  const members = useMemo(
    () => (Array.isArray(data.members) ? data.members : []),
    [data.members],
  );
  /** load 读取团队统计数据。 */
  async function load() {
    setLoading(true);
    try {
      const query = new URLSearchParams({ period });
      if (period === "custom") {
        if (startDate) query.set("start_date", startDate);
        if (endDate) query.set("end_date", endDate);
      }
      const next = await cloudRequest(`/api/team/stats?${query}`);
      setData(next);
      if (!startDate) setStartDate(next.start_date || "");
      if (!endDate) setEndDate(next.end_date || "");
    } catch (error) {
      notify(error instanceof Error ? error.message : "统计读取失败", "error");
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, [period]);
  return (
    <>
      <PageHeader
        title="团队统计"
        description={`${data.start_date || ""} 至 ${data.end_date || ""}，我先把大家的关键数字摆齐。`}
        actions={
          <Button
            icon={<ReloadOutlined aria-hidden="true" />}
            disabled={loading}
            onClick={() => void load()}
          >
            {loading ? "刷新中" : "刷新"}
          </Button>
        }
      />
      <SectionPanel style={{ marginBottom: 12 }}>
        <div
          className={styles.layout2}
          style={{
            display: "grid",
            gridTemplateColumns:
              period === "custom" ? "180px 180px 180px auto" : "180px auto",
            gap: 10,
            alignItems: "center",
          }}
        >
          <Form.Item
            htmlFor="goodhr-team-stats-field-1"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"时间周期"}
          >
            <Select
              id="goodhr-team-stats-field-1"
              onChange={(value) => setPeriod(value)}
              style={{ width: "100%" }}
              size="small"
              value={period}
            >
              {periodOptions.map((item) => (
                <Select.Option key={item.value} value={item.value}>
                  {item.label}
                </Select.Option>
              ))}
            </Select>
          </Form.Item>
          {period === "custom" ? (
            <>
              <Form.Item
                htmlFor="goodhr-team-stats-field-2"
                layout="vertical"
                style={{ marginBottom: 0, minWidth: 0 }}
                label={"开始日期"}
              >
                <DatePicker
                  id="goodhr-team-stats-field-2"
                  style={{ width: "100%" }}
                  size="small"
                  value={startDate ? dayjs(startDate) : null}
                  onChange={(date) =>
                    setStartDate(date ? date.format("YYYY-MM-DD") : "")
                  }
                />
              </Form.Item>
              <Form.Item
                htmlFor="goodhr-team-stats-field-3"
                layout="vertical"
                style={{ marginBottom: 0, minWidth: 0 }}
                label={"结束日期"}
              >
                <DatePicker
                  id="goodhr-team-stats-field-3"
                  style={{ width: "100%" }}
                  size="small"
                  value={endDate ? dayjs(endDate) : null}
                  onChange={(date) =>
                    setEndDate(date ? date.format("YYYY-MM-DD") : "")
                  }
                />
              </Form.Item>
            </>
          ) : null}
          <Button
            className={styles.layout1}
            type={"primary"}
            disabled={loading}
            onClick={() => void load()}
          >
            查询
          </Button>
        </div>
      </SectionPanel>
      <div
        className={styles.layout4}
        style={{ display: "grid", gap: 10, marginBottom: 12 }}
      >
        {metricFields.map(([key, label]) => (
          <MetricCard
            key={key}
            label={label}
            value={Number(data.totals?.[key] || 0)}
          />
        ))}
      </div>
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <Table
          rowKey="email"
          dataSource={members}
          loading={loading}
          size="small"
          pagination={false}
          scroll={{ x: 1020 }}
          locale={{
            emptyText: (
              <EmptyState text="这里暂时空空的，等团队跑起来我再认真记账" />
            ),
          }}
          columns={[
            {
              key: "email",
              title: "员工",
              dataIndex: "email",
              width: 240,
              render: (email) => email || "未命名员工",
            },
            ...metricFields.map(([key, label]) => ({
              key,
              title: label,
              dataIndex: key,
              align: "right" as const,
              render: (value: unknown) => Number(value || 0),
            })),
          ]}
        />
      </SectionPanel>
    </>
  );
}
/** MetricCard 展示单个统计数字。 */
/** MetricCard 使用统计组件展示单项数字。 */
function MetricCard({ label, value }: { label: string; value: number }) {
  return (
    <Card size="small">
      <Statistic title={label} value={value} />
    </Card>
  );
}
