// 文件作用说明：提供只供封装能力调用的最小鼠标移动、按键和真实滚轮原子操作。

import { actionContextStorage, traceOperation } from "../../logging/operation.js";
import type { Page } from "playwright-core";
import type { JsonObject } from "../../contracts/common.js";
import { resolveCamoufoxBinary } from "../session/camoufox-binary.js";
import { safeURL } from "../session/navigation.js";
import { pageDiagnostic } from "../session/page-diagnostics.js";
import { nativeInputSafetyDiagnostic } from "../session/native-input-safety.js";
import { WorkerError } from "../../errors/worker-error.js";
import { assertInputAllowed, trackNativeInput } from "./input-lifecycle.js";

/** MousePrimitive 封装 Playwright 鼠标最小操作，不查找元素也不决定流程。 */
export class MousePrimitive {
  private readonly pressed = new WeakMap<Page, Set<"left" | "right" | "middle">>();
  private browserVersion: string | undefined;
  /** 创建独立输入限时，正式为十八秒，给浏览器内部十五秒保护留出诊断回传时间。 */
  constructor(private readonly inputTimeoutMS = 18_000) {}
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
    assertInputAllowed();
    await this.traceMouse(page, "mouse_down", () => {
      // 只有通过全部取消检查、即将真正发送 down 时才记录，避免补发没有对应按下的 mouseup。
      const buttons = this.pressed.get(page) ?? new Set<"left" | "right" | "middle">();
      buttons.add(button);
      this.pressed.set(page, buttons);
      return page.mouse.down({ button });
    }, { button });
  }

  /** up 松开指定鼠标按键。 */
  async up(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    const buttons = this.pressed.get(page);
    // Camoufox 在未按下时收到多余的 mouseup 可能不返回；只释放本实例曾按下的键。
    if (!buttons?.has(button)) return;
    // 请求取消后仍只松开本实例已经按下的键，避免保留窗口时留下拖选状态。
    await this.traceMouse(page, "mouse_up", () => page.mouse.up({ button }).finally(() => buttons.delete(button)), { button }, true);
  }

  /** wheel 发送真实鼠标滚轮事件。 */
  async wheel(page: Page, deltaX: number, deltaY: number): Promise<void> {
    await this.traceMouse(page, "mouse_wheel", () => page.mouse.wheel(deltaX, deltaY), { delta_x: deltaX, delta_y: deltaY });
  }

  /** traceMouse 保留输入前后页面事件，原生调用失败或浏览器关闭时也能取得缓存证据。 */
  private async traceMouse(page: Page, step: string, operation: () => Promise<void>, input: JsonObject, cleanup = false): Promise<void> {
    if (!cleanup) assertInputAllowed();
    const fields = this.diagnosticFields(page, input);
    const context = actionContextStorage.getStore();
    const timeoutMS = Math.max(1, Math.min(this.inputTimeoutMS, (cleanup ? Infinity : context?.deadline_at ?? Infinity) - Date.now()));
    (fields.input as JsonObject).input_timeout_ms = timeoutMS;
    await traceOperation(step, async () => {
      const startedAt = Date.now();
      const wait: JsonObject = { origin: "worker", stage: "playwright_input", timeout_ms: timeoutMS, status: "pending" };
      let timer: ReturnType<typeof setTimeout> | undefined;
      /** timeoutError 标记无法撤销的输入超时，让队列保留浏览器并禁止后续动作。 */
      const timeoutError = (): WorkerError => {
        wait.status = "timed_out";
        wait.elapsed_ms = Date.now() - startedAt;
        return new WorkerError({ code: "WORKER_UNRESPONSIVE", message: "浏览器输入等太久了，岗位先停一下；窗口保留着，请检查页面后重新开始",
          action: context?.action ?? "interaction", step, trace_id: context?.trace_id ?? "", retryable: false,
          details: { reason: "native_input_deadline", recovery_required: true, browser_retained: true, request_id: context?.request_id ?? "",
            input: fields.input ?? {}, output: { input_wait: wait, page_state_after: pageDiagnostic(page) } } });
      };
      try {
        // 先启动 Worker 定时器，再发协议调用；底层 Promise 的迟到结果由 race 接收，不产生无人处理的拒绝。
        const deadline = new Promise<never>((_, reject) => { timer = setTimeout(() => reject(timeoutError()), timeoutMS); });
        await Promise.race([trackNativeInput(operation, cleanup), deadline]);
        if (Date.now() - startedAt >= timeoutMS) throw timeoutError();
        wait.status = "completed";
      } catch (error) {
        if (wait.status !== "timed_out") wait.status = "failed";
        throw error;
      } finally {
        clearTimeout(timer);
        wait.elapsed_ms = Date.now() - startedAt;
        fields.output = { input_wait: wait, page_state_after: pageDiagnostic(page) };
      }
    }, fields);
  }

  /** diagnosticFields 标记需要上传的鼠标首尾记录，只读取标准 Page 状态和组件版本。 */
  private diagnosticFields(page: Page, input: JsonObject): JsonObject {
    this.browserVersion ??= resolveCamoufoxBinary().version || "unknown";
    const summary: JsonObject = {
      ...input, pressed_buttons: [...(this.pressed.get(page) ?? [])],
      agent_version: process.env.GOODHR_AGENT_VERSION ?? "unknown",
      camoufox_version: this.browserVersion, node_version: process.version, platform: process.platform,
      native_input_patch: nativeInputSafetyDiagnostic(),
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
