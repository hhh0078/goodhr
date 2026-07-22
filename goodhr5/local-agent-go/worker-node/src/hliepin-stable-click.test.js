// 本文件测试猎聘父子唯一定位、位置稳定复核以及异常时禁止点击的行为。
import test from "node:test";
import assert from "node:assert/strict";

import {
  boxesApproximatelyEqual,
  createHLiepinStableClickAction,
  normalizeComparableText,
  pointInsideBox,
} from "./hliepin-stable-click.js";

/** createStableClickPage 创建只包含一个父级和一个目标的页面测试替身。 */
function createStableClickPage(box, targetText = "立即沟通") {
  const target = {
    count: async () => 1,
    first() { return this; },
    nth() { return this; },
    locator() { return this; },
    isVisible: async () => true,
    innerText: async () => targetText,
    boundingBox: async () => ({ ...box }),
    waitFor: async () => {},
  };
  const parent = {
    count: async () => 1,
    first() { return this; },
    isVisible: async () => true,
    locator: () => target,
  };
  return {
    page: {
      locator: () => parent,
      waitForTimeout: async () => {},
    },
    target,
  };
}

test("位置比较和落点判断使用允许误差", () => {
  assert.equal(boxesApproximatelyEqual({ x: 10, y: 20, width: 30, height: 40 }, { x: 11, y: 19, width: 31, height: 40 }, 2), true);
  assert.equal(pointInsideBox({ x: 25, y: 35 }, { x: 10, y: 20, width: 30, height: 40 }, 2), true);
  assert.equal(pointInsideBox({ x: 45, y: 35 }, { x: 10, y: 20, width: 30, height: 40 }, 2), false);
});

test("文字比较仅在显式开启时忽略猎聘按钮字间空白", () => {
  assert.equal(normalizeComparableText("确 定", false), "确 定");
  assert.equal(normalizeComparableText("确 定", true), "确定");
});

test("确认按钮启用空白归一化后仍按完整文字精确匹配", async () => {
  const { page } = createStableClickPage({ x: 100, y: 120, width: 80, height: 32 }, "确 定");
  let clickCount = 0;
  const stableClick = createHLiepinStableClickAction({
    ensurePage: async () => page,
    moveMouseToElement: async () => ({ x: 140, y: 136 }),
    humanMouseClick: async () => { clickCount += 1; return { clicked: true }; },
  });
  await stableClick({
    parent_selector: ".confirm-modal",
    target_selector: ".confirm-button",
    expected_text: "确定",
    exact_text: true,
    normalize_text_whitespace: true,
    stable_checks: 2,
  });
  assert.equal(clickCount, 1);
});

test("父级和目标唯一且位置稳定时只点击一次", async () => {
  const { page } = createStableClickPage({ x: 100, y: 120, width: 80, height: 32 });
  let clickCount = 0;
  const logs = [];
  const stableClick = createHLiepinStableClickAction({
    ensurePage: async () => page,
    moveMouseToElement: async () => ({ x: 140, y: 136 }),
    humanMouseClick: async () => { clickCount += 1; return { clicked: true }; },
    logWorker: (message, data) => logs.push({ message, data }),
  });
  const result = await stableClick({
    action_id: "test-action-1",
    parent_selector: ".parent",
    target_selector: ".target",
    expected_text: "立即沟通",
    exact_text: true,
    stable_checks: 2,
  });
  assert.equal(result.click_count, 1);
  assert.equal(clickCount, 1);
  assert.equal(result.action_id, "test-action-1");
  assert.equal(logs.filter((item) => item.data.stage === "physical-click-after").length, 1);
  assert.equal(logs.find((item) => item.data.stage === "physical-click-after")?.data.physical_click_count, 1);
});

test("鼠标移动后落点不在目标最新位置时取消点击", async () => {
  const { page } = createStableClickPage({ x: 300, y: 320, width: 80, height: 32 });
  let clickCount = 0;
  const stableClick = createHLiepinStableClickAction({
    ensurePage: async () => page,
    moveMouseToElement: async () => ({ x: 20, y: 20 }),
    humanMouseClick: async () => { clickCount += 1; return { clicked: true }; },
  });
  await assert.rejects(
    stableClick({ parent_selector: ".parent", target_selector: ".target", max_move_attempts: 2, stable_checks: 2 }),
    /发生位移/,
  );
  assert.equal(clickCount, 0);
});
