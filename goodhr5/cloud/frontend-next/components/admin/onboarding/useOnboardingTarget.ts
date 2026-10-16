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
    "runtime-install": "[data-onboarding='runtime-install']",
    "runtime-system": "[data-onboarding='runtime-system-actions']",
    "platform-select": "[data-onboarding='menu-positions']",
    "platform-open": "[data-onboarding='browser-open']",
    "platform-login": "[data-onboarding='browser-open']",
    "position-menu": "[data-onboarding='menu-positions']",
    "position-create": "[data-onboarding='position-create']",
    "position-platform": "[data-onboarding='position-platform']",
    "position-name": "[data-onboarding='position-name']",
    "position-filter-mode": "[data-onboarding='position-filter-mode']",
    "position-detail-mode": "[data-onboarding='position-detail-mode']",
    "position-requirement": "[data-onboarding='position-requirement']",
    "position-keywords": "[data-onboarding='position-keywords']",
    "position-save": "[data-onboarding='position-save']",
    "position-start": `[data-onboarding='position-start'][data-position-id='${position}']`,
    "platform-filter": "[data-onboarding='position-open-platform']",
    "position-run": "[data-onboarding='position-run-confirm']",
    "position-start-check": "[data-onboarding='position-start-feedback']",
    "position-start-error": "[data-onboarding='position-start-feedback']",
    "position-start-update": "[data-onboarding='position-start-feedback']",
    "first-result": "[data-onboarding='onboarding-result-wait']",
    "complete": "",
  };
  return selectors[step];
}

/** useOnboardingTarget 在路由切换和弹框动画结束后再显示遮罩，丢失目标时安全解除遮挡。 */
export function useOnboardingTarget(selector: string, enabled: boolean): { target: HTMLElement | null; targetGeometry: string } {
  const [located, setLocated] = useState<{ selector: string; element: HTMLElement | null; geometry: string }>({ selector: "", element: null, geometry: "" });
  useEffect(() => {
    if (!enabled || !selector) return;
    let frame = 0;
    let settleTimer: number | undefined;
    let observedTarget: HTMLElement | null = null;
    const targetResizeObserver = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    /** inspect 只读取当前控制台元素的可见性，移动端菜单折叠时先突出打开菜单按钮。 */
    function inspect() {
      const candidates = Array.from(document.querySelectorAll<HTMLElement>(selector));
      const visible = candidates.map((candidate) => candidate.dataset.onboarding === "position-start-feedback" ? candidate.closest<HTMLElement>(".ant-modal-container, .ant-modal-content") || candidate : candidate.closest<HTMLElement>("[role='menuitem']") || candidate).find((element) => {
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
        const body = element.closest<HTMLElement>(".ant-modal-body");
        const bodyBox = body?.getBoundingClientRect();
        // 浏览器滚动边界可能保留半像素差，不能因肉眼不可见的小数反复隐藏目标。
        const tolerance = 1;
        const clippedInBody = bodyBox && box.height <= bodyBox.height + tolerance && box.width <= bodyBox.width + tolerance && (box.top < bodyBox.top - tolerance || box.bottom > bodyBox.bottom + tolerance || box.left < bodyBox.left - tolerance || box.right > bodyBox.right + tolerance);
        if (clippedInBody) {
          // 只调整控制台自己的滚动容器；被正文裁切的输入框不能交给 Tour 充当可见目标。
          element.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "instant" });
          if (settleTimer !== undefined) window.clearTimeout(settleTimer);
          settleTimer = window.setTimeout(schedule, 60);
          return false;
        }
        return box.width > 0 && box.height > 0 && !element.closest(".ant-modal-hidden");
      });
      const mobileMenu = selector.includes("menu-") ? document.querySelector<HTMLElement>("[data-onboarding='mobile-menu']") : null;
      const fallback = mobileMenu && mobileMenu.getBoundingClientRect().width > 0 ? mobileMenu : null;
      const element = visible || fallback;
      if (observedTarget !== element) {
        targetResizeObserver?.disconnect();
        observedTarget = element;
        // 加载图标收起会通过连续尺寸动画改变按钮宽度；只看 class 变化会留下旧孔位。
        if (element) targetResizeObserver?.observe(element);
      }
      const box = element?.getBoundingClientRect();
      const geometry = box ? [box.left, box.top, box.width, box.height].map(Math.round).join(",") : "";
      // 同一元素在内部滚动后会换坐标，用明确的几何签名让展示层重新测量，避免依赖中间帧提交。
      setLocated((current) => current.selector === selector && current.element === element && current.geometry === geometry ? current : { selector, element, geometry });
    }
    /** schedule 合并大量 React/弹框挂载通知，避免每次 DOM 变动都重复刷新状态。 */
    function schedule() { cancelAnimationFrame(frame); frame = requestAnimationFrame(inspect); }
    const observer = new MutationObserver(schedule);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["class"] });
    window.addEventListener("resize", schedule);
    window.addEventListener("scroll", schedule, true);
    window.addEventListener("transitionend", schedule, true);
    window.addEventListener("animationend", schedule, true);
    inspect();
    return () => { observer.disconnect(); targetResizeObserver?.disconnect(); cancelAnimationFrame(frame); if (settleTimer !== undefined) window.clearTimeout(settleTimer); window.removeEventListener("resize", schedule); window.removeEventListener("scroll", schedule, true); window.removeEventListener("transitionend", schedule, true); window.removeEventListener("animationend", schedule, true); };
  }, [selector, enabled]);
  // 新步骤不能暂借上一步的孔位，否则提示“保存”时会把真实保存按钮挡住。
  return enabled && located.selector === selector ? { target: located.element, targetGeometry: located.geometry } : { target: null, targetGeometry: "" };
}
