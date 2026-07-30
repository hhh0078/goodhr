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

/** 验证列表读取只检查必要状态，不为每张卡片重复测量坐标和视口。 */
test("列表读取跳过坐标和视口测量", async () => {
  let boxReads = 0;
  let viewportReads = 0;
  let visibleReads = 0;
  const locator = {
    async count() {
      return 2;
    },
    nth() {
      return locator;
    },
    async getAttribute() {
      return null;
    },
    async isVisible() {
      visibleReads += 1;
      return true;
    },
    async isEnabled() {
      return true;
    },
    async boundingBox() {
      boxReads += 1;
      return { x: 10, y: 10, width: 100, height: 30 };
    },
  };
  const page = {
    locator() {
      return locator;
    },
    viewportSize() {
      viewportReads += 1;
      return { width: 1280, height: 720 };
    },
    url() {
      return "https://example.com/candidates";
    },
  };

  const result = await new LocatorPrimitive().resolveAll(
    page,
    {
      target: { selectors: [{ type: "css", value: ".candidate-card" }] },
      state: "visible",
      timeout_ms: 1_000,
      description: "候选人卡片",
    },
    20,
    {
      trace_id: "list-fast-path",
      action: "element.find_all",
      step: "find_all",
    },
  );

  assert.equal(result.length, 2);
  assert.equal(visibleReads, 2);
  assert.equal(boxReads, 0);
  assert.equal(viewportReads, 0);
});

/** 验证正在消失的隐藏元素不会触发耗时的坐标和视口读取。 */
test("隐藏元素跳过坐标和视口测量", async () => {
  let boxReads = 0;
  let viewportReads = 0;
  const locator = {
    async count() {
      return 1;
    },
    nth() {
      return locator;
    },
    async getAttribute() {
      return null;
    },
    async isVisible() {
      return false;
    },
    async isEnabled() {
      return false;
    },
    async boundingBox() {
      boxReads += 1;
      return null;
    },
  };
  const page = {
    locator() {
      return locator;
    },
    viewportSize() {
      viewportReads += 1;
      return { width: 1280, height: 720 };
    },
    url() {
      return "https://example.com/candidates";
    },
  };

  await assert.rejects(
    new LocatorPrimitive().resolve(
      page,
      {
        target: { selectors: [{ type: "css", value: ".closing-button" }] },
        state: "enabled",
        timeout_ms: 0,
        description: "正在关闭的按钮",
      },
      {
        trace_id: "hidden-fast-path",
        action: "element.click",
        step: "find",
        require_unique: true,
      },
    ),
  );
  assert.equal(boxReads, 0);
  assert.equal(viewportReads, 0);
});
