/** 文件作用说明：验证固定平台详情策略、旧OCR草稿与教程缓存兼容，避免重新展示已停用选择。 */
import assert from "node:assert/strict";
import test from "node:test";
import { positionDetailMode } from "./position-detail-mode.ts";
import { onboardingPositionDraftKey, readOnboardingPositionDraft } from "./onboarding-position-draft.ts";
import { createOnboardingState, onboardingStorageKey, readOnboardingState } from "./onboarding-state.ts";
import { positionUsesAI } from "./position-start-guard.ts";

test("旧ocr/ai/dom草稿按平台固定兼容，BOSS关键词仍需要AI详情", () => {
  for (const platform of ["boss", "liepin", "hliepin", "zhaopin", "51job"]) {
    const want = platform === "boss" ? "ai" : "dom";
    assert.equal(positionDetailMode(platform), want);
    for (const legacy of ["ocr", "ai", "dom"]) {
      const draft = { name: "旧岗位", platform_id: platform, mode_default: "keyword", detail_mode: legacy, position_requirement: "", keywords: "销售" };
      const raw = JSON.stringify({ version: 1, draft });
      const storage = { getItem: key => key === onboardingPositionDraftKey("test") ? raw : null };
      assert.equal(readOnboardingPositionDraft("test", storage)?.detail_mode, want);
      assert.equal(draft.detail_mode, legacy);
      assert.equal(positionUsesAI({ platform_id: platform, common_config: { mode_default: "keyword", detail_mode: legacy } }), true);
    }
  }
});

test("停在旧详情选择步骤的教程恢复时直接进入本平台真正需要的字段", () => {
  for (const platform of ["boss", "liepin"]) {
    const state = { ...createOnboardingState(), platformId: platform, status: "active", step: "position-detail-mode", filterMode: "keyword", detailMode: "ocr", completedFields: ["platform", "name", "filter-mode"] };
    const storage = { getItem: key => key === onboardingStorageKey("test") ? JSON.stringify(state) : null };
    const restored = readOnboardingState("test", storage);
    assert.equal(restored.detailMode, platform === "boss" ? "ai" : "dom");
    assert.equal(restored.step, "position-requirement");
  }
});
