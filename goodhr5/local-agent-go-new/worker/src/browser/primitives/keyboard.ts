// 文件作用说明：提供只供封装能力调用的最小键盘按键、单字符输入和文本插入原子操作。

import type { Page } from "playwright-core";

/** KeyboardPrimitive 封装 Playwright 键盘最小操作，不查找或聚焦元素。 */
export class KeyboardPrimitive {
  /** press 按下组合键或普通按键。 */
  async press(page: Page, key: string, delayMs = 0): Promise<void> {
    await page.keyboard.press(key, { delay: Math.max(0, delayMs) });
  }

  /** typeCharacter 输入一个 Playwright 支持的字符。 */
  async typeCharacter(page: Page, character: string): Promise<void> {
    await page.keyboard.type(character);
  }

  /** insertText 插入中文等键盘 type 无法稳定支持的文本。 */
  async insertText(page: Page, text: string): Promise<void> {
    await page.keyboard.insertText(text);
  }
}
