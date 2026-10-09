/** 本文件负责只读排障链接的错误置顶、搜索筛选和结构化入参出参展示。 */
"use client";

import {
  Alert,
  Button,
  Card,
  Collapse,
  Flex,
  Input,
  Spin,
  Typography,
} from "antd";
import "@/components/public-site.css";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import AIResponseDiagnosticDetails, {
  ordinaryDiagnosticOutput,
} from "@/components/admin/AIResponseDiagnosticDetails";

type DiagnosticEvent = {
  timestamp: string;
  category: string;
  is_error: boolean;
  error_message: string;
  input: Record<string, unknown>;
  output: Record<string, unknown>;
  message: string;
  task_id?: string;
  action?: string;
  step?: string;
  status?: string;
  duration_ms?: number;
};

type SharedLog = {
  id: string;
  level: string;
  message: string;
  created_at: string;
  event?: DiagnosticEvent;
};

/** DiagnosticShareView 加载固定快照，并优先显示错误与退出原因。 */
export default function DiagnosticShareView() {
  const params = useParams<{ token: string }>();
  const token = String(params?.token || "");
  const [logs, setLogs] = useState<SharedLog[]>([]);
  const [expiresAt, setExpiresAt] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [onlyErrors, setOnlyErrors] = useState(false);
  const [query, setQuery] = useState("");
  const [visibleCount, setVisibleCount] = useState(100);

  useEffect(() => {
    if (!/^[a-f0-9]{64}$/.test(token)) {
      setError("这个排障链接不完整，可以请对方重新复制。");
      setLoading(false);
      return;
    }
    let active = true;
    cloudRequest(`/api/public/position-log-shares/${token}`, { auth: false })
      .then((data) => {
        if (active) {
          setLogs(Array.isArray(data.logs) ? data.logs : []);
          setExpiresAt(String(data.expires_at || ""));
        }
      })
      .catch(() => {
        if (active) setError("链接已过期或被新链接替换，请对方重新复制。");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [token]);

  const ordered = useMemo(
    () =>
      [...logs].sort(
        (left, right) =>
          Number(Boolean(right.event?.is_error || right.level === "error")) -
            Number(Boolean(left.event?.is_error || left.level === "error")) ||
          Date.parse(right.created_at) - Date.parse(left.created_at),
      ),
    [logs],
  );
  const errors = useMemo(
    () =>
      ordered.filter((item) => item.event?.is_error || item.level === "error"),
    [ordered],
  );
  const filtered = useMemo(
    () =>
      (onlyErrors ? errors : ordered).filter((item) => {
        const text = [
          item.event?.message,
          item.event?.error_message,
          item.event?.action,
          item.event?.step,
          item.message,
        ]
          .join(" ")
          .toLowerCase();
        return text.includes(query.trim().toLowerCase());
      }),
    [onlyErrors, errors, ordered, query],
  );

  return (
    <main className="public-container">
      <Flex vertical gap={24} className="public-diagnostic-layout">
        <header>
          <Typography.Title
            level={1}
            style={{ margin: 0, fontSize: 32, fontWeight: 760 }}
          >
            岗位排障日志
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ margin: "8px 0 0" }}>
            这是创建链接时保存的只读日志快照。错误信息排在前面，入参和出参可按需展开。
          </Typography.Paragraph>
        </header>
        {loading ? (
          <Flex gap={8} align="center">
            <Spin size="small" />
            <Typography.Text>正在读取日志</Typography.Text>
          </Flex>
        ) : null}
        {error ? <Alert type="warning" title={error} showIcon /> : null}
        {!loading && !error ? (
          <>
            <Card
              className="public-diagnostic-panel"
              style={{ padding: 0 }}
              styles={{ body: { padding: 16 } }}
            >
              <Flex vertical gap={16}>
                <Flex wrap gap={16} align="center" justify="space-between">
                  <Typography.Paragraph style={{ margin: 0 }}>
                    共 {ordered.length} 条，错误 {errors.length} 条。链接有效至{" "}
                    {formatTime(expiresAt)}。
                  </Typography.Paragraph>
                  <Flex wrap gap={8} align="center" justify="flex-end">
                    <Button
                      type={onlyErrors ? "default" : "primary"}
                      onClick={() => {
                        setOnlyErrors(false);
                        setVisibleCount(100);
                      }}
                    >
                      全部日志
                    </Button>
                    <Button
                      type={onlyErrors ? "primary" : "default"}
                      danger
                      onClick={() => {
                        setOnlyErrors(true);
                        setVisibleCount(100);
                      }}
                    >
                      只看错误
                    </Button>
                  </Flex>
                </Flex>
                <Input
                  aria-label="搜索错误、方法或步骤"
                  value={query}
                  onChange={(event) => {
                    setQuery(event.target.value);
                    setVisibleCount(100);
                  }}
                  placeholder="搜索错误、方法或步骤"
                />
              </Flex>
            </Card>
            {errors.length > 0 && !onlyErrors && !query ? (
              <Alert
                type="error"
                title={
                  <>
                    最近一条错误：
                    {errors[0].event?.error_message ||
                      errors[0].event?.message ||
                      errors[0].message}
                  </>
                }
                showIcon
              />
            ) : null}
            <Flex vertical gap={12}>
              {filtered.slice(0, visibleCount).map((item, index) => (
                <DiagnosticLine
                  key={`${item.id || item.created_at}-${index}`}
                  item={item}
                />
              ))}
              {filtered.length === 0 ? (
                <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
                  这里没有符合条件的日志。
                </Typography.Paragraph>
              ) : null}
              {filtered.length > visibleCount ? (
                <Button onClick={() => setVisibleCount((count) => count + 100)}>
                  再显示 100 条（还剩 {filtered.length - visibleCount} 条）
                </Button>
              ) : null}
            </Flex>
          </>
        ) : null}
      </Flex>
    </main>
  );
}

/** DiagnosticLine 显示单条日志，错误正文直接可见，较长的参数默认折叠。 */
function DiagnosticLine({ item }: { item: SharedLog }) {
  const event = item.event;
  const isError = Boolean(event?.is_error || item.level === "error");
  return (
    <Card
      className="public-diagnostic-panel"
      style={{ padding: 0, borderColor: isError ? "#c83f49" : undefined }}
      styles={{ body: { padding: 16 } }}
    >
      <Flex vertical gap={8} style={{ minWidth: 0 }}>
        <Flex wrap gap={8} align="center">
          <Typography.Text
            type="secondary"
            style={{ fontSize: 12, minWidth: 205 }}
          >
            {formatTime(event?.timestamp || item.created_at)}
          </Typography.Text>
          <Typography.Text
            type={isError ? "danger" : "secondary"}
            style={{ fontSize: 12, fontWeight: 700 }}
          >
            {isError ? "错误" : "正常"}
          </Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {diagnosticActionLabel(event)}
            {event?.duration_ms != null ? ` · ${event.duration_ms}ms` : ""}
          </Typography.Text>
        </Flex>
        <Typography.Paragraph
          style={{
            margin: 0,
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            fontWeight: isError ? 650 : 400,
          }}
        >
          {event?.message || item.message}
        </Typography.Paragraph>
        {isError &&
        event?.error_message &&
        event.error_message !== event.message ? (
          <Alert type="error" title={event.error_message} showIcon />
        ) : null}
        {event ? <AIResponseDiagnosticDetails output={event.output} /> : null}
        {event ? (
          <Collapse
            ghost
            size="small"
            style={{ marginTop: 4, minWidth: 0, fontSize: 13 }}
            items={[
              {
                key: "parameters",
                label: (
                  <Typography.Text
                    style={{
                      color: "var(--goodhr-primary, #159a62)",
                      fontSize: 13,
                    }}
                  >
                    查看入参、出参和任务编号
                  </Typography.Text>
                ),
                children: (
                  <Flex vertical gap={8} style={{ minWidth: 0 }}>
                    <DataBlock label="入参" value={event.input} />
                    <DataBlock
                      label="出参"
                      value={ordinaryDiagnosticOutput(event.output)}
                    />
                    {event.task_id ? (
                      <Typography.Paragraph
                        type="secondary"
                        style={{ margin: 0, fontSize: 12 }}
                      >
                        任务编号：{event.task_id}
                      </Typography.Paragraph>
                    ) : null}
                  </Flex>
                ),
              },
            ]}
          />
        ) : null}
      </Flex>
    </Card>
  );
}

/** diagnosticActionLabel 将内部流程代号转换成分享页可读的中文说明。 */
function diagnosticActionLabel(event?: DiagnosticEvent) {
  if (!event) return "旧日志";
  if (event.action === "candidate") return event.step || "候选人处理";
  const labels: Record<string, string> = {
    lifecycle: "任务流程",
    greeting: "打招呼流程",
    auto_reply: "自动回复流程",
    browser_worker: "浏览器操作",
    platform: "招聘页面",
    preflight: "启动检查",
  };
  return labels[event.action || ""] || event.step || "岗位流程";
}

/** DataBlock 格式化显示已脱敏的 JSON 摘要。 */
function DataBlock({
  label,
  value,
}: {
  label: string;
  value: Record<string, unknown>;
}) {
  return (
    <Flex vertical style={{ minWidth: 0 }}>
      <Typography.Text strong style={{ fontSize: 12 }}>
        {label}
      </Typography.Text>
      <pre
        style={{
          margin: 0,
          padding: 8,
          background: "var(--goodhr-hover, #f2f7f4)",
          overflowX: "auto",
          fontSize: 12,
        }}
      >
        {JSON.stringify(value || {}, null, 2)}
      </pre>
    </Flex>
  );
}

/** formatTime 把 ISO 时间显示为本地可读时间。 */
function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "时间未知"
    : date.toLocaleString("zh-CN", { hour12: false });
}
