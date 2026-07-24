/** 本文件验证 Boss 候选人滚动失败时能够区分距离不足、滚轮未生效和方向来回切换。 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  buildBossCandidateScrollFailureDiagnostic,
  scrollStateDelta,
  scrollViewGap,
} from "./boss-scroll-diagnostic.js";

/** candidateView 构造测试所需的页面视口和候选人坐标。 */
function candidateView(y, height = 172) {
  return {
    visible: true,
    in_viewport: false,
    fully_visible: false,
    margin: 0,
    box: { x: 206, y, width: 1030, height },
    viewport: {
      width: 1180,
      height: 650,
      source: "window-inner",
      device_pixel_ratio: 2,
      visual_viewport_scale: 1,
    },
  };
}

/** scrollAttempt 构造一次带容器滚动状态的诊断轨迹。 */
function scrollAttempt(distance, beforeY, afterY, beforeTop, afterTop) {
  return {
    distance,
    before_view: candidateView(beforeY),
    after_view: candidateView(afterY),
    scroll_before: {
      target: "recommend-list",
      scroll_top: beforeTop,
      max_top: 9000,
    },
    scroll_after: {
      target: "recommend-list",
      scroll_top: afterTop,
      max_top: 9000,
    },
  };
}

/** 验证候选人位于视口上方时能够算出准确的剩余距离。 */
test("scrollViewGap calculates remaining vertical distance", () => {
  assert.equal(scrollViewGap(candidateView(-500)), 500);
  assert.equal(scrollViewGap(candidateView(400)), 0);
});

/** 验证只有同一个滚动容器才会计算实际滚动距离。 */
test("scrollStateDelta rejects different scroll containers", () => {
  assert.equal(
    scrollStateDelta(
      { target: "first", scroll_top: 100 },
      { target: "second", scroll_top: 220 },
    ),
    null,
  );
});

/** 验证目标持续接近但仍未到位时判断为重试总距离不足。 */
test("diagnostic identifies insufficient retry distance", () => {
  const diagnostic = buildBossCandidateScrollFailureDiagnostic({
    candidate_name: "测试候选人",
    requested_card_index: 0,
    final_card_index: 0,
    card_count: 45,
    viewport: {
      width: 1180,
      height: 650,
      source: "window-inner",
      device_pixel_ratio: 2,
      visual_viewport_scale: 1,
    },
    container: { usable: true, selector: ".recommend-list", reason: "ok" },
    outer_attempts: 18,
    attempts: [
      scrollAttempt(-120, -4000, -3880, 6000, 5880),
      scrollAttempt(-120, -3880, -3760, 5880, 5760),
    ],
  });

  assert.equal(diagnostic.diagnosis_code, "retry-distance-insufficient");
  assert.equal(diagnostic.actual_total_px, 240);
  assert.match(diagnostic.message, /DOM卡片数=45/);
  assert.match(diagnostic.message, /视口=1180x650/);
  assert.match(diagnostic.message, /视口来源=window-inner/);
  assert.match(diagnostic.message, /DPR=2/);
  assert.match(diagnostic.message, /逐次坐标请查看 browser-worker\.log/);
});

/** 验证滚轮指令发出但容器和卡片都不动时判断为滚轮未生效。 */
test("diagnostic identifies ineffective wheel input", () => {
  const diagnostic = buildBossCandidateScrollFailureDiagnostic({
    candidate_name: "测试候选人",
    requested_card_index: 2,
    final_card_index: 2,
    card_count: 15,
    viewport: { width: 1440, height: 900 },
    container: { usable: false, reason: "no-usable-container" },
    outer_attempts: 3,
    attempts: [
      scrollAttempt(120, 1200, 1200, 0, 0),
      scrollAttempt(120, 1200, 1200, 0, 0),
      scrollAttempt(120, 1200, 1200, 0, 0),
    ],
  });

  assert.equal(diagnostic.diagnosis_code, "wheel-not-effective");
  assert.equal(diagnostic.ineffective_attempts, 3);
  assert.match(diagnostic.message, /无效滚动=3次/);
});

/** 验证上下方向反复改变时判断为可视边界来回越界。 */
test("diagnostic identifies direction oscillation", () => {
  const diagnostic = buildBossCandidateScrollFailureDiagnostic({
    candidate_name: "测试候选人",
    requested_card_index: 1,
    final_card_index: 1,
    card_count: 15,
    viewport: { width: 1440, height: 900 },
    container: { usable: true, selector: ".recommend-list", reason: "ok" },
    outer_attempts: 3,
    attempts: [
      scrollAttempt(-120, -20, 100, 500, 380),
      scrollAttempt(120, 100, -20, 380, 500),
      scrollAttempt(-120, -20, 100, 500, 380),
    ],
  });

  assert.equal(diagnostic.diagnosis_code, "direction-oscillation");
  assert.equal(diagnostic.direction_changes, 2);
});
