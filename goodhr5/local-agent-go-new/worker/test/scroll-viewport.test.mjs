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
    deltas: [],
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
      async wheel(_page, x, y) {
        calls.wheel += 1;
        calls.deltas.push({ x, y });
        await overrides.wheel?.(x, y);
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
function foundElement(view, selector = ".target") {
  const page = {
    viewportSize() {
      return view.viewport;
    },
  };
  return {
    result: {
      element_ref: "element-1",
      description: "测试目标",
      matched_selector: { type: "css", value: selector },
      attempts: [],
      view,
      page_id: "page-1",
      page_url: "https://example.com",
    },
    resolved: {
      page,
      locator: {},
      matched_selector: { type: "css", value: selector },
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
  assert.ok(calls.wheel >= 1);
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
  }, "body");
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
  assert.ok(calls.wheel >= 1);
});

test("视口外且高度略小的 body 锚点仍按整页处理", async () => {
  const target = foundElement({
    box: { x: 100, y: 900, width: 600, height: 100 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: false,
    fully_in_viewport: false,
  });
  const body = foundElement({
    box: { x: 0, y: -1920, width: 1280, height: 719.5 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: false,
    fully_in_viewport: false,
  }, "body");
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
      viewport_margin: 48,
      require_full: true,
    },
    context,
  );

  assert.equal(calls.moveToViewportCenter, 1);
  assert.ok(calls.wheel >= 1);
});

test("真实列表容器继续限制候选人的安全可见区域", async () => {
  const target = foundElement({
    box: { x: 100, y: 520, width: 600, height: 100 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: true,
  });
  const list = foundElement({
    box: { x: 0, y: 100, width: 1280, height: 400 },
    viewport: { width: 1280, height: 720 },
    visible: true,
    enabled: true,
    in_viewport: true,
    fully_in_viewport: true,
  }, ".ant-table-body");
  let currentY = target.resolved.view.box.y;
  const { action, calls } = createScrollAction({
    find: { async one() { return list; } },
    locator: {
      async view(_page, locator) {
        if (locator === list.resolved.locator) {
          return list.resolved.view;
        }
        return {
          ...target.resolved.view,
          box: { ...target.resolved.view.box, y: currentY },
        };
      },
    },
    async wheel(_x, y) {
      currentY -= y;
    },
  });

  await action.ensureVisible(
    target,
    {
      wheel_anchor: {
        target: { selectors: [{ type: "css", value: ".ant-table-body" }] },
        description: "列表",
      },
      distance: 200,
      max_attempts: 2,
      require_full: true,
    },
    context,
  );

  assert.ok(calls.wheel >= 1);
  assert.ok(currentY + target.resolved.view.box.height <= 500);
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
  }, "body");
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

test("要求完整显示的超高候选人卡片自动保留安全可操作区域", async () => {
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
      require_full: true,
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

/** 创建能随真实滚轮参数移动的测试页面，用于验证方向与停止条件。 */
function movingTarget(box, width = 1006, height = 975) {
  const target = foundElement({
    box, viewport: { width, height }, visible: true, enabled: true,
    in_viewport: false, fully_in_viewport: false,
  });
  let current = { ...box };
  const { action, calls } = createScrollAction({
    find: { async one() { return target; } },
    locator: { async view() { return { ...target.resolved.view, box: { ...current } }; } },
    async wheel(x, y) { current = { ...current, x: current.x - x, y: current.y - y }; },
  });
  return { target, action, calls };
}

test("右侧按钮横向越界时仅向右滚动，不再反复上下滚动", async () => {
  const { target, action, calls } = movingTarget({ x: 1078, y: 400, width: 88, height: 32 });
  await action.ensureVisible(target, { distance: 160, max_attempts: 8, viewport_margin: 48, require_full: true }, context);
  assert.ok(calls.deltas.length > 0);
  assert.ok(calls.deltas.every(({ x, y }) => x > 0 && y === 0));
  assert.ok(target.resolved.view.box.x + 88 <= 958);
});

test("连续滚动更新当前位置并缩短最后一步，避免过冲后反向滚动", async () => {
  const { target, action, calls } = movingTarget({ x: 100, y: 1250, width: 200, height: 32 });
  await action.execute({ target: {}, distance: 160, max_attempts: 6, wait_ms: 50, viewport_margin: 48, require_full: true }, context);
  assert.ok(calls.deltas.every(({ x, y }) => x === 0 && y > 0 && y <= 160));
  assert.equal(calls.deltas.reduce((sum, { y }) => sum + y, 0), 355);
  assert.equal(target.resolved.view.box.y, 895);
});

test("超高卡片从上方进入窗口时按相交距离滚动，不用完整卡片高度来回校正", async () => {
  const { target, action, calls } = movingTarget({ x: 100, y: -1000, width: 200, height: 900 });
  await action.execute({ target: {}, distance: 180, max_attempts: 5, wait_ms: 50, viewport_margin: 48, require_full: false }, context);
  assert.ok(calls.wheel >= 1);
  assert.ok(calls.deltas.every(({ x, y }) => x === 0 && y < 0));
  assert.equal(calls.deltas.reduce((sum, { y }) => sum + y, 0), -156);
  assert.ok(target.resolved.view.box.y + 900 >= 48 + 8);
});

test("步数耗尽而目标未到位必须报错，不能假装滚动完成后继续点击", async () => {
  const { action } = movingTarget({ x: 100, y: 2000, width: 200, height: 32 });
  await assert.rejects(
    action.execute({ target: {}, distance: 100, max_attempts: 1, wait_ms: 50, require_full: true }, context),
    error => error.code === "SCROLL_FAILED" && error.message.includes("次数"),
  );
});
