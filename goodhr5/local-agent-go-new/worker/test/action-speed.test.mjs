// 文件作用说明：验证公共点击、鼠标移动和分词输入不会叠加不必要的拟人等待。

import assert from "node:assert/strict";
import test from "node:test";

import { ClickAction } from "../dist/browser/actions/click.js";
import { InputAction } from "../dist/browser/actions/input.js";
import { MoveAction } from "../dist/browser/actions/move.js";
import { WorkerError } from "../dist/errors/worker-error.js";

const context = {
  trace_id: "action-speed",
  action: "element.test",
  started_at: Date.now(),
};

/** 创建公共动作测试使用的可见元素。 */
function visibleElement(page, locator, x = 100) {
  const view = {
    box: { x, y: 100, width: 100, height: 40 },
    viewport: { width: 1400, height: 900 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: true,
  };
  return {
    result: {
      element_ref: "element-1",
      description: "测试元素",
      matched_selector: { type: "css", value: ".target" },
      attempts: [],
      view,
      page_id: "page-1",
      page_url: "https://example.com",
    },
    resolved: {
      page,
      locator,
      matched_selector: { type: "css", value: ".target" },
      attempts: [],
      view,
    },
  };
}

/** 创建不输出内容的测试日志器。 */
function silentLogger() {
  return {
    info() {},
    failure() {},
  };
}

test("鼠标移动只传目标坐标，不再记录会被 CloakBrowser 忽略的步数", async () => {
  const page = {};
  const moves = [];
  const action = new MoveAction(
    {
      async move(_page, x, y, ...options) {
        moves.push({ x, y, options });
      },
    },
    silentLogger(),
  );
  const originalRandom = Math.random;
  Math.random = () => 0.5;
  try {
    await action.toElement(visibleElement(page, {}, 1100).resolved, context);
    await action.toElement(visibleElement(page, {}, 1110).resolved, context);
  } finally {
    Math.random = originalRandom;
  }
  assert.equal(moves.length, 2);
  assert.deepEqual(moves.map((item) => item.options), [[], []]);
});

test("点击稳定检查只读取边界且隐藏验证使用即时探测", async () => {
  const page = {};
  const locator = {};
  const found = visibleElement(page, locator);
  let verifyTimeout;
  let receivedWheelAnchor;
  let cloakBrowserClicks = 0;
  const action = new ClickAction(
    {
      async one(selector) {
        if (selector.description === "已关闭弹层") {
          verifyTimeout = selector.timeout_ms;
          throw new WorkerError({
            code: "ELEMENT_NOT_FOUND",
            message: "弹层已经关闭",
            action: context.action,
            step: "find",
            trace_id: context.trace_id,
          });
        }
        return found;
      },
    },
    {
      async ensureVisible(_found, request) {
        receivedWheelAnchor = request.wheel_anchor;
      },
    },
    {
      async toElement() {
        return { x: 150, y: 120 };
      },
    },
    {
      async box() {
        return found.resolved.view.box;
      },
    },
    {
      async click() {
        cloakBrowserClicks += 1;
        return 180;
      },
      async down() {},
      async up() {},
    },
    silentLogger(),
  );

  const result = await action.execute(
    {
      selector: {
        target: { selectors: [{ type: "css", value: ".button" }] },
        description: "关闭按钮",
      },
      wheel_anchor: {
        target: { selectors: [{ type: "css", value: ".scroll-area" }] },
        description: "滚动区域",
      },
      verify: {
        target_hidden: {
          target: { selectors: [{ type: "css", value: ".panel" }] },
          timeout_ms: 5_000,
          description: "已关闭弹层",
        },
      },
    },
    context,
  );

  assert.equal(result.verified, true);
  assert.equal(result.click_mode, "cloakbrowser");
  assert.equal(result.hold_ms, 0);
  assert.equal(cloakBrowserClicks, 1);
  assert.equal(verifyTimeout, 0);
  assert.equal(receivedWheelAnchor.description, "滚动区域");
});

test("九字招呼语的字符和词语拟人等待不超过两秒", async () => {
  const page = {};
  let actual = "";
  const locator = {
    async inputValue() {
      return actual;
    },
  };
  const found = visibleElement(page, locator);
  const action = new InputAction(
    { async one() { return found; } },
    { async ensureVisible() {} },
    {
      async toElement() {
        return { x: 150, y: 120 };
      },
    },
    {
      async click() {
        return 180;
      },
    },
    {
      async press() {},
      async typeText(_page, text) {
        actual += text;
      },
      async insertText(_page, text) {
        actual += text;
      },
    },
    silentLogger(),
  );
  const originalRandom = Math.random;
  Math.random = () => 1;
  const startedAt = Date.now();
  try {
    await action.execute(
      {
        selector: {
          target: { selectors: [{ type: "css", value: ".input" }] },
          description: "聊天输入框",
        },
        text: "你好 能发个简历吗",
        clear: false,
      },
      context,
    );
  } finally {
    Math.random = originalRandom;
  }

  assert.equal(actual, "你好 能发个简历吗");
  assert.ok(Date.now() - startedAt < 2_000);
});
