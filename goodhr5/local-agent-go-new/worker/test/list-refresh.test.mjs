// 文件作用说明：复现列表刷新时字段消失，验证缺失字段不拖垮整批扫描，变化行不混用信息。
import assert from "node:assert/strict";
import test from "node:test";
import { FindAction } from "../dist/browser/actions/find.js";

/** harness 创建列表刷新场景，只使用标准 Locator 读取替身。 */
function harness(changed = false, text = "稳定记录") {
  let reads = 0;
  const root = { async innerText(options) {
    assert.equal(options.timeout, 1500);
    return changed && reads++ > 0 ? "另一条记录" : text;
  } };
  return new FindAction(
    { async requirePage() { return {}; }, elements: { remember() { return "ref"; } } },
    { async resolveAll() { return [{ locator: root }]; }, async resolveRelative() {
      return { locator: { async innerText() { throw new Error("field detached"); } } };
    } },
    { info() {}, warn() {}, failure() {} },
  );
}

test("字段在 resolve 后消失时返回空字段，不把异步拒绝漏出 catch", async () => {
  const rows = await harness().all({}, 1, { optional: {} }, { trace_id: "test", action: "find_all", started_at: Date.now() });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].fields.optional, "");
});

test("读取期间列表行变化时重新读取稳定的新行，不拼接不同候选人的信息", async () => {
  const rows = await harness(true).all({}, 1, { optional: {} }, { trace_id: "test", action: "find_all", started_at: Date.now() });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "另一条记录");
  assert.equal(rows[0].fields.optional, "");
});

test("没有文字的可见图标和弹框仍然保留，不能误判不存在", async () => {
  const rows = await harness(false, "").all({}, 1, {}, { trace_id: "test", action: "find_all", started_at: Date.now() });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].text, "");
});

test("大量卡片保留请求并发 10，同时限制字段读取为 4 且保持顺序", async () => {
  let active = 0;
  let maximum = 0;
  const resolved = Array.from({ length: 29 }, (_, index) => ({
    locator: { index, async innerText() { return `记录-${index}`; } },
  }));
  const action = new FindAction(
    { async requirePage() { return {}; }, elements: { remember(_page, root) { return `ref-${root.index}`; } } },
    { async resolveAll() { return resolved; }, async resolveRelative(_page, root) {
      active++;
      maximum = Math.max(maximum, active);
      await new Promise(resolve => setTimeout(resolve, 10));
      active--;
      return { locator: { async innerText() { return `姓名-${root.index}`; } } };
    } },
    { info() {}, warn() {}, failure() {} },
  );
  const rows = await action.all({}, 100, { name: {} }, { trace_id: "concurrency", action: "find_all", started_at: Date.now() }, false, 10);
  assert.equal(rows.length, 29);
  assert.equal(maximum, 4);
  assert.deepEqual(rows.map(row => row.index), Array.from({ length: 29 }, (_, index) => index));
});
