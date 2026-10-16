/** 本文件保存按账号隔离的新手引导位置，只记录界面进度，不替代云端的真实完成事实。 */
export const ONBOARDING_VERSION = 2;
export const ONBOARDING_SKIP_PHRASE = "让开，我不需要教程";
export const ONBOARDING_STEPS = [
  "agent-menu", "agent-download", "agent-connect", "runtime-install", "runtime-system", "platform-select", "platform-open", "platform-login",
  "position-menu", "position-create", "position-edit", "position-platform", "position-name", "position-filter-mode", "position-detail-mode", "position-requirement", "position-keywords", "position-save",
  "position-start", "platform-filter", "position-run", "position-start-check", "position-start-error", "position-start-update", "first-result", "complete",
] as const;
export type OnboardingStep = typeof ONBOARDING_STEPS[number];
export type OnboardingStatus = "invited" | "active" | "paused" | "declined" | "completed";
export type OnboardingState = {
  version: number;
  status: OnboardingStatus;
  step: OnboardingStep;
  platformId: string;
  positionId: string;
  environmentReady: boolean;
  platformLoggedIn: boolean;
  positionStarted: boolean;
  filterMode: "ai" | "keyword";
  detailMode: string;
  completedFields: string[];
  celebrated: string[];
  updatedAt: number;
  completionKind?: "ai" | "keyword";
  reviewPosition: boolean;
  replaying: boolean;
};
export const ONBOARDING_STAGES = ["准备本地程序", "创建岗位", "登录招聘平台", "筛选并开始", "看到筛选结果"];

/** createOnboardingState 为首次进入当前账号的用户创建尚未接受邀请的引导。 */
export function createOnboardingState(): OnboardingState {
  return { version: ONBOARDING_VERSION, status: "invited", step: "agent-menu", platformId: "boss", positionId: "", environmentReady: false, platformLoggedIn: false, positionStarted: false, filterMode: "ai", detailMode: "ai", completedFields: [], celebrated: [], reviewPosition: false, replaying: false, updatedAt: Date.now() };
}

/** restartOnboardingState 仅重置当前账号的教程微步骤，复核原岗位而不删除、复制或启停业务岗位。 */
export function restartOnboardingState(state: OnboardingState, environmentReady: boolean): OnboardingState {
  return { ...createOnboardingState(), platformId: state.platformId, positionId: state.positionId,
    filterMode: state.filterMode, detailMode: state.detailMode, environmentReady,
    reviewPosition: Boolean(state.positionId), replaying: true,
    step: environmentReady ? state.positionId ? "position-edit" : "position-menu" : "agent-menu" };
}

/** onboardingStorageKey 将版本和账号同时加入缓存键，换号时不会继承别人的进度。 */
export function onboardingStorageKey(identity: string): string {
  return `goodhr_onboarding_v${ONBOARDING_VERSION}_${encodeURIComponent(identity.trim().toLowerCase())}`;
}

/** readOnboardingState 安全读取当前账号的缓存，旧版本或损坏内容回到首次邀请。 */
export function readOnboardingState(identity: string, storage?: Pick<Storage, "getItem">): OnboardingState {
  const fallback = createOnboardingState();
  if (!identity.trim()) return fallback;
  try {
    const cache = storage || (typeof window !== "undefined" ? window.localStorage : undefined);
    const raw: unknown = JSON.parse(cache?.getItem(onboardingStorageKey(identity)) || "null");
    if (!isRecord(raw) || raw.version !== ONBOARDING_VERSION) return fallback;
    const statuses: OnboardingStatus[] = ["invited", "active", "paused", "declined", "completed"];
    const status = statuses.includes(raw.status as OnboardingStatus) ? raw.status as OnboardingStatus : "invited";
    const step = ONBOARDING_STEPS.includes(raw.step as OnboardingStep) ? raw.step as OnboardingStep : "agent-menu";
    const platformId = ["boss", "zhaopin", "liepin", "hliepin"].includes(String(raw.platformId)) ? String(raw.platformId) : "boss";
    const positionId = typeof raw.positionId === "string" ? raw.positionId.trim() : "";
    // 保留原账号缓存与岗位草稿；旧顺序还没建岗位时，先回岗位入口，不能继续要求登录。
    const normalizedStep = !positionId && ["platform-select", "platform-open", "platform-login"].includes(step) ? "position-menu" : step;
    return { ...fallback, status, step: normalizedStep, platformId, positionId, environmentReady: raw.environmentReady === true, platformLoggedIn: Boolean(positionId) && raw.platformLoggedIn === true, positionStarted: raw.positionStarted === true, filterMode: raw.filterMode === "keyword" ? "keyword" : "ai", detailMode: ["ai", "ocr", "dom"].includes(String(raw.detailMode)) ? String(raw.detailMode) : "ai", completedFields: stringList(raw.completedFields), celebrated: stringList(raw.celebrated), reviewPosition: Boolean(positionId) && raw.reviewPosition === true, replaying: raw.replaying === true, updatedAt: Number.isFinite(raw.updatedAt) ? Number(raw.updatedAt) : Date.now(), completionKind: raw.completionKind === "keyword" || raw.completionKind === "ai" ? raw.completionKind : undefined };
  } catch { return fallback; }
}

/** saveOnboardingState 缓存不可写时返回失败，由界面提示当前会话仍可继续。 */
export function saveOnboardingState(identity: string, state: OnboardingState, storage?: Pick<Storage, "setItem">): boolean {
  if (!identity.trim()) return false;
  try {
    const cache = storage || (typeof window !== "undefined" ? window.localStorage : undefined);
    if (!cache) return false;
    cache.setItem(onboardingStorageKey(identity), JSON.stringify({ ...state, version: ONBOARDING_VERSION, updatedAt: Date.now() }));
    return true;
  } catch { return false; }
}

/** onboardingStage 返回当前操作所属的大阶段，字段教学不会额外增加总步数。 */
export function onboardingStage(step: OnboardingStep): number {
  if (step.startsWith("agent-") || step.startsWith("runtime-")) return 0;
  if (step.startsWith("platform-") && step !== "platform-filter") return 2;
  if (step.startsWith("position-start") || ["platform-filter", "position-run"].includes(step)) return 3;
  if (step === "first-result" || step === "complete") return 4;
  return 1;
}

/** onboardingDoneCount 只根据已经发生的真实里程碑计算总进度。 */
export function onboardingDoneCount(state: OnboardingState): number {
  return [state.environmentReady, Boolean(state.positionId.trim()) && !state.reviewPosition, state.platformLoggedIn && Boolean(state.positionId.trim()) && !state.reviewPosition, state.positionStarted, state.status === "completed"].filter(Boolean).length;
}

/** onboardingRemainingTime 显示估计区间，网络安装与平台等待期间不制造虚假的倒计时。 */
export function onboardingRemainingTime(state: OnboardingState): string {
  if (state.status === "completed") return "已跑通";
  const estimates = [[3, 8], [2, 3], [1, 2], [1, 2], [1, 3]];
  const done = [state.environmentReady, Boolean(state.positionId.trim()) && !state.reviewPosition, state.platformLoggedIn && Boolean(state.positionId.trim()) && !state.reviewPosition, state.positionStarted, false];
  const remaining = estimates.reduce((sum, pair, index) => done[index] ? sum : [sum[0] + pair[0], sum[1] + pair[1]], [0, 0]);
  return `约 ${remaining[0]}–${remaining[1]} 分钟`;
}

/** needsPositionRequirement 判断基础或详情任一 AI 模式是否需要岗位要求。 */
export function needsPositionRequirement(state: Pick<OnboardingState, "filterMode" | "detailMode">): boolean {
  return state.filterMode === "ai" || state.detailMode === "ai";
}

/** nextOnboardingFormStep 在模式切换后重新选择真正缺少的字段，不把填写次数当业务完成。 */
export function nextOnboardingFormStep(state: OnboardingState): OnboardingStep {
  if (!state.completedFields.includes("platform")) return "position-platform";
  if (!state.completedFields.includes("name")) return "position-name";
  if (!state.completedFields.includes("filter-mode")) return "position-filter-mode";
  if (!state.completedFields.includes("detail-mode")) return "position-detail-mode";
  if (needsPositionRequirement(state) && !state.completedFields.includes("position-requirement")) return "position-requirement";
  if (state.filterMode === "keyword" && !state.completedFields.includes("keywords")) return "position-keywords";
  return "position-save";
}

export type OnboardingPositionSnapshot = { id?: string; platform_id?: string; is_open?: boolean; common_config?: { mode_default?: string; detail_mode?: string } };

/** normalizeOnboardingPosition 以实际保存的岗位确定平台，恢复时不沿用其他平台的登录事实或已失效岗位。 */
export function normalizeOnboardingPosition(state: OnboardingState, position: OnboardingPositionSnapshot | null): OnboardingState {
  if (!state.positionId) {
    const step = ["platform-select", "platform-open", "platform-login", "position-edit", "position-start", "platform-filter", "position-run"].includes(state.step) ? "position-menu" : state.step;
    return { ...state, reviewPosition: false, platformLoggedIn: false, step };
  }
  if (!position || position.id !== state.positionId || position.is_open === false) {
    return { ...state, positionId: "", reviewPosition: false, platformLoggedIn: false, positionStarted: false, completedFields: [], step: "position-menu", status: state.status === "completed" ? "active" : state.status, completionKind: undefined };
  }
  const platformId = typeof position.platform_id === "string" ? position.platform_id.trim() : "";
  // 实际岗位缺少有效平台时，不能回退到缓存平台打开错误网站；原岗位仍保留，由用户重新创建或修改。
  if (!["boss", "zhaopin", "liepin", "hliepin"].includes(platformId)) {
    return { ...state, platformId: "", platformLoggedIn: false, positionStarted: false, step: "position-create" };
  }
  const changed = platformId !== state.platformId;
  if (state.reviewPosition) return { ...state, platformId, platformLoggedIn: false, positionStarted: false };
  const platformLoggedIn = !changed && state.platformLoggedIn;
  const loginRequired = changed || state.step === "platform-select" || (!platformLoggedIn && ["position-start", "position-menu"].includes(state.step));
  return { ...state, platformId, platformLoggedIn,
    filterMode: position.common_config?.mode_default === "keyword" ? "keyword" : position.common_config?.mode_default === "ai" ? "ai" : state.filterMode,
    detailMode: position.common_config?.detail_mode || state.detailMode,
    step: loginRequired && !state.positionStarted && state.status !== "completed" ? "platform-open" : state.step };
}

/** stringList 只保留有限数量的短字符串，避免损坏缓存把异常值带入界面。 */
function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item.length <= 80).slice(0, 30) : [];
}

/** isRecord 判断缓存是否是可安全读取字段的普通对象。 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
