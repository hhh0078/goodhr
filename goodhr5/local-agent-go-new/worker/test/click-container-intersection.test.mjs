// 文件作用说明：用标准几何替身复现容器边缘目标，验证点击和输入只在最新可见交集内落点。
import assert from 'node:assert/strict';
import test from 'node:test';
import { ClickAction } from '../dist/browser/actions/click.js';
import { InputAction } from '../dist/browser/actions/input.js';
import { ScrollAction } from '../dist/browser/actions/scroll.js';
import { MoveAction } from '../dist/browser/actions/move.js';

/** fixture 连接生产 Scroll、Move、Click，所有页面和鼠标均为本地替身。 */
function fixture({ target = { x: 100, y: 306.1167, width: 200, height: 16.8 },
  container = { x: 80, y: 315, width: 500, height: 286 }, onBox, onMove } = {}) {
  const state = { target, container, visible: true, boxReads: 0, containerReads: 0, down: 0, up: 0, wheels: 0, points: [], events: [] };
  const viewport = { width: 1280, height: 900 };
  const page = { isClosed: () => false };
  const logger = { info(_ctx, step, status, data) { state.events.push({ step, status, ...data }); }, warn() {}, error() {}, failure() {} };
  /** view 生成当前的真实几何快照，避免初次定位被测试中的后续移动同步修改。 */
  const view = (box, visible = true) => ({ box: { ...box }, viewport, visible, enabled: true, in_viewport: true, fully_in_viewport: false });
  /** box 模拟稳定检查，允许目标和容器在等待期间改变布局。 */
  const box = async () => { state.boxReads++; onBox?.(state); return { ...state.target }; };
  const targetLocator = { boundingBox: box, locator: () => ({ count: async () => 1 }) };
  const anchorLocator = {};
  const targetFound = { result: { element_ref: 'target' }, resolved: { page, locator: targetLocator, view: view(target), matched_selector: { type: 'css', value: '.target' }, attempts: [] } };
  const anchor = { result: { element_ref: 'anchor' }, resolved: { page, locator: anchorLocator, view: view(container), matched_selector: { type: 'css', value: '.container' }, attempts: [] } };
  const find = { one: async selector => selector.description === '容器' ? anchor : targetFound };
  const locator = { box, async view(_page, element) {
    if (element === anchorLocator) { state.containerReads++; return view(state.container, state.visible); }
    return view(state.target);
  } };
  const mouse = { async move(_page, x, y) { state.points.push({ x, y }); await onMove?.(state); },
    async down() { state.down++; }, async up() { state.up++; }, async wheel() { state.wheels++; assert.fail('当前目标已有可见交集，不应额外滚动'); } };
  const move = new MoveAction(mouse, logger, locator);
  const scroll = new ScrollAction({}, find, move, locator, mouse, logger);
  const click = new ClickAction(find, scroll, move, locator, mouse, logger);
  const selector = { description: '目标', target: { selectors: [{ type: 'css', value: '.target' }] } };
  const wheel_anchor = { description: '容器', target: { selectors: [{ type: 'css', value: '.container' }] } };
  const context = { action: 'element.click', trace_id: 'container-intersection', started_at: Date.now() };
  return { state, move, scroll, click, find, mouse, logger, selector, wheel_anchor, context, targetFound };
}

/** assertInside 验证每次实际鼠标移动都严格落在目标、容器与窗口的交集内部。 */
function assertInside(point, target, container, viewport = { width: 1280, height: 900 }) {
  assert.ok(point.x > Math.max(0, target.x, container.x) && point.x < Math.min(viewport.width, target.x + target.width, container.x + container.width), `x=${point.x}`);
  assert.ok(point.y > Math.max(0, target.y, container.y) && point.y < Math.min(viewport.height, target.y + target.height, container.y + container.height), `y=${point.y}`);
}

test('实测部分露出的16.8px行只点击315以下的可见部分', async () => {
  const f = fixture();
  await f.click.execute({ selector: f.selector, wheel_anchor: f.wheel_anchor }, f.context);
  assert.equal(f.state.down, 1);
  assert.equal(f.state.wheels, 0);
  assert.equal(f.state.containerReads, 1);
  assertInside(f.state.points[0], f.state.target, f.state.container);
  assert.ok(f.state.events.some(event => event.phase === 'before_move'));
});

for (const target of [{ x: -100, y: -20, width: 180, height: 60 }, { x: -100, y: -300, width: 1000, height: 1500 }]) {
  test(`负坐标及超大目标保留安全部分可见支持：${target.height}`, async () => {
    const f = fixture({ target, container: { x: 0, y: 10, width: 50, height: 100 } });
    await f.click.execute({ selector: f.selector, wheel_anchor: f.wheel_anchor }, f.context);
    assertInside(f.state.points[0], target, f.state.container);
    assert.equal(f.state.down, 1);
  });
}

test('坐标稳定期间容器位置改变，最终落点使用刷新后的交集', async () => {
  const f = fixture({ onBox(state) { state.target.y = 320; state.container.y = 325; } });
  await f.click.execute({ selector: f.selector, wheel_anchor: f.wheel_anchor }, f.context);
  assert.ok(f.state.boxReads >= 4);
  assertInside(f.state.points[0], f.state.target, f.state.container);
  assert.ok(f.state.points[0].y > 325);
});

for (const hidden of [false, true]) {
  test(`稳定后交集为空或容器隐藏，不移动和按下鼠标：${hidden}`, async () => {
    const f = fixture({ onBox(state) { if (hidden) state.visible = false; else state.container.y = 500; } });
    await assert.rejects(f.click.execute({ selector: f.selector, wheel_anchor: f.wheel_anchor }, f.context), error => error.details.click_started === false);
    assert.equal(f.state.points.length, 0);
    assert.equal(f.state.down, 0);
    assert.equal(f.state.up, 0);
  });
}

test('原生移动重试会重新读取容器，而不是复用首次裁剪范围', async () => {
  const f = fixture({ onMove(state) {
    if (state.points.length === 1) { state.target.y = 420; state.container.y = 425; throw new Error('GOODHR_MOUSE_ACK_TIMEOUT:mousemove'); }
  } });
  await f.click.execute({ selector: f.selector, wheel_anchor: f.wheel_anchor }, f.context);
  assert.equal(f.state.points.length, 2);
  assert.equal(f.state.containerReads, 2);
  assertInside(f.state.points[1], f.state.target, f.state.container);
  assert.equal(f.state.down, 1);
});

test('输入聚焦复用动态视口范围，现有协议不增加容器选择器', async () => {
  const f = fixture({ target: { x: -100, y: -300, width: 2000, height: 1500 } });
  const input = new InputAction(f.find, f.scroll, f.move, f.mouse, {}, f.logger);
  await input.execute({ selector: f.selector, text: '', clear: false, verify: false }, { ...f.context, action: 'element.input' });
  assertInside(f.state.points[0], f.state.target, { x: 0, y: 0, width: 1280, height: 900 });
  assert.ok(f.state.events.some(event => event.phase === 'before_move' && event.anchor_box === null));
  assert.equal(f.state.containerReads, 0);
  assert.equal(f.state.down, 1);
});
