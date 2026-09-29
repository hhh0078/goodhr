// 文件作用说明：提供只供封装能力调用的最小鼠标移动、按键和真实滚轮原子操作。

import { traceOperation } from "../../logging/operation.js";
import type { Page } from "playwright-core";
import type { JsonObject } from "../../contracts/common.js";
import { resolveCamoufoxBinary } from "../session/camoufox-binary.js";
import { safeURL } from "../session/navigation.js";
import { pageDiagnostic } from "../session/page-diagnostics.js";

/** MousePrimitive 封装 Playwright 鼠标最小操作，不查找元素也不决定流程。 */
export class MousePrimitive {
  private readonly pressed = new WeakMap<Page, Set<"left" | "right" | "middle">>();
  private browserVersion: string | undefined;
  /** move 发送一次目标坐标，等待 Camoufox 原生拟人轨迹执行完成。 */
  async move(
    page: Page,
    x: number,
    y: number,
  ): Promise<void> {
    // 本原子仅用于悬停定位，不执行拖拽；先释放遗留按键，避免移动时选中页面文字。
    await this.up(page, "left");
    // 不再拆成多段直线，否则每一小段都会重复触发浏览器的拟人轨迹。
    await this.traceMouse(page, "mouse_move", () => page.mouse.move(x, y), { x, y });
  }

  /** down 按下指定鼠标按键。 */
  async down(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page) ?? new Set<"left" | "right" | "middle">();
    buttons.add(button);
    this.pressed.set(page, buttons);
    await this.traceMouse(page, "mouse_down", () => page.mouse.down({ button }), { button });
  }

  /** up 松开指定鼠标按键。 */
  async up(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page);
    // Camoufox 在未按下时收到多余的 mouseup 可能不返回；只释放本实例曾按下的键。
    if (!buttons?.has(button)) return;
    await this.traceMouse(page, "mouse_up", () => page.mouse.up({ button }), { button });
    buttons.delete(button);
  }

  /** wheel 发送真实鼠标滚轮事件。 */
  async wheel(page: Page, deltaX: number, deltaY: number): Promise<void> {
    await this.traceMouse(page, "mouse_wheel", () => page.mouse.wheel(deltaX, deltaY), { delta_x: deltaX, delta_y: deltaY });
  }

  /** traceMouse 保留输入前后页面事件，原生调用失败或浏览器关闭时也能取得缓存证据。 */
  private async traceMouse(page: Page, step: string, operation: () => Promise<void>, input: JsonObject): Promise<void> {
    const fields = this.diagnosticFields(page, input);
    await traceOperation(step, async () => {
      try { await operation(); }
      finally { fields.output = { page_state_after: pageDiagnostic(page) }; }
    }, fields);
  }

  /** diagnosticFields 标记需要上传的鼠标首尾记录，只读取标准 Page 状态和组件版本。 */
  private diagnosticFields(page: Page, input: JsonObject): JsonObject {
    this.browserVersion ??= resolveCamoufoxBinary().version || "unknown";
    const summary: JsonObject = {
      ...input, pressed_buttons: [...(this.pressed.get(page) ?? [])],
      agent_version: process.env.GOODHR_AGENT_VERSION ?? "unknown",
      camoufox_version: this.browserVersion, node_version: process.version, platform: process.platform,
    };
    try {
      const viewport = page.viewportSize();
      summary.page_closed = page.isClosed();
      summary.page_url = safeURL(page.url());
      summary.viewport = viewport ? { width: viewport.width, height: viewport.height } : null;
      summary.page_state = pageDiagnostic(page);
    } catch { summary.page_state_unavailable = true; }
    return {
      diagnostic: true,
      input: summary,
    };
  }
}
