// 文件作用说明：复现20段截不完的长详情，验证40段完整覆盖、只截可见交集和分享日志保留结束原因。
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { ScreenshotAction } from '../dist/browser/actions/screenshot.js';
import { screenshotClip } from '../dist/browser/actions/long-screenshot-viewport.js';
import { parseLongScreenshotRequest } from '../dist/validation/action-requests.js';

/** fixture 合成可滚动的6500像素详情，页面截图无副作用，元素截图一旦被调用立即失败。 */
function fixture() {
  let position = 0; const logs = [];
  const box = { x: 0, y: 50, width: 600, height: 500 };
  const locator = { boundingBox: async () => ({ ...box }), screenshot: async () => { throw Error('禁止元素截图隐式滚动'); } };
  const page = { async screenshot(options) {
    assert.deepEqual(options.clip, box); assert.equal(options.fullPage, false);
    return createHash('sha256').update(String(position)).digest();
  } };
  const found = { resolved: { locator, page, view: { box, viewport: { width: 1200, height: 900 } } } };
  const action = new ScreenshotAction({ requirePage: async () => page }, { one: async () => found }, { toElement: async () => {} },
    { async wheel(_page, x, y) { assert.equal(x, 0); position = Math.max(0, Math.min(6500, position + y)); } },
    { info(_context, step, status, fields) { if (fields?.diagnostic) logs.push({ step, status, ...fields }); }, failure() {} });
  return { action, logs };
}
test('同一长详情20段失败、40段能完整结束，保留分段和结束诊断', async t => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-40-parts-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const request = { target: { description: '合成长详情' }, viewport_only: true, directory, filename: 'long.png', distance: 520, wait_ms: 50 };
  const context = { action: 'element.screenshot_long', trace_id: 'long-fixture' };
  const old = fixture();
  await assert.rejects(old.action.long({ ...request, max_parts: 20 }, context), error => error.code === 'SCREENSHOT_FAILED' && error.details.captured_parts === 20 && error.details.completion_reason === 'part_limit');
  const fixed = fixture();
  const result = await fixed.action.long({ ...request, max_parts: 40 }, context);
  assert.equal(result.complete, true); assert.ok(result.count > 20 && result.count <= 40);
  const summary = fixed.logs.find(log => log.input.stage === 'complete');
  assert.equal(summary.output.complete, true); assert.equal(summary.output.completion_reason, 'bottom_confirmed');
  assert.equal(summary.output.max_parts, 40); assert.equal(summary.output.capture_mode, 'viewport_clip');
  assert.ok(fixed.logs.some(log => log.input.stage === 'capture' && log.output.captured_parts > 20 && log.output.image_bytes > 0));
  assert.equal(old.logs.at(-1).output.completion_reason, 'part_limit');
});
test('截图只保留目标与嵌套iframe的交集，未知区域拒绝截图', () => {
  assert.deepEqual(screenshotClip([{ x: -20, y: -800, width: 1300, height: 3000 }, { x: 120, y: 100, width: 800, height: 600 }, { x: 150, y: 120, width: 700, height: 500 }], { width: 1200, height: 900 }), { x: 150, y: 120, width: 700, height: 500 });
  assert.throws(() => screenshotClip([null], { width: 1200, height: 900 }));
  assert.throws(() => screenshotClip([{ x: 0, y: 890, width: 600, height: 200 }], { width: 1200, height: 900 }));
});
test('长截图协议保留仅视口参数，拒绝非法布尔值', () => {
  const data = { target: { target: { selectors: [{ type: 'css', value: '.detail' }] }, description: '合成详情' }, directory: '/tmp/fixture', filename: 'fixture.png', viewport_only: true, max_parts: 40 };
  assert.equal(parseLongScreenshotRequest(data, 'fixture', 'element.screenshot_long').viewport_only, true);
  assert.throws(() => parseLongScreenshotRequest({ ...data, viewport_only: 'true' }, 'fixture', 'element.screenshot_long'));
});

test('滚轮卡在中途时不把重复画面误认为已经到底', async t => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-blocked-scroll-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  let position = 0;
  const bounds = () => ({ x: 0, y: 50 - position, width: 600, height: 3000 });
  const page = { screenshot: async () => createHash('sha256').update(String(position)).digest() };
  const found = { resolved: { page, locator: { boundingBox: async () => bounds() }, view: { box: bounds(), viewport: { width: 1200, height: 900 } } } };
  const events = [];
  const action = new ScreenshotAction({ requirePage: async () => page }, { one: async () => found }, { toElement: async () => {} },
    { async wheel(_page, _x, y) { position = Math.max(0, Math.min(100, position + y)); } },
    { info(_context, _step, _status, fields) { if (fields?.diagnostic) events.push(fields); }, failure() {} });
  await assert.rejects(action.long({ target: { description: '合成滚轮受阻详情' }, viewport_only: true, directory, filename: 'blocked.png', max_parts: 40, wait_ms: 50 }, { action: 'element.screenshot_long', trace_id: 'blocked-fixture' }),
    error => error.code === 'SCREENSHOT_FAILED' && error.details.completion_reason === 'scroll_stalled_before_bottom');
  assert.equal(events.at(-1).output.bottom_edge_visible, false);
  assert.equal(events.at(-1).output.complete, false);
});
