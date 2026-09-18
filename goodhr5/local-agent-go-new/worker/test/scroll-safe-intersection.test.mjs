// 文件作用：验证窗口边缘不足一像素的元素不会被当作可点击目标，并保留贴边抽屉的部分可见支持。
import test from 'node:test';
import assert from 'node:assert/strict';
import { visibleArea, viewAccepted, directedWheel, intersectsViewport } from '../dist/browser/actions/scroll-geometry.js';

/** makeView 构造实际 Boss 窗口尺寸下的元素位置。 */
function makeView(box) {
  return { visible: true, box, viewport: { width: 1265.6, height: 920.8 } };
}

test('窗口底部只露出不到一像素时继续向下滚动', () => {
  const view = makeView({ x: 318, y: 920, width: 248, height: 22 });
  const area = visibleArea(view, undefined, 0);
  assert.equal(viewAccepted(view, false, area), false);
  assert.equal(intersectsViewport(view), false);
  assert.deepEqual(directedWheel(view, 160, area, false), { x: 0, y: 9 });
  assert.equal(viewAccepted(makeView({ ...view.box, y: 911 }), false, area), true);
});

test('贴边抽屉部分可见时不要求整个元素进入窗口', () => {
  const view = makeView({ x: -100, y: 100, width: 180, height: 40 });
  const area = visibleArea(view, undefined, 0);
  assert.equal(viewAccepted(view, false, area), true);
  assert.equal(viewAccepted(view, true, area), false);
});
