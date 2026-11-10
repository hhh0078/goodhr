/** 文件作用说明：验证新资料状态中文、按锚点合并刷新和跨会话拒绝，不访问接口或保存业务数据。 */
import assert from "node:assert/strict";
import test from "node:test";
import { historyHasEventAnchor, historyHasMessageAnchor, mergeReplyHistoryRefresh, replyHistoryStatus } from "./resume-reply-history.ts";

/** fixture 为已分页历史提供稳定编号，时间相同也必须保留服务器给出的顺序。 */
function fixture(messages, events) {
  return { thread_id: "thread-a", threads: [], messages: messages.map(id => ({ id, text: id })), events: events.map(id => ({ id, title: id })), goals: [], next_message_cursor: "old-messages", next_event_cursor: "old-events" };
}

test("原生资料新状态均中文，未知结果明确暂不重复，未知协议不露英文", () => {
  for (const status of ["requested", "already_requested", "received", "declined", "deferred", "unavailable", "warning", "planned", "unknown", "waiting_resume"]) {
    assert.match(replyHistoryStatus(status)[0], /[\u4e00-\u9fff]/);
    assert.notEqual(replyHistoryStatus(status)[0], status);
  }
  assert.equal(replyHistoryStatus("unknown", "已成功")[0], "不确定，暂不重复");
  assert.equal(replyHistoryStatus("future_status", "future_status")[0], "暂未确认");
  assert.equal(replyHistoryStatus("future_status", "等待人工核实")[0], "等待人工核实");
});

test("刷新更新同编号回执、添加新消息事件，保留旧分页顺序和完整两目标", () => {
  const current = fixture(["old", "m1", "m2"], ["e2", "e1", "older-event"]);
  const fresh = fixture(["before-window", "m1", "m2", "m3"], ["e3", "e2", "e1"]);
  fresh.messages[2].sender_type = "ai";
  fresh.events[1].status = "completed";
  fresh.goals = [{ id: "goal-1", status: "matched" }, { id: "goal-2", status: "pending" }];
  fresh.contacts = { phone: "synthetic-only", wechat: "" };
  fresh.next_message_cursor = "fresh-message-cursor";
  fresh.next_event_cursor = "fresh-event-cursor";
  const merged = mergeReplyHistoryRefresh(current, fresh);
  assert.deepEqual(merged.messages.map(row => row.id), ["old", "m1", "m2", "m3"]);
  assert.deepEqual(merged.events.map(row => row.id), ["e3", "e2", "e1", "older-event"]);
  assert.equal(merged.messages[2].sender_type, "ai");
  assert.equal(merged.events[1].status, "completed");
  assert.equal(merged.next_message_cursor, "old-messages");
  assert.equal(merged.next_event_cursor, "old-events");
  assert.deepEqual(merged.goals, fresh.goals);
  assert.deepEqual(merged.contacts, fresh.contacts);
  assert.equal(current.messages.length, 3);
});

test("跨会话和新窗口中间未衔接时拒绝合并，不能把缺口当完整历史", () => {
  const current = fixture(["m1"], ["e1"]);
  const gap = fixture(["m3"], ["e3"]);
  assert.equal(historyHasMessageAnchor(current, gap), false);
  assert.equal(historyHasEventAnchor(current, gap), false);
  assert.throws(() => mergeReplyHistoryRefresh(current, gap), /没接完整/);
  const other = { ...current, thread_id: "thread-b" };
  assert.throws(() => mergeReplyHistoryRefresh(current, other), /没接完整/);
  assert.deepEqual(mergeReplyHistoryRefresh(fixture([], []), current).messages, current.messages);
});
