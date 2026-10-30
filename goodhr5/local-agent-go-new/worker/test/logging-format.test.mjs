// 文件作用说明：验证 Worker 的正常与失败操作都输出完整、脱敏的一行 JSON 日志。

import assert from "node:assert/strict";
import test from "node:test";
import { WorkerLogger } from "../dist/logging/logger.js";

test("Worker 日志包含固定字段、操作摘要并隐藏凭据", () => {
  const original = process.stdout.write;
  const lines = [];
  process.stdout.write = (chunk) => { lines.push(String(chunk)); return true; };
  try {
    const logger = new WorkerLogger();
    const context = { trace_id: "task-1", action: "element.find", started_at: Date.now() };
    logger.info(context, "find", "success", { target_description: "搜索按钮", matches_checked: 2, token: "private-value" });
    logger.error(context, "click", "failed", { error_message: "点击失败 password=private-password 13800138000 candidate@example.com", error_code: "CLICK_FAILED" });
  } finally {
    process.stdout.write = original;
  }
  assert.equal(lines.length, 2);
  const normal = JSON.parse(lines[0]);
  const failure = JSON.parse(lines[1]);
  for (const item of [normal, failure]) {
    for (const field of ["timestamp", "category", "is_error", "error_message", "input", "output"]) {
      assert.ok(Object.hasOwn(item, field), field);
    }
  }
  assert.equal(normal.is_error, false);
  assert.equal(normal.input.target_description, "搜索按钮");
  assert.equal(normal.output.matches_checked, 2);
  assert.equal(normal.token, "[已隐藏]");
  assert.equal(failure.is_error, true);
  assert.equal(failure.output.error_code, "CLICK_FAILED");
  assert.doesNotMatch(JSON.stringify(failure), /private-password/);
  assert.doesNotMatch(JSON.stringify(failure), /13800138000|candidate@example.com/);
});

/** 验证自动回复穿插运行时，普通读写字段也不能把聊天正文带进诊断日志。 */
test("Worker 日志递归隐藏页面读写正文并保留原生诊断", () => {
  const lines = [];
  const logger = new WorkerLogger((line) => lines.push(line));
  logger.info({ trace_id: "reply-private", action: "element.input", started_at: Date.now() }, "verify", "success", {
    input: { text: "候选人私密问题", selector: { text: "私密候选人" } },
    output: { value: "候选人的回答", records: [{ content: "聊天原文", inner_text: "简历原文", text_content: "简历全文" }], native_mouse: { x: 12, y: 34 }, verified: true },
  });
  const entry = JSON.parse(lines[0]);
  assert.doesNotMatch(lines[0], /候选人私密问题|私密候选人|候选人的回答|聊天原文|简历原文|简历全文/);
  assert.equal(entry.input.text, "[已隐藏]");
  assert.equal(entry.input.selector.text, "[已隐藏]");
  assert.equal(entry.output.records[0].content, "[已隐藏]");
  assert.deepEqual(entry.output.native_mouse, { x: 12, y: 34 });
  assert.equal(entry.output.verified, true);
});
