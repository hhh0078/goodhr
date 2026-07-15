/** 本文件验证所有招聘平台详情长图共用的滚动等待节奏。 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  detailScrollWaits,
  effectiveDetailWheelDistance,
} from "./detail-scroll.js";

test("全平台详情长图默认使用缩短后的滚动节奏", () => {
  assert.deepEqual(detailScrollWaits(), {
    captureWaitMs: 250,
    initialCaptureWaitMs: 250,
  });
});

test("平台仍可按需覆盖详情长图滚动节奏", () => {
  assert.deepEqual(
    detailScrollWaits({
      detail_capture_wait_ms: 360,
    }),
    {
      captureWaitMs: 360,
      initialCaptureWaitMs: 360,
    },
  );
});

test("详情已在底部时自动改为向上滚动", () => {
  assert.equal(
    effectiveDetailWheelDistance(260, {
      can_scroll_down: false,
      can_scroll_up: true,
    }),
    -260,
  );
});

test("详情已在顶部时自动改为向下滚动", () => {
  assert.equal(
    effectiveDetailWheelDistance(-180, {
      can_scroll_down: true,
      can_scroll_up: false,
    }),
    180,
  );
});
