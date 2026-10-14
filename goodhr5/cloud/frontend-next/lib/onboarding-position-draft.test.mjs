/** 本文件验证新手岗位草稿只保留核心字段、隔离账号且缓存异常不会影响真实操作。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  onboardingPositionDraftKey,
  readOnboardingPositionDraft,
  saveOnboardingPositionDraft,
  clearOnboardingPositionDraft,
} from "./onboarding-position-draft.ts";

/** storageFixture 使用独立内存模拟浏览器缓存，不访问用户的真实草稿。 */
function storageFixture() {
  const values = new Map();
  return {
    /** getItem 返回指定账号的模拟缓存。 */
    getItem(key) { return values.get(key) ?? null; },
    /** setItem 写入当前测试的模拟缓存。 */
    setItem(key, value) { values.set(key, value); },
    /** removeItem 仅移除指定账号的模拟缓存。 */
    removeItem(key) { values.delete(key); },
  };
}

/** draftFixture 创建包含部分未填字段的核心草稿，恢复不替用户伪造输入。 */
function draftFixture(overrides = {}) {
  return { name: "Java 开发", platform_id: "boss", mode_default: "keyword", detail_mode: "ocr", position_requirement: "", keywords: "Java, Spring", ...overrides };
}

test("只持久化六项核心字段，完整岗位设置和账号数据被排除", () => {
  const storage = storageFixture();
  const draft = draftFixture();
  assert.equal(saveOnboardingPositionDraft("user-1", { ...draft, token: "must-not-cache", account: { email: "private" }, match_limit: 50, filter_prompt: "private" }, storage), true);
  const raw = JSON.parse(storage.getItem(onboardingPositionDraftKey("user-1")));
  assert.deepEqual(raw.draft, draft);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage), draft);
  assert.equal(JSON.stringify(raw).includes("must-not-cache"), false);
});

test("账号大小写和空格归一，不同账号隔离，创建成功清理只影响当前账号", () => {
  const storage = storageFixture();
  saveOnboardingPositionDraft(" HR@example.COM ", draftFixture(), storage);
  saveOnboardingPositionDraft("other@example.com", draftFixture({ name: "产品经理" }), storage);
  assert.equal(readOnboardingPositionDraft("hr@example.com", storage).name, "Java 开发");
  assert.equal(readOnboardingPositionDraft("other@example.com", storage).name, "产品经理");
  assert.equal(readOnboardingPositionDraft("third@example.com", storage), null);
  assert.equal(clearOnboardingPositionDraft("hr@example.com", storage), true);
  assert.equal(readOnboardingPositionDraft("hr@example.com", storage), null);
  assert.equal(readOnboardingPositionDraft("other@example.com", storage).name, "产品经理");
});

test("空账号、损坏缓存、未知版本和无效模式均不恢复为新岗位", () => {
  const storage = storageFixture();
  for (const identity of ["", " \t "]) {
    assert.equal(saveOnboardingPositionDraft(identity, draftFixture(), storage), false);
    assert.equal(readOnboardingPositionDraft(identity, storage), null);
    assert.equal(clearOnboardingPositionDraft(identity, storage), false);
  }
  for (const raw of ["{", "null", "[]", JSON.stringify({ version: 0, draft: draftFixture() }), JSON.stringify({ version: 1, draft: draftFixture({ mode_default: "invalid" }) }), JSON.stringify({ version: 1, draft: draftFixture({ detail_mode: "invalid" }) }), JSON.stringify({ version: 1, draft: draftFixture({ platform_id: "invalid" }) }), JSON.stringify({ version: 1, draft: draftFixture({ name: null }) })]) {
    storage.setItem(onboardingPositionDraftKey("user-1"), raw);
    assert.equal(readOnboardingPositionDraft("user-1", storage), null, raw);
  }
});

test("半填草稿保留真实空值，AI 和关键词内容不会相互替代", () => {
  const storage = storageFixture();
  const draft = draftFixture({ name: "", detail_mode: "ai", keywords: "Java", position_requirement: "" });
  saveOnboardingPositionDraft("user-1", draft, storage);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage), draft);
});

test("浏览器拒绝读写和清理时安全返回，不阻断创建和关闭弹框", () => {
  const denied = {
    /** getItem 模拟隐私设置阻止读取。 */
    getItem() { throw new Error("denied"); },
    /** setItem 模拟缓存容量已满。 */
    setItem() { throw new Error("quota"); },
    /** removeItem 模拟浏览器阻止清理。 */
    removeItem() { throw new Error("denied"); },
  };
  assert.equal(readOnboardingPositionDraft("user-1", denied), null);
  assert.equal(saveOnboardingPositionDraft("user-1", draftFixture(), denied), false);
  assert.equal(clearOnboardingPositionDraft("user-1", denied), false);
});
