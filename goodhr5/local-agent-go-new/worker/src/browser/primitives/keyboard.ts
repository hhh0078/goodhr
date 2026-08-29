// 文件作用说明：提供只供封装能力调用的键盘按键、CloakBrowser 文本输入和安全文本插入操作。

import type { Page } from "playwright-core";

/** KeyboardPrimitive 封装 Playwright 键盘最小操作，不查找或聚焦元素。 */
export class KeyboardPrimitive {
  /** press 按下组合键或普通按键。 */
  async press(page: Page, key: string, delayMs = 0): Promise<void> {
    await page.keyboard.press(key, { delay: Math.max(0, delayMs) });
  }

  /** typeText 把完整文本交给 CloakBrowser 安排按键和字符间节奏。 */
  async typeText(page: Page, text: string): Promise<void> {
    await page.keyboard.type(text);
  }

  /** insertText 插入中文等键盘 type 无法稳定支持的文本。 */
  async insertText(page: Page, text: string): Promise<void> {
    await page.keyboard.insertText(text);
  }
}
