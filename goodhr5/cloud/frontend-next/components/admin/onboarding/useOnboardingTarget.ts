/** 本文件只观察 GoodHR 自己的界面，等待真实引导目标挂载或显示，不操作招聘平台页面。 */
"use client";
import { useEffect, useState } from "react";
import type { OnboardingStep } from "@/lib/onboarding-state";

/** onboardingTargetSelector 为当前小步骤选择稳定的真实菜单、表单或操作区域。 */
export function onboardingTargetSelector(step: OnboardingStep, positionId: string): string {
  const position = positionId.replace(/[^a-zA-Z0-9_-]/g, "");
  const selectors: Record<OnboardingStep, string> = {
    "agent-menu": "[data-onboarding='menu-agent-download']",
    "agent-download": "[data-onboarding='onboarding-agent-download']",
    "agent-connect": "[data-onboarding='onboarding-agent-connect']",
    "runtime-install": "[data-onboarding='runtime-install'], [data-onboarding='runtime-system-check']",
    "platform-select": "[data-onboarding='onboarding-platform-choice']",
    "platform-open": "[data-onboarding='onboarding-platform-open']",
    "platform-login": "[data-onboarding='onboarding-platform-wait']",
    "position-menu": "[data-onboarding='menu-positions']",
    "position-create": "[data-onboarding='position-create']",
    "position-name": "[data-onboarding='position-name']",
    "position-filter-mode": "[data-onboarding='position-filter-mode']",
    "position-detail-mode": "[data-onboarding='position-detail-mode']",
    "position-requirement": "[data-onboarding='position-requirement']",
    "position-keywords": "[data-onboarding='position-keywords']",
    "position-save": "[data-onboarding='position-save']",
    "position-start": `[data-onboarding='position-start'][data-position-id='${position}']`,
    "platform-filter": "[data-onboarding='position-open-platform']",
    "position-run": "[data-onboarding='position-run-confirm']",
    "first-result": "[data-onboarding='onboarding-result-wait']",
    "complete": "",
  };
  return selectors[step];
}

/** useOnboardingTarget 在路由切换和弹框动画结束后再显示遮罩，丢失目标时安全解除遮挡。 */
export function useOnboardingTarget(selector: string, enabled: boolean): HTMLElement | null {
  const [located, setLocated] = useState<{ selector: string; element: HTMLElement | null }>({ selector: "", element: null });
  useEffect(() => {
    if (!enabled || !selector) return;
    let frame = 0;
    let settleTimer: number | undefined;
    /** inspect 只读取当前控制台元素的可见性，移动端菜单折叠时先突出打开菜单按钮。 */
    function inspect() {
      const candidates = Array.from(document.querySelectorAll<HTMLElement>(selector));
      const visible = candidates.map((candidate) => candidate.closest<HTMLElement>("[role='menuitem']") || candidate).find((element) => {
        const box = element.getBoundingClientRect();
        const modal = element.closest<HTMLElement>(".ant-modal");
        const drawer = element.closest<HTMLElement>(".ant-drawer-content-wrapper");
        const animating = [modal, drawer].some((surface) => surface?.getAnimations?.().some((animation) => animation.playState === "running")) || Boolean(modal?.matches(".ant-zoom-appear-active, .ant-zoom-enter-active")) || Boolean(drawer?.matches(".ant-drawer-panel-motion-left-enter-active, .ant-drawer-panel-motion-left-appear-active, .ant-drawer-panel-motion-right-enter-active, .ant-drawer-panel-motion-right-appear-active"));
        // 抽屉入场时元素已有宽高，但坐标仍在屏幕外；等动画真正结束再交给 Tour。
        const outsideDrawer = Boolean(drawer && (box.left < 0 || box.right > window.innerWidth));
        if (animating || outsideDrawer) {
          if (settleTimer !== undefined) window.clearTimeout(settleTimer);
          settleTimer = window.setTimeout(schedule, 60);
          return false;
        }
        return box.width > 0 && box.height > 0 && !element.closest(".ant-modal-hidden");
      });
      const mobileMenu = selector.includes("menu-") ? document.querySelector<HTMLElement>("[data-onboarding='mobile-menu']") : null;
      const fallback = mobileMenu && mobileMenu.getBoundingClientRect().width > 0 ? mobileMenu : null;
      const element = visible || fallback;
      setLocated((current) => current.selector === selector && current.element === element ? current : { selector, element });
    }
    /** schedule 合并大量 React/弹框挂载通知，避免每次 DOM 变动都重复刷新状态。 */
    function schedule() { cancelAnimationFrame(frame); frame = requestAnimationFrame(inspect); }
    const observer = new MutationObserver(schedule);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["class"] });
    window.addEventListener("resize", schedule);
    window.addEventListener("transitionend", schedule, true);
    window.addEventListener("animationend", schedule, true);
    inspect();
    return () => { observer.disconnect(); cancelAnimationFrame(frame); if (settleTimer !== undefined) window.clearTimeout(settleTimer); window.removeEventListener("resize", schedule); window.removeEventListener("transitionend", schedule, true); window.removeEventListener("animationend", schedule, true); };
  }, [selector, enabled]);
  // 新步骤不能暂借上一步的孔位，否则提示“保存”时会把真实保存按钮挡住。
  return enabled && located.selector === selector ? located.element : null;
}
