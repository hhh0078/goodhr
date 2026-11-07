// 文件作用：验证内嵌弹框诊断不调用可能触发真实输入的 trial 点击，实际关闭只执行一次鼠标按下和松开。
import assert from 'node:assert/strict';
import test from 'node:test';
import { ClickAction } from '../dist/browser/actions/click.js';
import { parseElementClickRequest } from '../dist/validation/action-requests.js';

test('内嵌弹框的诊断只读状态，关闭按钮只发送一次实际点击', async () => {
  const currentY = 20;
  let clickedY;
  let buttonDown = false;
  let clicks = 0;
  const page = { url: () => 'https://example.test/list', isClosed: () => false,
    context: () => ({ pages: () => [page] }), frames: () => [] };
  const box = () => ({ x: 20, y: currentY, width: 150, height: 45 });
  const resolved = { page, locator: { locator: () => ({ count: async () => 1 }), async click() { assert.fail('诊断不得发送 trial 点击'); }, isVisible: async () => true, isEnabled: async () => true },
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

/** closeFixture 构造关闭动作与移动、验证顺序的协议替身，不连接招聘页面。 */
function closeFixture(moveError) {
  const events = [];
  const view = { box: { x: 1100, y: 20, width: 40, height: 40 }, viewport: { width: 1280, height: 900 }, visible: true, enabled: true };
  const page = { isClosed: () => false };
  const resolved = { page, view, locator: { locator: () => ({ count: async () => 1 }) } };
  const action = new ClickAction(
    { one: async () => ({ resolved, result: { element_ref: 'close' } }), all: async () => { events.push('verify'); return []; } },
    { ensureVisible: async () => {} },
    { toElement: async () => ({ x: 1120, y: 40, view }), async toViewportCenter(actualPage, width, height) {
      assert.equal(actualPage, page); assert.equal(width, 1280); assert.equal(height, 900);
      events.push('center'); if (moveError) throw moveError;
    } },
    { box: async () => view.box },
    { down: async () => { events.push('down'); }, up: async () => { events.push('up'); } },
    { info() {}, failure() {} },
  );
  const request = { selector: { target: { selectors: [{ type: 'css', value: '.close' }] }, description: '关闭按钮' },
    verify: { target_hidden: { target: { selectors: [{ type: 'css', value: '.dialog' }] }, description: '弹框' } } };
  return { events, run: enabled => action.execute({ ...request, move_to_center_after_click: enabled },
    { action: 'element.click', trace_id: 'close-center', started_at: Date.now() }) };
}

test('启用后松开关闭按钮立即移到中央，然后验证；默认点击不移动', async () => {
  const enabled = closeFixture(); await enabled.run(true);
  assert.deepEqual(enabled.events, ['down', 'up', 'center', 'verify']);
  const disabled = closeFixture(); await disabled.run(undefined);
  assert.deepEqual(disabled.events, ['down', 'up', 'verify']);
});

test('关闭后移动失败保留已点击标记，停止验证且不重复点击', async () => {
  const fixture = closeFixture(new Error('移动未完成'));
  await assert.rejects(fixture.run(true), error => error.details.click_started === true && error.details.post_click_move_failed === true);
  assert.deepEqual(fixture.events, ['down', 'up', 'center']);
});

test('点击后移到中央参数通过请求校验，旧请求保持默认关闭', () => {
  const selector = { target: { selectors: [{ type: 'css', value: '.close' }] }, description: '关闭按钮' };
  assert.equal(parseElementClickRequest({ selector, move_to_center_after_click: true }, 'trace', 'element.click').move_to_center_after_click, true);
  assert.equal(parseElementClickRequest({ selector }, 'trace', 'element.click').move_to_center_after_click, undefined);
  assert.equal(parseElementClickRequest({ selector, move_to_center_after_click: 'true' }, 'trace', 'element.click').move_to_center_after_click, undefined);
});
