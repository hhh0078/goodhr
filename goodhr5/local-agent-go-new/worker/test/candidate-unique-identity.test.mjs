// 文件作用说明：用合成可换位名单验证正式定位器不依赖旧行号，并拒绝无法区分的同名同经历者。
import test from 'node:test';
import assert from 'node:assert/strict';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';

/** fixture 模拟标准Locator的动态查询，未提供页面脚本、鼠标或发送能力。 */
function fixture() {
  let rows = Array.from({ length: 20 }, (_, id) => ({ id, text: `合成人选${id} 30岁 5年经验 合成学校${id} 本科` }));
  /** locator 每次读取当前名单，不把数组换位误写成静态测试结果。 */
  function locator(get) {
    return {
      locator(css) { return css === ':scope' ? locator(get) : locator(() => get().flatMap(item => item.rows ?? [])); },
      filter({ hasText }) { return locator(() => get().filter(item => item.text?.includes(hasText))); },
      nth(index) { return locator(() => get().slice(index, index + 1)); },
      count: async () => get().length, isVisible: async () => get().length === 1, isEnabled: async () => true,
      getAttribute: async () => null, boundingBox: async () => ({ x: 0, y: 200, width: 600, height: 200 }), values: get,
    };
  }
  return { page: { url: () => 'https://synthetic.invalid/list', viewportSize: () => ({ width: 1200, height: 900 }), locator: () => locator(() => [{ rows }]) },
    rotate() { rows = [...rows.slice(1), rows[0]]; },
    duplicate() { rows.push({ ...rows.find(row => row.id === 12) }); },
    sameNameOtherSchool() { rows.push({ id: 99, text: '合成人选12 30岁 5年经验 其他学校 本科' }); },
  };
}
const parent = { selectors: [{ type: 'css', value: '.list' }] };
const card = { selectors: [{ type: 'css', value: '.row' }] };
const unique = { parents: [parent], target: { ...card, texts: ['合成人选12', '30岁 5年经验', '合成学校12 本科'] }, state: 'visible', timeout_ms: 0, description: '合成身份' };
const legacy = { ...unique, parents: [parent, { ...card, index: 12 }], target: { selectors: [{ type: 'css', value: ':scope' }], texts: ['合成人选12', '30岁'] } };
const context = { action: 'element.scroll', step: 'find', trace_id: 'identity-fixture', require_unique: true };

test('20人名单换位后旧索引失配，新定位仍找到同一人', async () => {
  const f = fixture(), p = new LocatorPrimitive();
  assert.equal((await p.resolve(f.page, legacy, context)).locator.values()[0].id, 12);
  f.rotate();
  await assert.rejects(p.resolve(f.page, legacy, context), { code: 'ELEMENT_NOT_FOUND' });
  assert.equal((await p.resolve(f.page, unique, context)).locator.values()[0].id, 12);
});
test('同名同龄不同学校仍可准确区分', async () => {
  const f = fixture(); f.rotate(); f.sameNameOtherSchool();
  assert.equal((await new LocatorPrimitive().resolve(f.page, unique, context)).locator.values()[0].id, 12);
});
test('身份完全重复时拒绝选择第一位', async () => {
  const f = fixture(); f.duplicate();
  await assert.rejects(new LocatorPrimitive().resolve(f.page, unique, context), { code: 'ELEMENT_AMBIGUOUS' });
});
