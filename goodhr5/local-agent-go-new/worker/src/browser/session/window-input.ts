// 文件作用说明：为 Windows 浏览器关闭遮挡跟踪，避免后台窗口暂停帧回调而阻塞标准鼠标滚轮。
import type { LaunchOptions } from "playwright-core";

/** useBackgroundWindowInput 合并 Mozilla 推荐的 Windows 自动化偏好，保留其他启动配置且不修改传入对象。 */
export function useBackgroundWindowInput(
  options: LaunchOptions,
  platform: string = process.platform,
): LaunchOptions {
  if (platform !== "win32") return options;
  // Mozilla Bug 1802473：自动化窗口被遮挡时仍需要渲染，关闭 Windows 遮挡跟踪。
  return {
    ...options,
    firefoxUserPrefs: {
      ...options.firefoxUserPrefs,
      "widget.windows.window_occlusion_tracking.enabled": false,
    },
  };
}
