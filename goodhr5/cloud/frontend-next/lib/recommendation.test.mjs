/** 文件作用说明：验证自动回复报告只有明确公开授权时才展示可访问的分享状态。 */
import test from "node:test";
import assert from "node:assert/strict";
import { normalizeRecommendation } from "./recommendation.ts";

test("结构化报告缺少公开授权时保持未公开，明确授权后才允许分享", () => {
  assert.equal(normalizeRecommendation({ public_id: "synthetic-report" }).shareEnabled, false);
  assert.equal(normalizeRecommendation({ public_id: "synthetic-report", share_enabled: true }).shareEnabled, true);
  assert.equal(normalizeRecommendation({ public_id: "synthetic-report", share_enabled: false }).shareEnabled, false);
});
