// 文件作用：验证浏览器协议不返回时 Worker 独立限时、队列隔离、迟到拒绝处理和补丁状态白名单。
import assert from 'node:assert/strict';
import test from 'node:test';
import { MousePrimitive } from '../dist/browser/primitives/mouse.js';
import { ActionQueue } from '../dist/browser/actions/action-queue.js';
import { actionContextStorage } from '../dist/logging/operation.js';
import { WorkerLogger } from '../dist/logging/logger.js';
import { nativeInputSafetyDiagnostic } from '../dist/browser/session/native-input-safety.js';

/** pageFixture 提供仅含标准页面状态的内存替身，不访问招聘账号或修改页面。 */
function pageFixture(wheel) {
  const page = { mouse: { wheel }, viewportSize: () => ({ width: 1280, height: 900 }), isClosed: () => false,
    url: () => 'https://example.test/list', frames: () => [], context: () => ({ pages: () => [page] }) };
  return page;
}

test('单次滚轮卡住时提前隔离队列，迟到的原生拒绝不会重放后续输入', async () => {
  let finish; let recoveries = 0; let subsequent = 0;
  const page = pageFixture(() => new Promise((_, reject) => { finish = reject; }));
  const queue = new ActionQueue(() => { recoveries++; });
  const context = { action: 'page.scroll', trace_id: 'input-deadline', request_id: 'wheel-hang', started_at: Date.now(), deadline_at: Date.now() + 2000 };
  const first = actionContextStorage.run(context, () => queue.run(() => new MousePrimitive(80).wheel(page, 0, 300), context));
  const second = queue.run(async () => { subsequent++; }, { ...context, request_id: 'next-input' });
  await Promise.all([assert.rejects(first, error => {
    assert.equal(error.code, 'WORKER_UNRESPONSIVE'); assert.equal(error.step, 'mouse_wheel');
    assert.equal(error.details.recovery_required, true);
    assert.equal(error.details.output.input_wait.origin, 'worker');
    assert.equal(error.details.output.input_wait.status, 'timed_out');
    assert.equal(error.details.output.input_wait.timeout_ms, 80);
    const lines = []; new WorkerLogger(line => lines.push(JSON.parse(line))).failure(context, error);
    assert.equal(lines[0].output.input_wait.stage, 'playwright_input');
    assert.ok(lines[0].input.native_input_patch);
    return true;
  }), assert.rejects(second, { code: 'WORKER_UNRESPONSIVE' })]);
  assert.equal(recoveries, 1); assert.equal(subsequent, 0);
  finish(new Error('底层调用迟到失败'));
  await new Promise(resolve => setTimeout(resolve, 10));
  await assert.rejects(queue.run(async () => { subsequent++; }, context));
  assert.equal(subsequent, 0);
});

test('输入限时不会超过剩余请求时间，正常和立即失败路径保持原结果', async () => {
  const context = { action: 'page.scroll', trace_id: 'remaining-budget', started_at: Date.now(), deadline_at: Date.now() + 100 };
  await assert.rejects(actionContextStorage.run(context, () => new MousePrimitive().wheel(pageFixture(() => new Promise(() => {})), 0, 100)), error => {
    assert.ok(error.details.output.input_wait.timeout_ms <= 100); return true;
  });
  let calls = 0;
  await new MousePrimitive(500).wheel(pageFixture(async () => { calls++; }), 0, 100);
  assert.equal(calls, 1);
  const failure = new Error('真实协议失败');
  await assert.rejects(new MousePrimitive(500).wheel(pageFixture(async () => { throw failure; }), 0, 100), error => error === failure);
});

test('启动前补丁证据只保留白名单，损坏结果也不能阻断任务日志', () => {
  const saved = process.env.GOODHR_NATIVE_INPUT_SAFETY;
  try {
    process.env.GOODHR_NATIVE_INPUT_SAFETY = JSON.stringify({ ready: true, installed_version: 'v7', expected_version: 'v7', resource_sha256: 'a'.repeat(64), cookie: 'private-cookie' });
    const value = nativeInputSafetyDiagnostic();
    assert.equal(value.ready, true); assert.equal(value.installed_version, 'v7');
    assert.equal(value.runtime_loaded_version, 'unconfirmed'); assert.doesNotMatch(JSON.stringify(value), /private-cookie/);
    process.env.GOODHR_NATIVE_INPUT_SAFETY = '{broken';
    assert.equal(nativeInputSafetyDiagnostic().ready, false);
  } finally {
    if (saved === undefined) delete process.env.GOODHR_NATIVE_INPUT_SAFETY; else process.env.GOODHR_NATIVE_INPUT_SAFETY = saved;
  }
});
