/** 文件作用说明：提供全局 AI 弹框、十会话切换、流式正文、可折叠思考摘要和工具调用明细。 */
"use client";
import { Alert, Button, Collapse, Empty, Flex, Input, Popconfirm, Select, Spin, Tag, Tooltip, Typography } from "antd";
import { DeleteOutlined, PlusOutlined, RobotOutlined, SendOutlined, StopOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import JsonTree from "../JsonTree";
import { useAssistant } from "./useAssistant";
import RecommendationCards from "./RecommendationCards";
import { assistantAccountKey } from "@/lib/assistant-storage";
import { assistantSessionBusy, type AssistantMessage, type AssistantTool, type AssistantReferenceCheck } from "@/lib/assistant-protocol";
import styles from "./assistant.module.css";

/** ToolRecord 展示服务端已确认的方法、参数、结果、耗时与尝试次数，默认折叠。 */
function ToolRecord({ tool }: { tool: AssistantTool }) {
  return <Collapse size="small" items={[{
    key: tool.call_id,
    label: <Flex gap={8} align="center" wrap><Typography.Text>{tool.name}</Typography.Text><Tag color={tool.error ? "error" : tool.finished ? "success" : "processing"}>{tool.error ? "未完成" : tool.finished ? "已完成" : "处理中"}</Tag>{tool.attempt ? <Typography.Text type="secondary">第 {tool.attempt} 次</Typography.Text> : null}{tool.duration_ms != null ? <Typography.Text type="secondary">{tool.duration_ms} ms</Typography.Text> : null}</Flex>,
    children: <div className={styles.toolBody}><Typography.Text strong>传入参数</Typography.Text><JsonTree value={tool.arguments ?? {}} />{tool.finished ? <><Typography.Text strong>返回结果</Typography.Text><JsonTree value={tool.result ?? null} /></> : null}{tool.error ? <Alert type="warning" title={tool.error} /> : null}</div>,
  }]} />;
}

/** AssistantReply 在正文出现时收起可展示摘要，用户仍可手动展开查看进度。 */
function AssistantReply({ message, connection, referenceCheck }: { message: AssistantMessage; connection?: string; referenceCheck?: AssistantReferenceCheck }) {
  const run = message.run;
  const [expanded, setExpanded] = useState<string[]>(["summary"]);
  const hasText = Boolean(run?.text);
  const historical = referenceCheck === "checking" || referenceCheck === "unverified";
  useEffect(() => { setExpanded(hasText ? [] : ["summary"]); }, [hasText]);
  return <div className={`${styles.message} ${styles.assistant}`}>
    <Flex gap={8} align="center"><RobotOutlined aria-hidden="true" /><Typography.Text strong>招聘小助手</Typography.Text>{run?.status === "running" ? <Spin size="small" aria-label="助手正在处理" /> : null}</Flex>
    {historical ? <Alert type="warning" showIcon title={referenceCheck === "checking" ? "正在核对历史资料与权限" : "历史结果尚未核对，请联网后重新打开助手"} description="对话仍保存在本机，核对完成后再展示候选资料和原简历入口。" /> : <>
    {run && (run.summary || run.phase) ? <Collapse size="small" ghost activeKey={expanded} onChange={(keys) => setExpanded(Array.isArray(keys) ? keys : [keys])} items={[{ key: "summary", label: run.status === "running" ? run.phase || "正在处理" : run.status === "completed" ? "查看处理过程" : run.phase || "任务已结束", children: <Typography.Paragraph className={styles.text} type="secondary">{run.summary || run.phase}</Typography.Paragraph> }]} /> : null}
    {run?.tools.length ? <Flex vertical gap={8} className={styles.tools}>{run.tools.map((tool) => <ToolRecord key={tool.call_id} tool={tool} />)}</Flex> : null}
    {message.content ? <Typography.Paragraph className={styles.text}>{message.content}</Typography.Paragraph> : run?.status === "running" ? <Typography.Text type="secondary">正在核对岗位和简历，进度会实时显示。</Typography.Text> : null}
    {run?.status === "completed" ? <RecommendationCards result={run.result} /> : null}
    </>}
    {run?.error ? <Alert type={run.status === "expired" ? "info" : "warning"} title={run.error} /> : null}
    {run?.status === "running" ? <Typography.Text type="secondary" className={styles.meta}>{connection || "正在连接"} · 关掉弹框也会继续处理</Typography.Text> : null}
  </div>;
}

/** AssistantDialog 以顶部纯图标打开助手，状态和连接始终留在全局布局中。 */
export default function AssistantDialog({ user }: { user: { id?: string; email?: string; tenant_id?: string } | null }) {
  const account = assistantAccountKey(String(user?.tenant_id || ""), String(user?.id || ""));
  const assistant = useAssistant(account);
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState("");
  const [cancelling, setCancelling] = useState(false);
  const body = useRef<HTMLDivElement>(null);
  const follow = useRef(true);
  const selected = assistant.sessions.find((session) => session.id === assistant.selectedID);
  const activeRun = selected?.messages.findLast((message) => message.run?.status === "running")?.run;
  const busy = Boolean(activeRun || assistant.sendingIDs.includes(assistant.selectedID));
  const uncertain = Boolean(selected && assistantSessionBusy(selected) && !busy);
  const last = selected?.messages.at(-1); const previous = selected?.messages.at(-2);
  const retryMessage = last?.role === "assistant" && !last.run && previous?.role === "user" && previous.requestID ? previous : null;
  useEffect(() => { setOpen(false); setInput(""); }, [account]);
  useEffect(() => { follow.current = true; setInput(""); }, [assistant.selectedID]);
  useEffect(() => { if (open && follow.current && body.current) body.current.scrollTo({ top: body.current.scrollHeight }); }, [open, selected]);
  /** send 提交后清空输入，失败消息通过同一幂等编号明确重试。 */
  function send() { if (busy || uncertain || !input.trim() || !selected) return; const question = input; setInput(""); follow.current = true; void assistant.sendMessage(question); }
  /** stop 等待停止接口确认，避免连续发送停止请求。 */
  async function stop() { if (!activeRun || cancelling) return; setCancelling(true); try { await assistant.cancelRun(activeRun.id); } finally { setCancelling(false); } }
  return <>
    <Tooltip title="AI 助手"><Button type="text" icon={<RobotOutlined aria-hidden="true" />} aria-label="AI 助手" onClick={() => { void assistant.revalidateCached(); setOpen(true); }} /></Tooltip>
    <AdminDialog open={open} title="AI 招聘助手" maxWidth="lg" showCancel={false} onClose={() => setOpen(false)}>
      <div className={styles.shell} data-testid="assistant-dialog">
        <Flex gap={8} align="center" wrap>
          <Select aria-label="AI 会话" showSearch optionFilterProp="label" value={selected?.id} onChange={assistant.setSelectedID} placeholder="先开个新对话" className={styles.sessionSelect} options={assistant.sessions.map((session) => ({ value: session.id, label: `${session.title}${session.messages.some((message) => message.run?.status === "running") ? " · 处理中" : ""}` }))} />
          <Button icon={<PlusOutlined aria-hidden="true" />} disabled={!assistant.loaded} onClick={assistant.createSession}>新对话</Button>
          <Popconfirm title="删除这个本地会话？" description="只清理当前浏览器里的对话，岗位和简历会保留。" onConfirm={() => assistant.removeSession(assistant.selectedID)} okText="删除" cancelText="保留" disabled={!selected || busy || uncertain}>
            <Button type="text" danger aria-label="删除本地会话" icon={<DeleteOutlined aria-hidden="true" />} disabled={!selected || busy || uncertain} />
          </Popconfirm>
          <Typography.Text type="secondary" className={styles.meta}>{assistant.sessions.length}/10 个本地会话</Typography.Text>
        </Flex>
        {!account ? <Alert type="warning" title="团队信息还没读完整，请刷新后再打开助手。" /> : null}
        {assistant.cacheError ? <Alert type="warning" title={assistant.cacheError} /> : null}
        {assistant.error ? <Alert type="warning" title={assistant.error} /> : null}
        <div ref={body} className={styles.messages} data-testid="assistant-messages" aria-live="polite" aria-relevant="additions text" onScroll={() => { const element = body.current; if (element) follow.current = element.scrollHeight - element.scrollTop - element.clientHeight < 80; }}>
          {!assistant.loaded ? <Spin aria-label="正在恢复对话" /> : selected?.messages.length ? selected.messages.map((message) => message.role === "user" ? <div className={`${styles.message} ${styles.user}`} key={message.id}><Typography.Text type="secondary" className={styles.meta}>我</Typography.Text><Typography.Paragraph className={styles.text}>{message.content}</Typography.Paragraph></div> : <AssistantReply key={message.id} message={message} connection={message.run ? assistant.connections[message.run.id] : undefined} referenceCheck={message.run ? assistant.referenceChecks[message.run.id] : undefined} />) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={selected ? "比如：帮我根据高级产品经理岗位，找几位合适的候选人。" : "新建一个对话，岗位和简历我来帮你找。"} />}
        </div>
        {retryMessage && !busy ? <Button onClick={() => void assistant.sendMessage(retryMessage.content, true)}>重试发送上一条</Button> : null}
        <Flex gap={8} align="end">
          <Input.TextArea aria-label="给 AI 助手发消息" placeholder={selected ? "说说你要找什么岗位和人选，Enter 发送，Shift+Enter 换行" : "先点击新对话"} value={input} onChange={(event) => setInput(event.target.value)} disabled={!selected || !assistant.loaded} autoSize={{ minRows: 2, maxRows: 5 }} maxLength={10000} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); send(); } }} />
          {activeRun ? <Button danger icon={<StopOutlined aria-hidden="true" />} loading={cancelling} onClick={() => void stop()}>停止</Button> : <Button type="primary" icon={<SendOutlined aria-hidden="true" />} aria-label="发送 AI 消息" loading={busy} disabled={!selected || !input.trim() || busy || uncertain} onClick={send}>发送</Button>}
        </Flex>
        <Typography.Text type="secondary" className={styles.meta}>任务在最后一条消息后保留 2 小时；本机最多保存 10 个会话，不设时间期限。新建第 11 个会话时移除最早结束的会话。</Typography.Text>
      </div>
    </AdminDialog>
  </>;
}
