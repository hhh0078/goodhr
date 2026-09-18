// 文件作用说明：提供只供封装能力调用的最小鼠标移动、按键和真实滚轮原子操作。

import { traceOperation } from "../../logging/operation.js";
import type { Page } from "playwright-core";

/** MousePrimitive 封装 Playwright 鼠标最小操作，不查找元素也不决定流程。 */
export class MousePrimitive {
  private readonly pressed = new WeakMap<Page, Set<"left" | "right" | "middle">>();
  /** move 发送一次目标坐标，等待 Camoufox 原生拟人轨迹执行完成。 */
  async move(
    page: Page,
    x: number,
    y: number,
  ): Promise<void> {
    // 本原子仅用于悬停定位，不执行拖拽；先释放遗留按键，避免移动时选中页面文字。
    await this.up(page, "left");
    // 不再拆成多段直线，否则每一小段都会重复触发浏览器的拟人轨迹。
    await traceOperation("mouse_move", () => page.mouse.move(x, y));
  }

  /** down 按下指定鼠标按键。 */
  async down(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page) ?? new Set<"left" | "right" | "middle">();
    buttons.add(button);
    this.pressed.set(page, buttons);
    await traceOperation("mouse_down", () => page.mouse.down({ button }));
  }

  /** up 松开指定鼠标按键。 */
  async up(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page);
    // Camoufox 在未按下时收到多余的 mouseup 可能不返回；只释放本实例曾按下的键。
    if (!buttons?.has(button)) return;
    await traceOperation("mouse_up", () => page.mouse.up({ button }));
    buttons.delete(button);
  }

  /** wheel 发送真实鼠标滚轮事件。 */
  async wheel(page: Page, deltaX: number, deltaY: number): Promise<void> {
    await traceOperation("mouse_wheel", () => page.mouse.wheel(deltaX, deltaY));
  }
}
