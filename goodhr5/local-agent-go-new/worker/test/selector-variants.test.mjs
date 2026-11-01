// 文件作用：验证完整选择器对象数组按独立路径降级，保留旧对象、序号、状态与候选人作用域。
import assert from 'node:assert/strict';
import test from 'node:test';
import { parseSelectorSpec } from '../dist/validation/selector.js';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { selectorDiagnostic } from '../dist/browser/actions/diagnostic-state.js';
import { ReadAction } from '../dist/browser/actions/read.js';
import { parseElementReadRequest } from '../dist/validation/action-requests.js';

/** group 创建含单个静态 CSS 选择器的测试层级。 */
const group = value => ({ selectors: [{ type: 'css', value }] });
const context = { trace_id: 'selector-variants', action: 'element.find', step: 'resolve', require_unique: true };

/** scope 模拟独立的页面与父级层次，只提供标准 Locator 读取能力。 */
function scope(entries, queries = []) {
  return {
    locator(value) {
      queries.push(value);
      const item = entries[value] ?? {};
      const child = scope(item.children ?? {}, queries);
      const locator = {
        locator: child.locator,
        count: async () => item.count ?? (item.present ? 1 : 0),
        nth(index) { item.selectedIndex = index; return locator; },
        filter() { return locator; },
        contentFrame: () => item.frame,
        isVisible: async () => item.visible ?? true,
        isEnabled: async () => item.enabled ?? true,
        getAttribute: async () => null,
        boundingBox: async () => ({ x: 20, y: 20, width: 200, height: 100 }),
      };
      return locator;
    },
    url: () => 'https://example.test/people',
    viewportSize: () => ({ width: 1280, height: 900 }),
  };
}

/** selector 创建原格式完整对象，测试中仅覆盖必要字段。 */
function selector(parent, target, overrides = {}) {
  return { parents: [group(parent)], target: group(target), description: '候选人入口', timeout_ms: 100, ...overrides };
}

test('旧对象校验结果保持原样，不增加备用路径', () => {
  const original = selector('.recommend', '.person', { state: 'enabled' });
  assert.deepEqual(parseSelectorSpec(original, 'legacy', 'element.find'), original);
  assert.equal(Object.hasOwn(parseSelectorSpec(original, 'legacy', 'element.find'), 'alternatives'), false);
});

test('主路径缺失时按备用完整父级和目标定位，不交叉组合', async () => {
  const queries = [];
  const page = scope({ '.recommend': { present: true, children: { '.search-person': { present: true } } },
    '.search': { present: true, children: { '.row': { present: true, children: { '.search-person': { present: true } } } } } }, queries);
  const spec = parseSelectorSpec([selector('.recommend', '.recommend-person'),
    { ...selector('.search', '.search-person'), parents: [group('.search'), group('.row')] }], 'variants', 'element.find');
  const result = await new LocatorPrimitive().resolve(page, { ...spec, timeout_ms: 0 }, context);
  assert.equal(result.matched_selector.value, '.search-person');
  assert.deepEqual(queries, ['.recommend', '.recommend-person', '.search', '.row', '.search-person']);
  assert.equal(result.attempts.at(-1).frame_path, 1);
});

test('主路径成功时仍优先沿用原路径，不查询备用页面', async () => {
  const queries = [];
  const page = scope({ '.recommend': { present: true, children: { '.person': { present: true } } } }, queries);
  const spec = parseSelectorSpec([selector('.recommend', '.person'), selector('.search', '.person')], 'variants', 'element.find');
  await new LocatorPrimitive().resolve(page, spec, context);
  assert.deepEqual(queries, ['.recommend', '.person']);
});

test('备用路径保留列表序号和独立读取属性，并用于列表及相对字段入口', async () => {
  const person = { count: 5 };
  const page = scope({ '.search': { present: true, children: { '.person': person } } });
  const spec = parseSelectorSpec([selector('.recommend', '.person'), selector('.search', '.person',
    { target: { ...group('.person'), index: 3 }, read_attribute: 'title', state: 'attached' })], 'variants', 'element.find');
  const results = await new LocatorPrimitive().resolveAll(page, { ...spec, timeout_ms: 0 }, 100, context);
  assert.equal(results.length, 1);
  assert.equal(results[0].list_index, 3);
  assert.equal(person.selectedIndex, 3);
  assert.equal(results[0].resolved_spec.read_attribute, 'title');
  const relative = await new LocatorPrimitive().resolveRelative(page, page, { ...spec, timeout_ms: 0 }, context);
  assert.equal(relative.resolved_spec.read_attribute, 'title');
  assert.equal(selectorDiagnostic(spec).alternatives[0].target.index, 3);
});

test('旧路径匹配有歧义时保持停止规则，不用备用路径掩盖问题', async () => {
  const queries = [];
  const page = scope({ '.recommend': { count: 2 }, '.search': { present: true, children: { '.person': { present: true } } } }, queries);
  const spec = parseSelectorSpec([selector('.recommend', '.person'), selector('.search', '.person')], 'variants', 'element.find');
  await assert.rejects(new LocatorPrimitive().resolve(page, { ...spec, timeout_ms: 0 }, context), { code: 'ELEMENT_AMBIGUOUS' });
  assert.deepEqual(queries, ['.recommend']);
});

test('损坏的选择器数组在浏览器动作前被拒绝', () => {
  const original = selector('.recommend', '.person');
  for (const invalid of [[], [null], ['.person'], [[]], [{}], Array(9).fill(original),
    { ...original, alternatives: null }, { ...original, alternatives: Array(64).fill(original) }]) {
    assert.throws(() => parseSelectorSpec(invalid, 'variants', 'element.find'), { code: 'INVALID_REQUEST' });
  }
});

test('备用读取使用命中方案的属性，旧对象仍遵守请求原有读取方式', async () => {
  const primary = selector('.recommend', '.person', { read_attribute: 'href' });
  const fallback = selector('.search', '.person', { read_attribute: 'title' });
  const parsed = parseSelectorSpec([primary, fallback], 'read-variants', 'element.read');
  const find = { one: async () => ({ result: { element_ref: 'person' }, resolved: {
    resolved_spec: parsed.alternatives[0], locator: { getAttribute: async name => `属性:${name}`, innerText: async () => '正文' },
  } }) };
  const logger = { info() {}, failure() {} };
  const action = new ReadAction(find, logger);
  const request = parseElementReadRequest({ selector: parsed, attribute: 'href', use_selector_read: true }, 'read-variants', 'element.read');
  const resolved = await action.execute(request, { ...context, action: 'element.read', started_at: Date.now() });
  assert.equal(resolved.value, '属性:title');
  const legacy = await action.execute({ selector: primary, attribute: 'href' }, { ...context, action: 'element.read', started_at: Date.now() });
  assert.equal(legacy.value, '属性:href');
  assert.equal(Object.hasOwn(parseElementReadRequest({ selector: primary, attribute: 'href' }, 'legacy', 'element.read'), 'use_selector_read'), false);
});
