// 文件作用说明：验证取消、超时与排队读取不会叠加负载，且真正卡住的旧读取仍保持隔离。
import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { ReadPrimitive } from '../dist/browser/primitives/read.js';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { actionContextStorage } from '../dist/logging/operation.js';

/** deferred 创建受控读取，用来精确观测取消前后仍未结束的协议命令。 */
function deferred() { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; }
/** context 创建独立请求，普通读取拥有充分预算，超时场景单独指定。 */
function context(action, signal, budget = 5000) { return { action, signal, trace_id: 'read-cancel', started_at: Date.now(), deadline_at: Date.now() + budget }; }

/** harness 提供没有平台逻辑的字段读取替身，统计请求取消后的新增读取及真实并发峰值。 */
function harness(count = 100) {
  const entered = deferred(), release = deferred();
  let reads = 0, active = 0, peak = 0;
  const page = { url: () => 'http://127.0.0.1/fixture', isClosed: () => false };
  /** textRead 模拟一个已经发出但尚未返回的标准 Locator 读取。 */
  const textRead = async () => { reads++; active++; peak = Math.max(peak, active); entered.resolve(); try { await release.promise; return 'synthetic'; } finally { active--; } };
  const resolved = Array.from({ length: count }, (_, index) => ({ page, list_index: index, locator: { innerText: textRead }, view: { visible: true }, matched_selector: { type: 'css', value: '.card' }, attempts: [] }));
  const actions = new ActionService();
  actions.session.requirePage = async () => page;
  actions.locator.resolveAll = async () => resolved;
  actions.locator.resolveRelative = async () => ({ locator: { innerText: textRead }, view: { visible: true }, attempts: [] });
  const selector = { description: 'synthetic', target: { selectors: [{ type: 'css', value: '.card' }] } };
  const request = { selector, max_items: count, read_concurrency: 100, fields: { name: selector, age: selector } };
  return { actions, request, entered, release, state: () => ({ reads, active, peak }) };
}

test('取消列表请求后不再开始字段或补读，旧读取排空后新请求不会叠加', async () => {
  const h = harness(); const cancel = new AbortController();
  const pending = h.actions.findElements(h.request, context('element.find_all', cancel.signal));
  await h.entered.promise; await nextTurn();
  assert.equal(h.state().active, 4);
  cancel.abort();
  await assert.rejects(pending, { code: 'WORKER_UNRESPONSIVE' });
  const before = h.state().reads;
  assert.equal(h.actions.healthStatus().queue.active_action, 'element.find_all');
  await assert.rejects(h.actions.findElements(h.request, context('element.find_all')), { code: 'WORKER_UNRESPONSIVE' });
  assert.equal(h.state().reads, before);
  h.release.resolve(); await nextTurn();
  assert.equal(h.state().active, 0);
  assert.equal(h.state().reads, before);
  assert.equal(h.actions.healthStatus().queue.blocked, false);
  const result = await h.actions.findElements(h.request, context('element.find_all'));
  assert.equal(result.length, 100);
  assert.equal(h.state().peak, 4);
});

test('没有断开 HTTP 连接的纯截止时间超时也停止后续读取', async () => {
  const h = harness();
  const pending = h.actions.findElements(h.request, context('element.find_all', undefined, 100));
  await h.entered.promise;
  await assert.rejects(pending, { code: 'WORKER_UNRESPONSIVE' });
  const before = h.state().reads;
  h.release.resolve(); await nextTurn();
  assert.equal(h.state().reads, before);
  assert.equal(h.actions.healthStatus().queue.active_action, '');
});

test('两个有效列表请求顺序执行，共享最多四个读取槽位', async () => {
  const h = harness(3);
  const first = h.actions.findElements(h.request, context('element.find_all'));
  await h.entered.promise;
  const second = h.actions.findElements(h.request, context('element.find_all'));
  await nextTurn();
  assert.equal(h.state().reads, 3);
  h.release.resolve();
  assert.deepEqual((await Promise.all([first, second])).map(items => items.length), [3, 3]);
  assert.ok(h.state().peak <= 4);
});

test('已取消的单元素查找与读取不会发出任何浏览器命令', async () => {
  const actions = new ActionService(); const cancel = new AbortController(); cancel.abort();
  let calls = 0;
  actions.findAction.one = async () => { calls++; throw new Error('unexpected'); };
  actions.readAction.execute = async () => { calls++; throw new Error('unexpected'); };
  await assert.rejects(actions.findElement({}, context('element.find', cancel.signal)), { code: 'ACTION_CANCELLED' });
  await assert.rejects(actions.readElement({}, context('element.read', cancel.signal)), { code: 'ACTION_CANCELLED' });
  assert.equal(calls, 0);
});

test('读取原子能力收到取消后不会调用浏览器，不能把取消吞成空字符串', async () => {
  const cancel = new AbortController(); cancel.abort();
  const read = new ReadPrimitive(); let calls = 0;
  const locator = { innerText: async () => { calls++; return 'unexpected'; } };
  await assert.rejects(actionContextStorage.run(context('element.read', cancel.signal), () => read.text(locator)), { code: 'ACTION_CANCELLED' });
  assert.equal(calls, 0);
});


test('字段读取取消后必须等两个已发出命令都结束，不能提前解除隔离', async () => {
  const h = harness(1); const first = deferred(), second = deferred();
  let fieldCalls = 0;
  h.actions.locator.resolveAll = async () => [{ page: {}, locator: { innerText: async () => 'synthetic' }, view: { visible: true }, matched_selector: { type: 'css', value: '.card' }, attempts: [] }];
  h.actions.locator.resolveRelative = async () => ({ locator: { innerText: async () => {
    const current = fieldCalls++;
    if (fieldCalls === 2) h.entered.resolve();
    await (current === 0 ? first.promise : second.promise);
    return 'synthetic';
  } }, view: { visible: true }, attempts: [] });
  const cancel = new AbortController();
  const pending = h.actions.findElements(h.request, context('element.find_all', cancel.signal));
  await h.entered.promise;
  cancel.abort();
  await assert.rejects(pending, { code: 'WORKER_UNRESPONSIVE' });
  first.resolve(); await nextTurn();
  assert.equal(h.actions.healthStatus().queue.blocked, true);
  assert.equal(h.actions.healthStatus().queue.active_action, 'element.find_all');
  second.resolve(); await nextTurn();
  assert.equal(h.actions.healthStatus().queue.blocked, false);
  assert.equal(h.actions.healthStatus().queue.active_action, '');
  assert.equal(fieldCalls, 2);
  assert.equal(h.actions.session.elements.entries.size, 0);
});

test('选择器查询期间取消不会继续备用选择器或吞成元素未找到', async () => {
  const cancel = new AbortController(); let calls = 0;
  const page = { url: () => 'http://127.0.0.1/fixture', locator: () => ({ count: async () => { calls++; cancel.abort(); return 0; } }) };
  const spec = { description: 'synthetic', target: { selectors: [{ type: 'css', value: '.first' }, { type: 'css', value: '.fallback' }] }, timeout_ms: 1000 };
  const request = context('element.find_all', cancel.signal);
  await assert.rejects(actionContextStorage.run(request, () => new LocatorPrimitive().resolveAll(page, spec, 100, { trace_id: request.trace_id, action: request.action, step: 'find_all' })), { code: 'ACTION_CANCELLED' });
  assert.equal(calls, 1);
});
