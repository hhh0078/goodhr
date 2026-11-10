// 文件作用：验证长截图回到顶部并完整覆盖，保留重叠，在底部稳定后停止。
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { createHash } from 'node:crypto';
import { ScreenshotAction } from '../dist/browser/actions/screenshot.js';
import { actionContextStorage } from '../dist/logging/operation.js';

test('长截图从顶部完整覆盖，滚动不会越过相邻画面', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-long-shot-'));
  let position = 350;
  const positions = [];
  const wheelSteps = []; const buffers = new Map();
  const page = {};
  const locator = { async screenshot() { positions.push(position); const buffer = createHash('sha256').update(String(position)).digest(); buffers.set(buffer.toString('hex'), position); return buffer; } };
  const found = { resolved: { locator, page, view: { box: { x: 0, y: 50, width: 600, height: 500 }, viewport: { width: 1200, height: 800 } } } };
  const action = new ScreenshotAction(
    { async requirePage() { return page; } }, { async one() { return found; } },
    { async toElement() {} },
    { async wheel(_page, x, y) { assert.equal(x, 0); wheelSteps.push(y); position = Math.max(0, Math.min(700, position + y)); } },
    { info() {}, failure() {} },
  );
  try {
    const result = await action.long({ target: { description: '测试详情' }, directory, filename: 'detail.png', distance: 520, max_parts: 12, wait_ms: 50 }, { action: 'element.screenshot_long', trace_id: 'test' });
    assert.equal(result.complete, true);
    const captured = await Promise.all(result.parts.map(async part => buffers.get((await fs.readFile(part.path)).toString('hex'))));
    assert.equal(captured[0], 0);
    assert.equal(captured.at(-1), 700);
    for (let i = 1; i < captured.length; i++) assert.ok(captured[i] > captured[i-1] && captured[i] - captured[i-1] <= 325);
    assert.ok(wheelSteps.some(value => value < 0));
    assert.ok(new Set(wheelSteps.filter(value => value > 0)).size > 1);
    assert.ok(positions.filter(value => value === 700).length >= 3);
  } finally { await fs.rm(directory, { recursive: true, force: true }); }
});

test('仅截图调用自身的 TimeoutError 标为五秒，文件错误和页面等待不冒充截图超时', async t => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-shot-timeout-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  for (const stage of ['capture', 'page', 'file']) {
    const error = Object.assign(new Error('fixture'), { name: stage === 'file' ? 'Error' : 'TimeoutError' });
    const action = new ScreenshotAction({ async requirePage() {
      if (stage === 'page') throw error;
      return { async screenshot() { throw error; } };
    } }, {}, {}, {}, { info() {}, failure() {} });
    await assert.rejects(action.execute({ directory, filename: 'fixture.png' }, {action: 'page.screenshot', trace_id:'fixture'}), failure =>
      failure.code === 'SCREENSHOT_FAILED' && failure.details.capture_timeout_ms === (stage === 'capture' ? 5000 : undefined));
  }
});

test('队列截止后才返回的原生截图不重建已清目录或新增分段', async t => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-late-shot-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  let resolveCapture, enteredCapture;
  const entered = new Promise(resolve => { enteredCapture = resolve; });
  const late = new Promise(resolve => { resolveCapture = resolve; });
  let captures = 0, wheels = 0, saved = 0;
  const sameFrame = Buffer.from('synthetic-same-frame');
  const locator = { async screenshot() { captures++; if (captures === 4) { enteredCapture(); return late; } return sameFrame; } };
  const page = {}, found = { resolved: { locator, page, view: { box: { x: 0, y: 0, width: 400, height: 400 }, viewport: { width: 800, height: 600 } } } };
  const action = new ScreenshotAction({ async requirePage() { return page; } }, { async one() { return found; } }, { async toElement() {} },
    { async wheel() { wheels++; } }, { info(_context, step) { if (step === 'capture_part') saved++; }, failure() {} });
  const request = { action: 'element.screenshot_long', trace_id: 'late-native-shot', deadline_at: Date.now() + 60_000 };
  // 显式入口context仍有效，只有ALS队列执行截止，防止测试误用入口signal替代真实队列预算。
  const execution = { ...request };
  const pending = actionContextStorage.run(execution, () => action.long({ target: { description: '合成截图' }, directory, filename: 'profile.png', distance: 100, max_parts: 2, wait_ms: 50 }, request));
  await entered;
  const wheelsBeforeLateReturn = wheels;
  await fs.rm(directory, { recursive: true });
  execution.deadline_at = Date.now() - 1;
  resolveCapture(sameFrame);
  await assert.rejects(pending, { code: 'ACTION_CANCELLED' });
  assert.equal(saved, 0); assert.equal(wheels, wheelsBeforeLateReturn);
  await assert.rejects(fs.stat(directory), { code: 'ENOENT' });
});
