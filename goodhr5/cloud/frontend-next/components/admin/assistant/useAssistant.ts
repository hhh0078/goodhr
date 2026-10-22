/** 文件作用说明：管理全局 AI 会话、幂等发送、IndexedDB 历史及可恢复的带鉴权 SSE 长连接。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { APIRequestError, CLOUD_API_BASE, cloudRequest, getToken } from "@/lib/admin-api";
import { TOKEN_KEY } from "@/lib/api";
import { applyAssistantEvent, assistantRunReferences, assistantSessionBusy, createAssistantSSEParser, isAssistantRecord, newAssistantRun, type AssistantSession, type AssistantRun, type AssistantReference, type AssistantReferenceCheck } from "@/lib/assistant-protocol";
import { readAssistantSessions, writeAssistantSessions } from "@/lib/assistant-storage";

/** abortableDelay 在断网重试间隔内允许换账号或卸载立即取消。 */
function abortableDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(done, milliseconds);
    /** done 清理计时器与监听，避免长期连接积累事件处理器。 */
    function done() { clearTimeout(timer); signal.removeEventListener("abort", done); resolve(); }
    signal.addEventListener("abort", done, { once: true });
    if (signal.aborted) done();
  });
}

/** useAssistant 按账号维护十个会话，关闭弹框不影响后端任务和长连接。 */
export function useAssistant(account: string) {
  const [sessions, setSessions] = useState<AssistantSession[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [loadedAccount, setLoadedAccount] = useState("");
  const [cacheError, setCacheError] = useState("");
  const [error, setError] = useState("");
  const [sendingIDs, setSendingIDs] = useState<string[]>([]);
  const [connections, setConnections] = useState<Record<string, string>>({});
  const [referenceChecks, setReferenceChecks] = useState<Record<string, AssistantReferenceCheck>>({});
  const currentSessions = useRef(sessions);
  const scope = useRef(0);
  const subscriptions = useRef(new Map<string, AbortController>());
  const sendRequests = useRef(new Map<string, AbortController>());
  const sending = useRef(new Set<string>());
  const writeQueue = useRef(Promise.resolve());
  const referenceValidation = useRef<AbortController | null>(null);
  currentSessions.current = sessions;

  useEffect(() => {
    const generation = ++scope.current;
    setSessions([]); setSelectedID(""); setLoadedAccount(""); setError(""); setCacheError(""); setSendingIDs([]); setConnections({}); setReferenceChecks({});
    if (account) void readAssistantSessions(account).then((items) => {
      if (generation !== scope.current) return;
      setSessions(items); setSelectedID(items[0]?.id || ""); setLoadedAccount(account);
      void revalidateCached(items);
      for (const session of items) void recoverPending(session, generation);
    }).catch((cause) => { if (generation === scope.current) { setCacheError(cause.message); setLoadedAccount(account); } });
    return () => {
      scope.current += 1;
      subscriptions.current.forEach((controller) => controller.abort()); subscriptions.current.clear();
      sendRequests.current.forEach((controller) => controller.abort()); sendRequests.current.clear(); sending.current.clear();
      referenceValidation.current?.abort(); referenceValidation.current = null;
    };
  }, [account]);

  /** revalidateCached 在恢复和打开弹框时免费复核所有已结束任务；未核对前隐藏旧结果，失效只清理该轮资料。 */
  async function revalidateCached(snapshot = currentSessions.current) {
    if (referenceValidation.current || !account) return;
    const targets = snapshot.flatMap((session) => session.messages.flatMap((message) => {
      if (!message.run || message.run.status === "running") return [];
      const details = assistantRunReferences(message.run);
      return details.references.length || details.incomplete ? [{ id: message.run.id, ...details }] : [];
    }));
    if (!targets.length) return;
    const generation = scope.current;
    const controller = new AbortController(); referenceValidation.current = controller;
    setReferenceChecks((checks) => ({ ...checks, ...Object.fromEntries(targets.map((target) => [target.id, "checking" as const])) }));
    const references = [...new Map(targets.flatMap((target) => target.references).map((reference) => [JSON.stringify(reference), reference])).values()];
    const invalid = new Set<string>();
    let failed = false;
    try {
      for (let index = 0; index < references.length; index += 200) {
        const batch = references.slice(index, index + 200);
        const requested = new Set(batch.map((reference) => JSON.stringify(reference)));
        const timer = setTimeout(() => controller.abort(), 15000);
        try {
          const result = await cloudRequest("/api/assistant/references/validate", { method: "POST", body: { references: batch }, signal: controller.signal });
          if (result.ok !== true || !Array.isArray(result.invalid)) throw new Error("引用核对结果不完整");
          for (const item of result.invalid) {
            if (!isAssistantRecord(item) || typeof item.candidate_id !== "string" || !Number.isSafeInteger(item.resume_version)) throw new Error("引用核对格式不正确");
            const key = JSON.stringify({ candidate_id: item.candidate_id, resume_version: Number(item.resume_version) } satisfies AssistantReference);
            if (!requested.has(key)) throw new Error("引用核对范围不正确");
            invalid.add(key);
          }
        } finally { clearTimeout(timer); }
      }
    } catch { failed = true; }
    finally {
      if (referenceValidation.current === controller) referenceValidation.current = null;
      if (generation !== scope.current) return;
      const invalidRuns = new Set(targets.filter((target) => target.references.some((reference) => invalid.has(JSON.stringify(reference)))).map((target) => target.id));
      setSessions((items) => items.map((session) => ({ ...session, messages: session.messages.map((message) => message.run && invalidRuns.has(message.run.id) ? { ...message, content: "", run: { ...message.run, text: "", tools: [], result: undefined, summary: "", phase: "", error: "资料或权限已变化，请重新检索" } } : message) })));
      setReferenceChecks((checks) => ({ ...checks, ...Object.fromEntries(targets.map((target) => [target.id, !invalidRuns.has(target.id) && (failed || target.incomplete) ? "unverified" as const : "verified" as const])) }));
    }
  }

  /** recoverPending 刷新后主动核对丢失回包的消息，只按原请求编号接回既有任务，不重复提交。 */
  async function recoverPending(session: AssistantSession, generation: number) {
    const pending = session.messages.findLast((message) => !message.run && ["pending", "uncertain"].includes(message.delivery || ""));
    const index = pending ? session.messages.findIndex((message) => message.id === pending.id) : -1;
    const requestID = session.messages[index - 1]?.requestID;
    if (!pending || !requestID) return;
    const controller = new AbortController(); sendRequests.current.set(`recover:${session.id}`, controller);
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      const result = await cloudRequest(`/api/assistant/sessions/${encodeURIComponent(session.id)}`, { signal: controller.signal });
      if (generation !== scope.current) return;
      if (!Array.isArray(result.runs)) throw new Error("任务状态没读完整");
      const run = result.runs.find((item: { id?: string; request_id?: string }) => item?.request_id === requestID && typeof item.id === "string");
      setSessions((items) => items.map((item) => item.id === session.id ? { ...item, messages: item.messages.map((message) => message.id === pending.id && !message.run ? run
        ? { ...message, delivery: undefined, content: "", run: newAssistantRun(run.id) }
        : { ...message, delivery: "rejected", content: "上条消息尚未启动任务，可以重新发送或继续提问。" } : message) } : item));
    } catch (cause) {
      if (generation !== scope.current) return;
      if (cause instanceof APIRequestError && [404, 410].includes(cause.status)) {
        setSessions((items) => items.map((item) => item.id === session.id ? { ...item, messages: item.messages.map((message) => message.id === pending.id && !message.run ? { ...message, delivery: "rejected", content: "后台会话已过期，本地历史还在。可以继续提问。" } : message) } : item));
      } else setError("上条消息的任务暂时没核对到。点击重试会使用原编号，避免重复开工。");
    } finally { clearTimeout(timer); if (sendRequests.current.get(`recover:${session.id}`) === controller) sendRequests.current.delete(`recover:${session.id}`); }
  }

  useEffect(() => {
    if (!account || loadedAccount !== account) return;
    const generation = scope.current;
    const timer = setTimeout(() => {
      writeQueue.current = writeQueue.current.catch(() => undefined).then(() => {
        if (scope.current === generation) return writeAssistantSessions(account, currentSessions.current);
      }).catch((cause) => { if (scope.current === generation) setCacheError(cause.message); });
    }, 150);
    /** flush 保存离开页面前的最新快照，文字和事件游标始终同时提交。 */
    function flush() { void writeAssistantSessions(account, currentSessions.current).catch(() => undefined); }
    window.addEventListener("pagehide", flush);
    return () => { clearTimeout(timer); window.removeEventListener("pagehide", flush); };
  }, [sessions, loadedAccount, account]);

  /** updateRun 按独立任务编号更新状态，跨会话旧请求不会覆盖当前会话。 */
  function updateRun(runID: string, update: Partial<AssistantRun>) {
    setSessions((items) => items.map((session) => ({ ...session, messages: session.messages.map((message) => message.run?.id === runID ? { ...message, content: update.text ?? message.content, run: { ...message.run, ...update } } : message) })));
  }

  /** subscribe 只重接既有任务，不重发用户消息、不创建任务或延长后台有效期。 */
  async function subscribe(sessionID: string, runID: string, controller: AbortController, generation: number) {
    let retries = 0;
    while (!controller.signal.aborted && generation === scope.current) {
      let terminal = false;
      try {
        const run = currentSessions.current.find((item) => item.id === sessionID)?.messages.find((message) => message.run?.id === runID)?.run;
        if (!run || run.status !== "running") return;
        const token = getToken();
        setConnections((state) => ({ ...state, [runID]: retries ? "正在重新连接" : "正在连接" }));
        const response = await fetch(`${CLOUD_API_BASE}/api/assistant/runs/${encodeURIComponent(runID)}/events?after=${encodeURIComponent(run.lastEventID)}`, {
          headers: { Authorization: `Bearer ${token}`, Accept: "text/event-stream" }, cache: "no-store", signal: controller.signal,
        });
        if (response.status === 404 || response.status === 410) { updateRun(runID, { status: "expired", error: "后台任务已过期，本地历史还在。继续发消息可以重新开工。" }); return; }
        if (response.status === 401 || response.status === 403) {
          if (response.status === 401 && getToken() === token) { localStorage.removeItem(TOKEN_KEY); window.location.replace(`/login?next=${encodeURIComponent(window.location.pathname)}`); }
          updateRun(runID, { status: "failed", text: "", tools: [], error: "当前账号无法继续查看这个任务，请重新登录或检查团队权限。" }); return;
        }
        if (!response.ok || !response.body) throw new Error("长连接暂时没接上，正在重连。");
        if (!response.headers.get("content-type")?.includes("text/event-stream")) { updateRun(runID, { status: "failed", error: "助手返回的格式不正确，未接收任何正文。" }); return; }
        setConnections((state) => ({ ...state, [runID]: "已连接" })); retries = 0;
        const parser = createAssistantSSEParser((event) => {
          if (event.run_id !== runID || event.session_id !== sessionID || generation !== scope.current || controller.signal.aborted) return;
          terminal = ["completed", "failed", "cancelled"].includes(event.type);
          setSessions((items) => items.map((session) => applyAssistantEvent(session, event)));
        });
        const reader = response.body.getReader(); const decoder = new TextDecoder();
        try {
          while (!controller.signal.aborted) {
            const { value, done } = await reader.read();
            if (done) { parser.push(decoder.decode()); parser.finish(); break; }
            try { parser.push(decoder.decode(value, { stream: true })); }
            catch { updateRun(runID, { status: "failed", text: "", result: undefined, error: "助手返回了无效 JSON，已拒绝接收。" }); return; }
            if (terminal) return;
          }
        } finally { await reader.cancel().catch(() => undefined); }
        if (terminal) return;
      } catch {
        if (controller.signal.aborted || generation !== scope.current) return;
      }
      if (!controller.signal.aborted && generation === scope.current) {
        setConnections((state) => ({ ...state, [runID]: "连接断开，正在自动重连" }));
        await abortableDelay(Math.min(15000, 1000 * 2 ** Math.min(retries++, 4)), controller.signal);
      }
    }
  }

  const runningSignature = sessions.flatMap((session) => session.messages.filter((message) => message.run?.status === "running").map((message) => `${session.id}/${message.run!.id}`)).join("|");
  useEffect(() => {
    if (loadedAccount !== account || !account) return;
    const active = new Set<string>();
    for (const session of sessions) for (const message of session.messages) {
      const run = message.run; if (run?.status !== "running") continue;
      active.add(run.id);
      if (!subscriptions.current.has(run.id)) {
        const controller = new AbortController(); subscriptions.current.set(run.id, controller);
        void subscribe(session.id, run.id, controller, scope.current);
      }
    }
    subscriptions.current.forEach((controller, id) => { if (!active.has(id)) { controller.abort(); subscriptions.current.delete(id); } });
  }, [runningSignature, loadedAccount, account]);

  /** createSession 新建本地会话；到达十个时只移除最早结束的会话，不丢弃运行中任务。 */
  function createSession() {
    if (!account || loadedAccount !== account) return;
    if (sessions.length >= 10 && sessions.every((item) => assistantSessionBusy(item) || sending.current.has(item.id))) { setError("十个会话都还在忙，先停止或完成一个再新建。 "); return; }
    const session: AssistantSession = { id: crypto.randomUUID(), title: "新对话", updatedAt: Date.now(), messages: [] };
    setSessions((items) => {
      const next = [...items].sort((a, b) => b.updatedAt - a.updatedAt);
      if (next.length >= 10) { const last = [...next].reverse().find((item) => !assistantSessionBusy(item) && !sending.current.has(item.id)); if (last) next.splice(next.findIndex((item) => item.id === last.id), 1); }
      return [session, ...next];
    });
    setSelectedID(session.id); setError("");
  }

  /** sendMessage 先持久保存消息与幂等编号再启动任务，超时重试仍用原编号；缓存失败时不发起付费任务。 */
  async function sendMessage(content: string, retry = false) {
    const session = currentSessions.current.find((item) => item.id === selectedID);
    if (!account || loadedAccount !== account || !session || !content.trim() || sending.current.has(session.id) || session.messages.some((message) => message.run?.status === "running") || (!retry && assistantSessionBusy(session))) return;
    const generation = scope.current; const controller = new AbortController();
    const previousUser = session.messages[session.messages.length - 2]; const previousAssistant = session.messages.at(-1);
    const retryExisting = retry && previousUser?.role === "user" && previousUser.requestID && previousAssistant?.role === "assistant" && !previousAssistant.run;
    const requestID = retryExisting ? previousUser.requestID! : crypto.randomUUID();
    const assistantID = retryExisting ? previousAssistant.id : crypto.randomUUID();
    const question = retryExisting ? previousUser.content : content.trim();
    sending.current.add(session.id); setSendingIDs([...sending.current]); sendRequests.current.set(session.id, controller); setError("");
    const userID = crypto.randomUUID();
    // 同步合并 React 队列中的其他会话更新，让持久化屏障读取包含本条 pending 消息的完整最新快照。
    flushSync(() => setSessions((items) => items.map((item) => item.id === session.id ? { ...item, title: item.messages.length ? item.title : question.slice(0, 24), updatedAt: Date.now(), messages: retryExisting ? item.messages.map((message) => message.id === assistantID ? { ...message, content: "", delivery: "pending" } : message) : [...item.messages, { id: userID, role: "user", content: question, requestID }, { id: assistantID, role: "assistant", content: "", delivery: "pending" }] } : item)));
    const timer = setTimeout(() => controller.abort(), 25000);
    let persisted = false;
    try {
      const pendingWrite = writeQueue.current.catch(() => undefined).then(() => {
        if (generation === scope.current && !controller.signal.aborted) return writeAssistantSessions(account, currentSessions.current);
      });
      writeQueue.current = pendingWrite;
      await pendingWrite;
      if (generation !== scope.current) return;
      if (controller.signal.aborted) throw new Error("发送等待超时，可以用原编号重试。");
      persisted = true; setCacheError("");
      const history = (retryExisting ? session.messages.slice(0, -2) : session.messages).filter((message) => message.content).map(({ role, content: text }) => ({ role, content: text }));
      await cloudRequest("/api/assistant/sessions", { method: "POST", body: { id: session.id, history }, signal: controller.signal });
      if (generation !== scope.current) return;
      if (controller.signal.aborted) throw new Error("发送等待超时，可以用原编号重试。");
      const result = await cloudRequest(`/api/assistant/sessions/${encodeURIComponent(session.id)}/messages`, { method: "POST", body: { content: question, request_id: requestID }, signal: controller.signal });
      if (generation !== scope.current) return;
      if (!result.run_id || result.session_id !== session.id) throw new Error("任务没有确认成功，点击重试会核对原任务。");
      setSessions((items) => items.map((item) => item.id === session.id ? { ...item, messages: item.messages.map((message) => message.id === assistantID ? { ...message, content: "", delivery: undefined, run: newAssistantRun(String(result.run_id)) } : message) } : item));
    } catch (cause) {
      if (generation === scope.current) {
        const message = !persisted ? "对话还没保存到本机，这次没有启动后台任务。允许本地存储后点击重试。" : cause instanceof APIRequestError ? cause.message : "消息暂时没确认收到，可以重试发送；我会核对原任务，避免重复开工。";
        setError(message);
        if (!persisted) setCacheError(message);
        const delivery = !persisted || cause instanceof APIRequestError && cause.status < 500 && cause.status !== 409 ? "rejected" as const : "uncertain" as const;
        setSessions((items) => items.map((item) => item.id === session.id ? { ...item, messages: item.messages.map((entry) => entry.id === assistantID ? { ...entry, content: message, delivery } : entry) } : item));
      }
    } finally {
      clearTimeout(timer);
      if (generation === scope.current) { sending.current.delete(session.id); sendRequests.current.delete(session.id); setSendingIDs([...sending.current]); }
    }
  }

  /** cancelRun 只停止选定服务端任务，接口失败时仍保留连接以读取真实状态。 */
  async function cancelRun(runID: string) {
    const generation = scope.current;
    try { await cloudRequest(`/api/assistant/runs/${encodeURIComponent(runID)}/cancel`, { method: "POST" }); if (generation === scope.current) updateRun(runID, { status: "cancelled", phase: "已停止", error: "" }); }
    catch (cause) { if (generation === scope.current) setError(cause instanceof Error ? cause.message : "停止请求没有确认，请稍后再试。"); }
  }

  /** removeSession 仅删除当前浏览器的已结束会话，不调用岗位或简历删除接口。 */
  function removeSession(id: string) {
    const session = sessions.find((item) => item.id === id);
    if (!session || sending.current.has(id) || assistantSessionBusy(session)) return;
    const next = sessions.filter((item) => item.id !== id); setSessions(next); if (selectedID === id) setSelectedID(next[0]?.id || "");
  }
  return { sessions, selectedID, setSelectedID, createSession, sendMessage, cancelRun, removeSession, revalidateCached, referenceChecks, loaded: Boolean(account && loadedAccount === account), sendingIDs, connections, cacheError, error };
}
