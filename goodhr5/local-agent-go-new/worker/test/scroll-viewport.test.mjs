// 文件作用说明：验证公共滚动能处理视口外目标、整页滚轮锚点和 Retina 截图尺寸。

import assert from "node:assert/strict";
import test from "node:test";

import { ScrollAction } from "../dist/browser/actions/scroll.js";
import { ViewportPrimitive } from "../dist/browser/primitives/viewport.js";

const context = {
  trace_id: "scroll-viewport",
  action: "element.click",
  started_at: Date.now(),
};

/** 创建只记录公共滚动调用的最小测试对象。 */
function createScrollAction(overrides = {}) {
  const calls = {
    moveToElement: 0,
    moveToViewportCenter: 0,
    wheel: 0,
  };
  const action = new ScrollAction(
    overrides.session ?? {
      async requirePage() {
        return {
          viewportSize() {
            return { width: 1280, height: 720 };
          },
        };
      },
    },
    overrides.find ?? {},
    {
      async toElement() {
        calls.moveToElement += 1;
      },
      async toViewportCenter() {
        calls.moveToViewportCenter += 1;
      },
    },
    overrides.locator ?? {},
    {
      async wheel() {
        calls.wheel += 1;
      },
    },
    {
      info() {},
      failure() {},
    },
  );
  return { action, calls };
}

/** 创建公共滚动测试使用的元素结果。 */
function foundElement(view) {
  const page = {
    viewportSize() {
      return view.viewport;
    },
  };
  return {
    result: {
      element_ref: "element-1",
      description: "测试目标",
      matched_selector: { type: "css", value: ".target" },
      attempts: [],
      view,
      page_id: "page-1",
      page_url: "https://example.com",
    },
    resolved: {
      page,
      locator: {},
      matched_selector: { type: "css", value: ".target" },
      attempts: [],
      view,
    },
  };
}

test("目标完全位于视口外时先把鼠标移到视口中心再滚动", async () => {
  const target = foundElement({
    box: { x: 20, y: 900, width: 200, height: 80 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: false,
    fully_in_viewport: false,
  });
  const { action, calls } = createScrollAction({
    locator: {
      async view() {
        return {
          ...target.resolved.view,
          box: { ...target.resolved.view.box, y: 600 },
          in_viewport: true,
          fully_in_viewport: true,
        };
      },
    },
  });

  await action.ensureVisible(
    target,
    { distance: 200, max_attempts: 2, require_full: true },
    context,
  );

  assert.equal(calls.moveToViewportCenter, 1);
  assert.equal(calls.moveToElement, 0);
  assert.equal(calls.wheel, 1);
});

test("执行滚动时视口外锚点也会回退到窗口中心", async () => {
  const target = foundElement({
    box: { x: 100, y: 900, width: 600, height: 100 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: false,
    fully_in_viewport: false,
  });
  const body = foundElement({
    box: { x: 0, y: -1920, width: 1280, height: 720 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: false,
    fully_in_viewport: false,
  });
  const { action, calls } = createScrollAction({
    find: {
      async one(selector) {
        return selector.description === "整页" ? body : target;
      },
    },
    locator: {
      async view(_page, locator) {
        if (locator === body.resolved.locator) {
          return body.resolved.view;
        }
        return {
          ...target.resolved.view,
          box: { ...target.resolved.view.box, y: 500 },
          in_viewport: true,
          fully_in_viewport: true,
        };
      },
    },
  });

  await action.execute(
    {
      target: {
        target: { selectors: [{ type: "css", value: ".target" }] },
        description: "目标",
      },
      wheel_anchor: {
        target: { selectors: [{ type: "css", value: "body" }] },
        description: "整页",
      },
      distance: 500,
      max_attempts: 1,
      require_full: true,
    },
    context,
  );

  assert.equal(calls.moveToViewportCenter, 1);
  assert.equal(calls.moveToElement, 0);
  assert.equal(calls.wheel, 1);
});

test("大于视口的整页锚点不限制候选人可见区域", async () => {
  const target = foundElement({
    box: { x: 100, y: 520, width: 600, height: 129 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: true,
  });
  const body = foundElement({
    box: { x: 0, y: -1981, width: 1280, height: 2000 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: false,
  });
  const { action, calls } = createScrollAction({
    find: {
      async one() {
        return body;
      },
    },
  });

  await action.ensureVisible(
    target,
    {
      wheel_anchor: {
        target: { selectors: [{ type: "css", value: "body" }] },
        description: "整页",
      },
      require_full: true,
    },
    context,
  );

  assert.equal(calls.wheel, 0);
  assert.equal(calls.moveToElement, 0);
});

test("目标在浏览器视口内但被内层容器裁剪时先真实滚轮再点击", async () => {
  const target = foundElement({
    box: { x: 760, y: 180, width: 120, height: 32 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: true,
  });
  const container = foundElement({
    box: { x: 700, y: 300, width: 500, height: 320 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: true,
  });
  const { action, calls } = createScrollAction({
    find: {
      async one() {
        return container;
      },
    },
    locator: {
      async view(_page, locator) {
        if (locator === container.resolved.locator) {
          return container.resolved.view;
        }
        return {
          ...target.resolved.view,
          box: { ...target.resolved.view.box, y: 340 },
        };
      },
    },
  });

  await action.ensureVisible(
    target,
    {
      wheel_anchor: {
        target: { selectors: [{ type: "css", value: ".chat-scroll" }] },
        description: "聊天历史滚动区域",
      },
      distance: 160,
      max_attempts: 2,
      require_full: true,
    },
    context,
  );

  assert.equal(calls.moveToElement, 1);
  assert.equal(calls.moveToViewportCenter, 0);
  assert.equal(calls.wheel, 1);
});

test("超高候选人卡片只需部分进入安全区域", async () => {
  const target = foundElement({
    box: { x: 100, y: 80, width: 600, height: 900 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: false,
  });
  const { action, calls } = createScrollAction();

  await action.ensureVisible(
    target,
    {
      distance: 180,
      max_attempts: 2,
      viewport_margin: 48,
      require_full: false,
    },
    context,
  );

  assert.equal(calls.wheel, 0);
  assert.equal(calls.moveToElement, 0);
  assert.equal(calls.moveToViewportCenter, 0);
});

test("无固定 viewport 时按 HTML 宽度换算 Retina 截图尺寸", async () => {
  const png = Buffer.alloc(24);
  png[1] = "P".charCodeAt(0);
  png[2] = "N".charCodeAt(0);
  png[3] = "G".charCodeAt(0);
  png.writeUInt32BE(2560, 16);
  png.writeUInt32BE(1440, 20);
  const page = {
    viewportSize() {
      return null;
    },
    async screenshot() {
      return png;
    },
    locator(selector) {
      assert.equal(selector, "html");
      return {
        async boundingBox() {
          return { x: 0, y: -3000, width: 1280, height: 6000 };
        },
      };
    },
  };

  const viewport = await new ViewportPrimitive().size(page);

  assert.deepEqual(viewport, { width: 1280, height: 720 });
});
