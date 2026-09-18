// 文件作用：验证长截图回到顶部并完整覆盖，保留重叠，在底部稳定后停止。
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { createHash } from 'node:crypto';
import { ScreenshotAction } from '../dist/browser/actions/screenshot.js';

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
