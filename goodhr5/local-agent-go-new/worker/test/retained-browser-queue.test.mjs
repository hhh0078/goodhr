// 文件作用说明：验证故障只隔离输入、保留浏览器，旧动作及原生调用结束后能明确恢复，且不补发历史操作。
import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { ActionQueue } from '../dist/browser/actions/action-queue.js';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { KeyboardPrimitive } from '../dist/browser/primitives/keyboard.js';
import { MousePrimitive } from '../dist/browser/primitives/mouse.js';
import { BrowserSession } from '../dist/browser/session/browser-session.js';
import { WorkerLogger } from '../dist/logging/logger.js';

/** context 创建独立请求，避免上一请求的取消状态影响新的明确启动。 */
function context(action = 'keyboard.press', timeout = 50, signal) {
  return { action, trace_id: 'retain-browser', request_id: action, started_at: Date.now(), deadline_at: Date.now() + timeout, signal };
}

/** deferred 创建可控底层调用，模拟迟到返回而不使用页面注入。 */
function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

/** pageFixture 提供输入和标准页面信息，按真实协议 Promise 记录动作。 */
function pageFixture(mouse, keyboard = {}) {
  const page = { mouse, keyboard, viewportSize: () => ({ width: 1280, height: 900 }), isClosed: () => false,
    url: () => 'http://127.0.0.1/fixture', frames: () => [], context: () => ({ pages: () => [page] }) };
  return page;
}

test('超时保留隔离，旧操作迟到后禁止新的按键，明确重新开始才恢复', async () => {
  const queue = new ActionQueue();
  const gate = deferred();
  const entered = deferred();
  let sends = 0;
  const keyboard = new KeyboardPrimitive();
  const page = pageFixture({}, { press: async () => { sends++; } });
  const first = queue.run(async () => { entered.resolve(); await gate.promise; await keyboard.press(page, 'Enter'); }, context());
  await entered.promise;
  await assert.rejects(first, { code: 'WORKER_UNRESPONSIVE' });
  await assert.rejects(queue.runAfterRecovery(async () => { sends++; }, context('browser.start')), { step: 'queue_recovery_wait' });
  gate.resolve();
  await nextTurn();
  assert.equal(sends, 0);
  assert.equal(queue.status().recoverable, true);
  await assert.rejects(queue.run(async () => { sends++; }, context()), { code: 'WORKER_UNRESPONSIVE' });
  await queue.runAfterRecovery(async () => keyboard.press(page, 'Tab'), context('browser.start', 1000));
  assert.equal(sends, 1);
  assert.equal(queue.status().blocked, false);
});

test('鼠标限时已返回也必须等原生 Promise 结束，不能提前重新启动', async () => {
  const queue = new ActionQueue();
  const raw = deferred();
  const page = pageFixture({ wheel: () => raw.promise });
  const mouse = new MousePrimitive(25);
  await assert.rejects(queue.run(() => mouse.wheel(page, 0, 100), context('page.scroll', 1000)), { step: 'mouse_wheel' });
  assert.equal(queue.status().recoverable, false);
  await assert.rejects(queue.runAfterRecovery(async () => {}, context('browser.start')), { step: 'queue_recovery_wait' });
  raw.resolve();
  await nextTurn();
  assert.equal(queue.status().recoverable, true);
  const cancelled = new AbortController();
  cancelled.abort();
  await assert.rejects(queue.runAfterRecovery(async () => {}, context('browser.start', 1000, cancelled.signal)), { code: 'ACTION_CANCELLED' });
  assert.equal(queue.status().blocked, true);
  await queue.runAfterRecovery(async () => {}, context('browser.start', 1000));
  assert.equal(queue.status().blocked, false);
});

test('进行中断开连接后逐字输入不会继续，新请求也不会继承旧取消信号', async () => {
  const queue = new ActionQueue();
  const controller = new AbortController();
  const entered = deferred();
  const firstKey = deferred();
  const sends = [];
  const keyboard = new KeyboardPrimitive();
  const page = pageFixture({}, { type: async value => { sends.push(value); entered.resolve(); await firstKey.promise; } });
  const request = context('element.input', 1000, controller.signal);
  const input = queue.run(async () => { await keyboard.typeCharacter(page, 'A'); await keyboard.typeCharacter(page, 'B'); }, request);
  await entered.promise;
  controller.abort();
  await assert.rejects(input, { code: 'WORKER_UNRESPONSIVE' });
  firstKey.resolve();
  await nextTurn();
  assert.deepEqual(sends, ['A']);
  assert.equal(request.signal, controller.signal);
  await queue.runAfterRecovery(async () => {}, context('browser.start', 1000));
});

test('取消后只允许已有鼠标按键清理松开，不允许继续新的按下', async () => {
  const queue = new ActionQueue();
  const gate = deferred();
  const pressed = deferred();
  const events = [];
  const page = pageFixture({ down: async () => { events.push('down'); }, up: async () => { events.push('up'); } });
  const mouse = new MousePrimitive();
  const task = queue.run(async () => {
    try { await mouse.down(page, 'left'); pressed.resolve(); await gate.promise; }
    finally { await mouse.up(page, 'left'); }
    await mouse.down(page, 'left');
  }, context('element.click'));
  await pressed.promise;
  await assert.rejects(task, { code: 'WORKER_UNRESPONSIVE' });
  gate.resolve();
  await nextTurn();
  assert.deepEqual(events, ['down', 'up']);
  assert.equal(queue.status().recoverable, true);
});

test('封装入口故障不调用关闭，健康状态可读且明确启动后继续复用', async () => {
  const actions = new ActionService();
  const gate = deferred();
  let stops = 0;
  let starts = 0;
  actions.session.requirePage = async () => pageFixture({}, {});
  actions.session.stop = async () => { stops++; return { running: false }; };
  actions.session.start = async () => { starts++; return { running: true, reused: true }; };
  actions.keyboardPrimitive.press = () => gate.promise;
  await assert.rejects(actions.pressKey({ key: 'Enter' }, context()), { code: 'WORKER_UNRESPONSIVE' });
  assert.equal(stops, 0);
  assert.equal(actions.healthStatus().status, 'ok');
  assert.equal(actions.healthStatus().queue.blocked, true);
  gate.resolve();
  await nextTurn();
  assert.equal((await actions.startBrowser({}, context('browser.start', 1000))).reused, true);
  assert.equal(starts, 1);
  assert.equal(stops, 0);
  await actions.stopBrowser(context('browser.stop', 1000));
  assert.equal(stops, 1);
});

test('启动请求在检查会话时超时，迟到后不得关闭原窗口或清理会话', async () => {
  for (const running of [true, false]) {
    const queue = new ActionQueue();
    const session = new BrowserSession(new WorkerLogger(() => {}));
    const gate = deferred();
    const entered = deferred();
    let closes = 0;
    session.browser = {};
    session.isRunning = async () => { entered.resolve(); await gate.promise; return running; };
    session.stop = async () => { closes++; };
    session.dispose = async () => { closes++; };
    const request = context('browser.start');
    const explicitContext = { ...request, deadline_at: Date.now() + 1000 };
    const task = queue.run(() => session.start({ user_data_dir: 'different-profile' }, explicitContext), request);
    await entered.promise;
    await assert.rejects(task, { code: 'WORKER_UNRESPONSIVE' });
    gate.resolve();
    await nextTurn();
    assert.equal(closes, 0);
    assert.equal(queue.status().recoverable, true);
  }
});

test('排队前已经过期的操作不会开始执行', async () => {
  const queue = new ActionQueue();
  let runs = 0;
  await assert.rejects(queue.run(async () => { runs++; }, context('keyboard.press', -1)), { code: 'ACTION_CANCELLED' });
  assert.equal(runs, 0);
  assert.equal(queue.status().blocked, false);
});

test('省略账号配置时复用原窗口，明确切换配置必须先手动关闭', async () => {
  const session = new BrowserSession(new WorkerLogger(() => {}));
  let closes = 0;
  session.isRunning = async () => true;
  session.status = async reused => ({ running: true, reused, current_url: '' });
  session.stop = async () => { closes++; };
  session.userDataDir = 'original-profile';
  session.extensionPaths = ['original-extension'];
  assert.equal((await session.start({}, context('browser.start', 1000))).reused, true);
  for (const config of [{ user_data_dir: 'another-profile' }, { extension_paths: [] }]) {
    await assert.rejects(session.start(config, context('browser.start', 1000)), { step: 'browser_config_change' });
  }
  assert.equal(closes, 0);
});

test('诊断检查时取消的鼠标按下不得留下记录或补发松开', async () => {
  const controller = new AbortController();
  const queue = new ActionQueue();
  const events = [];
  const page = pageFixture({ down: async () => events.push('down'), up: async () => events.push('up') });
  page.viewportSize = () => { controller.abort(); return { width: 1280, height: 900 }; };
  const mouse = new MousePrimitive();
  const request = queue.run(async () => { try { await mouse.down(page, 'left'); } finally { await mouse.up(page, 'left'); } },
    context('element.click', 1000, controller.signal));
  await assert.rejects(request, { code: 'WORKER_UNRESPONSIVE' });
  await nextTurn();
  assert.deepEqual(events, []);
  assert.equal(queue.status().recoverable, true);
});

test('鼠标松开迟到结束后恢复，不补发第二次松开', async () => {
  const queue = new ActionQueue();
  const gate = deferred();
  const events = [];
  const page = pageFixture({ down: async () => events.push('down'), up: async () => { events.push('up'); await gate.promise; },
    move: async () => events.push('move') });
  const mouse = new MousePrimitive(25);
  await assert.rejects(queue.run(async () => { await mouse.down(page, 'left'); await mouse.up(page, 'left'); },
    context('element.click', 1000)), { step: 'mouse_up' });
  assert.equal(queue.status().recoverable, false);
  gate.resolve();
  await nextTurn();
  await queue.runAfterRecovery(() => mouse.move(page, 50, 50), context('browser.start', 1000));
  assert.deepEqual(events, ['down', 'up', 'move']);
});
