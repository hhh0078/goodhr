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

test('候选人卡片只露出半张时不能通过完整可见检查', () => {
  const view = makeView({ x: 100, y: 800, width: 700, height: 240 });
  const area = visibleArea(view, undefined, 48);
  assert.equal(viewAccepted(view, true, area), false);
  assert.equal(directedWheel(view, 260, area, true).y, 168);
  assert.equal(viewAccepted(makeView({ ...view.box, y: 632 }), true, area), true);
});

test('卡片与列表等宽时保留真实容器宽度，不重复扣除窗口边距', () => {
  const view = makeView({ x: 206, y: 106, width: 1004.8, height: 172 });
  const container = makeView({ ...view.box, height: 2718 });
  const area = visibleArea(view, container, 48);
  assert.equal(viewAccepted(view, true, area), true);
});

test('宽卡片只缺横向留白时允许完整显示，真正超出窗口仍不放行', () => {
  const view = makeView({ x: 172, y: 472, width: 1064, height: 156 });
  const container = makeView({ x: 172, y: 408, width: 1064, height: 2500 });
  const area = visibleArea(view, container, 48);
  assert.equal(viewAccepted(view, true, area), true);
  assert.equal(viewAccepted(makeView({ ...view.box, x: 220 }), true, area), false);
});

test('边缘不足一像素的坐标误差不再触发无效滚轮', () => {
  const area = { left: 48, top: 48, right: 1218, bottom: 873, width: 1170, height: 825 };
  const subpixelView = makeView({ x: 47.6, y: 48, width: 1000, height: 156 });
  assert.equal(viewAccepted(subpixelView, true, area), true);
  assert.deepEqual(directedWheel(subpixelView, 260, area, true), { x: 0, y: 0 });

  const outsideTolerance = makeView({ x: 46.7, y: 48, width: 1000, height: 156 });
  assert.equal(viewAccepted(outsideTolerance, true, area), false);
  assert.deepEqual(directedWheel(outsideTolerance, 260, area, true), { x: -2, y: 0 });
});
