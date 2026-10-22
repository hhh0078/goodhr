/** 文件作用说明：验证真实助手协议的分包 JSON、事件去重、隔离、终态与十会话无期限规则。 */
import assert from "node:assert/strict";
import test from "node:test";
import { applyAssistantEvent, assistantSessionBusy, createAssistantSSEParser, newAssistantRun, normalizeAssistantSessions, parseAssistantEvent } from "./assistant-protocol.ts";

/** event 构造服务端 JSON 合同事件，只用于纯协议测试。 */
function event(id, type = "text", data = { text: "正文" }, overrides = {}) { return { id: String(id), session_id: "session-one", run_id: "run-one", type, data, created_at: "2026-10-22T10:00:00Z", ...overrides }; }
/** session 构造含独立任务的本地历史，不访问业务接口。 */
function session() { return { id: "session-one", title: "岗位找人", updatedAt: 1, messages: [{ id: "answer", role: "assistant", content: "", run: newAssistantRun("run-one") }] }; }

test("完整JSON事件校验拒绝普通文字、代码围栏和缺字段", () => {
  for (const raw of ["我准备找简历", "```json\n{}\n```", "{}", JSON.stringify(event("1", "text", { text: 10 })), JSON.stringify(event("1", "tool_start", { name: "search" }))]) assert.throws(() => parseAssistantEvent(raw));
  assert.equal(parseAssistantEvent(JSON.stringify(event(2))).id, "2");
});

test("SSE跨中文分包、CRLF、多事件和心跳只产生完整事件", () => {
  const received = []; const parser = createAssistantSSEParser((value) => received.push(value));
  const raw = `: heartbeat\r\n\r\ndata: ${JSON.stringify(event(1, "text", { text: "杭州产品经理" }))}\r\n\r\ndata: ${JSON.stringify(event(2, "completed", {}))}\n\n`;
  for (let index = 0; index < raw.length; index += 3) parser.push(raw.slice(index, index + 3));
  parser.finish(); assert.deepEqual(received.map((item) => item.id), ["1", "2"]); assert.equal(received[0].data.text, "杭州产品经理");
});

test("半包不会被渲染或保存游标，重新连接可补发", () => {
  const received = []; const parser = createAssistantSSEParser((value) => received.push(value));
  parser.push(`data: ${JSON.stringify(event(1))}`); assert.deepEqual(received, []); assert.throws(() => parser.finish());
});

test("按会话任务隔离且重复事件不会重复拼接文字", () => {
  let state = session(); state = applyAssistantEvent(state, event(1)); const first = state;
  assert.equal(applyAssistantEvent(state, event(1)), first);
  assert.equal(applyAssistantEvent(state, event(2, "text", { text: "泄露" }, { session_id: "other" })), first);
  assert.equal(applyAssistantEvent(state, event(2, "text", { text: "泄露" }, { run_id: "other" })), first);
  state = applyAssistantEvent(state, event(2, "text", { text: "继续" })); assert.equal(state.messages[0].content, "正文继续");
});

test("工具开始结果按call_id合并并保留入参出参错误和耗时", () => {
  let state = applyAssistantEvent(session(), event(1, "tool_start", { call_id: "tool-one", name: "find_jobs", arguments: { keyword: "产品经理" } }));
  state = applyAssistantEvent(state, event(2, "tool_result", { call_id: "tool-one", name: "find_jobs", result: { items: [] }, error: "没有岗位", duration_ms: 20, attempt: 2 }));
  const tools = state.messages[0].run.tools; assert.equal(tools.length, 1); assert.equal(tools[0].arguments.keyword, "产品经理"); assert.equal(tools[0].duration_ms, 20); assert.equal(tools[0].finished, true);
});

test("终态不接收迟到正文，失败保持明确错误", () => {
  const state = applyAssistantEvent(session(), event(1, "failed", { error: "已达到5次尝试" }));
  assert.equal(state.messages[0].run.error, "已达到5次尝试"); assert.equal(applyAssistantEvent(state, event(2)), state);
});

test("请求结果不明仍视为在途，明确拒绝和完成才允许同会话继续", () => {
  const state = session(); assert.equal(assistantSessionBusy(state), true);
  state.messages[0].run = undefined; state.messages[0].delivery = "uncertain"; assert.equal(assistantSessionBusy(state), true);
  state.messages[0].delivery = "rejected"; assert.equal(assistantSessionBusy(state), false);
});

test("重试清理本轮无效正文，权限撤销失败清空正文工具和缓存结果", () => {
  let state = applyAssistantEvent(session(), event(1));
  state = applyAssistantEvent(state, event(2, "retry", { reset_text: true, attempt: 2 })); assert.equal(state.messages[0].content, "");
  state = applyAssistantEvent(state, event(3, "tool_start", { call_id: "private-call", name: "search_candidates", arguments: {} }));
  state = applyAssistantEvent(state, event(4, "text", { text: "旧推荐" }));
  state = applyAssistantEvent(state, event(5, "failed", { clear_result: true, error: "权限已经变化" }));
  assert.equal(state.messages[0].content, ""); assert.deepEqual(state.messages[0].run.tools, []); assert.equal(state.messages[0].run.error, "权限已经变化");
});

test("很久以前的有效会话不因时间删除，最多保存最新十个并剔除重复和损坏", () => {
  const old = { ...session(), updatedAt: 1 }; assert.equal(normalizeAssistantSessions([old]).length, 1);
  const items = Array.from({ length: 12 }, (_, index) => ({ ...session(), id: `session-${index}`, updatedAt: index }));
  const result = normalizeAssistantSessions([...items, items[11], null, { id: "broken" }]);
  assert.equal(result.length, 10); assert.equal(result[0].id, "session-11"); assert.equal(result.at(-1).id, "session-2");
});
