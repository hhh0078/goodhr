// 文件作用说明：通过真实 HTTP 路由验证列表读取统计、原数组兼容及取消后的冻结，页面读取仅使用受控替身。
import assert from 'node:assert/strict';
import test from 'node:test';
import http from 'node:http';
import { randomUUID } from 'node:crypto';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { WorkerRouter } from '../dist/http/router.js';
import { WorkerError } from '../dist/errors/worker-error.js';
import { parseElementFindAllRequest } from '../dist/validation/action-requests.js';

const selector = { description: 'synthetic-row', target: { selectors: [{ type: 'css', value: '.row' }] } };

/** deferred 创建可控制的读取结束点，不使用真实招聘页面。 */
function deferred() { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; }

/** harness 复用正式 ActionService、查找动作和路由，只替换标准页面读取结果。 */
async function harness(t, count = 3) {
  const actions = new ActionService(), release = deferred(), partial = deferred();
  const completed = new Set(), roots = Array.from({ length: count }, (_, index) => ({ index }));
  const page = { isClosed: () => false, url: () => 'http://127.0.0.1/fixture' };
  const view = { visible: true, box: { x: 0, y: 0, width: 100, height: 20 }, viewport: { width: 1024, height: 768 } };
  const action = actions.findAction;
  action.session.requirePage = async () => page;
  action.session.elements.remember = (_page, root) => {
    completed.add(root.index);
    if (completed.size === count - 1) partial.resolve();
    return `ref-${root.index}`;
  };
  action.primitive.resolveAll = async () => roots.map((locator, index) => ({ locator, page, view, list_index: index + 40 }));
  action.primitive.resolveRelative = async (_page, root) => ({ locator: { field: true, index: root.index }, view, attempts: [] });
  action.readPrimitive.text = async locator => {
    if (locator.field) return '';
    return `private-card-${locator.index}`;
  };
  const server = http.createServer((req, res) => void new WorkerRouter(actions).handle(req, res));
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  t.after(async () => { release.resolve(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); });
  /** read 发送正式列表协议，最大数刻意大于实际匹配数。 */
  const read = (extra = {}, options = {}) => {
    const id = randomUUID();
    const response = fetch(base + '/api/v1/element/find-all', { method: 'POST', headers: {
      'content-type': 'application/json', 'x-request-id': id, 'x-worker-timeout-ms': String(options.timeout ?? 2000),
    }, body: JSON.stringify({ selector, max_items: 100, expected_missing: true, fields: { optional: selector }, ...extra }),
    ...(options.signal ? { signal: options.signal } : {}) }).then(res => res.json());
    return { id, response };
  };
  /** inspect 仅通过已有健康接口查看本请求的冻结进度。 */
  const inspect = async id => (await (await fetch(base + '/health', { headers: { 'x-inspect-request-id': id } })).json()).data;
  return { action, actions, roots, read, inspect, release, partial };
}

test('显式统计保持原data数组、真实计划数、空字段及成功返回数量', async t => {
  const h = await harness(t);
  const legacy = await h.read().response;
  const { id, response } = h.read({ read_stats: true, unknown_metadata: { ignored: true } });
  const result = await response;
  assert.equal(result.ok, true);
  assert.ok(Array.isArray(result.data));
  assert.deepEqual(result.data, legacy.data);
  assert.equal(result.data[0].fields.optional, '');
  assert.equal(legacy.read_stats, undefined);
  assert.deepEqual({ ...result.read_stats, elapsed_ms: 0 }, {
    request_id: id, elapsed_ms: 0, planned_count: 3, raw_completed_count: 3, returned_count: 3,
  });
  assert.ok(result.read_stats.elapsed_ms >= 0);
  assert.doesNotMatch(JSON.stringify(result.read_stats), /private-card|optional|\.row/);
  assert.deepEqual((await h.inspect(id)).request_progress.read_stats, result.read_stats);
});

test('真实空列表成功统计为零，不把max_items当计划人数', async t => {
  const h = await harness(t, 0);
  const result = await h.read({ read_stats: true }).response;
  assert.deepEqual(result.data, []);
  assert.equal(result.read_stats.planned_count, 0);
  assert.equal(result.read_stats.raw_completed_count, 0);
  assert.equal(result.read_stats.returned_count, 0);
});

test('乱序完成时只统计整卡，原返回顺序保持不变', async t => {
  const h = await harness(t);
  h.action.readPrimitive.text = async locator => {
    if (locator.field && locator.index === 1) await h.release.promise;
    return locator.field ? '' : `private-card-${locator.index}`;
  };
  const { id, response } = h.read({ read_stats: true });
  await h.partial.promise;
  const running = (await h.inspect(id)).request_progress.read_stats;
  assert.equal(running.planned_count, 3);
  assert.equal(running.raw_completed_count, 2);
  assert.equal(running.returned_count, undefined);
  h.release.resolve();
  const result = await response;
  assert.deepEqual(result.data.map(item => item.index), [40, 41, 42]);
  assert.equal(result.read_stats.raw_completed_count, 3);
});

for (const cancel of [false, true]) {
  test(cancel ? 'HTTP取消冻结部分数量，晚到排空不能增加完成数' : '整请求超时冻结部分数量，停止保护与排空行为保持', { timeout: 5000 }, async t => {
    const h = await harness(t, 2), controller = new AbortController();
    h.action.readPrimitive.text = async locator => {
      if (locator.field && locator.index === 1) await h.release.promise;
      return locator.field ? '' : `private-card-${locator.index}`;
    };
    const { id, response } = h.read({ read_stats: true }, { timeout: cancel ? 2000 : 150, signal: controller.signal });
    // 在浏览器命令仍未返回时就登记拒绝处理，避免取消形成未处理异常。
    const outcome = response.then(value => ({ value }), error => ({ error }));
    await h.partial.promise;
    if (cancel) controller.abort();
    const done = await outcome;
    if (cancel) assert.equal(done.error?.name, 'AbortError');
    else {
      assert.equal(done.value.error.code, 'WORKER_UNRESPONSIVE');
      assert.equal(done.value.error.details.request_progress.read_stats.raw_completed_count, 1);
      assert.equal(done.value.error.details.request_progress.read_stats.returned_count, undefined);
    }
    let health;
    for (let attempt = 0; attempt < 50; attempt++) {
      health = await h.inspect(id);
      if (health.queue.blocked) break;
      await nextTurn();
    }
    assert.equal(health.queue.blocked, true);
    const frozen = health.request_progress.read_stats;
    assert.equal(frozen.planned_count, 2);
    assert.equal(frozen.raw_completed_count, 1);
    assert.equal(frozen.returned_count, undefined);
    h.release.resolve();
    for (let attempt = 0; attempt < 50; attempt++) {
      health = await h.inspect(id);
      if (!health.queue.blocked) break;
      await nextTurn();
    }
    assert.equal(health.queue.blocked, false);
    assert.deepEqual(health.request_progress.read_stats, frozen);
  });
}

test('计划解析失败只留耗时，未知人数不假装为零', async t => {
  const h = await harness(t);
  h.action.primitive.resolveAll = async () => { throw new WorkerError({ code: 'ELEMENT_NOT_FOUND', action: 'element.find_all', step: 'resolve_list', trace_id: 'synthetic', message: '未找到列表' }); };
  const result = await h.read({ read_stats: true }).response;
  assert.equal(result.ok, false);
  const stats = result.error.details.request_progress.read_stats;
  assert.deepEqual(Object.keys(stats).sort(), ['elapsed_ms', 'request_id']);
});

test('正式读取在排队时超时也保留耗时，未解析的计划和完成数量仍未知', async t => {
  const h = await harness(t, 1), entered = deferred();
  h.action.readPrimitive.text = async locator => {
    if (locator.field) { entered.resolve(); await h.release.promise; }
    return locator.field ? '' : 'private-card';
  };
  const active = h.read();
  await entered.promise;
  const { id, response } = h.read({ read_stats: true }, { timeout: 150 });
  const result = await response;
  assert.equal(result.error.code, 'ACTION_CANCELLED');
  const stats = result.error.details.request_progress.read_stats;
  assert.equal(stats.request_id, id);
  assert.ok(stats.elapsed_ms >= 150);
  assert.deepEqual(Object.keys(stats).sort(), ['elapsed_ms', 'request_id']);
  assert.equal((await h.inspect(id)).queue.blocked, false);
  h.release.resolve();
  assert.equal((await active.response).ok, true);
  assert.deepEqual((await h.inspect(id)).request_progress.read_stats, stats);
});

test('旧调用、关闭统计、探测、视口和无字段请求均不生成统计', async t => {
  const h = await harness(t);
  for (const extra of [{}, { read_stats: false }, { read_stats: 'true' }, { read_stats: true, probe_only: true },
    { read_stats: true, fields: {} }, { read_stats: true, fields: undefined }, { read_stats: true, fields: {}, probe_only: true, max_items: 1, viewport_start: {} }]) {
    const { id, response } = h.read(extra);
    const result = await response;
    assert.equal(result.ok, true);
    assert.equal(result.read_stats, undefined);
    assert.equal((await h.inspect(id)).request_progress.read_stats, undefined);
  }
  assert.equal(parseElementFindAllRequest({ selector, read_stats: true }, 'trace', 'element.find_all').read_stats, true);
  assert.equal(parseElementFindAllRequest({ selector }, 'trace', 'element.find_all').read_stats, undefined);
});

test('已读整卡因字段补读发生变化后重读，同一槽位只算一次', async t => {
  const h = await harness(t, 1);
  let rootReads = 0, fieldReads = 0;
  h.action.readPrimitive.text = async locator => locator.field ? (++fieldReads > 1 ? 'filled' : '') : (++rootReads > 2 ? 'new-card' : 'old-card');
  const result = await h.read({ read_stats: true, fields: { name: selector } }).response;
  assert.equal(result.ok, true);
  assert.ok(rootReads >= 5);
  assert.equal(result.data[0].text, 'new-card');
  assert.equal(result.read_stats.raw_completed_count, 1);
  assert.equal(result.read_stats.returned_count, 1);
});
