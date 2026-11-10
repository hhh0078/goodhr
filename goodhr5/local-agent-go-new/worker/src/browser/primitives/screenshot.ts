// 文件作用说明：提供只供封装能力调用的页面和元素 PNG 截图原子操作。

import fs from "node:fs/promises";
import path from "node:path";
import { actionContextStorage, traceOperation } from "../../logging/operation.js";
import { markRequestProgress } from "../../logging/request-progress.js";
import type { Locator, Page } from "playwright-core";

export const screenshotTimeoutMS = 5_000;
const nativeTimeouts = new WeakSet<Error>();

/** screenshotTimedOut 仅接受本文件捕获的 Playwright 截图超时，不把文件或上层截止误认成五秒截图调用。 */
export function screenshotTimedOut(error: unknown): boolean {
  return error instanceof Error && nativeTimeouts.has(error);
}

/** captureFrame 从实际画面调用记录阶段及五秒超时，传出相同错误以保留原有行为。 */
async function captureFrame<T>(capture: () => Promise<T>): Promise<T> {
  const context = actionContextStorage.getStore();
  if (context?.action.includes("screenshot")) markRequestProgress(context, "screenshot_capture");
  try {
    return await traceOperation("screenshot_capture", capture);
  } catch (error) {
    if (error instanceof Error && error.name === "TimeoutError") nativeTimeouts.add(error);
    throw error;
  }
}

/** ScreenshotPrimitive 封装页面和元素截图最小操作。 */
export class ScreenshotPrimitive {
  /** clippedPage 只裁剪当前页面视口，不调用会自动滚动的元素截图。 */
  async clippedPage(page: Page, filePath: string, clip: { x: number; y: number; width: number; height: number }): Promise<number> {
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await captureFrame(() => page.screenshot({ path: filePath, clip, fullPage: false, type: "png", timeout: screenshotTimeoutMS }));
    return (await fs.stat(filePath)).size;
  }
  /** pageBuffer 截取当前页面视口并返回内存中的 PNG，不写入本地文件。 */
  async pageBuffer(page: Page): Promise<Buffer> {
    return captureFrame(() => page.screenshot({ type: "png", timeout: screenshotTimeoutMS }));
  }

  /** elementBuffer 截取指定元素并返回内存中的 PNG，不写入本地文件。 */
  async elementBuffer(locator: Locator): Promise<Buffer> {
    return captureFrame(() => locator.screenshot({ type: "png", timeout: screenshotTimeoutMS }));
  }

  /** page 保存页面截图并返回文件大小。 */
  async page(
    page: Page,
    filePath: string,
    fullPage: boolean,
  ): Promise<number> {
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await captureFrame(() => page.screenshot({ path: filePath, fullPage, type: "png", timeout: screenshotTimeoutMS }));
    return (await fs.stat(filePath)).size;
  }

  /** element 保存指定元素截图并返回文件大小。 */
  async element(locator: Locator, filePath: string): Promise<number> {
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await captureFrame(() => locator.screenshot({ path: filePath, type: "png", timeout: screenshotTimeoutMS }));
    return (await fs.stat(filePath)).size;
  }
}
