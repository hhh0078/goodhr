/** 本文件验证新手引导的账号缓存边界、真实进度、预计耗时和筛选模式的必填字段。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  ONBOARDING_VERSION,
  ONBOARDING_SKIP_PHRASE,
  ONBOARDING_STEPS,
  ONBOARDING_STAGES,
  createOnboardingState,
  restartOnboardingState,
  onboardingStorageKey,
  readOnboardingState,
  saveOnboardingState,
  onboardingStage,
  onboardingDoneCount,
  onboardingRemainingTime,
  needsPositionRequirement,
  nextOnboardingFormStep,
  normalizeOnboardingPosition,
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
  const state = activeState({ version: ONBOARDING_VERSION - 1, step: "platform-open", positionId: "real-position" });
  assert.equal(saveOnboardingState(identity, state, storage), true);
  const raw = JSON.parse(storage.getItem(onboardingStorageKey(identity)));
  assert.equal(raw.version, ONBOARDING_VERSION);
  assert.equal(readOnboardingState(identity, storage).step, "position-start");
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
    const state = activeState({ step, completedFields: ["platform", "name", "filter-mode", "detail-mode", "position-requirement", "keywords"] });
    assert.equal(onboardingDoneCount(state), 0, step);
    assert.equal(onboardingRemainingTime(state), "约 8–18 分钟", step);
  }
  const facts = [
    { environmentReady: true },
    { environmentReady: true, positionId: "real-position" },
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
  const base = activeState({ filterMode: "keyword", detailMode: "ai", completedFields: ["platform", "name", "filter-mode", "detail-mode"] });
  assert.equal(nextOnboardingFormStep(base), "position-requirement");
  assert.equal(nextOnboardingFormStep({ ...base, completedFields: [...base.completedFields, "keywords"] }), "position-requirement");
  assert.equal(nextOnboardingFormStep({ ...base, completedFields: [...base.completedFields, "position-requirement"] }), "position-keywords");
  assert.equal(nextOnboardingFormStep({ ...base, completedFields: [...base.completedFields, "position-requirement", "keywords"] }), "position-save");
  assert.equal(nextOnboardingFormStep({ ...base, detailMode: "ocr" }), "position-keywords");
  assert.equal(nextOnboardingFormStep({ ...base, filterMode: "ai", detailMode: "ocr", completedFields: [...base.completedFields, "position-requirement"] }), "position-save");
});

test("表单先确认平台，再确认名称和两种模式，AI/OCR 各走正确字段", () => {
  let state = activeState({ filterMode: "ai", detailMode: "ocr" });
  assert.equal(nextOnboardingFormStep(state), "position-platform");
  state = { ...state, completedFields: ["platform"] };
  assert.equal(nextOnboardingFormStep(state), "position-name");
  state = { ...state, completedFields: ["platform", "name"] };
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

test("岗位开始入口与弹框开窗登录归入平台准备，确认运行才进入筛选并开始阶段", () => {
  assert.deepEqual(ONBOARDING_STAGES, ["准备本地程序", "创建岗位", "登录招聘平台", "筛选并开始", "看到筛选结果"]);
  assert.equal(onboardingStage("runtime-install"), 0);
  for (const preparing of ["position-start", "platform-filter", "platform-login"]) assert.equal(onboardingStage(preparing), 2, preparing);
  for (const fieldStep of ["position-menu", "position-create", "position-edit", "position-platform", "position-name", "position-filter-mode", "position-detail-mode", "position-requirement", "position-keywords", "position-save"]) {
    assert.equal(onboardingStage(fieldStep), 1, fieldStep);
  }
  for (const starting of ["position-run", "position-start-check", "position-start-error", "position-start-update"]) assert.equal(onboardingStage(starting), 3, starting);
  assert.equal(onboardingStage("first-result"), 4);
});

test("预计耗时随着真实完成事实递减，全部跑通后不再显示等待时间", () => {
  const state = activeState();
  assert.equal(onboardingRemainingTime(state), "约 8–18 分钟");
  state.environmentReady = true;
  assert.equal(onboardingRemainingTime(state), "约 5–10 分钟");
  state.positionId = "real-position";
  assert.equal(onboardingRemainingTime(state), "约 3–7 分钟");
  state.platformLoggedIn = true;
  assert.equal(onboardingRemainingTime(state), "约 2–5 分钟");
  state.positionStarted = true;
  assert.equal(onboardingRemainingTime(state), "约 1–3 分钟");
  state.status = "completed";
  assert.equal(onboardingRemainingTime(state), "已跑通");
});

test("旧v2平台及开始弹框缓存没有岗位时回创建，保持账号缓存键并撤销旧登录标记", () => {
  const storage = memoryStorage();
  for (const step of ["platform-select", "platform-open", "platform-login", "platform-filter", "position-run", "position-start-check", "position-start-error", "position-start-update"]) {
    const identity = `${step}@example.invalid`;
    const state = activeState({ step, platformLoggedIn: true, environmentReady: true, status: "paused" });
    assert.equal(saveOnboardingState(identity, state, storage), true);
    assert.match(onboardingStorageKey(identity), /^goodhr_onboarding_v2_/);
    const restored = readOnboardingState(identity, storage);
    assert.equal(restored.status, "paused");
    assert.equal(restored.step, "position-menu");
    assert.equal(restored.positionId, "");
    assert.equal(restored.platformLoggedIn, false);
    assert.equal(restored.environmentReady, true);
  }
});

test("恢复已有岗位时使用服务器平台，缓存平台变更须撤销旧登录并回该岗位开始", () => {
  const state = activeState({ step: "position-start", positionId: "real-position", platformId: "boss", platformLoggedIn: true, completedFields: ["platform", "name"] });
  const restored = normalizeOnboardingPosition(state, { id: "real-position", platform_id: "zhaopin", is_open: true });
  assert.equal(restored.positionId, "real-position");
  assert.equal(restored.platformId, "zhaopin");
  assert.equal(restored.platformLoggedIn, false);
  assert.equal(restored.step, "position-start");
  assert.equal(state.platformId, "boss", "归一过程不能改写传入的缓存对象");
});

test("真实岗位仍在且平台一致时保留登录和运行中结果等待，缺失岗位则清理关联回创建", () => {
  const running = activeState({ step: "first-result", positionId: "real-position", platformId: "boss", platformLoggedIn: true, positionStarted: true });
  const same = normalizeOnboardingPosition(running, { id: "real-position", platform_id: "boss", is_open: true });
  assert.equal(same.step, "first-result");
  assert.equal(same.platformLoggedIn, true);
  assert.equal(same.positionStarted, true);
  const missing = normalizeOnboardingPosition(running, null);
  assert.equal(missing.step, "position-menu");
  assert.equal(missing.positionId, "");
  assert.equal(missing.platformLoggedIn, false);
  assert.equal(missing.positionStarted, false);
});

test("实际岗位平台为空或未知时不能沿用缓存平台打开浏览器", () => {
  const state = activeState({ step: "platform-login", positionId: "real-position", platformId: "boss", platformLoggedIn: true });
  for (const platformID of ["", "unknown-platform"]) {
    const restored = normalizeOnboardingPosition(state, { id: "real-position", platform_id: platformID, is_open: true });
    assert.equal(restored.platformId, "", platformID);
    assert.equal(restored.platformLoggedIn, false, platformID);
    assert.equal(restored.step, "position-create", platformID);
    assert.equal(state.platformId, "boss");
  }
});

test("重新学习保留同一个已保存岗位，撤销本轮登录启动与字段确认并重新邀请", () => {
  const previous = activeState({ status: "completed", step: "complete", positionId: "saved-position", platformId: "zhaopin", environmentReady: true,
    platformLoggedIn: true, positionStarted: true, filterMode: "keyword", detailMode: "ocr", completionKind: "keyword",
    completedFields: ["platform", "name", "filter-mode", "detail-mode", "keywords"], celebrated: ["environment", "position", "platform", "started", "complete"] });
  const snapshot = structuredClone(previous);
  const restarted = restartOnboardingState(previous, true);
  assert.equal(restarted.positionId, "saved-position", "重新学习不能改成新建第二份岗位");
  assert.equal(restarted.platformId, "zhaopin");
  assert.equal(restarted.filterMode, "keyword");
  assert.equal(restarted.detailMode, "ocr");
  assert.equal(restarted.environmentReady, true);
  assert.equal(restarted.status, "invited");
  assert.equal(restarted.step, "position-edit");
  assert.equal(restarted.reviewPosition, true);
  assert.equal(restarted.replaying, true);
  assert.equal(restarted.platformLoggedIn, false);
  assert.equal(restarted.positionStarted, false);
  assert.equal(restarted.completionKind, undefined);
  assert.deepEqual(restarted.completedFields, []);
  assert.deepEqual(restarted.celebrated, []);
  assert.deepEqual(previous, snapshot, "微步骤重置不应改写原状态或真实业务事实");
});

test("重新学习依据当前组件就绪事实选择准备入口，没有原岗位时才回创建", () => {
  for (const positionId of ["", "saved-position"]) {
    for (const ready of [false, true]) {
      const previous = activeState({ positionId, environmentReady: !ready, platformLoggedIn: true });
      const restarted = restartOnboardingState(previous, ready);
      assert.equal(restarted.positionId, positionId);
      assert.equal(restarted.environmentReady, ready, "不能沿用上一轮的组件就绪缓存");
      assert.equal(restarted.step, !ready ? "agent-menu" : positionId ? "position-edit" : "position-menu");
      assert.equal(restarted.reviewPosition, Boolean(positionId));
      assert.equal(restarted.replaying, true);
      assert.equal(restarted.status, "invited");
    }
  }
});

test("复核和重学标记按账号保存，刷新恢复邀请或暂停后仍复核原岗位", () => {
  const storage = memoryStorage();
  for (const status of ["invited", "paused", "active"]) {
    const identity = `replay-${status}@example.invalid`;
    const state = { ...restartOnboardingState(activeState({ positionId: "saved-position", platformId: "liepin" }), true), status };
    assert.equal(saveOnboardingState(identity, state, storage), true);
    const restored = readOnboardingState(identity, storage);
    assert.equal(restored.positionId, "saved-position");
    assert.equal(restored.platformId, "liepin");
    assert.equal(restored.step, "position-edit");
    assert.equal(restored.status, status);
    assert.equal(restored.reviewPosition, true);
    assert.equal(restored.replaying, true);
    assert.equal(restored.platformLoggedIn, false);
    assert.equal(restored.positionStarted, false);
  }
  const another = readOnboardingState("another-replay@example.invalid", storage);
  assert.equal(another.reviewPosition, false);
  assert.equal(another.replaying, false);
  assert.equal(another.positionId, "");
});

test("同岗位复核的读取不会绕过字段确认跳回浏览器，即使实际平台已经调整", () => {
  for (const step of ["position-edit", "position-platform", "position-name", "position-save", "position-menu"]) {
    const reviewing = activeState({ step, positionId: "saved-position", platformId: "boss", reviewPosition: true, replaying: true, platformLoggedIn: true, positionStarted: true });
    for (const platformId of ["boss", "zhaopin"]) {
      const normalized = normalizeOnboardingPosition(reviewing, { id: "saved-position", platform_id: platformId, is_open: true });
      assert.equal(normalized.step, step, `${step} / ${platformId} 不应跳到打开浏览器`);
      assert.equal(normalized.positionId, "saved-position");
      assert.equal(normalized.platformId, platformId);
      assert.equal(normalized.reviewPosition, true);
      assert.equal(normalized.replaying, true);
      assert.equal(normalized.platformLoggedIn, false);
      assert.equal(normalized.positionStarted, false);
    }
  }
});

test("待复核岗位不算本轮岗位和登录已确认，预计时间保留岗位复核与登录步骤", () => {
  const reviewing = activeState({ environmentReady: true, positionId: "saved-position", reviewPosition: true, replaying: true, platformLoggedIn: true });
  assert.equal(onboardingDoneCount(reviewing), 1);
  assert.equal(onboardingRemainingTime(reviewing), "约 5–10 分钟");
  const confirmed = { ...reviewing, reviewPosition: false, platformLoggedIn: false };
  assert.equal(onboardingDoneCount(confirmed), 2);
  assert.equal(onboardingRemainingTime(confirmed), "约 3–7 分钟");
  assert.equal(onboardingDoneCount({ ...confirmed, platformLoggedIn: true }), 3);
});

test("复核的实际岗位丢失关闭或ID不符时解除关联与复核，避免编辑错误岗位", () => {
  const reviewing = activeState({ step: "position-edit", positionId: "saved-position", reviewPosition: true, replaying: true, platformLoggedIn: true, positionStarted: true, completedFields: ["platform", "name"] });
  for (const position of [null, { id: "saved-position", platform_id: "boss", is_open: false }, { id: "another-position", platform_id: "boss", is_open: true }]) {
    const normalized = normalizeOnboardingPosition(reviewing, position);
    assert.equal(normalized.positionId, "");
    assert.equal(normalized.reviewPosition, false);
    assert.equal(normalized.replaying, true);
    assert.equal(normalized.step, "position-menu");
    assert.equal(normalized.platformLoggedIn, false);
    assert.equal(normalized.positionStarted, false);
    assert.deepEqual(normalized.completedFields, []);
  }
  const noPosition = normalizeOnboardingPosition({ ...reviewing, positionId: "" }, null);
  assert.equal(noPosition.reviewPosition, false);
  assert.equal(noPosition.step, "position-menu");
});

test("旧v2缓存缺少复核重学字段仍保持原岗位流程，不误触发重新学习", () => {
  const storage = memoryStorage();
  const identity = "legacy-replay@example.invalid";
  const { reviewPosition, replaying, ...legacy } = activeState({ step: "position-start", positionId: "saved-position", platformId: "hliepin", environmentReady: true, platformLoggedIn: true });
  storage.setItem(onboardingStorageKey(identity), JSON.stringify(legacy));
  const restored = readOnboardingState(identity, storage);
  assert.equal(restored.positionId, "saved-position");
  assert.equal(restored.step, "position-start");
  assert.equal(restored.platformLoggedIn, true);
  assert.equal(restored.reviewPosition, false);
  assert.equal(restored.replaying, false);
  assert.equal(onboardingDoneCount(restored), 3);
});

test("无岗位和非布尔复核标记不会凭缓存产生需要编辑的业务岗位", () => {
  const storage = memoryStorage();
  const identity = "invalid-review@example.invalid";
  for (const overrides of [{ positionId: "", reviewPosition: true, replaying: true }, { positionId: "saved-position", reviewPosition: "true", replaying: 1 }]) {
    storage.setItem(onboardingStorageKey(identity), JSON.stringify(activeState(overrides)));
    const restored = readOnboardingState(identity, storage);
    assert.equal(restored.reviewPosition, false);
    assert.equal(restored.replaying, overrides.replaying === true);
  }
});

test("开始弹框不持久化，旧开窗登录和筛选缓存刷新后回同一个岗位开始", () => {
  const storage = memoryStorage();
  for (const step of ["platform-select", "platform-open", "platform-login", "platform-filter", "position-run", "position-start-check", "position-start-error", "position-start-update"]) {
    for (const status of ["active", "paused"]) {
      const identity = `${step}-${status}@example.invalid`;
      const saved = activeState({ step, status, positionId: "saved-position", platformId: "zhaopin", environmentReady: true,
        platformLoggedIn: false, completedFields: ["platform", "name"], filterMode: "keyword", detailMode: "ocr" });
      assert.equal(saveOnboardingState(identity, saved, storage), true);
      const restored = readOnboardingState(identity, storage);
      assert.equal(restored.step, "position-start", `${step} 刷新后没有开始弹框，必须先回岗位开始入口`);
      assert.equal(restored.positionId, "saved-position", "恢复不能新建或改用其他岗位");
      assert.equal(restored.platformId, "zhaopin");
      assert.equal(restored.status, status);
      assert.equal(restored.environmentReady, true);
      assert.equal(restored.platformLoggedIn, false);
      assert.equal(restored.positionStarted, false);
      assert.equal(restored.filterMode, "keyword");
      assert.equal(restored.detailMode, "ocr");
      assert.deepEqual(restored.completedFields, ["platform", "name"]);
      assert.equal(saved.step, step, "缓存迁移不修改当前会话对象");
    }
  }
});

test("实际岗位平台变化时撤销旧平台登录，在开始入口重新准备而不是自动开窗", () => {
  const actual = { id: "saved-position", platform_id: "liepin", is_open: true, common_config: { mode_default: "keyword", detail_mode: "ocr" } };
  for (const step of ["position-start", "platform-open", "platform-login", "platform-filter", "position-run"]) {
    const previous = activeState({ step, positionId: "saved-position", platformId: "boss", platformLoggedIn: true, filterMode: "ai", detailMode: "ai" });
    const normalized = normalizeOnboardingPosition(previous, actual);
    assert.equal(normalized.positionId, "saved-position");
    assert.equal(normalized.platformId, "liepin");
    assert.equal(normalized.platformLoggedIn, false);
    assert.equal(normalized.positionStarted, false);
    assert.equal(normalized.step, "position-start");
    assert.equal(normalized.filterMode, "keyword");
    assert.equal(normalized.detailMode, "ocr");
    assert.equal(previous.platformId, "boss");
  }
});

test("旧平台选择和顶部开窗微步骤归到岗位开始，同会话真实登录等待仍保留", () => {
  const actual = { id: "saved-position", platform_id: "boss", is_open: true };
  for (const step of ["platform-select", "platform-open"]) {
    const normalized = normalizeOnboardingPosition(activeState({ step, positionId: "saved-position", platformId: "boss" }), actual);
    assert.equal(normalized.step, "position-start");
    assert.equal(normalized.positionId, "saved-position");
    assert.equal(normalized.platformLoggedIn, false);
  }
  const waiting = activeState({ step: "platform-login", positionId: "saved-position", platformId: "boss", platformLoggedIn: false });
  const normalized = normalizeOnboardingPosition(waiting, actual);
  assert.equal(normalized.step, "platform-login", "开始弹框仍打开时，岗位读取不能打断本轮登录检测");
  assert.equal(normalized.positionId, "saved-position");
  assert.equal(normalized.platformLoggedIn, false);
  assert.equal(onboardingDoneCount(normalized), 1, "未登录不能因为处于等待界面上涨登录进度");
});

test("正常开始与结果等待刷新不退回旧开窗教程，已完成事实继续保留", () => {
  const storage = memoryStorage();
  for (const state of [
    activeState({ step: "position-start", positionId: "saved-position", platformLoggedIn: true, environmentReady: true }),
    activeState({ step: "first-result", positionId: "saved-position", platformLoggedIn: true, positionStarted: true, environmentReady: true }),
    activeState({ step: "complete", status: "completed", positionId: "saved-position", platformLoggedIn: true, positionStarted: true, environmentReady: true, completionKind: "ai" }),
  ]) {
    const identity = `${state.step}@example.invalid`;
    assert.equal(saveOnboardingState(identity, state, storage), true);
    const restored = readOnboardingState(identity, storage);
    const normalized = normalizeOnboardingPosition(restored, { id: "saved-position", platform_id: "boss", is_open: true });
    assert.equal(normalized.step, state.step);
    assert.equal(normalized.status, state.status);
    assert.equal(normalized.positionStarted, state.positionStarted);
    assert.equal(normalized.positionId, "saved-position");
    assert.equal(normalized.platformLoggedIn, true);
    assert.equal(normalized.completionKind, state.completionKind);
    assert.equal(onboardingDoneCount(normalized), onboardingDoneCount(state));
  }
});

test("旧弹框缓存若已要求原岗位复核，刷新后回编辑而不能直接进入开始", () => {
  const storage = memoryStorage();
  for (const step of ["platform-open", "platform-login", "platform-filter", "position-run", "position-start-error"]) {
    const identity = `review-${step}@example.invalid`;
    const state = activeState({ step, positionId: "saved-position", platformId: "boss", reviewPosition: true, replaying: true, completedFields: [] });
    assert.equal(saveOnboardingState(identity, state, storage), true);
    const restored = readOnboardingState(identity, storage);
    const normalized = normalizeOnboardingPosition(restored, { id: "saved-position", platform_id: "zhaopin", is_open: true });
    assert.equal(normalized.step, "position-edit");
    assert.equal(normalized.positionId, "saved-position");
    assert.equal(normalized.platformId, "zhaopin");
    assert.equal(normalized.reviewPosition, true);
    assert.equal(normalized.replaying, true);
    assert.equal(normalized.platformLoggedIn, false);
    assert.equal(normalized.positionStarted, false);
    assert.deepEqual(normalized.completedFields, []);
  }
});
