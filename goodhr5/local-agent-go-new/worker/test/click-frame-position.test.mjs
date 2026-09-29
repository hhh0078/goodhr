// 文件作用：验证内嵌弹框诊断不调用可能触发真实输入的 trial 点击，实际关闭只执行一次鼠标按下和松开。
import assert from 'node:assert/strict';
import test from 'node:test';
import { ClickAction } from '../dist/browser/actions/click.js';

test('内嵌弹框的诊断只读状态，关闭按钮只发送一次实际点击', async () => {
  const currentY = 20;
  let clickedY;
  let buttonDown = false;
  let clicks = 0;
  const page = { url: () => 'https://example.test/list', isClosed: () => false,
    context: () => ({ pages: () => [page] }), frames: () => [] };
  const box = () => ({ x: 20, y: currentY, width: 150, height: 45 });
  const resolved = { page, locator: { async click() { assert.fail('诊断不得发送 trial 点击'); }, isVisible: async () => true, isEnabled: async () => true },
    matched_selector: { type: 'css', value: '.close' }, attempts: [],
    view: { box: box(), viewport: { width: 1280, height: 900 }, visible: true, enabled: true, in_viewport: true, fully_in_viewport: false } };
  const action = new ClickAction(
    { one: async () => ({ resolved, result: { element_ref: 'close' } }) },
    { ensureVisible: async () => {} },
    { async toElement(found) { clickedY = found.view.box.y + 20; return { x: 80, y: clickedY, view: found.view }; } },
    { box: async () => box() },
    { async down() { buttonDown = true; clicks++; }, async up() { assert.ok(buttonDown); buttonDown = false; assert.ok(clickedY >= 20 && clickedY <= 65, '关闭按钮必须落在实际区域'); } },
    { info() {}, failure() {}, fromUnknown: value => ({ value }) },
  );
  await action.execute({ selector: { target: { selectors: [{ type: 'css', value: '.close' }] }, description: '详情关闭按钮' }, diagnostic: true },
    { action: 'element.click', trace_id: 'frame-position', request_id: 'close', started_at: Date.now() });
  assert.equal(clickedY, 40);
  assert.equal(clicks, 1);
});
