// 文件作用：验证备用 iframe 路径检查完整目标状态、共用超时并保留路径证据，防止并列与内嵌弹窗互相回归。
import assert from 'node:assert/strict';
import test from 'node:test';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { parseSelectorSpec } from '../dist/validation/selector.js';
import { selectorDiagnostic } from '../dist/browser/actions/diagnostic-state.js';

/** group 创建一层静态测试选择器。 */
const group = value => ({ selectors: [{ type: 'css', value }] });
const context = { trace_id: 'frame-fallback', action: 'element.find', step: 'resolve', require_unique: true };

/** scope 创建带真实层级关系的 Locator 替身，不使用页面脚本或候选人内容。 */
function scope(entries, queries = []) {
  return {
    locator(value) {
      queries.push(value);
      const item = entries[value] ?? {};
      const locator = { count: async () => item.count ?? (item.present ? 1 : 0), nth() { return locator; },
        contentFrame: () => item.frame, isVisible: async () => item.visible ?? true, isEnabled: async () => true,
        getAttribute: async () => null, boundingBox: async () => ({ x: 20, y: 20, width: 200, height: 100 }) };
      return locator;
    },
    url: () => 'https://example.test/candidates', viewportSize: () => ({ width: 1280, height: 900 }),
  };
}

/** nestedPage 创建推荐框架内的详情及可选并列详情，供完整路径查找测试复用。 */
function nestedPage(primary = {}) {
  const queries = [];
  const detail = scope({ '#body': { present: true } }, queries);
  const recommend = scope({ 'iframe.detail': { present: true, frame: detail } }, queries);
  return { page: scope({ 'iframe.detail': primary, 'iframe.recommend': { present: true, frame: recommend } }, queries), queries };
}

const spec = { frames: [group('iframe.detail')], frame_fallbacks: [[group('iframe.recommend'), group('iframe.detail')]],
  target: group('#body'), description: '详情正文', timeout_ms: 0 };

test('内嵌详情能完整进入两层框架并记录主路径失败与备用路径成功', async () => {
  const { page } = nestedPage();
  const result = await new LocatorPrimitive().resolve(page, spec, context);
  assert.deepEqual(result.attempts.map(item => item.frame_path), [0, 1, 1, 1]);
  assert.equal(result.attempts[0].matches, 0);
  assert.equal(result.attempts.at(-1).matches, 1);
});

test('并列详情仍优先使用，成功后不会再进入推荐框架', async () => {
  const { page, queries } = nestedPage({ present: true, frame: scope({ '#body': { present: true } }) });
  const result = await new LocatorPrimitive().resolve(page, spec, context);
  assert.ok(result.attempts.every(item => item.frame_path === 0));
  assert.ok(!queries.includes('iframe.recommend'));
});

test('主框架存在但正文隐藏时仍检查备用路径的实际目标', async () => {
  const { page } = nestedPage({ present: true, frame: scope({ '#body': { present: true, visible: false } }) });
  const result = await new LocatorPrimitive().resolve(page, spec, context);
  assert.equal(result.attempts.at(-1).frame_path, 1);
});

test('弹窗探测在主页面无目标时进入备用框架，不依赖简历正文', async () => {
  const { page } = nestedPage();
  const result = await new LocatorPrimitive().resolveAll(page, { frames: [], frame_fallbacks: [[group('iframe.recommend')]],
    target: group('iframe.detail'), description: '详情弹窗', timeout_ms: 0 }, 1, context);
  assert.equal(result.length, 1);
  assert.equal(result[0].attempts.at(-1).frame_path, 1);
});

test('主路径存在歧义时停止，不猜测备用路径来掩盖多弹窗问题', async () => {
  const { page, queries } = nestedPage({ count: 2 });
  await assert.rejects(new LocatorPrimitive().resolve(page, spec, context), { code: 'ELEMENT_AMBIGUOUS' });
  assert.ok(!queries.includes('iframe.recommend'));
});

test('所有框架路径共用一次等待期限，最终日志包含每条路径的失败', async () => {
  const started = Date.now();
  await assert.rejects(new LocatorPrimitive().resolve(scope({}), { ...spec, timeout_ms: 100 }, context), error => {
    assert.equal(error.code, 'ELEMENT_NOT_FOUND');
    assert.equal(error.details.timeout_ms, 100);
    assert.deepEqual(error.details.attempts.map(item => item.frame_path), [0, 1]);
    return true;
  });
  assert.ok(Date.now() - started < 500);
});

test('备用路径校验拒绝损坏层级，并允许空路径表示主页面', () => {
  const parsed = parseSelectorSpec({ ...spec, timeout_ms: 100, frame_fallbacks: [[]] }, 'parse', 'element.find');
  assert.deepEqual(parsed.frame_fallbacks, [[]]);
  for (const frame_fallbacks of [null, [null], [{}], [[{}]], [[], [], [], [], []]]) {
    assert.throws(() => parseSelectorSpec({ ...spec, frame_fallbacks }, 'parse', 'element.find'), { code: 'INVALID_REQUEST' });
  }
  assert.equal(selectorDiagnostic(spec).frame_fallbacks[0].length, 2);
});
