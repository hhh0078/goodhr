// 文件作用说明：验证列表项内部字段查找会复用统一选择器轮询机制。

import assert from "node:assert/strict";
import test from "node:test";

import { FindAction } from "../dist/browser/actions/find.js";
import { LocatorPrimitive } from "../dist/browser/primitives/locator.js";
import { parseElementFindAllRequest } from "../dist/validation/action-requests.js";

/** 验证相对选择器在元素延迟出现时会继续查找。 */
test("相对选择器会轮询等待延迟出现的元素", async () => {
  let queryCount = 0;
  const locator = {
    async count() {
      queryCount += 1;
      return queryCount >= 3 ? 1 : 0;
    },
    nth() {
      return locator;
    },
    async getAttribute() {
      return null;
    },
    async boundingBox() {
      return { x: 10, y: 10, width: 100, height: 30 };
    },
    async isVisible() {
      return true;
    },
    async isEnabled() {
      return true;
    },
  };
  const root = {
    locator() {
      return locator;
    },
  };
  const page = {
    url() {
      return "https://example.com/candidates";
    },
    async evaluate() {
      return { width: 1280, height: 720 };
    },
  };

  const startedAt = Date.now();
  const result = await new LocatorPrimitive().resolveRelative(
    page,
    root,
    {
      target: { selectors: [{ type: "css", value: ".candidate-name" }] },
      state: "visible",
      timeout_ms: 1_000,
      description: "候选人姓名",
    },
    {
      trace_id: "relative-retry",
      action: "element.find_all",
      step: "read_field:name",
      require_unique: false,
    },
  );

  assert.equal(result.locator, locator);
  assert.equal(queryCount, 3);
  assert.ok(Date.now() - startedAt >= 180);
});

/** 验证预期元素不存在时保留强类型参数并关闭过程日志。 */
test("预期元素不存在时不写错误过程日志", async () => {
  const request = parseElementFindAllRequest(
    {
      selector: {
        target: { selectors: [{ type: "css", value: ".closed-detail" }] },
        description: "已经关闭的详情",
      },
      max_items: 1,
      expected_missing: true,
    },
    "expected-missing",
    "element.find_all",
  );
  assert.equal(request.expected_missing, true);

  let logCount = 0;
  const action = new FindAction(
    {
      async requirePage() {
        return {};
      },
    },
    {
      async resolveAll() {
        throw new Error("没有找到元素");
      },
    },
    {
      info() {
        logCount += 1;
      },
      failure() {
        logCount += 1;
      },
    },
  );
  await assert.rejects(
    action.all(
      request.selector,
      request.max_items,
      {},
      {
        trace_id: "expected-missing",
        action: "element.find_all",
        started_at: Date.now(),
      },
      false,
    ),
  );
  assert.equal(logCount, 0);
});
