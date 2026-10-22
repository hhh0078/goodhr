/** 文件作用说明：严格校验 AI JSON 事件、解析分包 SSE，并按任务事件编号更新本地会话。 */
export type AssistantEvent = { id: string; session_id: string; run_id: string; type: string; data: Record<string, unknown>; created_at: string };
export type AssistantTool = { call_id: string; name: string; arguments?: unknown; result?: unknown; error?: string; attempt?: number; duration_ms?: number; finished?: boolean };
export type AssistantRun = { id: string; status: "running" | "completed" | "failed" | "cancelled" | "expired"; lastEventID: string; summary: string; phase: string; text: string; tools: AssistantTool[]; error?: string; attempt?: number; result?: unknown };
export type AssistantMessage = { id: string; role: "user" | "assistant"; content: string; requestID?: string; delivery?: "pending" | "uncertain" | "rejected"; run?: AssistantRun };
export type AssistantSession = { id: string; title: string; updatedAt: number; messages: AssistantMessage[] };
export type AssistantReference = { candidate_id: string; resume_version: number };
export type AssistantReferenceCheck = "checking" | "verified" | "unverified";
const eventTypes = new Set(["phase", "summary", "text", "tool_start", "tool_result", "retry", "completed", "failed", "cancelled"]);

/** isAssistantRecord 判断不可信数据是否为可读取的 JSON 对象。 */
export function isAssistantRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

/** assistantRunReferences 从最终结果和工具结果的两个候选池提取去重引用；缺少版本的数据不视为可核实。 */
export function assistantRunReferences(run: AssistantRun): { references: AssistantReference[]; incomplete: boolean } {
  const references = new Map<string, AssistantReference>();
  let incomplete = false;
  for (const result of [run.result, ...run.tools.map((tool) => tool.result)]) {
    if (!isAssistantRecord(result)) continue;
    for (const pool of [result.recommendations, result.pending_verification]) {
      if (!Array.isArray(pool)) continue;
      for (const item of pool) {
        if (!isAssistantRecord(item) || typeof item.candidate_id !== "string" || !item.candidate_id || !Number.isSafeInteger(item.resume_version) || Number(item.resume_version) < 1) { incomplete = true; continue; }
        const reference = { candidate_id: item.candidate_id, resume_version: Number(item.resume_version) };
        references.set(JSON.stringify(reference), reference);
      }
    }
  }
  return { references: [...references.values()], incomplete };
}

/** parseAssistantEvent 只认可完整 JSON 对象与已知事件，拒绝普通文字和错误结构。 */
export function parseAssistantEvent(raw: string): AssistantEvent {
  const value: unknown = JSON.parse(raw);
  if (!isAssistantRecord(value) || !["string", "number"].includes(typeof value.id) || !/^\d+$/.test(String(value.id))
    || typeof value.session_id !== "string" || !value.session_id || typeof value.run_id !== "string" || !value.run_id
    || typeof value.type !== "string" || !eventTypes.has(value.type) || !isAssistantRecord(value.data)
    || typeof value.created_at !== "string") throw new Error("助手返回了不合规的 JSON 事件，已停止接收。");
  if (["text", "summary"].includes(value.type) && typeof value.data.text !== "string") throw new Error("助手文字事件缺少正文，已停止接收。");
  if (["tool_start", "tool_result"].includes(value.type) && (typeof value.data.call_id !== "string" || !value.data.call_id || typeof value.data.name !== "string")) throw new Error("助手工具记录不完整，已停止接收。");
  return { ...value, id: String(value.id) } as AssistantEvent;
}

/** createAssistantSSEParser 缓冲跨包/中文事件；只在空行结束后校验 JSON，心跳不改变游标。 */
export function createAssistantSSEParser(onEvent: (event: AssistantEvent) => void) {
  let buffer = "";
  return {
    /** push 追加 UTF-8 解码后的分包，并逐个解析已经完整的 SSE 事件。 */
    push(chunk: string) {
      buffer += chunk;
      if (buffer.length > 2 * 1024 * 1024) throw new Error("助手单条事件过大，已停止接收。");
      let match: RegExpExecArray | null;
      while ((match = /\r?\n\r?\n/.exec(buffer))) {
        const block = buffer.slice(0, match.index); buffer = buffer.slice(match.index + match[0].length);
        const data = block.split(/\r?\n/).filter((line) => line.startsWith("data:")).map((line) => line.slice(5).replace(/^ /, "")).join("\n");
        if (data) onEvent(parseAssistantEvent(data));
      }
    },
    /** finish 不接受缺少事件结束标记的半包，重连时由服务端从已确认游标补发。 */
    finish() { if (buffer.trim() && !buffer.trim().startsWith(":")) throw new Error("连接在事件接收完成前中断。"); },
  };
}

/** newAssistantRun 为刚创建的服务端任务建立独立的事件与工具记录。 */
export function newAssistantRun(id: string): AssistantRun {
  return { id, status: "running", lastEventID: "", summary: "", phase: "任务已排队", text: "", tools: [] };
}

/** assistantSessionBusy 将未确认的消息也视为在途，防止丢失任务后再次创建并重复扣费。 */
export function assistantSessionBusy(session: AssistantSession): boolean {
  return session.messages.some((message) => message.run?.status === "running" || ["pending", "uncertain"].includes(message.delivery || ""));
}

/** applyAssistantEvent 拒绝跨任务消息和重复事件；终态之后不再接受迟到正文。 */
export function applyAssistantEvent(session: AssistantSession, event: AssistantEvent): AssistantSession {
  if (session.id !== event.session_id) return session;
  const index = session.messages.findIndex((message) => message.run?.id === event.run_id);
  const old = session.messages[index]; const run = old?.run;
  if (!run || run.status !== "running" || (run.lastEventID && BigInt(event.id) <= BigInt(run.lastEventID))) return session;
  const next: AssistantRun = { ...run, lastEventID: event.id, tools: [...run.tools] };
  const data = event.data;
  if (event.type === "text") next.text += String(data.text);
  if (event.type === "summary") next.summary = String(data.text);
  if (event.type === "phase") next.phase = String(data.text || data.phase || data.message || "正在处理");
  if (event.type === "retry") { next.attempt = Number(data.attempt) || 1; next.phase = `正在重试，第 ${next.attempt} 次`; next.error = String(data.error || data.message || ""); if (data.reset_text === true) { next.text = ""; next.result = undefined; } }
  if (["tool_start", "tool_result"].includes(event.type)) {
    const toolIndex = next.tools.findIndex((tool) => tool.call_id === data.call_id);
    const tool: AssistantTool = { ...(next.tools[toolIndex] || {}), ...data, call_id: String(data.call_id), name: String(data.name), finished: event.type === "tool_result" };
    if (toolIndex < 0) next.tools.push(tool); else next.tools[toolIndex] = tool;
  }
  if (["completed", "failed", "cancelled"].includes(event.type)) {
    next.status = event.type as AssistantRun["status"];
    if (event.type === "completed") next.result = data.result;
    next.error = event.type === "failed" ? String(data.error || data.message || "这次没有完成，可以调整问题后再试。") : "";
    if (event.type === "failed" && data.clear_result === true) { next.text = ""; next.tools = []; next.result = undefined; next.summary = ""; }
    next.phase = event.type === "completed" ? "已完成" : event.type === "cancelled" ? "已停止" : "未完成";
  }
  const messages = [...session.messages]; messages[index] = { ...old, content: next.text, run: next };
  return { ...session, updatedAt: Date.now(), messages };
}

/** normalizeAssistantSessions 仅接收本应用缓存结构，保留最新十个且不按时间删除。 */
export function normalizeAssistantSessions(value: unknown): AssistantSession[] {
  if (!Array.isArray(value)) return [];
  const ids = new Set<string>();
  return value.filter((entry): entry is AssistantSession => {
    if (!isAssistantRecord(entry) || typeof entry.id !== "string" || !entry.id || ids.has(entry.id) || typeof entry.title !== "string"
      || typeof entry.updatedAt !== "number" || !Number.isFinite(entry.updatedAt) || !Array.isArray(entry.messages)) return false;
    if (!entry.messages.every((message) => isAssistantRecord(message) && typeof message.id === "string" && ["user", "assistant"].includes(String(message.role)) && typeof message.content === "string"
      && (!message.run || (isAssistantRecord(message.run) && typeof message.run.id === "string" && ["running", "completed", "failed", "cancelled", "expired"].includes(String(message.run.status))
        && typeof message.run.text === "string" && typeof message.run.summary === "string" && typeof message.run.phase === "string" && typeof message.run.lastEventID === "string" && /^\d*$/.test(message.run.lastEventID)
        && Array.isArray(message.run.tools) && message.run.tools.every((tool) => isAssistantRecord(tool) && typeof tool.name === "string" && typeof tool.call_id === "string"))))) return false;
    ids.add(entry.id); return true;
  }).sort((first, second) => second.updatedAt - first.updatedAt).slice(0, 10);
}
