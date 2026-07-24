/** 本文件验证猎聘列表项滚动诊断能够识别横向边距误判和上下方向反复切换。 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  buildListClickScrollFailureDiagnostic,
  directionSwitchCount,
  listClickViewDecision,
} from "./list-click-scroll-diagnostic.js";

/** listClickView 构造列表点击诊断所需的目标坐标和视口状态。 */
function listClickView(y, options = {}) {
  const x = Number(options.x ?? 0);
  const width = Number(options.width ?? 1200);
  const margin = Number(options.margin ?? 12);
  const viewport = { width: 1280, height: 900 };
  const height = 155;
  const verticallyFullyVisible = y >= margin && y + height <= viewport.height - margin;
  const horizontalFull = x >= margin && x + width <= viewport.width - margin;
  const horizontallyVisible = x + width > 0 && x < viewport.width;
  const verticalOnly = Boolean(options.vertical_only);
  return {
    visible: true,
    in_viewport:
      verticallyFullyVisible &&
      (verticalOnly ? horizontallyVisible : horizontalFull),
    partially_visible: true,
    fully_visible: verticallyFullyVisible && horizontalFull,
    vertically_visible: y + height > margin && y < viewport.height - margin,
    vertically_fully_visible: verticallyFullyVisible,
    horizontally_visible: horizontallyVisible,
    vertical_only: verticalOnly,
    require_full: true,
    margin,
    box: { x, y, width, height },
    viewport,
  };
}

/** listClickAttempt 构造一次猎聘目标上下切换的滚轮轨迹。 */
function listClickAttempt(distance, beforeY, afterY, beforeTop, afterTop) {
  return {
    distance,
    mouse: { wheel_target: "configured" },
    before_view: listClickView(beforeY),
    after_view: listClickView(afterY),
    scroll_before: {
      target: "document",
      scroll_top: beforeTop,
      max_top: 4642,
    },
    scroll_after: {
      target: "document",
      scroll_top: afterTop,
      max_top: 4642,
    },
  };
}

/** 验证目标纵向完整可见但横向未满足安全边距时能指出唯一失败维度。 */
test("listClickViewDecision identifies horizontal margin blocking", () => {
  const decision = listClickViewDecision(listClickView(511));

  assert.equal(decision.vertical_ready, true);
  assert.equal(decision.horizontal_ready, false);
  assert.deepEqual(decision.failed_dimensions, ["horizontal-margin"]);
  assert.equal(decision.horizontal_overflow.left, 12);
});

/** 验证猎聘仅判断纵向时，横向贴边但仍与视口重叠的目标能够直接通过。 */
test("listClickViewDecision accepts edge-aligned target in vertical-only mode", () => {
  const decision = listClickViewDecision(
    listClickView(139, { vertical_only: true }),
  );

  assert.equal(decision.accepted, true);
  assert.equal(decision.vertical_ready, true);
  assert.equal(decision.horizontal_ready, true);
  assert.deepEqual(decision.failed_dimensions, []);
});

/** 验证连续上下滚动能够统计准确的方向切换次数。 */
test("directionSwitchCount counts alternating wheel directions", () => {
  const attempts = [
    listClickAttempt(-560, -49, 511, 560, 0),
    listClickAttempt(560, 511, -49, 0, 560),
    listClickAttempt(-560, -49, 511, 560, 0),
  ];

  assert.equal(directionSwitchCount(attempts), 2);
});

/** 验证实际猎聘坐标模式优先诊断为横向安全边距误判并输出完整错误摘要。 */
test("diagnostic explains hliepin horizontal margin oscillation", () => {
  const attempts = Array.from({ length: 12 }, (_, index) => {
    const up = index % 2 === 0;
    return listClickAttempt(
      up ? -560 : 560,
      up ? -49 : 511,
      up ? 511 : -49,
      up ? 560 : 0,
      up ? 0 : 560,
    );
  });
  const diagnostic = buildListClickScrollFailureDiagnostic({
    action_id: "list-click-test",
    platform: "hliepin",
    platform_name: "猎聘",
    action: "读取候选人详情",
    candidate_name: "王**",
    index: 0,
    locator_count: 20,
    item_selector: "tbody tr",
    click_selector: "a",
    viewport: { width: 1280, height: 900 },
    margin: 12,
    require_full: true,
    vertical_only: false,
    attempts,
    final_view: listClickView(-49),
  });

  assert.equal(diagnostic.diagnosis_code, "horizontal-margin-blocked");
  assert.equal(diagnostic.direction_changes, 11);
  assert.ok(diagnostic.repeated_position_hits > 0);
  assert.match(diagnostic.message, /目标垂直方向已经完整可见/);
  assert.match(diagnostic.message, /安全边距=12px/);
  assert.match(diagnostic.message, /方向切换=11次/);
  assert.match(diagnostic.message, /逐轮坐标、失败维度和滚动对象请查看 browser-worker\.log/);
});
