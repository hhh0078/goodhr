/** 本文件验证新手引导的账号缓存边界、真实进度、预计耗时和筛选模式的必填字段。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  ONBOARDING_VERSION,
  ONBOARDING_SKIP_PHRASE,
  ONBOARDING_STEPS,
  createOnboardingState,
  onboardingStorageKey,
  readOnboardingState,
  saveOnboardingState,
  onboardingStage,
  onboardingDoneCount,
  onboardingRemainingTime,
  needsPositionRequirement,
  nextOnboardingFormStep,
} from "./onboarding-state.ts";

/** memoryStorage 创建独立内存缓存，验证时不读写用户真实浏览器数据。 */
function memoryStorage() {
  const values = new Map();
  return {
    /** getItem 模拟浏览器读取当前账号缓存。 */
    getItem(key) { return values.get(key) ?? null; },
    /** setItem 模拟浏览器保存当前账号缓存。 */
    setItem(key, value) { values.set(key, value); },
  };
}

/** activeState 返回独立的引导状态，测试之间不共享数组或完成事实。 */
function activeState(overrides = {}) {
  return { ...createOnboardingState(), status: "active", ...overrides };
}

test("新用户只有邀请，尚未开始或跑通任何业务步骤", () => {
  const state = createOnboardingState();
  assert.equal(state.version, ONBOARDING_VERSION);
  assert.equal(state.status, "invited");
  assert.equal(state.step, "agent-menu");
  assert.equal(onboardingDoneCount(state), 0);
  assert.equal(ONBOARDING_SKIP_PHRASE, "让开，我不需要教程");
});

test("同一个账号的大小写和首尾空格归一，不同账号的进度互相隔离", () => {
  const storage = memoryStorage();
  const first = activeState({ step: "position-name", platformId: "hliepin", environmentReady: true });
  assert.equal(saveOnboardingState(" HR@Example.COM ", first, storage), true);
  assert.equal(onboardingStorageKey(" HR@Example.COM "), onboardingStorageKey("hr@example.com"));
  const restored = readOnboardingState("hr@example.com", storage);
  assert.equal(restored.step, "position-name");
  assert.equal(restored.platformId, "hliepin");
  assert.equal(restored.environmentReady, true);
  assert.equal(readOnboardingState("another@example.com", storage).status, "invited");
  assert.notEqual(onboardingStorageKey("hr@example.com"), onboardingStorageKey("another@example.com"));
});

test("没有账号或账号仅包含空格时不继承也不保存进度", () => {
  const storage = memoryStorage();
  storage.setItem(onboardingStorageKey(""), JSON.stringify(activeState({ step: "complete", status: "completed" })));
  for (const identity of ["", " ", "\t\n"]) {
    assert.equal(saveOnboardingState(identity, activeState(), storage), false, `${JSON.stringify(identity)} 不应写入空账号缓存`);
    assert.equal(readOnboardingState(identity, storage).status, "invited", "空账号不应读出其他会话的完成状态");
  }
});

test("损坏 JSON、非对象和旧版本缓存都回到安全的首次邀请", () => {
  const storage = memoryStorage();
  const identity = "broken@example.com";
  for (const raw of ["{", "null", "[]", "42", '"active"', JSON.stringify({ ...activeState(), version: ONBOARDING_VERSION - 1 })]) {
    storage.setItem(onboardingStorageKey(identity), raw);
    const state = readOnboardingState(identity, storage);
    assert.equal(state.status, "invited", raw);
    assert.equal(state.step, "agent-menu", raw);
    assert.equal(onboardingDoneCount(state), 0, raw);
  }
});

test("新版本保存不会复用旧版本，读写存储被拒绝时仍能继续当前会话", () => {
  const storage = memoryStorage();
  const identity = "storage@example.com";
  const state = activeState({ version: ONBOARDING_VERSION - 1, step: "platform-open" });
  assert.equal(saveOnboardingState(identity, state, storage), true);
  const raw = JSON.parse(storage.getItem(onboardingStorageKey(identity)));
  assert.equal(raw.version, ONBOARDING_VERSION);
  assert.equal(readOnboardingState(identity, storage).step, "platform-open");
  const denied = {
    /** getItem 模拟浏览器隐私或权限设置拒绝读取。 */
    getItem() { throw new Error("Access denied"); },
    /** setItem 模拟容量或权限导致写入失败。 */
    setItem() { throw new Error("Quota exceeded"); },
  };
  assert.doesNotThrow(() => readOnboardingState(identity, denied));
  assert.equal(readOnboardingState(identity, denied).status, "invited");
  assert.equal(saveOnboardingState(identity, state, denied), false);
  assert.equal(state.status, "active", "缓存错误不应改坏当前会话状态");
});

test("缓存里的异常字段不会被当成真实连接、登录或岗位事实", () => {
  const storage = memoryStorage();
  const identity = "invalid-fields@example.com";
  storage.setItem(onboardingStorageKey(identity), JSON.stringify({
    ...activeState(), status: "bad-status", step: "bad-step", platformId: "unknown",
    environmentReady: "true", platformLoggedIn: 1, positionStarted: [], positionId: 100,
    completedFields: ["name", 1, null, "x".repeat(81)], celebrated: {}, updatedAt: "bad-date",
  }));
  const state = readOnboardingState(identity, storage);
  assert.equal(state.status, "invited");
  assert.equal(state.step, "agent-menu");
  assert.equal(state.platformId, "boss");
  assert.equal(state.positionId, "");
  assert.equal(state.environmentReady, false);
  assert.equal(state.platformLoggedIn, false);
  assert.equal(state.positionStarted, false);
  assert.deepEqual(state.completedFields, ["name"]);
  assert.deepEqual(state.celebrated, []);
  assert.equal(Number.isFinite(state.updatedAt), true);
  assert.equal(onboardingDoneCount(state), 0);
});

test("损坏的识别方式和只有空格的岗位 ID 不应产生错误必填项或岗位里程碑", () => {
  const storage = memoryStorage();
  const identity = "bad-mode@example.com";
  storage.setItem(onboardingStorageKey(identity), JSON.stringify(activeState({ detailMode: "broken-mode", positionId: " \t " })));
  const state = readOnboardingState(identity, storage);
  assert.equal(state.detailMode, "ai");
  assert.equal(state.positionId, "");
  assert.equal(onboardingDoneCount(state), 0);
});

test("跳过和暂停保留各自状态，不冒充完成，也不会凭最后一个界面步骤涨进度", () => {
  const storage = memoryStorage();
  for (const status of ["paused", "declined"]) {
    const state = activeState({ status, step: "complete", celebrated: ["complete"] });
    assert.equal(saveOnboardingState(`${status}@example.com`, state, storage), true);
    const restored = readOnboardingState(`${status}@example.com`, storage);
    assert.equal(restored.status, status);
    assert.equal(onboardingDoneCount(restored), 0);
    assert.notEqual(onboardingRemainingTime(restored), "已跑通");
  }
});

test("所有教学点击和填写字段都只改变位置，总进度只计算真实里程碑", () => {
  for (const step of ONBOARDING_STEPS) {
    const state = activeState({ step, completedFields: ["name", "filter-mode", "detail-mode", "position-requirement", "keywords"] });
    assert.equal(onboardingDoneCount(state), 0, step);
    assert.equal(onboardingRemainingTime(state), "约 8–18 分钟", step);
  }
  const facts = [
    { environmentReady: true },
    { environmentReady: true, platformLoggedIn: true },
    { environmentReady: true, platformLoggedIn: true, positionId: "real-position" },
    { environmentReady: true, platformLoggedIn: true, positionId: "real-position", positionStarted: true },
    { environmentReady: true, platformLoggedIn: true, positionId: "real-position", positionStarted: true, status: "completed" },
  ];
  facts.forEach((fact, index) => assert.equal(onboardingDoneCount(activeState(fact)), index + 1));
});

test("已扫描、正在 AI 请求、初筛分数或 AI 错误均不等于首次结果完成", () => {
  const running = activeState({ environmentReady: true, platformLoggedIn: true, positionId: "real-position", positionStarted: true, step: "first-result" });
  for (const analysis of [
    undefined,
    { kind: "ai", phase: "loading", terminal: false },
    { kind: "ai", phase: "result", terminal: false, score: 60 },
    { kind: "ai", phase: "error", terminal: true },
  ]) {
    const observed = { ...running, scanned_count: 10, analysis };
    assert.equal(onboardingDoneCount(observed), 4);
    assert.equal(onboardingRemainingTime(observed), "约 1–3 分钟");
    assert.notEqual(observed.status, "completed");
  }
});

test("AI 基础或 AI 详情任一启用就需要岗位要求，纯关键词本地识别无需 AI 要求", () => {
  for (const detailMode of ["ai", "ocr", "dom"]) {
    assert.equal(needsPositionRequirement({ filterMode: "ai", detailMode }), true);
  }
  assert.equal(needsPositionRequirement({ filterMode: "keyword", detailMode: "ai" }), true);
  assert.equal(needsPositionRequirement({ filterMode: "keyword", detailMode: "ocr" }), false);
  assert.equal(needsPositionRequirement({ filterMode: "keyword", detailMode: "dom" }), false);
});

test("关键词加 AI 详情必须同时有岗位要求和关键词，切换方式后重新找缺项", () => {
  const base = activeState({ filterMode: "keyword", detailMode: "ai", completedFields: ["name", "filter-mode", "detail-mode"] });
  assert.equal(nextOnboardingFormStep(base), "position-requirement");
  assert.equal(nextOnboardingFormStep({ ...base, completedFields: [...base.completedFields, "keywords"] }), "position-requirement");
  assert.equal(nextOnboardingFormStep({ ...base, completedFields: [...base.completedFields, "position-requirement"] }), "position-keywords");
  assert.equal(nextOnboardingFormStep({ ...base, completedFields: [...base.completedFields, "position-requirement", "keywords"] }), "position-save");
  assert.equal(nextOnboardingFormStep({ ...base, detailMode: "ocr" }), "position-keywords");
  assert.equal(nextOnboardingFormStep({ ...base, filterMode: "ai", detailMode: "ocr", completedFields: [...base.completedFields, "position-requirement"] }), "position-save");
});

test("名称、基础模式和详情模式尚未确认时优先补这三项，AI/OCR 各走正确字段", () => {
  let state = activeState({ filterMode: "ai", detailMode: "ocr" });
  assert.equal(nextOnboardingFormStep(state), "position-name");
  state = { ...state, completedFields: ["name"] };
  assert.equal(nextOnboardingFormStep(state), "position-filter-mode");
  state = { ...state, completedFields: [...state.completedFields, "filter-mode"] };
  assert.equal(nextOnboardingFormStep(state), "position-detail-mode");
  state = { ...state, completedFields: [...state.completedFields, "detail-mode"] };
  assert.equal(nextOnboardingFormStep(state), "position-requirement");
  const keyword = { ...state, filterMode: "keyword" };
  assert.equal(nextOnboardingFormStep(keyword), "position-keywords");
  assert.equal(nextOnboardingFormStep({ ...keyword, completedFields: [...keyword.completedFields, "keywords"] }), "position-save");
  assert.equal(onboardingDoneCount(state), 0, "填写完字段还没有真实创建岗位");
});

test("字段教学归入创建岗位，平台筛选归入实际启动，总共只有五个大阶段", () => {
  assert.equal(onboardingStage("runtime-install"), 0);
  assert.equal(onboardingStage("platform-login"), 1);
  for (const fieldStep of ["position-name", "position-filter-mode", "position-detail-mode", "position-requirement", "position-keywords", "position-save"]) {
    assert.equal(onboardingStage(fieldStep), 2, fieldStep);
  }
  assert.equal(onboardingStage("platform-filter"), 3);
  assert.equal(onboardingStage("position-run"), 3);
  assert.equal(onboardingStage("first-result"), 4);
});

test("预计耗时随着真实完成事实递减，全部跑通后不再显示等待时间", () => {
  const state = activeState();
  assert.equal(onboardingRemainingTime(state), "约 8–18 分钟");
  state.environmentReady = true;
  assert.equal(onboardingRemainingTime(state), "约 5–10 分钟");
  state.platformLoggedIn = true;
  assert.equal(onboardingRemainingTime(state), "约 4–8 分钟");
  state.positionId = "real-position";
  assert.equal(onboardingRemainingTime(state), "约 2–5 分钟");
  state.positionStarted = true;
  assert.equal(onboardingRemainingTime(state), "约 1–3 分钟");
  state.status = "completed";
  assert.equal(onboardingRemainingTime(state), "已跑通");
});
