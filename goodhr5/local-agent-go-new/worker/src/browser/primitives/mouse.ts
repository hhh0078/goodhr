// 文件作用说明：提供只供封装能力调用的最小鼠标移动、按键和真实滚轮原子操作。

import type { Page } from "playwright-core";

/** MousePrimitive 封装 Playwright 鼠标最小操作，不查找元素也不决定流程。 */
export class MousePrimitive {
  private readonly pressed = new WeakMap<Page, Set<"left" | "right" | "middle">>();
  /** move 把鼠标移动到指定坐标。 */
  async move(
    page: Page,
    x: number,
    y: number,
    steps: number,
  ): Promise<void> {
    // 本原子仅用于悬停定位，不执行拖拽；先释放遗留按键，避免移动时选中页面文字。
    await this.up(page, "left");
    await page.mouse.move(x, y, { steps: Math.max(1, Math.floor(steps)) });
  }

  /** down 按下指定鼠标按键。 */
  async down(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page) ?? new Set<"left" | "right" | "middle">();
    buttons.add(button);
    this.pressed.set(page, buttons);
    await page.mouse.down({ button });
  }

  /** up 松开指定鼠标按键。 */
  async up(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page);
    // Camoufox 在未按下时收到多余的 mouseup 可能不返回；只释放本实例曾按下的键。
    if (!buttons?.has(button)) return;
    await page.mouse.up({ button });
    buttons.delete(button);
  }

  /** wheel 发送真实鼠标滚轮事件。 */
  async wheel(page: Page, deltaX: number, deltaY: number): Promise<void> {
    await page.mouse.wheel(deltaX, deltaY);
  }
}
