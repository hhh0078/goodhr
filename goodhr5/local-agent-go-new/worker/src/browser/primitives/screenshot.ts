// 文件作用说明：提供只供封装能力调用的页面和元素 PNG 截图原子操作。

import fs from "node:fs/promises";
import path from "node:path";
import type { Locator, Page } from "playwright-core";

/** ScreenshotPrimitive 封装页面和元素截图最小操作。 */
export class ScreenshotPrimitive {
  /** page 保存页面截图并返回文件大小。 */
  async page(
    page: Page,
    filePath: string,
    fullPage: boolean,
  ): Promise<number> {
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await page.screenshot({ path: filePath, fullPage, type: "png" });
    return (await fs.stat(filePath)).size;
  }

  /** element 保存指定元素截图并返回文件大小。 */
  async element(locator: Locator, filePath: string): Promise<number> {
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await locator.screenshot({ path: filePath, type: "png" });
    return (await fs.stat(filePath)).size;
  }
}
