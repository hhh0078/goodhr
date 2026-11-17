// 文件作用说明：验证长名单起点查询只测量一次视口、保持真实序号，并在名单或位置变化时拒绝猜测。
import test from 'node:test';
import assert from 'node:assert/strict';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { actionContextStorage } from '../dist/logging/operation.js';
import { parseElementFindAllRequest } from '../dist/validation/action-requests.js';

const spec = { target: { selectors: [{ type: 'css', value: '.row' }] }, timeout_ms: 0, description: '合成纵向名单' };
const insets = { top_inset: 100, bottom_inset: 40 };

/** fixture 只提供标准Locator和截图接口，记录每次测量；不提供任何脚本或滚动能力。 */
function fixture(boxes) {
 const state = { captures: 0, reads: [], counts: 0, move: false, changed: false };
 const png = Buffer.alloc(24); png.write('PNG', 1); png.writeUInt32BE(1200, 16); png.writeUInt32BE(900, 20);
 const list = { async count() { state.counts++; return boxes.length + (state.changed && state.counts > 1 ? 1 : 0); }, nth(index) {
  return { isVisible: async () => true, isEnabled: async () => true, async boundingBox() { state.reads.push(index); const box = boxes[index]; return box && { ...box, y: box.y + (state.move ? state.reads.filter(n => n === index).length : 0) }; } };
 } };
 const page = { viewportSize: () => null, async screenshot() { state.captures++; return png; }, url: () => 'https://example.test/list', locator(css) {
  if (css === '.row') return list;
  assert.equal(css, 'html'); return { boundingBox: async () => ({ x: 0, y: 0, width: 1200, height: 200000 }) };
 } };
 return { state, page };
}

/** run 在真实请求生命周期内调用现有列表解析，不替换被测定位方法。 */
function run(f) {
 const context = { action: 'element.find_all', step: 'find_all', trace_id: 'viewport-regression', started_at: Date.now() };
 return actionContextStorage.run(context, () => new LocatorPrimitive().resolveAll(f.page, spec, 1, context, 0, false, false, insets));
}

test('869张名单只读二分定位当前首张完整卡片，单请求只测量一次窗口', async () => {
 const f = fixture(Array.from({ length: 869 }, (_, i) => ({ x: 10, y: (i - 700) * 200 + 20, width: 500, height: 180 })));
 const result = await run(f);
 assert.equal(result[0].list_index, 701); assert.equal(result[0].list_total, 869);
 assert.equal(result[0].view.fully_in_viewport, true); assert.equal(f.state.captures, 1);
 assert.ok(f.state.reads.length <= 13, '禁止线性扫描前方700人');
});

test('同一行超高卡片不阻止后面的完整卡片，横向越界不能作为起点', async () => {
 const f = fixture([{ x: 10, y: 60, width: 500, height: 180 }, { x: 10, y: 260, width: 500, height: 700 }, { x: 1100, y: 260, width: 500, height: 180 }, { x: 550, y: 260, width: 500, height: 180 }, { x: 10, y: 960, width: 500, height: 180 }]);
 assert.equal((await run(f))[0].list_index, 3);
});

for (const change of ['move', 'changed']) test(`读取期间${change}变化必须停止`, async () => {
 const f = fixture([{ x: 10, y: 200, width: 500, height: 180 }]); f.state[change] = true;
 await assert.rejects(run(f), { code: 'ACTION_TIMEOUT' });
});

test('屏幕没有完整卡片返回空，不向屏幕外查找', async () => {
 const f = fixture([{ x: 0, y: 20, width: 500, height: 180 }, { x: 0, y: 900, width: 500, height: 180 }]);
 assert.deepEqual(await run(f), []);
});

test('视口探测协议仅接收单项无字段只读请求', () => {
 const input = { selector: spec, probe_only: true, max_items: 1, viewport_start: insets };
 assert.deepEqual(parseElementFindAllRequest(input, 'test', 'element.find_all').viewport_start, insets);
 for (const extra of [{ probe_only: false }, { max_items: 20 }, { from_end: true }, { start_index: 1 }, { fields: { name: spec } }, { viewport_start: { top_inset: -1 } }]) {
  assert.throws(() => parseElementFindAllRequest({ ...input, ...extra }, 'test', 'element.find_all'));
 }
});
