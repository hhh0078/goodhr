// 文件作用：验证列表先按显式属性筛选再分页，防止靠后候选人被小窗口漏读，并保留显式序号旧语义。
import assert from "node:assert/strict";
import test from "node:test";
import { LocatorPrimitive } from "../dist/browser/primitives/locator.js";

const context = { trace_id: "attribute-pagination", action: "element.find_all", step: "find_all" };

/** decodeAttribute 还原标准CSS十六进制属性名转义，模拟真实Locator对attributeFilter的解析。 */
function decodeAttribute(value) {
  return value.replace(/\\([0-9a-fA-F]{1,6})\s?/g, (_, hex) => String.fromCodePoint(Number.parseInt(hex, 16)));
}

/** attributePage 构造只支持标准Locator属性交集的列表替身，不访问真实招聘页面。 */
function attributePage(rows) {
  let intersections = 0;
  /** query 保留实际元素身份，交集后的列表顺序不变化。 */
  function query(items, binding = "list") {
    return {
      items,
      and(other) { intersections++; return query(items.filter(item => other.items.includes(item))); },
      count: async () => items.length,
      nth(index) {
        const item = items[index];
        return { binding, originalIndex: item.originalIndex, count: async () => 1, getAttribute: async name => item.attributes[name] ?? null,
          isVisible: async () => true, isEnabled: async () => true };
      },
    };
  }
  return {
    locator(selector) {
      if (selector === ".row") return query(rows);
      const filters = [...selector.matchAll(/\[([^=\]]+)="([^"\\]*)"\]/g)];
      assert.ok(filters.length > 0, "本测试只接收实际attributeFilter产生的属性条件");
      return query(rows.filter(row => filters.every(([, name, value]) => row.attributes[decodeAttribute(name)] === decodeAttribute(value))), "attribute");
    },
    url: () => "https://example.test/contacts",
    intersectionCount: () => intersections,
  };
}

/** fixtureRows 生成目标原本在第十一行之后的联系人列表，避免仅用两行样本掩盖分页顺序错误。 */
function fixtureRows() {
  return Array.from({ length: 14 }, (_, originalIndex) => ({ originalIndex,
    attributes: { key: originalIndex === 11 ? "target" : `row-${originalIndex}`, group: [2, 5, 11, 13].includes(originalIndex) ? "chosen" : "other" } }));
}

/** listSpec 创建不附加平台名或CSS特例的通用属性列表配置。 */
function listSpec(attributes, index) {
  return { target: { selectors: [{ type: "css", value: ".row" }], ...(attributes ? { attributes } : {}), ...(index !== undefined ? { index } : {}) },
    state: "attached", timeout_ms: 0, description: "已约束联系人" };
}

test("原列表第十一行之后的唯一目标在MaxItems2时仍可找到", async () => {
  const page = attributePage(fixtureRows());
  const result = await new LocatorPrimitive().resolveAll(page, listSpec({ key: "target" }), 2, context);
  assert.equal(result.length, 1);
  assert.equal(result[0].locator.originalIndex, 11);
  assert.equal(result[0].list_index, 0);
  assert.equal(result[0].list_total, 1);
  assert.equal(result[0].attempts.at(-1).matches, 1);
  assert.equal(page.intersectionCount(), 1);
});

test("startIndex和尾部窗口均在属性筛选后分页并返回筛选后序号总数", async () => {
  const page = attributePage(fixtureRows());
  const primitive = new LocatorPrimitive();
  const middle = await primitive.resolveAll(page, listSpec({ group: "chosen" }), 2, context, 1);
  assert.deepEqual(middle.map(item => item.locator.originalIndex), [5, 11]);
  assert.deepEqual(middle.map(item => [item.list_index, item.list_total]), [[1, 4], [2, 4]]);
  const tail = await primitive.resolveAll(page, listSpec({ group: "chosen" }), 2, context, 0, true);
  assert.deepEqual(tail.map(item => item.locator.originalIndex), [11, 13]);
  assert.deepEqual(tail.map(item => [item.list_index, item.list_total]), [[2, 4], [3, 4]]);
});

test("未配置属性的原始列表分页与尾部窗口保持不变", async () => {
  const page = attributePage(fixtureRows());
  const primitive = new LocatorPrimitive();
  const middle = await primitive.resolveAll(page, listSpec(), 2, context, 10);
  assert.deepEqual(middle.map(item => [item.locator.originalIndex, item.list_index, item.list_total]), [[10, 10, 14], [11, 11, 14]]);
  const tail = await primitive.resolveAll(page, listSpec(), 2, context, 0, true);
  assert.deepEqual(tail.map(item => item.locator.originalIndex), [12, 13]);
  assert.equal(page.intersectionCount(), 0);
});

test("显式index仍针对原列表定位后核属性，不自动改到匹配项", async () => {
  const page = attributePage(fixtureRows());
  const primitive = new LocatorPrimitive();
  const selected = await primitive.resolveAll(page, listSpec({ key: "target" }, 11), 2, context);
  assert.deepEqual(selected.map(item => [item.locator.originalIndex, item.list_index, item.list_total]), [[11, 11, 14]]);
  await assert.rejects(primitive.resolveAll(page, listSpec({ key: "target" }, 10), 2, context), { code: "ELEMENT_NOT_FOUND" });
  assert.equal(page.intersectionCount(), 0);
});

test("长名单只读项绑定作用域唯一id，原行号和总数不变", async () => {
  const rows = fixtureRows();
  for (const row of rows) row.attributes.id = `public-${row.originalIndex}`;
  const page = attributePage(rows);
  const result = await new LocatorPrimitive().resolveAll(page, listSpec(), 2, context, 10, false, true);
  assert.deepEqual(result.map(item => [item.locator.originalIndex, item.list_index, item.list_total]), [[10, 10, 14], [11, 11, 14]]);
  // 字段用唯一id读取；跨动作element_ref仍保留原选择器，不扩大优化范围。
  assert.deepEqual(result.map(item => item.list_read_locator.binding), ["attribute", "attribute"]);
  assert.deepEqual(result.map(item => item.locator.binding), ["list", "list"]);
});

test("重复id不得把列表项绑定到另一行", async () => {
  const rows = fixtureRows();
  for (const row of rows) row.attributes.id = "duplicate";
  const result = await new LocatorPrimitive().resolveAll(attributePage(rows), listSpec(), 2, context, 10, false, true);
  assert.deepEqual(result.map(item => [item.locator.originalIndex, item.list_index]), [[10, 10], [11, 11]]);
  assert.deepEqual(result.map(item => item.list_read_locator.binding), ["list", "list"]);
});


test("iframe列表只绑定框架内id，不读取主文档同名项", async () => {
  const rows = fixtureRows();
  for (const row of rows) row.attributes.id = `public-${row.originalIndex}`;
  const frame = attributePage(rows);
  const page = {
    url: () => "https://example.test/frames",
    locator(selector) {
      assert.equal(selector, "iframe.list", "框架内卡片不得跳回主文档按id查找");
      return { count: async () => 1, nth() { return this; }, contentFrame: () => frame };
    },
  };
  const spec = { ...listSpec(), frames: [{ selectors: [{ type: "css", value: "iframe.list" }] }] };
  const result = await new LocatorPrimitive().resolveAll(page, spec, 2, context, 10, false, true);
  assert.deepEqual(result.map(item => [item.list_read_locator.originalIndex, item.list_read_locator.binding]), [[10, "attribute"], [11, "attribute"]]);
});
