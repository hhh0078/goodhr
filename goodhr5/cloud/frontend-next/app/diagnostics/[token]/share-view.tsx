/** 本文件负责只读排障链接的错误置顶、搜索筛选和结构化入参出参展示。 */
"use client";

import { Alert, Button, Input, Spin } from "antd";
import "@/components/public-site.css";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";

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
    <main className="public-container public-diagnostic-layout">
      <header>
        <h1 style={{ margin: 0, fontSize: 32, fontWeight: 760 }}>
          岗位排障日志
        </h1>
        <p className="public-diagnostic-muted">
          这是创建链接时保存的只读日志快照。错误信息排在前面，入参和出参可按需展开。
        </p>
      </header>
      {loading ? (
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <Spin size="small" />
          <span>正在读取日志</span>
        </div>
      ) : null}
      {error ? <Alert type="warning" title={error} showIcon /> : null}
      {!loading && !error ? (
        <>
          <section className="public-diagnostic-panel">
            <div className="public-diagnostic-toolbar">
              <p style={{ margin: 0 }}>
                共 {ordered.length} 条，错误 {errors.length} 条。链接有效至{" "}
                {formatTime(expiresAt)}。
              </p>
              <div className="public-login-actions">
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
              </div>
            </div>
            <Input
              aria-label="搜索错误、方法或步骤"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setVisibleCount(100);
              }}
              placeholder="搜索错误、方法或步骤"
              style={{ marginTop: 16 }}
            />
          </section>
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
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            {filtered.slice(0, visibleCount).map((item, index) => (
              <DiagnosticLine
                key={`${item.id || item.created_at}-${index}`}
                item={item}
              />
            ))}
            {filtered.length === 0 ? (
              <p className="public-diagnostic-muted">
                这里没有符合条件的日志。
              </p>
            ) : null}
            {filtered.length > visibleCount ? (
              <Button onClick={() => setVisibleCount((count) => count + 100)}>
                再显示 100 条（还剩 {filtered.length - visibleCount} 条）
              </Button>
            ) : null}
          </div>
        </>
      ) : null}
    </main>
  );
}

/** DiagnosticLine 显示单条日志，错误正文直接可见，较长的参数默认折叠。 */
function DiagnosticLine({ item }: { item: SharedLog }) {
  const event = item.event;
  const isError = Boolean(event?.is_error || item.level === "error");
  return (
    <article
      className="public-diagnostic-panel"
      style={{ borderColor: isError ? "#c83f49" : undefined }}
    >
      <div
        className="public-diagnostic-toolbar"
        style={{
          fontSize: 12,
          justifyContent: "flex-start",
          gap: 8,
          color: "var(--goodhr-text-secondary, #637069)",
        }}
      >
        <time style={{ minWidth: 205 }}>
          {formatTime(event?.timestamp || item.created_at)}
        </time>
        <span
          style={{ fontWeight: 700, color: isError ? "#c83f49" : undefined }}
        >
          {isError ? "错误" : "正常"}
        </span>
        <span>
          {diagnosticActionLabel(event)}
          {event?.duration_ms != null ? ` · ${event.duration_ms}ms` : ""}
        </span>
      </div>
      <p
        style={{
          margin: "8px 0 0",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          fontWeight: isError ? 650 : 400,
        }}
      >
        {event?.message || item.message}
      </p>
      {isError &&
      event?.error_message &&
      event.error_message !== event.message ? (
        <Alert
          type="error"
          title={event.error_message}
          showIcon
          style={{ marginTop: 8 }}
        />
      ) : null}
      {event ? (
        <details style={{ marginTop: 12, fontSize: 13 }}>
          <summary
            style={{
              cursor: "pointer",
              color: "var(--goodhr-primary, #159a62)",
            }}
          >
            查看入参、出参和任务编号
          </summary>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 8,
              marginTop: 8,
            }}
          >
            <DataBlock label="入参" value={event.input} />
            <DataBlock label="出参" value={event.output} />
            {event.task_id ? (
              <p className="public-diagnostic-muted" style={{ fontSize: 12 }}>
                任务编号：{event.task_id}
              </p>
            ) : null}
          </div>
        </details>
      ) : null}
    </article>
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
    <div>
      <p style={{ margin: 0, fontWeight: 700, fontSize: 12 }}>{label}</p>
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
    </div>
  );
}

/** formatTime 把 ISO 时间显示为本地可读时间。 */
function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "时间未知"
    : date.toLocaleString("zh-CN", { hour12: false });
}
