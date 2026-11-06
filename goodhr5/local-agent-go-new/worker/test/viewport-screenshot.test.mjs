// 文件作用：验证可见区域截图不会通过元素截图暗中滚动，并拒绝被聊天容器裁剪或截图期间移动的头像。
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { ScreenshotAction } from '../dist/browser/actions/screenshot.js';
import { parseScreenshotRequest } from '../dist/validation/action-requests.js';

const target = { description: '头像', target: { selectors: [{ type: 'css', value: '.avatar' }] } };
const container = { ...target, description: '聊天容器' };

/** fixture 仅模拟标准查找和截图API，元素截图故意报错以证明不会触发隐式滚动。 */
async function fixture(t, options = {}) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-visible-shot-'));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const box = { x: 30, y: 130, width: 36, height: 36, ...options.box };
  const view = { box, visible: true, fully_in_viewport: true, viewport: { width: 800, height: 600 }, ...options.view };
  const boundary = { box: { x: 20, y: 100, width: 500, height: 400 }, visible: true, ...options.container };
  const captures = [];
  const page = { async screenshot(request) { captures.push(request); await fs.writeFile(request.path, 'synthetic-png'); } };
  const locator = { async screenshot() { throw new Error('元素截图会自动滚动，禁止调用'); }, async boundingBox() { return options.after ?? box; } };
  const find = { async one(spec) { return { resolved: { locator, view: spec.description === container.description ? boundary : view } }; } };
  const action = new ScreenshotAction({ async requirePage() { return page; } }, find, {}, {}, { info() {}, failure() {} });
  return { captures, run: () => action.execute({ target, clip_container: container, viewport_only: true, directory, filename: 'avatar.png' }, { action: 'screenshot', trace_id: 'fixture' }) };
}

test('完整位于聊天容器内的头像仅裁剪页面视口，不调用元素截图', async t => {
  const f = await fixture(t); await f.run();
  assert.equal(f.captures.length, 1);
  assert.deepEqual(f.captures[0].clip, { x: 30, y: 130, width: 36, height: 36 });
  assert.equal(f.captures[0].fullPage, false);
});

for (const [name, options] of [
  ['历史顶部离屏', { view: { fully_in_viewport: false }, box: { y: -900 } }],
  ['大视口内但被聊天页头裁剪', { box: { y: 80 } }],
  ['头像部分超出聊天底部', { box: { y: 480 } }],
  ['聊天容器隐藏', { container: { visible: false } }],
]) test(`${name}时拒绝截图，不滚动页面`, async t => {
  const f = await fixture(t, options);
  await assert.rejects(f.run(), error => error.code === 'ELEMENT_NOT_VISIBLE');
  assert.equal(f.captures.length, 0);
});

test('截图期间头像移动时丢弃图片', async t => {
  const f = await fixture(t, { after: { x: 30, y: 90, width: 36, height: 36 } });
  await assert.rejects(f.run(), /目标位置发生变化/);
});

test('HTTP截图契约保留可见模式和容器选择器', () => {
  const request = parseScreenshotRequest({ target, clip_container: container, viewport_only: true, directory: '/tmp', filename: 'avatar.png' }, 'fixture', 'screenshot');
  assert.equal(request.viewport_only, true);
  assert.equal(request.clip_container.description, '聊天容器');
});
