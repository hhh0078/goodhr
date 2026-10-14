/** 本文件将全局新手流程控制器接入遮罩界面，保持跨后台页面的同一份教程状态。 */
"use client";
import OnboardingExperience from "./OnboardingExperience";
import { useInteractiveOnboarding, type InteractiveOnboardingOptions } from "./useInteractiveOnboarding";

/** InteractiveOnboarding 展示当前账号的邀请、操作引导和真实完成反馈。 */
export default function InteractiveOnboarding(options: InteractiveOnboardingOptions) {
  const view = useInteractiveOnboarding(options);
  return <OnboardingExperience {...view} />;
}
