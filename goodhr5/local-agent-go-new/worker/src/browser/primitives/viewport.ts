// 文件作用说明：使用 Playwright 标准能力读取视口尺寸，仅在同一次操作内复用自然窗口测量，不向招聘页面注入脚本。

import { actionContextStorage, traceOperation } from "../../logging/operation.js";
import type { Page } from "playwright-core";
import type { ActionContext } from "../../contracts/common.js";
import type { ViewportSize } from "../../contracts/selector.js";

type ViewportMeasurement = { size: ViewportSize; cacheable: boolean };
const requestMeasurements = new WeakMap<ActionContext, WeakMap<Page, Promise<ViewportMeasurement>>>();

/** ViewportPrimitive 提供只供封装能力调用的视口尺寸读取。 */
export class ViewportPrimitive {
  /** size 优先读取配置尺寸，同一ALS操作的同一页面只测量一次；跨请求或缺少上下文时重新测量。 */
  async size(page: Page): Promise<ViewportSize> {
    const configured = page.viewportSize();
    if (configured && Number.isFinite(configured.width) && Number.isFinite(configured.height) && configured.width > 0 && configured.height > 0) {
      return configured;
    }
    const context = actionContextStorage.getStore();
    if (!context) return (await this.measure(page)).size;
    let pages = requestMeasurements.get(context);
    if (!pages) {
      pages = new WeakMap();
      requestMeasurements.set(context, pages);
    }
    let pending = pages.get(page);
    if (!pending) {
      pending = this.measure(page);
      pages.set(page, pending);
    }
    try {
      const measured = await pending;
      if (!measured.cacheable && pages.get(page) === pending) pages.delete(page);
      return measured.size;
    } catch (error) {
      if (pages.get(page) === pending) pages.delete(page);
      throw error;
    }
  }

  /** measure 用CSS像素截图与真实HTML宽度换算自然窗口，尺寸未完整核实时保留旧回退但不缓存。 */
  private async measure(page: Page): Promise<ViewportMeasurement> {
    const screenshot = await traceOperation("viewport_capture", () => page.screenshot({ type: "png", scale: "css", timeout: 5_000 }));
    if (screenshot.length < 24 || screenshot.subarray(1, 4).toString("ascii") !== "PNG") {
      throw new Error("没有读到有效的浏览器视口尺寸");
    }
    const width = screenshot.readUInt32BE(16);
    const height = screenshot.readUInt32BE(20);
    if (width <= 0 || height <= 0) throw new Error("浏览器视口尺寸不正确");
    const rootBox = await page.locator("html").boundingBox().catch(() => null);
    if (!rootBox || !Number.isFinite(rootBox.width) || rootBox.width <= 0) {
      return { size: { width, height }, cacheable: false };
    }
    const screenshotScale = width / rootBox.width;
    if (!Number.isFinite(screenshotScale) || screenshotScale <= 0) {
      return { size: { width, height }, cacheable: false };
    }
    const cssHeight = height / screenshotScale;
    if (!Number.isFinite(cssHeight) || cssHeight <= 0) throw new Error("浏览器视口尺寸不正确");
    return { size: { width: rootBox.width, height: cssHeight }, cacheable: true };
  }
}
