// 文件作用：验证原生移动回执超时只重试一次移动，并重新读取目标，不重放点击。
import test from 'node:test';
import assert from 'node:assert/strict';
import { MoveAction } from '../dist/browser/actions/move.js';

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
