// 文件作用说明：使用 Playwright 标准能力读取视口尺寸，不向招聘页面注入或执行脚本。

import type { Page } from "playwright-core";
import type { ViewportSize } from "../../contracts/selector.js";

/** ViewportPrimitive 提供只供封装能力调用的视口尺寸读取。 */
export class ViewportPrimitive {
  /** size 优先读取 Playwright 视口配置，无固定视口时把截图物理像素换算为 CSS 视口尺寸。 */
  async size(page: Page): Promise<ViewportSize> {
    const configured = page.viewportSize();
    if (configured && configured.width > 0 && configured.height > 0) {
      return configured;
    }
    const screenshot = await page.screenshot({ type: "png" });
    if (
      screenshot.length < 24 ||
      screenshot.subarray(1, 4).toString("ascii") !== "PNG"
    ) {
      throw new Error("没有读到有效的浏览器视口尺寸");
    }
    const width = screenshot.readUInt32BE(16);
    const height = screenshot.readUInt32BE(20);
    if (width <= 0 || height <= 0) {
      throw new Error("浏览器视口尺寸不正确");
    }
    const rootBox = await page
      .locator("html")
      .boundingBox()
      .catch(() => null);
    if (!rootBox || rootBox.width <= 0) {
      return { width, height };
    }
    const screenshotScale = width / rootBox.width;
    if (!Number.isFinite(screenshotScale) || screenshotScale <= 0) {
      return { width, height };
    }
    return {
      width: rootBox.width,
      height: height / screenshotScale,
    };
  }
}
