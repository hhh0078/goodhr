// 文件作用：验证标准Locator先按稳定属性收紧父级，再检查唯一性，并保持已有显式序号的原列表语义。
import assert from 'node:assert/strict';
import test from 'node:test';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';

const context = { trace_id: 'synthetic-attributes', action: 'element.find', step: 'resolve', require_unique: true };
const targetValue = '{"thread":"target", "quote":"\\\"", "line":"换行\n第二行"}';

/** decodeCSS 仅为隔离替身解码标准CSS十六进制转义，真实浏览器复核使用同一份生产选择器。 */
function decodeCSS(value) {
  return value.replace(/\\([0-9a-f]{1,6})\s?/gi, (_, hex) => String.fromCodePoint(parseInt(hex, 16)));
}

/** fixture 用可组合的Locator集合模拟联系人列表，不提供执行页面脚本的能力。 */
function fixture(rows) {
  const filters = [];
  /** locator 返回保持集合交集、原始序号和只读属性的标准Locator替身。 */
  function locator(items) {
    return {
      items,
      and(other) { return locator(items.filter(item => other.items.includes(item))); },
      async count() { return items.length; },
      nth(index) { return locator(items.slice(index, index + 1)); },
      locator(selector) { return query(items.flatMap(item => item.children || []), selector); },
      async getAttribute(name) { return items[0]?.attributes?.[name] ?? null; },
      async isVisible() { return true; },
      async isEnabled() { return true; },
      async boundingBox() { return { x: 10, y: 10, width: 100, height: 30 }; },
    };
  }
  /** query 只解析测试使用的类名与属性等值选择器。 */
  function query(items, selector) {
    if (!selector.startsWith('[')) return locator(items.filter(item => item.selector === selector));
    filters.push(selector);
    const attributes = [...selector.matchAll(/\[([^=]+)="([^"]*)"\]/g)].map(([, name, value]) => [decodeCSS(name), decodeCSS(value)]);
    assert.ok(attributes.length, '必须生成实际属性约束');
    return locator(items.filter(item => attributes.every(([name, value]) => item.attributes?.[name] === value)));
  }
  return { filters, page: { locator: selector => query(rows, selector), viewportSize: () => ({ width: 900, height: 700 }), url: () => 'https://fixture.invalid/' } };
}

/** row 创建具有稳定身份属性和姓名子元素的合成行。 */
function row(value) { return { selector: '.row', attributes: { 'data-meta': value }, children: [{ selector: '.name' }] }; }

/** spec 创建与生产守卫相同的无序号父级属性限定，最后定位行内姓名。 */
function spec(attributes, index) {
  return { parents: [{ selectors: [{ type: 'css', value: '.row' }], attributes, ...(index === undefined ? {} : { index }) }], target: { selectors: [{ type: 'css', value: '.name' }] }, state: 'visible', description: '合成姓名', timeout_ms: 0 };
}

test('68行中先按完整属性限定唯一父级，再定位行内姓名', async () => {
  const rows = Array.from({ length: 68 }, (_, index) => row(`thread-${index}`));
  rows[31] = row(targetValue);
  const { page, filters } = fixture(rows);
  const result = await new LocatorPrimitive().resolve(page, spec({ 'data-meta': targetValue }), context);
  assert.equal(result.locator.items[0], rows[31].children[0]);
  assert.equal(result.attempts[0].matches, 1);
  assert.equal(filters.length, 1);
});

test('相同稳定属性仍对应两行时拒绝，不默认点击首个', async () => {
  const { page } = fixture([row(targetValue), row(targetValue)]);
  await assert.rejects(new LocatorPrimitive().resolve(page, spec({ 'data-meta': targetValue }), context), error => error.code === 'ELEMENT_AMBIGUOUS');
});

test('属性不匹配时拒绝，不回退到其他联系人', async () => {
  const { page } = fixture([row('other'), row('another')]);
  await assert.rejects(new LocatorPrimitive().resolve(page, spec({ 'data-meta': targetValue }), context), error => error.code === 'ELEMENT_NOT_FOUND');
});

test('显式index保留原始列表序号，属性只复核选定行，不能重排后套用旧序号', async () => {
  const rows = [row('other'), row(targetValue), row('last')];
  const { page, filters } = fixture(rows);
  const result = await new LocatorPrimitive().resolve(page, spec({ 'data-meta': targetValue }, 1), context);
  assert.equal(result.locator.items[0], rows[1].children[0]);
  await assert.rejects(new LocatorPrimitive().resolve(page, spec({ 'data-meta': targetValue }, 0), context), error => error.code === 'ELEMENT_NOT_FOUND');
  assert.deepEqual(filters, []);
});

test('特殊字符和多个属性保持精确交集，字符串不能扩展为任意选择器', async () => {
  const value = '"] , .row [data-other="\\\n中文';
  const rows = [row(value), row(value), row('different')];
  rows[1].attributes['data-kind'] = 'target';
  const { page } = fixture(rows);
  const result = await new LocatorPrimitive().resolve(page, spec({ 'data-meta': value, 'data-kind': 'target' }), context);
  assert.equal(result.locator.items[0], rows[1].children[0]);
});
