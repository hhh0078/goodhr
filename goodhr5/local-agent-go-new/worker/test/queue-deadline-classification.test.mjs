// 文件作用：用受控时钟验证队列预算先于定时器被发现时仍正确隔离，并区分用户取消和局部读取预算。
import assert from "node:assert/strict";
import test from "node:test";
import { setImmediate as nextTurn } from "node:timers/promises";
import { ActionQueue } from "../dist/browser/actions/action-queue.js";
import { assertRequestActive } from "../dist/browser/primitives/request-lifecycle.js";

/** controlledClock 只推进逻辑时间，不依赖CPU负载或几十毫秒真实定时器竞争。 */
function controlledClock(t) {
  let now = Date.now();
  t.mock.method(Date, "now", () => now);
  return { advance(value) { now += value; } };
}

/** context 创建拥有充分实际定时预算的合成请求，过期由测试明确推进。 */
function context(action, signal, budget = 60_000) {
  return { action, signal, started_at: Date.now(), deadline_at: Date.now() + budget, trace_id: "queue-deadline-classification" };
}

/** deferred 让测试显式控制已开始请求的完成，不接触真实鼠标、键盘或浏览器。 */
function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

test("操作先于定时器发现整份预算过期，仍返回队列超时并隔离输入", async t => {
  const clock = controlledClock(t), queue = new ActionQueue();
  let sends = 0;
  const operation = queue.run(async () => { clock.advance(60_001); assertRequestActive(); sends++; }, context("element.click"));
  await assert.rejects(operation, error => error.code === "WORKER_UNRESPONSIVE" && error.details.reason === "deadline_exceeded");
  await nextTurn();
  assert.equal(sends, 0);
  assert.equal(queue.status().blocked, true);
  assert.equal(queue.status().recoverable, true);
  await assert.rejects(queue.run(async () => { sends++; }, context("keyboard.press")), { code: "WORKER_UNRESPONSIVE" });
  assert.equal(sends, 0);
});

test("滚轮先发现整份预算过期，排空后仅按已有安全滚轮规则恢复", async t => {
  const clock = controlledClock(t), queue = new ActionQueue();
  let wheels = 0, reads = 0;
  const operation = queue.runScroll(async () => { wheels++; clock.advance(60_001); assertRequestActive(); wheels++; }, context("element.scroll"));
  await assert.rejects(operation, error => error.code === "WORKER_UNRESPONSIVE" && error.details.reason === "deadline_exceeded");
  await nextTurn();
  assert.equal(wheels, 1);
  await queue.runRead(async () => { reads++; }, context("element.read"));
  assert.equal(reads, 1); assert.equal(queue.status().blocked, false);
});

test("开始前用户取消没有页面动作，原因不能误写成预算耗尽", async () => {
  const queue = new ActionQueue(), controller = new AbortController();
  controller.abort();
  let actions = 0;
  await assert.rejects(queue.run(async () => { actions++; }, context("element.click", controller.signal)), error => error.code === "ACTION_CANCELLED" && error.details.reason === "client_disconnected");
  await nextTurn();
  assert.equal(actions, 0); assert.equal(queue.status().blocked, false);
});

test("进行中用户取消滚轮必须保留隔离，不能按纯预算滚轮自动恢复", async () => {
  const queue = new ActionQueue(), controller = new AbortController(), entered = deferred(), release = deferred();
  let actions = 0;
  const operation = queue.runScroll(async () => { entered.resolve(); await release.promise; assertRequestActive(); actions++; }, context("element.scroll", controller.signal));
  await entered.promise;
  controller.abort();
  await assert.rejects(operation, error => error.code === "WORKER_UNRESPONSIVE" && error.details.reason === "client_disconnected");
  release.resolve(); await nextTurn();
  assert.equal(actions, 0); assert.equal(queue.status().blocked, true); assert.equal(queue.status().recoverable, true);
});

test("局部读取预算先过期但整份请求仍有效时不伪装成队列未知输入", async () => {
  const queue = new ActionQueue(), request = context("element.read");
  await assert.rejects(queue.run(async () => assertRequestActive({ ...request, deadline_at: Date.now() - 1 }), request), error => error.code === "ACTION_CANCELLED" && error.details.reason === "deadline_exceeded");
  await nextTurn();
  assert.equal(queue.status().blocked, false);
});
