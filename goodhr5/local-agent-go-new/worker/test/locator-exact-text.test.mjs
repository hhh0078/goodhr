// 文件作用说明：验证 CSS 选择器的精确文本约束不会把数字页码 2 误匹配成 12。

import assert from "node:assert/strict";
import test from "node:test";

import { LocatorPrimitive } from "../dist/browser/primitives/locator.js";

/** 验证 CSS 精确文本会转换为首尾锚定的文本规则。 */
test("CSS 精确文本只匹配完整页码", async () => {
  let capturedText;
  const locator = {
    filter(options) {
      capturedText = options.hasText;
      return locator;
    },
    async count() {
      return 1;
    },
    nth() {
      return locator;
    },
    async getAttribute() {
      return null;
    },
    async boundingBox() {
      return { x: 10, y: 10, width: 30, height: 30 };
    },
    async isVisible() {
      return true;
    },
    async isEnabled() {
      return true;
    },
  };
  const page = {
    locator() {
      return locator;
    },
    viewportSize() {
      return { width: 1280, height: 720 };
    },
    url() {
      return "https://example.com/candidates";
    },
  };

  await new LocatorPrimitive().resolve(
    page,
    {
      target: {
        selectors: [{ type: "css", value: ".page-number" }],
        text: "2",
        exact_text: true,
      },
      description: "第 2 页",
    },
    {
      trace_id: "exact-page-number",
      action: "element.find",
      step: "find",
      require_unique: false,
    },
  );

  assert.ok(capturedText instanceof RegExp);
  assert.equal(capturedText.test("2"), true);
  assert.equal(capturedText.test(" 2 "), true);
  assert.equal(capturedText.test("12"), false);
  assert.equal(capturedText.test("20"), false);
});
