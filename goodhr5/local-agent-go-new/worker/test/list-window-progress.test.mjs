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
function progressHarness(y, movement, containerHeight = 6000, hooks = {}) {
  let position = y;
  let screenshots = 0;
  const reads = [];
  const metrics = { wheelCalls: 0, screenshots: 0 };
  const viewport = { width: 1200, height: 800 };
  const page = { async screenshot() {
    const image = Buffer.from(`动画${screenshots++}`);
    metrics.screenshots++;
    const nextPosition = await hooks.afterScreenshot?.(screenshots);
    if (typeof nextPosition === 'number') position = nextPosition;
    return image;
  }, viewportSize() { return viewport; } };
  const progressView = () => ({ visible: true, box: { x: 100, y: position + (hooks.anchorY?.() ?? 0), width: 800, height: 160 }, viewport });
  const anchorView = { visible: true, box: { x: 100, y: 0, width: 800, height: containerHeight }, viewport };
  const progress = { locator: { kind: 'progress', async boundingBox() { return progressView().box; } }, view: progressView(), page, matched_selector: { type: 'css', value: '.row' } };
  const anchor = { locator: { kind: 'anchor', async boundingBox() { return { ...anchorView.box, y: hooks.anchorY?.() ?? 0 }; } }, view: anchorView, page, matched_selector: { type: 'css', value: '.list' } };
  const moves = [];
  const logger = { info() {}, failure() {} };
  const mouse = {
    async move(_page, x, y) { moves.push({ x, y }); },
    async wheel(_page, _x, deltaY) { position -= deltaY * (hooks.movement?.(moves) ?? movement); metrics.wheelCalls++; hooks.onWheel?.(); },
  };
  const action = new ScrollAction(
    { async requirePage() { return page; } },
    { async one(spec) { return { resolved: spec.description === '列表项' ? progress : anchor }; } },
    new MoveAction(mouse, logger),
    { async view(_page, locator) { reads.push(locator.kind); const changed = hooks.onGeometryRead?.(locator.kind); if (typeof changed === 'number') position = changed; return locator.kind === 'progress' ? progressView() : anchorView; } },
    mouse,
    logger,
  );
  action.moves = moves;
  action.reads = reads;
  action.metrics = metrics;
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


test('从尾部读取超过五千条的消息，保留最新消息与真实总数', async () => {
  const { action } = listHarness(5003);
  const rows = await action.all(selector, 5000, {}, context, false, 100, 0, undefined, true);
  assert.equal(rows.length, 5000);
  assert.equal(rows[0].index, 3);
  assert.equal(rows.at(-1).index, 5002);
  assert.equal(rows.at(-1).text, '记录5002');
  assert.equal(rows.at(-1).total_matches, 5003);
  const last = await action.all(selector, 1, {}, context, false, 100, 0, undefined, true);
  assert.equal(last[0].index, 5002);
});

test('尾部窗口不和起点或固定序号混用，默认读取仍从头开始', async () => {
  assert.equal(parseElementFindAllRequest({ selector, from_end: true }, 'window', 'element.find_all').from_end, true);
  assert.throws(() => parseElementFindAllRequest({ selector, from_end: true, start_index: 1 }, 'window', 'element.find_all'));
  assert.throws(() => parseElementFindAllRequest({ selector: { ...selector, target: { ...selector.target, index: 0 } }, from_end: true }, 'window', 'element.find_all'));
  const { action } = listHarness(5003);
  assert.equal((await action.all(selector, 2, {}, context, false))[0].index, 0);
});

test('向上滚动检查首边，消息下半段露出不能证明已到顶部', async () => {
  const hiddenHead = await progressHarness(-80, 0).execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: -620, wait_ms: 0 }, context);
  assert.equal(hiddenHead.progress_target_visible, false);
  const visibleHead = await progressHarness(1, 0).execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: -620, wait_ms: 0 }, context);
  assert.equal(visibleHead.progress_target_visible, true);
});

test('尾部已可见时恢复最新端不发送多余滚轮', async () => {
  const result = await progressHarness(600, 1).execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, stop_when_progress_visible: true, max_attempts: 100, wait_ms: 0 }, context);
  assert.equal(result.attempts, 0);
  assert.equal(result.progress_target_visible, true);
  assert.equal(result.progress_target_moved, false);
  assert.throws(() => parseScrollRequest({ distance: 620, stop_when_progress_visible: true }, 'window', 'element.scroll'));
});

test('一次滚轮只做一次结束进度复核，保留原有前后截图与scrolled语义', async () => {
  const action = progressHarness(4000, 1);
  const result = await action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, max_attempts: 1, wait_ms: 0 }, context);
  assert.deepEqual(action.reads, ['progress', 'anchor']);
  assert.equal(action.metrics.screenshots, 2);
  assert.equal(result.attempts, 1);
  assert.equal(result.scrolled, true);
  assert.equal(typeof result.before.screenshot_hash, 'string');
  assert.equal(typeof result.after.screenshot_hash, 'string');
  assert.equal(result.progress_target_moved, true);
  assert.equal(result.progress_target_visible, false);
});

test('多轮滚动仍在继续前刷新进度，最后一轮不重复刷新', async () => {
  const action = progressHarness(4000, 1);
  const result = await action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, max_attempts: 2, wait_ms: 0 }, context);
  assert.deepEqual(action.reads, ['progress', 'anchor', 'progress', 'anchor']);
  assert.equal(result.attempts, 2);
  assert.equal(action.metrics.screenshots, 3);
});

test('零轮滚动仍最终复核异步插入后的末条位置，不复用开始时的可见快照', async () => {
  const action = progressHarness(600, 0, 6000, { onGeometryRead(kind) { if (kind === 'progress') return 1000; } });
  const result = await action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, stop_when_progress_visible: true, max_attempts: 1, wait_ms: 0 }, context);
  assert.equal(result.attempts, 0);
  assert.equal(action.metrics.wheelCalls, 0);
  assert.equal(result.scrolled, false);
  assert.notDeepEqual(result.before, result.after);
  assert.deepEqual(action.reads, ['progress']);
  assert.equal(result.progress_target_moved, true);
  assert.equal(result.progress_target_visible, false);
});

test('滚轮后异步插入更早消息时，结束复核仍按新的首边判断', async () => {
  const action = progressHarness(-100, 0, 6000, { async afterScreenshot(count) { if (count === 2) { await Promise.resolve(); return -900; } } });
  const result = await action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: -620, max_attempts: 1, wait_ms: 0 }, context);
  assert.deepEqual(action.reads, ['progress', 'anchor']);
  assert.equal(result.attempts, 1);
  assert.equal(result.progress_target_moved, true);
  assert.equal(result.progress_target_visible, false);
});

test('滚轮期间取消后不补发滚轮或执行结束读取', async () => {
  const controller = new AbortController();
  const action = progressHarness(4000, 1, 6000, { onWheel() { controller.abort(); } });
  await assert.rejects(action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' }, distance: 620, max_attempts: 1, wait_ms: 0 }, { ...context, signal: controller.signal }));
  assert.equal(action.metrics.wheelCalls, 1);
  assert.equal(action.metrics.screenshots, 1);
  assert.deepEqual(action.reads, []);
});

test('边缘仅露窄缝时从容器内部滚动，正确证明末边到达', async () => {
  const action = progressHarness(592, 0, 600, { movement(moves) { return moves[0].y < 450 ? 0.35 : 0; } });
  const result = await action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' },
    distance: 620, max_attempts: 100, wait_ms: 0, stop_when_progress_visible: true }, context);
  assert.ok(action.moves[0].y >= 180 && action.moves[0].y <= 420);
  assert.equal(result.progress_target_visible, true);
  assert.ok(result.attempts >= 1 && result.attempts <= 2);
  assert.equal(action.metrics.screenshots, 0);
});

test('外页和子列表同移不算容器内靠近，改点一次后有界停止', async () => {
  let outerY = 0;
  // 两个盒子一起随外页移动，相对位置始终不变。
  const action = progressHarness(590, 0, 600, { anchorY: () => outerY, onWheel() { outerY -= 2; } });
  await assert.rejects(action.execute({ progress_target: selector, wheel_anchor: { ...selector, description: '列表' },
    distance: 2400, max_attempts: 100, wait_ms: 0, stop_when_progress_visible: true }, context),
    error => error.code === 'SCROLL_NO_PROGRESS' && error.details.attempts === 6);
  assert.equal(action.moves.length, 2);
  assert.ok(action.moves[1].x < action.moves[0].x);
  assert.equal(action.metrics.screenshots, 0);
});
