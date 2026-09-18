// 文件作用说明：验证卡死隔离、排队取消和超时后的禁止重放，防止拖选与重复发送再次出现。
import assert from "node:assert/strict";
import test from "node:test";
import { ActionQueue } from "../dist/browser/actions/action-queue.js";
import { ViewportPrimitive } from "../dist/browser/primitives/viewport.js";

/** context 创建带短超时的测试请求，不修改实际生产默认值。 */
function context(milliseconds, signal) {
  return { trace_id: "test", action: "element.scroll", started_at: Date.now(), deadline_at: Date.now() + milliseconds, signal };
}

test("正在执行的操作卡死后立即隔离所有排队动作，旧操作后来结束也不能放行", async () => {
  let finish;
  let recoveries = 0;
  let sends = 0;
  const queue = new ActionQueue(() => { recoveries++; });
  const first = queue.run(() => new Promise(resolve => { finish = resolve; }), context(30));
  const second = queue.run(async () => { sends++; }, context(500));
  await Promise.all([
    assert.rejects(first, { code: "WORKER_UNRESPONSIVE" }),
    assert.rejects(second, { code: "WORKER_UNRESPONSIVE" }),
  ]);
  finish();
  await assert.rejects(queue.run(async () => { sends++; }), { code: "WORKER_UNRESPONSIVE" });
  assert.equal(sends, 0);
  assert.equal(recoveries, 1);
  assert.equal(queue.status().blocked, true);
});

test("等待期间断开连接的请求永远不会补执行，也不影响前一正常操作", async () => {
  const queue = new ActionQueue();
  let finish;
  let sends = 0;
  const controller = new AbortController();
  const first = queue.run(() => new Promise(resolve => { finish = resolve; }), context(500));
  const second = queue.run(async () => { sends++; }, context(500, controller.signal));
  const checked = assert.rejects(second, { code: "ACTION_CANCELLED" });
  controller.abort();
  await checked;
  finish();
  await first;
  await queue.run(async () => { sends++; });
  assert.equal(sends, 1);
  assert.equal(queue.status().blocked, false);
});

test("正在执行时连接断开会触发隔离，不能在后台继续处理下一条消息", async () => {
  const controller = new AbortController();
  let recoveries = 0;
  const queue = new ActionQueue(() => { recoveries++; });
  const active = queue.run(() => new Promise(() => {}), context(500, controller.signal));
  await Promise.resolve();
  controller.abort();
  await assert.rejects(active, { code: "WORKER_UNRESPONSIVE" });
  assert.equal(recoveries, 1);
});

test("自动窗口尺寸的截图必须设置独立超时", async () => {
  await assert.rejects(new ViewportPrimitive().size({
    viewportSize() { return null; },
    async screenshot(options) { assert.equal(options.timeout, 5000); throw new Error("capture timeout"); },
  }), /capture timeout/);
});
