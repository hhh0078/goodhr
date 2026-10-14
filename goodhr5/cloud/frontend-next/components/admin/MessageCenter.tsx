/** 文件作用说明：在后台右上角显示个人消息，串行检查未读状态，展开正文后标记已读。 */
"use client";
import { Alert, Badge, Button, Collapse, Empty, Flex, Pagination, Popover, Spin, Typography } from "antd";
import { BellOutlined } from "@ant-design/icons";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";

type MessageItem = { id: string; title: string; body?: string; action_url?: string; created_at?: string; read_at?: string };
/** safeMessageAction 仅允许站内后台路径，消息正文不作为 HTML 或外部跳转执行。 */
function safeMessageAction(value?: string) { return value?.startsWith("/admin/") && !value.includes("\\") ? value : ""; }

/** MessageCenter 使用当前账号独立状态，离开页面或切号时取消旧请求。 */
export default function MessageCenter({ userKey }: { userKey: string }) {
  const [open, setOpen] = useState(false);
  const [count, setCount] = useState(0);
  const [version, setVersion] = useState("");
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [messages, setMessages] = useState<MessageItem[]>([]);
  const [details, setDetails] = useState<Record<string, MessageItem>>({});
  const [expanded, setExpanded] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [detailErrors, setDetailErrors] = useState<Record<string, string>>({});
  const [refresh, setRefresh] = useState(0);
  const generation = useRef(0);
  const requests = useRef(new Map<string, AbortController>());
  const acceptedVersion = useRef(-1);

  /** applySummary 忽略晚返回的旧计数，防止展开消息后红点重新变多。 */
  function applySummary(summary: { unread_count?: number; version?: number | string }) {
    const nextVersion = Number(summary.version);
    if (!Number.isSafeInteger(nextVersion) || nextVersion < acceptedVersion.current) return;
    acceptedVersion.current = nextVersion;
    setCount(Math.max(0, Number(summary.unread_count) || 0)); setVersion(String(nextVersion));
  }

  useEffect(() => {
    const current = ++generation.current;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    acceptedVersion.current = -1;
    setOpen(false); setCount(0); setPage(1); setTotal(0); setMessages([]); setDetails({}); setExpanded([]); setError(""); setDetailErrors({}); setVersion("");
    /** pollSummary 等待一次请求结束再安排下一次，慢网络时不会堆叠轮询。 */
    async function pollSummary() {
      if (!userKey || controller.signal.aborted) return;
      try {
        const result = await cloudRequest("/api/messages/summary", { signal: controller.signal });
        if (current !== generation.current) return;
        applySummary(result);
      } catch { /* 轮询暂时断网不弹通知；打开后可以重新读取列表。 */ }
      finally { if (!controller.signal.aborted && current === generation.current) timer = setTimeout(() => void pollSummary(), 10000); }
    }
    void pollSummary();
    return () => { generation.current += 1; controller.abort(); clearTimeout(timer); requests.current.forEach((request) => request.abort()); requests.current.clear(); };
  }, [userKey]);

  useEffect(() => {
    if (!open || !userKey) return;
    const current = generation.current;
    const requestVersion = acceptedVersion.current;
    const controller = new AbortController();
    setLoading(true); setError("");
    cloudRequest(`/api/messages?page=${page}&page_size=20`, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted || current !== generation.current || requestVersion < acceptedVersion.current) return;
      setMessages(Array.isArray(result.messages) ? result.messages : []); setTotal(Number(result.total) || 0);
    }).catch((cause) => {
      if (!controller.signal.aborted && current === generation.current) setError(cause instanceof Error ? cause.message : "消息没读出来，可以重试读取。");
    }).finally(() => { if (!controller.signal.aborted && current === generation.current) setLoading(false); });
    return () => controller.abort();
  }, [open, page, version, refresh, userKey]);

  /** readMessage 请求当前账号的消息正文，成功后按服务端结果同步未读数量。 */
  async function readMessage(id: string) {
    if (details[id] || requests.current.has(id)) return;
    const current = generation.current;
    const controller = new AbortController(); requests.current.set(id, controller);
    setDetailErrors((previous) => ({ ...previous, [id]: "" }));
    try {
      const result = await cloudRequest(`/api/messages/${encodeURIComponent(id)}/read`, { method: "POST", signal: controller.signal });
      if (current !== generation.current || controller.signal.aborted) return;
      setDetails((previous) => ({ ...previous, [id]: result.message }));
      setMessages((previous) => previous.map((item) => item.id === id ? { ...item, read_at: result.message?.read_at } : item));
      if (result.summary) applySummary(result.summary);
    } catch (cause) {
      if (current === generation.current && !controller.signal.aborted) setDetailErrors((previous) => ({ ...previous, [id]: cause instanceof Error ? cause.message : "内容没读出来，再点一次试试。" }));
    } finally { if (requests.current.get(id) === controller) requests.current.delete(id); }
  }

  /** expandMessages 仅在实际展开时取正文和已读状态，关闭悬浮框不改变阅读状态。 */
  function expandMessages(keys: string | string[]) {
    const next = Array.isArray(keys) ? keys : [keys]; setExpanded(next);
    next.forEach((id) => void readMessage(id));
  }
  const content = <div style={{ width: "min(380px, calc(100vw - 48px))" }} data-testid="message-center-panel">
    <Flex justify="space-between" align="center" style={{ marginBottom: 10 }}><Typography.Text strong>消息中心{count ? ` · ${count} 条未读` : ""}</Typography.Text></Flex>
    {error ? <Alert type="warning" title={error} action={<Button onClick={() => setRefresh((value) => value + 1)}>重试读取</Button>} style={{ marginBottom: 8 }} /> : null}
    <Spin spinning={loading}>
      <div style={{ maxHeight: "min(480px, 65vh)", overflowY: "auto" }}>
        {!messages.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这里暂时空空的，有进展我会来报信。" /> : <Collapse ghost activeKey={expanded} onChange={expandMessages} items={messages.map((item) => {
          const detail = details[item.id]; const action = safeMessageAction(detail?.action_url);
          return { key: item.id, label: <Flex gap={8} align="center"><Badge dot={!item.read_at} /><Typography.Text strong={!item.read_at} style={{ overflowWrap: "anywhere" }}>{item.title}</Typography.Text></Flex>, children: detailErrors[item.id] ? <><Typography.Text type="danger">{detailErrors[item.id]}</Typography.Text><Button type="link" onClick={() => void readMessage(item.id)}>重新读取</Button></> : !detail ? <Spin size="small" aria-label="正在读取消息" /> : <Flex vertical gap={8}>
            <Typography.Paragraph style={{ margin: 0, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{detail.body || "这条消息没有补充内容。"}</Typography.Paragraph>
            {action ? <Link href={action} onClick={() => setOpen(false)}>去处理</Link> : null}
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>{formatDate(detail.created_at)} · 已读消息 3 天后清理</Typography.Text>
          </Flex> };
        })} />}
      </div>
    </Spin>
    {total > 20 ? <Flex className="goodhr-table-pagination"><Pagination current={page} total={total} pageSize={20} showSizeChanger={false} onChange={setPage} /></Flex> : null}
  </div>;
  return <Popover placement="bottom" trigger="click" open={open} onOpenChange={setOpen} content={content}>
    <Badge count={count} overflowCount={99} size="small"><Button type="text" icon={<BellOutlined />} aria-label="消息中心" title={count ? `${count} 条未读消息` : "消息中心"} /></Badge>
  </Popover>;
}
