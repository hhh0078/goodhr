/** 本文件负责维护新版后台新手教程步骤缓存，并在全部完成后同步云端。 */
"use client";

import { cloudRequest } from "./admin-api";

export const ONBOARDING_STEPS = [
  "local_agent",
  "position_template",
  "task_started",
  "subscription_viewed",
] as const;

export type OnboardingStep = (typeof ONBOARDING_STEPS)[number];
export type OnboardingProgress = { completed: boolean; steps: Record<OnboardingStep, boolean> };

/** readOnboardingProgress 读取指定用户的新手教程本地进度。 */
export function readOnboardingProgress(email: string): OnboardingProgress {
  const fallback = defaultProgress();
  if (typeof window === "undefined" || !email.trim()) return fallback;
  try {
    const cached = JSON.parse(localStorage.getItem(storageKey(email)) || "{}");
    return {
      completed: Boolean(cached.completed),
      steps: { ...fallback.steps, ...(cached.steps || {}) },
    };
  } catch {
    return fallback;
  }
}

/** syncOnboardingProgress 合并多个步骤状态，并在全部完成时通知云端。 */
export async function syncOnboardingProgress(email: string, updates: Partial<Record<OnboardingStep, boolean>>, serverCompleted = false) {
  const current = readOnboardingProgress(email);
  const steps = { ...current.steps };
  ONBOARDING_STEPS.forEach((step) => {
    if (updates[step]) steps[step] = true;
  });
  const next: OnboardingProgress = {
    completed: current.completed || serverCompleted,
    steps,
  };
  saveOnboardingProgress(email, next);
  if (next.completed || !ONBOARDING_STEPS.every((step) => next.steps[step])) return next;
  try {
    await cloudRequest("/api/onboarding/complete", { method: "POST" });
    next.completed = true;
    saveOnboardingProgress(email, next);
  } catch {
    // 云端暂时不可用时保留本地步骤，后续进入控制台会再次同步。
  }
  return next;
}

/** markOnboardingStep 标记一个由用户操作完成的新手教程步骤。 */
export async function markOnboardingStep(email: string, step: OnboardingStep) {
  if (!email.trim()) return readOnboardingProgress(email);
  return syncOnboardingProgress(email, { [step]: true });
}

/** onboardingFinished 判断全部步骤是否已经满足。 */
export function onboardingFinished(progress: OnboardingProgress) {
  return progress.completed || ONBOARDING_STEPS.every((step) => Boolean(progress.steps[step]));
}

/** saveOnboardingProgress 保存指定用户的新手教程本地进度。 */
function saveOnboardingProgress(email: string, progress: OnboardingProgress) {
  if (typeof window === "undefined" || !email.trim()) return;
  localStorage.setItem(storageKey(email), JSON.stringify(progress));
}

/** storageKey 生成与旧版兼容的用户级新手教程缓存键。 */
function storageKey(email: string) {
  return `goodhr5_onboarding_${email.trim().toLowerCase()}`;
}

/** defaultProgress 生成包含全部步骤的默认进度。 */
function defaultProgress(): OnboardingProgress {
  return {
    completed: false,
    steps: Object.fromEntries(ONBOARDING_STEPS.map((step) => [step, false])) as Record<OnboardingStep, boolean>,
  };
}
