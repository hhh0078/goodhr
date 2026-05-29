/** 本文件负责维护前端新手教学本地步骤缓存，并在全部完成后通知后端。 */
import { completeOnboarding } from "./api/onboardingApi";

export const ONBOARDING_EVENT = "goodhr5:onboarding";
export const ONBOARDING_STEPS = [
  "local_agent",
  "platform_account",
  "position_template",
  "personal_config",
  "task_started",
  "subscription_viewed",
];

let currentEmail = "";
let completing = false;

/**
 * 初始化当前用户的新手教学缓存。
 * @param {any} user - 当前登录用户。
 * @returns {void} 无返回值。
 */
export function initOnboarding(user: any) {
  currentEmail = String(user?.email || "");
  if (!currentEmail) return;
  const cached = readOnboardingProgress();
  if (user?.onboarding?.completed) {
    saveProgress({ ...cached, completed: true });
    emitOnboardingChange();
    return;
  }
  saveProgress({ ...defaultProgress(), ...cached, completed: false });
  emitOnboardingChange();
}

/**
 * 标记指定教学步骤已完成。
 * @param {string} step - 教学步骤键。
 * @returns {Promise<void>} 无返回值。
 */
export async function markOnboardingStep(step: string) {
  if (!currentEmail || !ONBOARDING_STEPS.includes(step)) return;
  const progress = readOnboardingProgress();
  if (progress.completed) return;
  if (!progress.steps?.[step]) {
    progress.steps = { ...progress.steps, [step]: true };
    saveProgress(progress);
    emitOnboardingChange();
  }
  await checkOnboardingComplete();
}

/**
 * 读取当前用户的新手教学缓存。
 * @returns {any} 教学进度。
 */
export function readOnboardingProgress() {
  if (!currentEmail) return defaultProgress();
  try {
    const parsed = JSON.parse(localStorage.getItem(storageKey()) || "{}");
    return { ...defaultProgress(), ...parsed, steps: { ...defaultSteps(), ...(parsed.steps || {}) } };
  } catch {
    return defaultProgress();
  }
}

/**
 * 检查教学是否全部完成，完成后调用后端接口。
 * @returns {Promise<void>} 无返回值。
 */
export async function checkOnboardingComplete() {
  if (!currentEmail || completing) return;
  const progress = readOnboardingProgress();
  if (progress.completed) return;
  const done = ONBOARDING_STEPS.every((step) => Boolean(progress.steps?.[step]));
  if (!done) return;
  completing = true;
  try {
    const data = await completeOnboarding();
    saveProgress({ ...progress, completed: Boolean(data?.onboarding?.completed || true) });
    emitOnboardingChange();
  } catch {
    saveProgress(progress);
  } finally {
    completing = false;
  }
}

/**
 * 生成当前用户的缓存键。
 * @returns {string} localStorage 键。
 */
function storageKey() {
  return `goodhr5_onboarding_${currentEmail}`;
}

/**
 * 保存教学进度到本地缓存。
 * @param {any} progress - 教学进度对象。
 * @returns {void} 无返回值。
 */
function saveProgress(progress: any) {
  localStorage.setItem(storageKey(), JSON.stringify(progress));
}

/**
 * 生成默认教学进度。
 * @returns {any} 默认教学进度。
 */
function defaultProgress() {
  return { completed: false, steps: defaultSteps() };
}

/**
 * 生成默认步骤状态。
 * @returns {Record<string, boolean>} 默认步骤状态。
 */
function defaultSteps() {
  return Object.fromEntries(ONBOARDING_STEPS.map((step) => [step, false]));
}

/**
 * 通知页面刷新教学进度。
 * @returns {void} 无返回值。
 */
function emitOnboardingChange() {
  window.dispatchEvent(new CustomEvent(ONBOARDING_EVENT));
}
