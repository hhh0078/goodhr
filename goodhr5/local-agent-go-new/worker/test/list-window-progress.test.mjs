// 文件作用说明：验证列表读取窗口、原始序号和真实滚轮位置证据，防止跳过候选人后误判列表结束。
import test from 'node:test';
import assert from 'node:assert/strict';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { FindAction } from '../dist/browser/actions/find.js';
import { ScrollAction } from '../dist/browser/actions/scroll.js';
import { MoveAction } from '../dist/browser/actions/move.js';
import { parseElementFindAllRequest, parseScrollRequest } from '../dist/validation/action-requests.js';

const context = { trace_id: 'list-window-progress', action: 'element.scroll', started_at: Date.now() };
const selector = { target: { selectors: [{ type: 'css', value: '.row' }] }, state: 'visible', timeout_ms: 0, description: '列表项' };

/** listHarness 用标准 Locator 替身构造超过单批上限的列表，并保留真实 DOM 序号。 */
function listHarness(count, hidden = -1) {
  const root = { async count() { return count; }, nth(index) { return {
    index, async isVisible() { return index !== hidden; }, async getAttribute() { return null; },
    async innerText() { return `记录${index}`; },
  }; } };
  const page = { locator() { return root; }, url() { return 'https://example.com/list'; } };
  const primitive = new LocatorPrimitive();
  const action = new FindAction(
    { async requirePage() { return page; }, elements: { remember(_page, locator) { return `ref-${locator.index}`; } } },
    primitive, { info() {}, warn() {}, failure() {} },
  );
  return { primitive, page, action };
}

test('超过一百项时按指定窗口读取，序号继续从一百开始', async () => {
  const { action } = listHarness(250);
  const rows = await action.all(selector, 100, {}, context, false, 10, 100);
  assert.equal(rows.length, 100);
  assert.equal(rows[0].index, 100);
  assert.equal(rows.at(-1).index, 199);
  assert.equal(rows[0].text, '记录100');
});

test('过滤隐藏项后仍保留原始列表序号，末尾定位不会偏移', async () => {
  const { action } = listHarness(25, 21);
  const rows = await action.all(selector, 10, {}, context, false, 10, 20);
  assert.deepEqual(rows.map(row => row.index), [20, 22, 23, 24]);
});

test('下一批尚未加载时返回空窗口，保留加载等待而不误报元素不存在', async () => {
  const { action } = listHarness(100);
  assert.deepEqual(await action.all(selector, 100, {}, context, false, 10, 100), []);
});

test('读取起点必须是整数，滚动定位与进度观察不能混用', () => {
  assert.equal(parseElementFindAllRequest({ selector, start_index: 100 }, 'window', 'element.find_all').start_index, 100);
  assert.throws(() => parseElementFindAllRequest({ selector, start_index: 1.5 }, 'window', 'element.find_all'));
  assert.throws(() => parseScrollRequest({ target: selector, progress_target: selector, distance: 620 }, 'window', 'element.scroll'));
});

/** progressHarness 模拟动画截图和候选人位置，确认底部判断使用元素位移而不是截图变化。 */
function progressHarness(y, movement, containerHeight = 6000) {
  let position = y;
  let screenshots = 0;
  const viewport = { width: 1200, height: 800 };
  const page = { async screenshot() { return Buffer.from(`动画${screenshots++}`); }, viewportSize() { return viewport; } };
  const progressView = () => ({ visible: true, box: { x: 100, y: position, width: 800, height: 160 }, viewport });
  const anchorView = { visible: true, box: { x: 100, y: 0, width: 800, height: containerHeight }, viewport };
  const progress = { locator: { kind: 'progress' }, view: progressView(), page, matched_selector: { type: 'css', value: '.row' } };
  const anchor = { locator: { kind: 'anchor' }, view: anchorView, page, matched_selector: { type: 'css', value: '.list' } };
  const moves = [];
  const logger = { info() {}, failure() {} };
  const mouse = {
    async move(_page, x, y) { moves.push({ x, y }); },
    async wheel(_page, _x, deltaY) { position -= deltaY * movement; },
  };
  const action = new ScrollAction(
    { async requirePage() { return page; } },
    { async one(spec) { return { resolved: spec.description === '列表项' ? progress : anchor }; } },
    new MoveAction(mouse, logger),
    { async view(_page, locator) { return locator.kind === 'progress' ? progressView() : anchorView; } },
    mouse,
    logger,
  );
  action.moves = moves;
  return action;
}

test('末尾还在窗口外时继续滚动，记录真实列表移动', async () => {
  const result = await progressHarness(4000, 1).execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, max_attempts: 1, wait_ms: 0 }, context);
  assert.equal(result.progress_target_moved, true);
  assert.equal(result.progress_target_visible, false);
});

test('到底后即使页面动画让截图变化，仍能确认末尾没有继续移动', async () => {
  const result = await progressHarness(600, 0).execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, max_attempts: 1, wait_ms: 0 }, context);
  assert.equal(result.scrolled, true, '截图动画确实改变了');
  assert.equal(result.progress_target_moved, false);
  assert.equal(result.progress_target_visible, true);
});

test('末尾只露出上半部分时不能确认到底', async () => {
  const result = await progressHarness(750, 0).execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, max_attempts: 1, wait_ms: 0 }, context);
  assert.equal(result.progress_target_moved, false);
  assert.equal(result.progress_target_visible, false);
});

test('末尾被列表裁掉时鼠标留在容器内，部分露出时落点限制在实际可见部分', async () => {
  const outside = progressHarness(650, 0, 600);
  await outside.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, wait_ms: 0 }, context);
  assert.ok(outside.moves[0].y > 0 && outside.moves[0].y < 600, '被列表完全裁掉的卡片不能接收滚轮');
  const partial = progressHarness(550, 0, 600);
  await partial.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, wait_ms: 0 }, context);
  assert.ok(partial.moves[0].y > 550 && partial.moves[0].y < 600, '部分可见卡片的鼠标落点必须位于列表交集内');
});
