// 本文件负责验证 AI 浮层始终按当前候选人复用同一张卡片。

import assert from "node:assert/strict";
import test from "node:test";

import { aiOverlayMatchKey } from "./ai-overlay-policy.js";

test("同一候选人的分析中和分析完成状态复用同一张卡片", () => {
  assert.equal(aiOverlayMatchKey("AI 正在评分", "张女士"), "张女士");
  assert.equal(aiOverlayMatchKey("AI 评分完成", "张女士"), "张女士");
});

test("不同候选人使用不同浮层标识", () => {
  assert.notEqual(
    aiOverlayMatchKey("AI 正在评分", "张女士"),
    aiOverlayMatchKey("AI 正在评分", "李先生"),
  );
});
