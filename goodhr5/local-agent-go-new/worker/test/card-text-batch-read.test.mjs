// 文件作用说明：模拟 300 张卡片的高并发读取，验证浏览器读取限流与卡片文字补读。
import assert from "node:assert/strict";
import test from "node:test";
import { FindAction } from "../dist/browser/actions/find.js";

const context = { trace_id: "card-text-batch", action: "element.find_all", request_id: "batch-300", started_at: Date.now() };
const selector = { target: { selectors: [{ type: "css", value: ".card" }] }, description: "候选人卡片" };
const fieldNames = ["name", "card_text", "age", "degree", "city", "experience"];

/** batchHarness 模拟浏览器同时读取过多字段就超时，并让两张卡片首次读取失败。 */
function batchHarness() {
  let active = 0;
  let maxActive = 0;
  const failures = new Set();
  const logs = [];
  const page = { url: () => "https://example.com/list" };
  /** textRead 模拟一次标准 Locator 文本读取，记录并发峰值。 */
  async function textRead(index, field) {
    active++;
    maxActive = Math.max(maxActive, active);
    try {
      await new Promise(resolve => setTimeout(resolve, 2));
      const key = `${index}:${field}`;
      if (active > 4 || ((key === "125:card_text" || key === "200:root") && !failures.has(key))) {
        failures.add(key);
        const error = new Error("read timeout");
        error.name = "TimeoutError";
        throw error;
      }
      if (field === "root" || field === "card_text") {
        return index === 8 ? "热搜牛人推荐 尚先生" : `普通候选人${index} 打招呼`;
      }
      return field === "name" ? `候选人${index}` : `${field}${index}`;
    } finally { active--; }
  }
  const resolved = Array.from({ length: 300 }, (_, index) => ({
    page, list_index: index, view: { visible: true },
    matched_selector: { type: "css", value: ".card" }, attempts: [],
    locator: { index, innerText: () => textRead(index, "root") },
  }));
  const action = new FindAction(
    { requirePage: async () => page, elements: { remember: (_page, locator) => `ref-${locator.index}` } },
    { resolveAll: async () => resolved, resolveRelative: async (_page, root, spec) => ({
      page, view: { visible: true }, attempts: [{ matches: 1 }],
      locator: { innerText: () => textRead(root.index, spec.description) },
    }) },
    { info: (_ctx, step, status, fields) => logs.push({ step, status, fields }),
      warn: (_ctx, step, status, fields) => logs.push({ step, status, fields }), failure: () => {} },
  );
  const fields = Object.fromEntries(fieldNames.map(name => [name, { ...selector, description: name }]));
  return { action, fields, logs, getMaxActive: () => maxActive };
}

test("并发 10 连续读取 300 张卡片，限流并补读超时文字，不误伤普通卡片", async () => {
  const harness = batchHarness();
  const items = await harness.action.all(selector, 300, harness.fields, context, true, 10);
  assert.equal(items.length, 300);
  assert.ok(harness.getMaxActive() <= 4);
  assert.equal(items[125].fields.card_text, "普通候选人125 打招呼");
  assert.equal(items[200].fields.name, "候选人200");
  assert.match(items[8].fields.card_text, /热搜牛人/);
  assert.doesNotMatch(items[9].fields.card_text, /热搜牛人/);
  assert.ok(harness.logs.some(log => log.step === "read_field:card_text" && log.status === "retry_read"));
  assert.ok(harness.logs.some(log => log.step === "read_list_item" && log.status === "retry_read"));
  assert.deepEqual(harness.logs.find(log => log.status === "summary").fields.dropped_indices, []);
});
