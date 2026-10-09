/** 本文件负责 管理员运行日志 的 Ant Design 页面。 */
"use client";
import dayjs from "dayjs";
import {
  Alert,
  Button,
  Collapse,
  DatePicker,
  Flex,
  Form,
  Input,
  Pagination,
  Select,
  Table,
  Tag,
  Typography,
} from "antd";
import styles from "./page.module.css";
import { useEffect, useState } from "react";
import { useAdmin } from "@/components/admin/AdminApp";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { cloudRequest } from "@/lib/admin-api";
import AIResponseDiagnosticDetails, {
  ordinaryDiagnosticOutput,
} from "@/components/admin/AIResponseDiagnosticDetails";
type LogEvent = {
  message?: string;
  error_message?: string;
  exit_reason?: string;
  last_action?: string;
  page_url?: string;
  action?: string;
  step?: string;
  status?: string;
  trace_id?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
};
type LogItem = {
  id: string;
  account: string;
  position_name: string;
  position_id: string;
  category: string;
  message: string;
  created_at: string;
  event?: LogEvent;
};
type Filters = {
  category: string;
  account: string;
  position: string;
  from: string;
  to: string;
};
const emptyFilters: Filters = {
  category: "",
  account: "",
  position: "",
  from: "",
  to: "",
};
const logActionLabels: Record<string, string> = {
  "browser.start": "启动浏览器",
  "browser.stop": "关闭浏览器",
  "browser.exit": "浏览器异常退出",
  "worker.exit": "浏览器工具异常退出",
  "agent.start": "启动本地程序",
  "agent.exit": "退出本地程序",
  "agent.previous_exit": "上次本地程序异常退出",
  start_browser: "启动浏览器",
  stop_browser: "关闭浏览器",
};
/** toISOString 将本地输入的时间转换为云端筛选使用的时间。 */
function toISOString(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}
/** logText 只读取文本字段，避免把空值或对象显示为日志正文。 */
function logText(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}
/** logEvent 兼容已解析的事件和旧接口返回的 JSON 字符串。 */
function logEvent(item: LogItem): LogEvent | undefined {
  if (
    item.event &&
    typeof item.event === "object" &&
    !Array.isArray(item.event)
  )
    return item.event;
  try {
    const parsed: unknown = JSON.parse(item.message);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed))
      return parsed as LogEvent;
  } catch {
    // 普通文本日志直接作为正文展示，无需按 JSON 解析。
  }
  return undefined;
}
/** readableMessage 优先展示错误或流程短句，将浏览器生命周期事件翻译成中文。 */
function readableMessage(item: LogItem, event?: LogEvent): string {
  const error = logText(event?.error_message);
  const message = logText(event?.message);
  if (item.category !== "运行" && error) return error;
  if (message) return message;
  if (error || logText(event?.exit_reason))
    return error || logText(event?.exit_reason);
  const action =
    logActionLabels[logText(event?.action)] ||
    logActionLabels[logText(event?.step)];
  if (action) {
    const status = logText(event?.status) || logText(event?.output?.status);
    if (status === "success") return `${action}成功`;
    if (status === "failed" || status === "error") return `${action}失败`;
    return action;
  }
  if (logText(event?.last_action))
    return `最后操作：${logText(event?.last_action)}`;
  return event ? "运行记录，展开查看详情" : logText(item.message) || "运行记录";
}
/** formatLogTime 显示电脑本地时区下的完整日期和时间，精确到秒。 */
function formatLogTime(value: string): string {
  const date = new Date(value);
  if (!value || Number.isNaN(date.getTime())) return "--";
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}
/** LogDetails 展示完整日志和上下文，并允许展开原始日志。 */
function LogDetails({ item }: { item: LogItem }) {
  const event = logEvent(item);
  const message = readableMessage(item, event);
  return (
    <Flex vertical gap={10} style={{ overflowWrap: "anywhere" }}>
      <Typography.Text style={{ whiteSpace: "pre-wrap" }}>
        日志内容：{message}
      </Typography.Text>
      <Typography.Text type="secondary">
        账号：{item.account || "--"}　岗位：{item.position_name || "无岗位"}
      </Typography.Text>
      {logText(event?.error_message) && (
        <Alert type="error" showIcon title={logText(event?.error_message)} />
      )}
      {(
        ["exit_reason", "last_action", "page_url", "action", "step"] as const
      ).map((key) =>
        logText(event?.[key]) ? (
          <Typography.Text key={key}>
            {
              {
                exit_reason: "退出原因",
                last_action: "退出前操作",
                page_url: "页面",
                action: "动作",
                step: "步骤",
              }[key]
            }
            ：{logText(event?.[key])}
          </Typography.Text>
        ) : null,
      )}
      {logText(event?.trace_id) && (
        <Typography.Text type="secondary">
          跟踪编号：{logText(event?.trace_id)}
        </Typography.Text>
      )}
      <AIResponseDiagnosticDetails output={event?.output} />
      {(event?.input || event?.output) && (
        <Flex gap={16} wrap>
          {(["input", "output"] as const).map((field) => (
            <div key={field} style={{ flex: "1 1 300px", minWidth: 0 }}>
              <Typography.Text strong>
                {field === "input" ? "入参" : "出参"}
              </Typography.Text>
              <pre
                style={{
                  margin: 0,
                  padding: 10,
                  maxHeight: 260,
                  overflow: "auto",
                  whiteSpace: "pre-wrap",
                  overflowWrap: "anywhere",
                  fontSize: 12,
                  background: "#fff",
                  borderRadius: 4,
                }}
              >
                {JSON.stringify(
                  field === "output"
                    ? ordinaryDiagnosticOutput(event?.output)
                    : event?.input || {},
                  null,
                  2,
                )}
              </pre>
            </div>
          ))}
        </Flex>
      )}
      {event && (
        <Collapse
          size="small"
          items={[
            {
              key: "raw",
              label: "查看原始日志",
              children: (
                <pre
                  style={{
                    maxHeight: 320,
                    overflow: "auto",
                    whiteSpace: "pre-wrap",
                    fontSize: 12,
                  }}
                >
                  {JSON.stringify(event, null, 2)}
                </pre>
              ),
            },
          ]}
        />
      )}
    </Flex>
  );
}
/** LogsPage 显示跨账号日志和异常退出详细信息。 */
export default function LogsPage() {
  const { user, notify } = useAdmin();
  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [applied, setApplied] = useState<Filters>(emptyFilters);
  const [items, setItems] = useState<LogItem[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  /** load 从云端读取指定页的全账号日志。 */
  async function load(nextPage: number, nextFilters: Filters) {
    setLoading(true);
    try {
      const params = new URLSearchParams({
        page: String(nextPage),
        page_size: "50",
      });
      if (nextFilters.category) params.set("category", nextFilters.category);
      if (nextFilters.account.trim())
        params.set("account", nextFilters.account.trim());
      if (nextFilters.position.trim())
        params.set("position", nextFilters.position.trim());
      if (nextFilters.from) params.set("from", toISOString(nextFilters.from));
      if (nextFilters.to) params.set("to", toISOString(nextFilters.to));
      const data = await cloudRequest(`/api/admin/logs?${params}`);
      setItems(Array.isArray(data.logs) ? data.logs : []);
      setTotal(Number(data.total || 0));
      setPage(nextPage);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "日志没读出来，请稍后再试",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    if (user?.role === "super_admin") void load(1, emptyFilters);
  }, [user?.role]);
  /** applyFilters 应用顶部筛选并回到第一页。 */
  function applyFilters() {
    setApplied(filters);
    void load(1, filters);
  }
  /** resetFilters 清空筛选条件并重新读取日志。 */
  function resetFilters() {
    setFilters(emptyFilters);
    setApplied(emptyFilters);
    void load(1, emptyFilters);
  }
  if (user?.role !== "super_admin")
    return (
      <Alert
        type={"warning"}
        showIcon={true}
        title={<>这里只给管理员看日志。</>}
      />
    );
  return (
    <Flex vertical={true} gap={2.5 * 8}>
      <PageHeader
        title="运行日志"
        description="按账号和岗位看看程序当时在忙什么，闪退原因也放在这里。"
      />
      <SectionPanel>
        <Typography.Title
          level={5}
          style={{ display: "block", margin: 0, marginBottom: 16 }}
        >
          筛选日志
        </Typography.Title>
        <Flex
          className={styles.layout19}
          gap={1.5 * 8}
          style={{ flexWrap: "wrap" }}
        >
          <Form.Item
            htmlFor="goodhr-logs-field-1"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"日志类型"}
          >
            <Select
              id="goodhr-logs-field-1"
              onChange={(value) => setFilters({ ...filters, category: value })}
              style={{ minWidth: 155, width: "100%" }}
              value={filters.category}
              size="small"
            >
              <Select.Option value="">全部</Select.Option>
              <Select.Option value="运行">运行</Select.Option>
              <Select.Option value="错误">错误</Select.Option>
              <Select.Option value="异常退出">异常退出</Select.Option>
            </Select>
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-logs-field-2"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"账号"}
          >
            <Input
              id="goodhr-logs-field-2"
              placeholder="邮箱关键词"
              value={filters.account}
              onChange={(event) =>
                setFilters({ ...filters, account: event.target.value })
              }
              size="small"
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-logs-field-3"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"岗位名称"}
          >
            <Input
              id="goodhr-logs-field-3"
              value={filters.position}
              onChange={(event) =>
                setFilters({ ...filters, position: event.target.value })
              }
              size="small"
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-logs-field-4"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"开始时间"}
          >
            <DatePicker
              id="goodhr-logs-field-4"
              showTime
              format="YYYY-MM-DD HH:mm"
              style={{ width: "100%" }}
              value={filters.from ? dayjs(filters.from) : null}
              onChange={(date) =>
                setFilters({
                  ...filters,
                  from: date ? date.format("YYYY-MM-DDTHH:mm") : "",
                })
              }
              size="small"
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-logs-field-5"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"结束时间"}
          >
            <DatePicker
              id="goodhr-logs-field-5"
              showTime
              format="YYYY-MM-DD HH:mm"
              style={{ width: "100%" }}
              value={filters.to ? dayjs(filters.to) : null}
              onChange={(date) =>
                setFilters({
                  ...filters,
                  to: date ? date.format("YYYY-MM-DDTHH:mm") : "",
                })
              }
              size="small"
            />
          </Form.Item>
          <Button type={"primary"} onClick={applyFilters} disabled={loading}>
            查询
          </Button>
          <Button type={"text"} onClick={resetFilters} disabled={loading}>
            清空
          </Button>
        </Flex>
      </SectionPanel>
      <SectionPanel>
        <Flex
          style={{
            alignItems: "center",
            justifyContent: "space-between",
            marginBottom: 12,
          }}
        >
          <Typography.Title level={5} style={{ display: "block", margin: 0 }}>
            日志记录（{total}）
          </Typography.Title>
          <Typography.Text
            style={{
              display: "block",
              margin: 0,
              color: "var(--goodhr-text-secondary)",
            }}
          >
            点击一行查看详情
          </Typography.Text>
        </Flex>
        {items.length === 0 ? (
          <EmptyState
            text={
              loading
                ? "正在读取日志，我马上整理好"
                : "这里暂时空空的，换个筛选条件再看看。"
            }
          />
        ) : (
          <Table<LogItem>
            rowKey="id"
            dataSource={items}
            loading={loading}
            pagination={false}
            size="small"
            scroll={{ x: 960 }}
            expandable={{
              expandRowByClick: true,
              expandedRowRender: (item) => <LogDetails item={item} />,
              columnTitle: "详情",
              columnWidth: 54,
            }}
            columns={[
              {
                title: "时间",
                dataIndex: "created_at",
                width: 176,
                render: formatLogTime,
              },
              {
                title: "类型",
                dataIndex: "category",
                width: 90,
                render: (category) => (
                  <Tag color={category !== "运行" ? "error" : "success"}>
                    {category || "运行"}
                  </Tag>
                ),
              },
              {
                title: "账号",
                dataIndex: "account",
                width: 200,
                ellipsis: true,
                render: (value) => value || "--",
              },
              {
                title: "岗位",
                dataIndex: "position_name",
                width: 172,
                ellipsis: true,
                render: (value) => value || "无岗位",
              },
              {
                title: "日志内容",
                key: "message",
                ellipsis: true,
                render: (_, item) => (
                  <Typography.Text
                    type={item.category !== "运行" ? "danger" : undefined}
                  >
                    {readableMessage(item, logEvent(item))}
                  </Typography.Text>
                ),
              },
            ]}
          />
        )}
        {total > 50 && (
          <Pagination
            current={page}
            total={total}
            pageSize={50}
            onChange={(nextPage) => void load(nextPage, applied)}
            showSizeChanger={false}
            style={{ marginTop: 16 }}
          />
        )}
      </SectionPanel>
    </Flex>
  );
}
