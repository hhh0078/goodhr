/** 本文件保存按账号隔离的新手引导位置，只记录界面进度，不替代云端的真实完成事实。 */
export const ONBOARDING_VERSION = 2;
export const ONBOARDING_SKIP_PHRASE = "让开，我不需要教程";
export const ONBOARDING_STEPS = [
  "agent-menu", "agent-download", "agent-connect", "runtime-install", "platform-select", "platform-open", "platform-login",
  "position-menu", "position-create", "position-name", "position-filter-mode", "position-detail-mode", "position-requirement", "position-keywords", "position-save",
  "position-start", "platform-filter", "position-run", "first-result", "complete",
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
};
export const ONBOARDING_STAGES = ["准备本地程序", "登录招聘平台", "创建岗位", "筛选并开始", "看到筛选结果"];

/** createOnboardingState 为首次进入当前账号的用户创建尚未接受邀请的引导。 */
export function createOnboardingState(): OnboardingState {
  return { version: ONBOARDING_VERSION, status: "invited", step: "agent-menu", platformId: "boss", positionId: "", environmentReady: false, platformLoggedIn: false, positionStarted: false, filterMode: "ai", detailMode: "ai", completedFields: [], celebrated: [], updatedAt: Date.now() };
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
    return { ...fallback, status, step, platformId, positionId: typeof raw.positionId === "string" ? raw.positionId.trim() : "", environmentReady: raw.environmentReady === true, platformLoggedIn: raw.platformLoggedIn === true, positionStarted: raw.positionStarted === true, filterMode: raw.filterMode === "keyword" ? "keyword" : "ai", detailMode: ["ai", "ocr", "dom"].includes(String(raw.detailMode)) ? String(raw.detailMode) : "ai", completedFields: stringList(raw.completedFields), celebrated: stringList(raw.celebrated), updatedAt: Number.isFinite(raw.updatedAt) ? Number(raw.updatedAt) : Date.now(), completionKind: raw.completionKind === "keyword" || raw.completionKind === "ai" ? raw.completionKind : undefined };
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
  if (step.startsWith("agent-") || step === "runtime-install") return 0;
  if (step.startsWith("platform-") && step !== "platform-filter") return 1;
  if (["position-start", "platform-filter", "position-run"].includes(step)) return 3;
  if (step === "first-result" || step === "complete") return 4;
  return 2;
}

/** onboardingDoneCount 只根据已经发生的真实里程碑计算总进度。 */
export function onboardingDoneCount(state: OnboardingState): number {
  return [state.environmentReady, state.platformLoggedIn, Boolean(state.positionId.trim()), state.positionStarted, state.status === "completed"].filter(Boolean).length;
}

/** onboardingRemainingTime 显示估计区间，网络安装与平台等待期间不制造虚假的倒计时。 */
export function onboardingRemainingTime(state: OnboardingState): string {
  if (state.status === "completed") return "已跑通";
  const estimates = [[3, 8], [1, 2], [2, 3], [1, 2], [1, 3]];
  const done = [state.environmentReady, state.platformLoggedIn, Boolean(state.positionId.trim()), state.positionStarted, false];
  const remaining = estimates.reduce((sum, pair, index) => done[index] ? sum : [sum[0] + pair[0], sum[1] + pair[1]], [0, 0]);
  return `约 ${remaining[0]}–${remaining[1]} 分钟`;
}

/** needsPositionRequirement 判断基础或详情任一 AI 模式是否需要岗位要求。 */
export function needsPositionRequirement(state: Pick<OnboardingState, "filterMode" | "detailMode">): boolean {
  return state.filterMode === "ai" || state.detailMode === "ai";
}

/** nextOnboardingFormStep 在模式切换后重新选择真正缺少的字段，不把填写次数当业务完成。 */
export function nextOnboardingFormStep(state: OnboardingState): OnboardingStep {
  if (!state.completedFields.includes("name")) return "position-name";
  if (!state.completedFields.includes("filter-mode")) return "position-filter-mode";
  if (!state.completedFields.includes("detail-mode")) return "position-detail-mode";
  if (needsPositionRequirement(state) && !state.completedFields.includes("position-requirement")) return "position-requirement";
  if (state.filterMode === "keyword" && !state.completedFields.includes("keywords")) return "position-keywords";
  return "position-save";
}

/** stringList 只保留有限数量的短字符串，避免损坏缓存把异常值带入界面。 */
function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item.length <= 80).slice(0, 30) : [];
}

/** isRecord 判断缓存是否是可安全读取字段的普通对象。 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
