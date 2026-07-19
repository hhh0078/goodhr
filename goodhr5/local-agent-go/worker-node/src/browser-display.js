/** 本文件负责统一浏览器内容视口、恢复100%缩放并读取显示诊断信息。 */

export const FIXED_BROWSER_VIEWPORT = Object.freeze({ width: 1280, height: 720 });

/** fixedBrowserViewport 返回可独立修改的固定视口参数。 */
export function fixedBrowserViewport() {
  return { ...FIXED_BROWSER_VIEWPORT };
}

/** browserDisplayAdjustmentMessage 生成浏览器显示不符合要求时可直接给用户处理的错误说明。 */
export function browserDisplayAdjustmentMessage(display = {}) {
  const targetWidth = Number(display.target_width || FIXED_BROWSER_VIEWPORT.width);
  const targetHeight = Number(display.target_height || FIXED_BROWSER_VIEWPORT.height);
  const innerWidth = Number(display.inner_width || 0);
  const innerHeight = Number(display.inner_height || 0);
  const widthScale = innerWidth > 0 ? targetWidth / innerWidth : 0;
  const heightScale = innerHeight > 0 ? targetHeight / innerHeight : 0;
  const scale = heightScale || widthScale;
  const scaleText = scale > 0 ? `，推测当前页面缩放约 ${Math.round(scale * 100)}%` : "";
  const shortcut = process.platform === "darwin" ? "Command+0" : "Ctrl+0";
  return `浏览器显示比例不符合任务要求：期望视口 ${targetWidth}x${targetHeight}，实际 ${innerWidth}x${innerHeight}${scaleText}。任务已停止，浏览器会保持打开；请在浏览器中按 ${shortcut} 恢复到 100%，确认后重新开始任务。`;
}

/** readBrowserDisplayMetrics 读取当前页面的实际视口、窗口、DPR和可视区缩放信息。 */
export async function readBrowserDisplayMetrics(currentPage) {
  if (!currentPage || typeof currentPage.evaluate !== "function") {
    return {
      inner_width: 0,
      inner_height: 0,
      outer_width: 0,
      outer_height: 0,
      device_pixel_ratio: 0,
      visual_viewport_scale: 0,
    };
  }
  return currentPage.evaluate(() => ({
    inner_width: Math.round(window.innerWidth || 0),
    inner_height: Math.round(window.innerHeight || 0),
    outer_width: Math.round(window.outerWidth || 0),
    outer_height: Math.round(window.outerHeight || 0),
    screen_width: Math.round(window.screen?.width || 0),
    screen_height: Math.round(window.screen?.height || 0),
    device_pixel_ratio: Number(window.devicePixelRatio || 0),
    visual_viewport_scale: Number(window.visualViewport?.scale || 1),
  }));
}

/** normalizeBrowserDisplay 将页面恢复到100%缩放和固定视口，并返回校验结果。 */
export async function normalizeBrowserDisplay(currentPage) {
  const errors = [];
  let zoomReset = false;
  let viewportReset = false;
  if (!currentPage) {
    return {
      target_width: FIXED_BROWSER_VIEWPORT.width,
      target_height: FIXED_BROWSER_VIEWPORT.height,
      matches_fixed: false,
      zoom_reset: false,
      viewport_reset: false,
      errors: ["浏览器页面不存在"],
    };
  }
  try {
    const shortcut = process.platform === "darwin" ? "Meta+0" : "Control+0";
    await currentPage.keyboard.press(shortcut);
    zoomReset = true;
  } catch (error) {
    errors.push(`恢复100%缩放失败：${error?.message || error}`);
  }
  try {
    if (typeof currentPage.setViewportSize !== "function") {
      throw new Error("当前页面不支持设置视口");
    }
    await currentPage.setViewportSize(fixedBrowserViewport());
    viewportReset = true;
  } catch (error) {
    errors.push(`设置固定视口失败：${error?.message || error}`);
  }
  if (typeof currentPage.waitForTimeout === "function") {
    await currentPage.waitForTimeout(120).catch(() => {});
  }
  const metrics = await readBrowserDisplayMetrics(currentPage).catch((error) => {
    errors.push(`读取显示参数失败：${error?.message || error}`);
    return {};
  });
  const matchesFixed =
    Number(metrics.inner_width || 0) === FIXED_BROWSER_VIEWPORT.width &&
    Number(metrics.inner_height || 0) === FIXED_BROWSER_VIEWPORT.height;
  return {
    ...metrics,
    target_width: FIXED_BROWSER_VIEWPORT.width,
    target_height: FIXED_BROWSER_VIEWPORT.height,
    matches_fixed: matchesFixed && zoomReset,
    zoom_reset: zoomReset,
    viewport_reset: viewportReset,
    errors,
  };
}
