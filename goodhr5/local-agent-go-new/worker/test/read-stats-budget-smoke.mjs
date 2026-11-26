// 文件作用说明：手动运行两个隔离 HTTP 队列，验证约46秒整批读取在43秒预算下超时、118秒预算下成功；不进入日常单测。
// 用法：npm run build 后执行 node test/read-stats-budget-smoke.mjs <证据目录>，不连接或下载真实浏览器。
import assert from 'node:assert/strict';
import http from 'node:http';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomUUID } from 'node:crypto';
import { performance } from 'node:perf_hooks';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { WorkerRouter } from '../dist/http/router.js';

/** deferred 控制本次独立字段读取的返回时间，不接触用户页面或资料。 */
function deferred() { let done; const promise = new Promise(resolve => { done = resolve; }); return { promise, resolve: done }; }

/** harness 创建独立真实 HTTP/队列链路，只把标准浏览器读取替换成受控结果。 */
async function harness(release) {
  const actions = new ActionService(), entered = deferred();
  const page = { isClosed: () => false }, rows = [{ index: 0 }, { index: 1 }];
  actions.findAction.session.requirePage = async () => page;
  actions.findAction.session.elements.remember = (_page, row) => `synthetic-${row.index}`;
  actions.findAction.primitive.resolveAll = async () => rows.map(locator => ({ locator, page }));
  actions.findAction.primitive.resolveRelative = async (_page, row) => ({ locator: { field: true, index: row.index } });
  actions.findAction.readPrimitive.text = async locator => {
    if (locator.field && locator.index === 1) { entered.resolve(); await release.promise; }
    return locator.field ? '' : 'synthetic-card';
  };
  const router = new WorkerRouter(actions);
  const server = http.createServer((req, res) => void router.handle(req, res));
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  /** inspect 通过既有健康接口只读取数量现场和队列状态。 */
  const inspect = async id => (await (await fetch(base + '/health', { headers: { 'x-inspect-request-id': id } })).json()).data;
  /** read 发出正式字段读取请求，客户端仅设整体安全上限，不代替Worker预算。 */
  const read = timeout => {
    const id = randomUUID(), started = performance.now();
    const response = fetch(base + '/api/v1/element/find-all', { method: 'POST', signal: AbortSignal.timeout(70_000),
      headers: { 'content-type': 'application/json', 'x-request-id': id, 'x-worker-timeout-ms': String(timeout) },
      body: JSON.stringify({ selector: { description: 'synthetic-list', target: { selectors: [{ type: 'css', value: '.row' }] } },
        max_items: 100, expected_missing: true, read_stats: true,
        fields: { optional: { description: 'synthetic-field', target: { selectors: [{ type: 'css', value: '.field' }] } } } }),
    }).then(async response => ({ status: response.status, body: await response.json(), elapsed_ms: Math.round(performance.now() - started) }));
    return { id, response };
  };
  /** close 仅关闭本验收创建的HTTP监听，读取门闩由外层统一释放。 */
  const close = async () => { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); };
  return { entered, inspect, read, close };
}

/** main 同时对照旧新预算，超时后先取冻结快照，再释放相同的46秒读取。 */
async function main() {
  assert.ok(process.argv[2], '请传入本次验收证据目录');
  const output = resolve(process.argv[2]);
  await mkdir(output, { recursive: true });
  const release = deferred(), oldQueue = await harness(release), newQueue = await harness(release);
  let timer;
  try {
    const old = oldQueue.read(43_000), current = newQueue.read(118_000);
    // 立即挂接两个响应，任何意外失败都进入本次清理，不产生未处理拒绝。
    const newOutcome = current.response.then(value => ({ value }), error => ({ error }));
    await Promise.all([oldQueue.entered.promise, newQueue.entered.promise]);
    const heldAt = performance.now();
    timer = setTimeout(() => release.resolve(), 46_000);
    const oldResult = await old.response;
    assert.equal(oldResult.body.ok, false);
    assert.equal(oldResult.body.error.code, 'WORKER_UNRESPONSIVE');
    assert.equal(oldResult.body.error.details.timeout_ms, 43_000);
    const frozen = oldResult.body.error.details.request_progress.read_stats;
    assert.equal(frozen.planned_count, 2);
    assert.equal(frozen.raw_completed_count, 1);
    assert.equal(frozen.returned_count, undefined);
    assert.ok(oldResult.elapsed_ms >= 42_000 && oldResult.elapsed_ms < 46_000);
    const oldBeforeDrain = await oldQueue.inspect(old.id);
    assert.equal(oldBeforeDrain.queue.blocked, true);
    assert.deepEqual(oldBeforeDrain.request_progress.read_stats, frozen);
    const outcome = await newOutcome;
    if (outcome.error) throw outcome.error;
    const next = outcome.value;
    assert.equal(next.status, 200);
    assert.equal(next.body.ok, true);
    assert.ok(Array.isArray(next.body.data));
    assert.deepEqual(next.body.data.map(item => item.index), [0, 1]);
    assert.equal(next.body.read_stats.planned_count, 2);
    assert.equal(next.body.read_stats.raw_completed_count, 2);
    assert.equal(next.body.read_stats.returned_count, 2);
    assert.ok(next.elapsed_ms >= 45_000 && next.elapsed_ms < 65_000);
    let oldAfterDrain;
    for (let attempt = 0; attempt < 50; attempt++) {
      oldAfterDrain = await oldQueue.inspect(old.id);
      if (!oldAfterDrain.queue.blocked) break;
      await nextTurn();
    }
    assert.equal(oldAfterDrain.queue.blocked, false);
    assert.deepEqual(oldAfterDrain.request_progress.read_stats, frozen);
    const result = { success: true, checked_at: new Date().toISOString(), independent_queues: true,
      held_read_elapsed_ms: Math.round(performance.now() - heldAt),
      old: { timeout_ms: 43_000, http_status: oldResult.status, elapsed_ms: oldResult.elapsed_ms,
        error_code: oldResult.body.error.code, read_stats: frozen, blocked_before_drain: true,
        blocked_after_drain: false, counts_frozen_after_drain: true },
      current: { timeout_ms: 118_000, http_status: next.status, elapsed_ms: next.elapsed_ms,
        data_is_array: true, returned_indices: next.body.data.map(item => item.index), read_stats: next.body.read_stats } };
    await writeFile(resolve(output, 'worker-budget-smoke-result.json'), JSON.stringify(result, null, 2) + '\n');
    console.log('WORKER_BUDGET_SMOKE_OK ' + JSON.stringify(result));
  } finally {
    clearTimeout(timer);
    release.resolve();
    await Promise.all([oldQueue.close(), newQueue.close()]);
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
