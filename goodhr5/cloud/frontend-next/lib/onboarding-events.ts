/** 本文件在 GoodHR 控制台内同步新手引导上下文和真实业务操作，不接触招聘平台页面。 */
export type OnboardingFormField = "name" | "filter-mode" | "detail-mode" | "position-requirement" | "keywords";
export type OnboardingGuideContext = { active: boolean; platformId: string; positionId?: string; step?: import("./onboarding-state").OnboardingStep; browserOpening?: boolean };
export type OnboardingScreeningResult = {
  kind: "ai" | "keyword";
  phase: "result";
  terminal: true;
  accepted: boolean;
  reason: string;
  score?: number;
};
export type OnboardingEvent = {
  type: "guide-requested" | "platform-browser-open-requested" | "platform-unavailable" | "position-form-close-requested" | "position-start-close-requested" | "position-form-opened" | "position-form-closed" | "form-field-completed" | "position-created" | "start-dialog-opened" | "start-dialog-closed" | "start-dialog-status" | "position-recover-requested" | "position-recovered" | "platform-opened" | "position-started" | "screening-result";
  recovered?: boolean;
  startStatus?: "ready" | "checking" | "error" | "update";
  message?: string;
  restored?: boolean;
  field?: OnboardingFormField;
  completed?: boolean;
  positionId?: string;
  platformId?: string;
  mode?: string;
  result?: OnboardingScreeningResult;
};
const eventListeners = new Set<(event: OnboardingEvent) => void>();
const contextListeners = new Set<(context: OnboardingGuideContext) => void>();
let currentContext: OnboardingGuideContext = { active: false, platformId: "boss" };

/** publishOnboardingEvent 将已发生的岗位操作通知当前控制台的引导器。 */
export function publishOnboardingEvent(event: OnboardingEvent): void {
  for (const listener of eventListeners) {
    try { listener(event); } catch { /* 教程监听器异常不能改变已经成功的岗位操作。 */ }
  }
}

/** subscribeOnboardingEvents 订阅业务操作，页面卸载时应调用返回的解除方法。 */
export function subscribeOnboardingEvents(listener: (event: OnboardingEvent) => void): () => void {
  eventListeners.add(listener);
  return () => { eventListeners.delete(listener); };
}

/** getOnboardingGuideContext 读取当前引导是否开启及已经选择的招聘平台。 */
export function getOnboardingGuideContext(): OnboardingGuideContext {
  return currentContext;
}

/** setOnboardingGuideContext 切换当前账号的引导上下文，仅由全局引导器调用。 */
export function setOnboardingGuideContext(context: OnboardingGuideContext): void {
  if (currentContext.active === context.active && currentContext.platformId === context.platformId && currentContext.step === context.step && currentContext.positionId === context.positionId && currentContext.browserOpening === context.browserOpening) return;
  currentContext = context;
  for (const listener of contextListeners) {
    try { listener(context); } catch { /* 某个页面卸载或异常时，其他页面仍可使用真实业务。 */ }
  }
}

/** subscribeOnboardingGuideContext 让岗位表单在引导开始、暂停或平台切换时同步显示。 */
export function subscribeOnboardingGuideContext(listener: (context: OnboardingGuideContext) => void): () => void {
  contextListeners.add(listener);
  return () => { contextListeners.delete(listener); };
}
