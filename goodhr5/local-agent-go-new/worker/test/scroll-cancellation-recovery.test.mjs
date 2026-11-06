// 文件作用：复现滚动子预算耗尽后立即进行身份读取，并验证排空、原生故障与点击发送隔离边界。
import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { ActionQueue } from '../dist/browser/actions/action-queue.js';
import { WorkerError } from '../dist/errors/worker-error.js';
import { actionContextStorage } from '../dist/logging/operation.js';
import { trackNativeInput } from '../dist/browser/primitives/input-lifecycle.js';
import { WorkerRouter } from '../dist/http/router.js';

/** deferred 提供受测试显式控制的异步结束点，不接触实际浏览器。 */
function deferred() { let resolve; const promise = new Promise(value => { resolve = value; }); return { promise, resolve }; }
/** context 创建独立预算，测试只缩短请求时间，不改变生产默认限时。 */
function context(action, budget = 2000, signal) { return { action, trace_id: 'scroll-cancel-fixture', started_at: Date.now(), deadline_at: Date.now() + budget, signal }; }
/** turn 等待队列的finally完成，不使用固定长暂停。 */
function turn() { return new Promise(resolve => setImmediate(resolve)); }

test('真实service滚动预算耗尽，紧随到达的身份读取等旧操作排空后成功', async () => {
  const actions = new ActionService(), entered = deferred(), release = deferred();
  let reads = 0, wheels = 0;
  actions.scrollAction.execute = async () => {
    wheels++; entered.resolve();
    const execution = actionContextStorage.getStore();
    await release.promise;
    execution.signal.throwIfAborted();
    wheels++;
  };
  actions.readAction.execute = async () => { reads++; return { value: '原候选人' }; };
  const first = actions.scroll({}, context('element.scroll', 30));
  await entered.promise;
  await assert.rejects(first, { code: 'WORKER_UNRESPONSIVE' });
  const next = actions.readElement({}, context('element.read'));
  await turn(); assert.equal(reads, 0); assert.equal(actions.healthStatus().queue.blocked, true);
  release.resolve();
  assert.deepEqual(await next, { value: '原候选人' });
  assert.equal(wheels, 1); assert.equal(reads, 1); assert.equal(actions.healthStatus().queue.blocked, false);
});

test('生产ScrollAction在阅读停顿耗尽预算后不会再发送下一轮滚轮', async () => {
  const actions = new ActionService(); let wheels = 0, reads = 0;
  const random = Math.random; Math.random = () => 0;
  const page = { viewportSize: () => ({ width: 900, height: 700 }), screenshot: async () => Buffer.from('synthetic-png'),
    mouse: { move: async () => {}, wheel: async () => { wheels++; } }, url: () => 'https://fixture.invalid/', isClosed: () => false,
    frames: () => [], context: () => ({ pages: () => [page] }) };
  actions.session.requirePage = async () => page;
  actions.readAction.execute = async () => { reads++; return { value: 'same-person' }; };
  try {
    await assert.rejects(actions.scroll({ distance: 620, max_attempts: 2, wait_ms: 800 }, context('element.scroll', 150)), { code: 'WORKER_UNRESPONSIVE' });
    await actions.readElement({}, context('element.read'));
    assert.equal(wheels, 3); assert.equal(reads, 1); assert.equal(actions.healthStatus().queue.blocked, false);
  } finally { Math.random = random; }
});

test('HTTP预算响应先结束，客户端随后取消不会误判为控制断开', async t => {
  const actions = new ActionService(), release = deferred(); let requestSignal, reads = 0;
  const scroll = actions.scroll.bind(actions);
  actions.scroll = (request, current) => { requestSignal = current.signal; return scroll(request, current); };
  actions.scrollAction.execute = async () => { const execution = actionContextStorage.getStore(); await release.promise; execution.signal.throwIfAborted(); };
  actions.readAction.execute = async () => { reads++; return { value: 'same-person' }; };
  const router = new WorkerRouter(actions);
  const server = http.createServer((request, response) => void router.handle(request, response));
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => { release.resolve(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); });
  const base = `http://127.0.0.1:${server.address().port}`;
  const client = new AbortController();
  const first = await fetch(`${base}/api/v1/element/scroll`, { method: 'POST', signal: client.signal,
    headers: { 'Content-Type': 'application/json', 'X-Worker-Timeout-Ms': '30' }, body: JSON.stringify({ distance: 620 }) });
  assert.equal((await first.json()).error.code, 'WORKER_UNRESPONSIVE');
  client.abort(); await turn();
  assert.equal(requestSignal.aborted, false, '完成的响应不应再因HTTP关闭取消原请求');
  const second = fetch(`${base}/api/v1/element/read`, { method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ selector: { target: { selectors: [{ type: 'css', value: '.name' }] } } }) });
  await turn(); assert.equal(reads, 0);
  release.resolve();
  const response = await second;
  assert.equal(response.status, 200); assert.equal((await response.json()).data.value, 'same-person');
  assert.equal(reads, 1); assert.equal(actions.healthStatus().queue.blocked, false);
});

test('滚动取消后未排空的读取仍拒绝，不能与旧操作并发', async () => {
  const queue = new ActionQueue(), release = deferred(); let reads = 0;
  const first = queue.runScroll(async () => { const execution = actionContextStorage.getStore(); await release.promise; execution.signal.throwIfAborted(); }, context('element.scroll', 20));
  await assert.rejects(first, { code: 'WORKER_UNRESPONSIVE' });
  await assert.rejects(queue.runRead(async () => { reads++; }, context('element.read', 25)), { code: 'WORKER_UNRESPONSIVE' });
  assert.equal(reads, 0); assert.equal(queue.status().blocked, true);
  release.resolve(); await turn();
});

test('原生输入未结束时不放行，迟到原生超时仍保持隔离', async () => {
  const queue = new ActionQueue(), release = deferred(), failed = deferred(); let reads = 0;
  const first = queue.runScroll(async () => {
    const native = trackNativeInput(() => release.promise);
    void native.catch(() => {});
    await failed.promise;
    throw new WorkerError({ code: 'WORKER_UNRESPONSIVE', message: '合成原生限时', action: 'element.scroll', step: 'mouse_wheel', trace_id: 'fixture' });
  }, context('element.scroll', 20));
  await assert.rejects(first, { code: 'WORKER_UNRESPONSIVE' });
  const next = queue.runRead(async () => { reads++; }, context('element.read'));
  const checked = assert.rejects(next, { code: 'WORKER_UNRESPONSIVE' });
  failed.resolve(); await turn();
  assert.equal(queue.status().active_action, 'element.scroll'); assert.equal(reads, 0);
  release.resolve(); await checked;
  assert.equal(queue.status().blocked, true); assert.equal(reads, 0);
});

test('取消期间出现真实控制通道错误时不自动解除隔离', async () => {
  const queue = new ActionQueue(), release = deferred();
  const first = queue.runScroll(async () => { await release.promise; throw new Error('synthetic control channel closed'); }, context('element.scroll', 20));
  await assert.rejects(first, { code: 'WORKER_UNRESPONSIVE' });
  release.resolve(); await turn();
  await assert.rejects(queue.runRead(async () => {}, context('element.read')), { code: 'WORKER_UNRESPONSIVE' });
});

test('连接断开与点击预算耗尽继续保持原有保护，不恢复或重放输入', async () => {
  for (const kind of ['disconnect', 'click']) {
    const queue = new ActionQueue(), release = deferred(), cancel = new AbortController(); let sends = 0;
    const operation = async () => { const execution = actionContextStorage.getStore(); await release.promise; execution.signal.throwIfAborted(); sends++; };
    const first = kind === 'click' ? queue.run(operation, context('element.click', 20)) : queue.runScroll(operation, context('element.scroll', 2000, cancel.signal));
    if (kind === 'disconnect') { await turn(); cancel.abort(); }
    await assert.rejects(first, { code: 'WORKER_UNRESPONSIVE' });
    release.resolve(); await turn();
    await assert.rejects(queue.runRead(async () => { sends++; }, context('element.read')), { code: 'WORKER_UNRESPONSIVE' });
    assert.equal(sends, 0);
  }
});
