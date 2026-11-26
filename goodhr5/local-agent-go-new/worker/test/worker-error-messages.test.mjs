// 文件作用：验证精简错误正文后仍保留原始异常、原生诊断和取消原因，不把展示文案当作停止或恢复依据。
import assert from "node:assert/strict";
import test from "node:test";
import { WorkerError, normalizeWorkerError } from "../dist/errors/worker-error.js";
import { assertRequestActive } from "../dist/browser/primitives/request-lifecycle.js";

const context = { action: "element.find_all", step: "read_field", trace_id: "error-message-diagnostics" };

test("普通错误正文使用动作说明，完整原始异常和既有诊断继续保留", () => {
  const source = new Error("locator.innerText: Timeout 1500ms exceeded while reading candidate field");
  const error = normalizeWorkerError(source, { ...context, code: "ACTION_TIMEOUT", message: "候选人字段读取超时", retryable: true,
    details: { field_key: "education", item_index: 12, timeout_ms: 1500 } });
  assert.equal(error.message, "候选人字段读取超时");
  assert.equal(error.cause, source);
  assert.equal(error.details.original_error, source.message);
  assert.equal(error.details.field_key, "education");
  assert.equal(error.details.item_index, 12);
  assert.equal(error.details.timeout_ms, 1500);
  assert.equal(error.code, "ACTION_TIMEOUT");
  assert.equal(error.retryable, true);
  const body = error.toBody();
  assert.equal(body.action, context.action);
  assert.equal(body.step, context.step);
  assert.equal(body.trace_id, context.trace_id);
  assert.deepEqual(body.details, error.details);
});

test("已有结构化错误不被外层提示覆盖，原始cause和恢复限制保持原样", () => {
  const source = new Error("底层协议错误");
  const error = new WorkerError({ ...context, code: "WORKER_UNRESPONSIVE", message: "实际输入错误", retryable: false,
    details: { original_error: source.message, recovery_required: true, browser_retained: true }, cause: source });
  assert.equal(normalizeWorkerError(error, { ...context, message: "外层备用说明" }), error);
  assert.equal(error.cause, source);
  assert.equal(error.retryable, false);
  assert.equal(error.details.recovery_required, true);
  assert.equal(error.details.browser_retained, true);
});

for (const scenario of [
  { code: "GOODHR_MOUSE_WINDOW_NOT_READY", message: "浏览器窗口尚未就绪", resultCode: "BROWSER_WINDOW_NOT_READY", stage: "window_ready" },
  { code: "GOODHR_MOUSE_INPUT_TIMEOUT", message: "浏览器输入未完成", resultCode: "WORKER_UNRESPONSIVE", stage: "activation" },
]) {
  test(`${scenario.message}保留原生错误和恢复证据`, () => {
    const native = { schema_version: 1, stage: scenario.stage, recovery_required: true, activation_wait_ms: 15000, events_sent: 0,
      window_at_failure: { window_minimized: true, chrome_has_focus: false } };
    const source = new Error(`${scenario.code}:mousemove GOODHR_MOUSE_DIAGNOSTICS=${JSON.stringify(native)}`);
    const error = normalizeWorkerError(source, { ...context, code: "MOVE_FAILED", message: "移动鼠标失败", retryable: true });
    assert.equal(error.message, scenario.message);
    assert.equal(error.code, scenario.resultCode);
    assert.equal(error.retryable, false);
    assert.equal(error.cause, source);
    assert.equal(error.details.original_error, `${scenario.code}:mousemove`);
    assert.equal(error.details.native_mouse.native_error_code, scenario.code);
    assert.equal(error.details.native_mouse.stage, native.stage);
    assert.equal(error.details.native_mouse.recovery_required, true);
    assert.equal(error.details.native_mouse.activation_wait_ms, 15000);
    assert.equal(error.details.native_mouse.events_sent, 0);
    assert.equal(error.details.native_mouse.window_at_failure.window_minimized, true);
  });
}

test("请求取消与预算耗尽正文区分事实，原因和错误码不变", () => {
  const cancelled = new AbortController();
  cancelled.abort();
  for (const [signal, deadline, expectedMessage, reason] of [
    [cancelled.signal, Date.now() + 10000, "浏览器请求已取消", "request_cancelled"],
    [undefined, Date.now() - 1, "浏览器请求超时", "deadline_exceeded"],
  ]) {
    assert.throws(() => assertRequestActive({ ...context, started_at: Date.now(), request_id: "read-request", signal, deadline_at: deadline }), error => {
      assert.equal(error.message, expectedMessage);
      assert.equal(error.code, "ACTION_CANCELLED");
      assert.equal(error.retryable, false);
      assert.equal(error.details.reason, reason);
      assert.equal(error.details.request_id, "read-request");
      return true;
    });
  }
});
