/** 本文件验证 Boss 候选人滚轮锚点严格遵守页面上下安全边距。 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  bossAdaptiveWheelDistance,
  bossCandidateVerticalGap,
  bossScrollAttemptBudget,
  bossWheelAnchorMoveDecision,
  bossWheelAnchorSafety,
} from "./boss-scroll-anchor.js";

/**
 * anchorView 构造滚轮锚点安全判断所需的卡片和视口数据。
 * @param {number} y - 卡片顶部纵坐标。
 * @param {number} height - 卡片高度。
 * @returns {Record<string, any>} 模拟的候选人卡片可视状态。
 */
function anchorView(y, height = 172) {
  return {
    visible: true,
    box: { x: 209, y, width: 1184, height },
    viewport: { width: 1440, height: 900 },
  };
}

/** 验证只露出底部区域的候选人卡片不能作为滚轮锚点。 */
test("wheel anchor rejects card below the 80px safe boundary", () => {
  const result = bossWheelAnchorSafety(anchorView(870), 80);

  assert.equal(result.safe, false);
  assert.equal(result.reason, "below-safe-area");
  assert.equal(result.safe_bottom, 820);
  assert.equal(result.card_bottom, 1042);
});

/** 验证完整位于上下安全区域内的候选人卡片可以作为滚轮锚点。 */
test("wheel anchor accepts card fully inside the 80px safe boundary", () => {
  const result = bossWheelAnchorSafety(anchorView(502), 80);

  assert.equal(result.safe, true);
  assert.equal(result.reason, "safe");
  assert.equal(result.safe_top, 80);
  assert.equal(result.safe_bottom, 820);
});

/** 验证即使未配置额外边距，也不能选择超出浏览器视口的卡片。 */
test("wheel anchor rejects card outside viewport when margin is zero", () => {
  const result = bossWheelAnchorSafety(anchorView(870), 0);

  assert.equal(result.safe, false);
  assert.equal(result.reason, "below-safe-area");
  assert.equal(result.safe_bottom, 900);
});

/** 验证横向未满足通用完整显示条件时，不会误伤纵向安全的滚轮锚点。 */
test("wheel anchor move accepts vertically safe card from real worker log", () => {
  const view = {
    ...anchorView(474),
    in_viewport: false,
    fully_visible: false,
    vertically_fully_visible: true,
    horizontally_visible: true,
    margin: 80,
  };
  const decision = bossWheelAnchorMoveDecision(view, 80);

  assert.equal(decision.allowed, true);
  assert.equal(decision.safety.reason, "safe");
  assert.equal(decision.safety.card_bottom, 646);
});

/** 验证远在页面上方的候选人会使用更大的向上滚轮步长。 */
test("adaptive wheel distance accelerates a far candidate above viewport", () => {
  const view = {
    ...anchorView(-4777),
    margin: 80,
  };

  assert.equal(bossCandidateVerticalGap(view), -4857);
  assert.equal(bossAdaptiveWheelDistance(view, 120), -600);
});

/** 验证接近安全区域时恢复基础步长，降低来回越界风险。 */
test("adaptive wheel distance keeps base step near safe area", () => {
  const view = {
    ...anchorView(20),
    margin: 80,
  };

  assert.equal(bossCandidateVerticalGap(view), -60);
  assert.equal(bossAdaptiveWheelDistance(view, 120), -120);
});

/** 验证真实日志中距离页面上方 4777px 的第一张卡片能在预算内滚到安全区域。 */
test("adaptive wheel distance converges for the real 4777px gap", () => {
  let y = -4777;
  let steps = 0;
  while (steps < 18) {
    const view = {
      ...anchorView(y),
      margin: 80,
    };
    if (bossCandidateVerticalGap(view) === 0) break;
    const distance = bossAdaptiveWheelDistance(view, 120);
    y -= distance;
    steps += 1;
  }

  assert.equal(bossCandidateVerticalGap({ ...anchorView(y), margin: 80 }), 0);
  assert.ok(steps < 18);
});

/** 验证超远目标会按初始距离扩大重试预算。 */
test("scroll attempt budget expands for very distant candidate", () => {
  const view = {
    ...anchorView(-20000),
    margin: 80,
  };

  assert.equal(bossScrollAttemptBudget(view, 18, 600), 38);
});
