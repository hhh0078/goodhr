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

test("岗位复核草稿按原岗位ID隔离，新建草稿、其他岗位和账号互不覆盖", () => {
  const storage = storageFixture();
  const newDraft = draftFixture({ name: "待新建岗位" });
  const editingA = draftFixture({ position_id: "岗位 A", name: "A 未保存修改" });
  const editingB = draftFixture({ position_id: "岗位 B", name: "B 未保存修改" });
  const otherAccount = draftFixture({ position_id: "岗位 A", name: "另一账号" });
  saveOnboardingPositionDraft("user-1", newDraft, storage);
  saveOnboardingPositionDraft("user-1", editingA, storage);
  saveOnboardingPositionDraft("user-1", editingB, storage);
  saveOnboardingPositionDraft("user-2", otherAccount, storage);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage), newDraft);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage, "岗位 A"), editingA);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage, "岗位 B"), editingB);
  assert.deepEqual(readOnboardingPositionDraft("user-2", storage, "岗位 A"), otherAccount);
  assert.equal(readOnboardingPositionDraft("user-1", storage, "不存在的岗位"), null);
  assert.equal(clearOnboardingPositionDraft("user-1", storage, "岗位 A"), true);
  assert.equal(readOnboardingPositionDraft("user-1", storage, "岗位 A"), null);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage), newDraft);
  assert.deepEqual(readOnboardingPositionDraft("user-1", storage, "岗位 B"), editingB);
  assert.deepEqual(readOnboardingPositionDraft("user-2", storage, "岗位 A"), otherAccount);
});

test("复核缓存必须同时匹配键与岗位元数据，错ID或新建缓存串入编辑均被拒绝", () => {
  const storage = storageFixture();
  /** write 构造键与岗位元数据不一致的缓存，以验证恢复不会串用其他岗位。 */
  const write = (keyID, metadataID) => storage.setItem(onboardingPositionDraftKey("user-1", keyID), JSON.stringify({ version: 1, draft: draftFixture({ position_id: metadataID }) }));
  write("A", "B");
  assert.equal(readOnboardingPositionDraft("user-1", storage, "A"), null);
  write("A", undefined);
  assert.equal(readOnboardingPositionDraft("user-1", storage, "A"), null);
  write("A", 123);
  assert.equal(readOnboardingPositionDraft("user-1", storage, "A"), null);
  write("", "A");
  assert.equal(readOnboardingPositionDraft("user-1", storage), null);
  assert.equal(saveOnboardingPositionDraft("user-1", draftFixture({ position_id: " A ", token: "must-not-cache", is_open: false, match_limit: 99 }), storage), true);
  const restored = readOnboardingPositionDraft("user-1", storage, " A ");
  assert.deepEqual(restored, draftFixture({ position_id: "A" }));
  assert.deepEqual(Object.keys(restored).sort(), ["position_id", "name", "platform_id", "mode_default", "detail_mode", "position_requirement", "keywords"].sort());
  assert.equal(storage.getItem(onboardingPositionDraftKey("user-1", "A")).includes("must-not-cache"), false);
});
