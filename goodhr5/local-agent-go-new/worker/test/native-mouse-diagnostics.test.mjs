// 文件作用说明：验证原生刷新证据经过 Worker 错误、日志及超时队列后仍可上传，且不会携带敏感字段。

import assert from 'node:assert/strict';
import test from 'node:test';
import { nativeMouseDiagnostics } from '../dist/errors/native-mouse-diagnostics.js';
import { normalizeWorkerError } from '../dist/errors/worker-error.js';
import { WorkerLogger } from '../dist/logging/logger.js';
import { ActionQueue } from '../dist/browser/actions/action-queue.js';
import { actionContextStorage, traceOperation } from '../dist/logging/operation.js';

const nativeEvidence = { schema_version: 1, mouse_safety_version: 'v6', stage: 'apz_repaint',
  apz_flush_requested: true, apz_flush_result: true, apz_repaint_received: false,
  events_sent: 0, events_acknowledged: 0, apz_wait_ms: 5001,
  window_at_failure: { window_minimized: true, chrome_has_focus: false, browser_rect: { x: 0, y: 30, width: 1200, height: 800 } } };

/** failure 创建与真实原生协议相同格式的刷新超时，附加敏感字段检验白名单。 */
function failure() {
  return new Error('mouse.move: Protocol error (Page.dispatchMouseEvent): GOODHR_MOUSE_APZ_TIMEOUT:mousemove GOODHR_MOUSE_DIAGNOSTICS=' +
    JSON.stringify({ ...nativeEvidence, cookie: 'private-cookie', page_text: 'private-resume' }));
}

test('刷新证据经过错误规范化和 JSON 日志后保留在出参', () => {
  const lines = [];
  const context = { action: 'element.scroll', trace_id: 'native-upload', request_id: 'native-request', started_at: Date.now() };
  const normalized = normalizeWorkerError(failure(), { ...context, step: 'move', code: 'MOVE_FAILED', message: '鼠标移动失败' });
  new WorkerLogger(line => lines.push(JSON.parse(line))).failure(context, normalized);
  assert.equal(lines[0].output.native_mouse.stage, 'apz_repaint');
  assert.equal(lines[0].output.native_mouse.window_at_failure.window_minimized, true);
  assert.equal(lines[0].output.native_mouse.events_sent, 0);
  assert.doesNotMatch(JSON.stringify(lines), /private-cookie|private-resume|GOODHR_MOUSE_DIAGNOSTICS=/);
});

test('历史、损坏和超长的原生证据保留错误类型且不会阻断处理', () => {
  for (const suffix of ['', ' GOODHR_MOUSE_DIAGNOSTICS={broken', ' GOODHR_MOUSE_DIAGNOSTICS=' + 'x'.repeat(9000)]) {
    const value = nativeMouseDiagnostics(new Error('GOODHR_MOUSE_APZ_TIMEOUT:mousemove' + suffix));
    assert.equal(value.stage, 'apz_repaint');
    assert.equal(value.diagnostic_available, false);
  }
  assert.equal(nativeMouseDiagnostics(new Error('ordinary failure')), undefined);
});

test('浏览器关闭附带的历史鼠标报错不能当成本次滚轮根因', () => {
  const closed = new Error('mouse.wheel: Target page, context or browser has been closed\nBrowser logs:\n' + failure().message);
  assert.equal(nativeMouseDiagnostics(closed), undefined);
});

test('激活超时要求回收浏览器，队列立即隔离且不执行后续动作', async () => {
  let recovered = 0;
  let secondRan = false;
  const context = { action: 'page.scroll', trace_id: 'activation-timeout', started_at: Date.now() };
  const error = normalizeWorkerError(new Error('GOODHR_MOUSE_INPUT_TIMEOUT:wheel GOODHR_MOUSE_DIAGNOSTICS=' +
    JSON.stringify({ schema_version: 1, mouse_safety_version: 'v7', stage: 'activation', recovery_required: true,
      activation_wait_ms: 15000, window_before: { tab_selected: false } })), { ...context, step: 'scroll' });
  const queue = new ActionQueue(() => { recovered++; });
  await assert.rejects(queue.run(async () => { throw error; }, context), value => value.code === 'WORKER_UNRESPONSIVE');
  await assert.rejects(queue.run(async () => { secondRan = true; }, context));
  assert.equal(recovered, 1);
  assert.equal(secondRan, false);
});

test('操作卡住时队列错误包含同任务最后一个鼠标开始记录，过滤其他任务', async () => {
  const context = { action: 'element.scroll', trace_id: 'queue-native', request_id: 'queue-request', started_at: Date.now(), deadline_at: Date.now() + 40 };
  new WorkerLogger(() => {}).info({ ...context, trace_id: 'other-task' }, 'mouse_move', 'start', { diagnostic: true, input: { x: 999 } });
  const queue = new ActionQueue();
  await assert.rejects(actionContextStorage.run(context, () => queue.run(() =>
    traceOperation('mouse_wheel', () => new Promise(() => {}), { diagnostic: true, input: { delta_y: 500 } }), context)), error => {
      assert.equal(error.code, 'WORKER_UNRESPONSIVE');
      const recent = error.details.output.diagnostics.recent_operations;
      assert.ok(recent.some(item => item.step === 'mouse_wheel' && item.status === 'start' && item.input.delta_y === 500));
      assert.ok(recent.every(item => item.trace_id === context.trace_id));
      assert.equal(error.details.output.queue.active_request_id, context.request_id);
      return true;
    });
});
