// 文件作用：验证原生移动回执超时只重试一次移动，并重新读取目标，不重放点击。
import test from 'node:test';
import assert from 'node:assert/strict';
import { MoveAction } from '../dist/browser/actions/move.js';
import { BrowserSession } from '../dist/browser/session/browser-session.js';

/** fixture 用独立计数器区分鼠标移动、目标重读和点击。 */
function fixture(alwaysFail = false) {
  let moves = 0, reads = 0, clicks = 0;
  const view = { visible: true, box: { x: 50, y: 50, width: 100, height: 40 }, viewport: { width: 800, height: 600 } };
  const found = { page: {}, locator: {}, view };
  const action = new MoveAction({
    async move() { moves++; if (moves === 1 || alwaysFail) throw new Error('GOODHR_MOUSE_ACK_TIMEOUT:mousemove'); },
    async down() { clicks++; },
  }, { info() {}, warn() {}, failure() {} }, {
    async view() { reads++; return view; },
  });
  return { action, found, counts: () => ({ moves, reads, clicks }) };
}

test('移动超时后重读目标并仅恢复一次移动', async () => {
  const f = fixture();
  await f.action.toElement(f.found, { action: 'element.click', trace_id: 'test', started_at: Date.now() });
  assert.deepEqual(f.counts(), { moves: 2, reads: 1, clicks: 0 });
});

test('连续移动超时不无限重试', async () => {
  const f = fixture(true);
  await assert.rejects(f.action.toElement(f.found, { action: 'element.click', trace_id: 'test', started_at: Date.now() }));
  assert.deepEqual(f.counts(), { moves: 2, reads: 1, clicks: 0 });
});

test('预算不足时不重试移动', async () => {
  const f = fixture();
  await assert.rejects(f.action.toElement(f.found, { action: 'element.click', trace_id: 'test', started_at: Date.now(), deadline_at: Date.now() + 1000 }));
  assert.deepEqual(f.counts(), { moves: 1, reads: 0, clicks: 0 });
});

/** windowError 构造与实际原生输入相同的零发送窗口错误。 */
function windowError(eventsSent = 0) {
  return new Error('GOODHR_MOUSE_WINDOW_NOT_READY:mousemove GOODHR_MOUSE_DIAGNOSTICS=' +
    JSON.stringify({ schema_version: 1, event_types: 'mousemove', events_sent: eventsSent, events_acknowledged: 0 }));
}

test('窗口未激活且鼠标未发出时，重选标签并重新读取目标后只重试一次移动', async () => {
  let moves = 0, activations = 0, reads = 0;
  const events = [];
  const page = { isClosed: () => false, async bringToFront() { activations++; } };
  const found = { page, locator: {}, view: { visible: true, box: { x: 50, y: 50, width: 100, height: 40 }, viewport: { width: 800, height: 600 } } };
  const action = new MoveAction({ async move() { if (++moves === 1) throw windowError(); } },
    { info(_ctx, step, status) { events.push([step, status]); }, warn(_ctx, step, status) { events.push([step, status]); }, failure() {}, error() {} },
    { async view() { reads++; return found.view; } });
  await action.toElement(found, { action: 'element.scroll', trace_id: 'window', started_at: Date.now() });
  assert.deepEqual({ moves, activations, reads }, { moves: 2, activations: 1, reads: 1 });
  assert.ok(events.some(([step, status]) => step === 'window_recovery' && status === 'retry'));
});

test('恢复一次后仍未激活就停止，不无限重试', async () => {
  let moves = 0, activations = 0;
  const page = { isClosed: () => false, async bringToFront() { activations++; } };
  const view = { visible: true, box: { x: 50, y: 50, width: 100, height: 40 }, viewport: { width: 800, height: 600 } };
  const action = new MoveAction({ async move() { moves++; throw windowError(); } },
    { info() {}, warn() {}, error() {}, failure() {} }, { async view() { return view; } });
  await assert.rejects(action.toElement({ page, locator: {}, view },
    { action: 'element.scroll', trace_id: 'window', started_at: Date.now() }), error => error.code === 'BROWSER_WINDOW_NOT_READY');
  assert.deepEqual({ moves, activations }, { moves: 2, activations: 1 });
});

test('重新激活后无法确认目标位置时保留窗口错误，避免按旧坐标移动', async () => {
  let moves = 0;
  const page = { isClosed: () => false, async bringToFront() {} };
  const view = { visible: true, box: { x: 50, y: 50, width: 100, height: 40 }, viewport: { width: 800, height: 600 } };
  const action = new MoveAction({ async move() { moves++; throw windowError(); } },
    { info() {}, warn() {}, error() {}, failure() {} }, { async view() { throw new Error('目标已变化'); } });
  await assert.rejects(action.toElement({ page, locator: {}, view },
    { action: 'element.scroll', trace_id: 'window', started_at: Date.now() }), error => error.code === 'BROWSER_WINDOW_NOT_READY');
  assert.equal(moves, 1);
});

test('已有原生输入时绝不重选标签或重试', async () => {
  let moves = 0, activations = 0;
  const page = { isClosed: () => false, async bringToFront() { activations++; } };
  const view = { visible: true, box: { x: 50, y: 50, width: 100, height: 40 }, viewport: { width: 800, height: 600 } };
  const action = new MoveAction({ async move() { moves++; throw windowError(1); } },
    { info() {}, warn() {}, failure() {} });
  await assert.rejects(action.toElement({ page, locator: {}, view },
    { action: 'element.scroll', trace_id: 'window', started_at: Date.now() }));
  assert.deepEqual({ moves, activations }, { moves: 1, activations: 0 });
});

test('桌面窗口激活成功后只重选原标签，不创建临时标签', async () => {
  const steps = [];
  const context = { async newPage() { throw new Error('不应创建临时标签'); } };
  const page = { isClosed: () => false, context: () => context,
    async bringToFront() { steps.push('page.front'); } };
  const session = new BrowserSession({ info() {}, error() {} }, async () => ({ status: 'focused', browser_pid: 123 }));
  session.context = context;
  session.currentPage = page;
  await session.reactivatePage(page, { action: 'element.scroll', trace_id: 'window', started_at: Date.now() });
  assert.deepEqual(steps, ['page.front']);
  assert.equal(session.currentPage, page);
});

test('桌面窗口无法激活时切换临时空白标签，随后回到原标签并清理', async () => {
  const steps = [];
  let temporaryClosed = false;
  const temporary = { async bringToFront() { steps.push('temporary.front'); },
    async close() { steps.push('temporary.close'); temporaryClosed = true; }, isClosed: () => temporaryClosed };
  const context = { async newPage() { steps.push('newPage'); return temporary; } };
  const page = { isClosed: () => false, context: () => context,
    async bringToFront() { steps.push('page.front'); } };
  const session = new BrowserSession({ info() {}, error() {} }, async () => ({ status: 'not_found' }));
  session.context = context;
  session.currentPage = page;
  await session.reactivatePage(page, { action: 'element.scroll', trace_id: 'window', started_at: Date.now() });
  assert.deepEqual(steps, ['newPage', 'temporary.front', 'page.front', 'temporary.close']);
  assert.equal(session.currentPage, page);
});

test('滚动锚点落在可见区域中央，避免随机落进顶部固定栏', async () => {
  const points = [];
  const action = new MoveAction({ async move(_page, x, y) { points.push({ x, y }); } }, { info() {}, failure() {} });
  const found = { page: {}, locator: {}, view: {
    visible: true, box: { x: 100, y: -500, width: 1000, height: 2200 }, viewport: { width: 1265.6, height: 920.8 },
  } };
  for (let i = 0; i < 20; i++) await action.toElement(found, { action: 'element.scroll', trace_id: 'test', started_at: Date.now() }, { centered: true });
  assert.ok(points.every(p => p.x >= 400 && p.x <= 800 && p.y > 275 && p.y < 645));
});
