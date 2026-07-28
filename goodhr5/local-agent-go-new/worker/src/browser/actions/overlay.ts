// 文件作用说明：实现与招聘平台无关的通用页面提示浮层显示和关闭能力。

import type {
  OverlayCloseRequest,
  OverlayShowRequest,
} from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { BrowserSession } from "../session/browser-session.js";

/** OverlayAction 管理页面右上角通用提示卡片。 */
export class OverlayAction {
  /** 创建通用浮层封装能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly logger: WorkerLogger,
  ) {}

  /** show 显示或更新指定浮层。 */
  async show(
    request: OverlayShowRequest,
    actionContext: ActionContext,
  ): Promise<JsonObject> {
    const step = "show_overlay";
    this.logger.info(actionContext, step, "start", {
      overlay_id: request.overlay_id,
      level: request.level ?? "info",
    });
    try {
      const page = await this.session.requirePage(actionContext, step);
      await page.evaluate((payload) => {
        const rootID = `goodhr-overlay-${payload.overlay_id}`;
        document.getElementById(rootID)?.remove();
        const root = document.createElement("section");
        root.id = rootID;
        root.setAttribute("data-goodhr-overlay", payload.overlay_id);
        root.style.cssText = [
          "position:fixed",
          "top:20px",
          "right:20px",
          "z-index:2147483647",
          "width:min(360px,calc(100vw - 40px))",
          "padding:14px 16px",
          "border-radius:12px",
          "background:#fffdf7",
          "border:1px solid #dfd4bd",
          "box-shadow:0 10px 28px rgba(64,50,30,.18)",
          "color:#302a22",
          "font:14px/1.55 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif",
        ].join(";");
        const title = document.createElement("strong");
        title.textContent = payload.title;
        title.style.cssText = "display:block;font-size:15px;margin-bottom:4px";
        root.append(title);
        if (payload.subtitle) {
          const subtitle = document.createElement("div");
          subtitle.textContent = payload.subtitle;
          subtitle.style.cssText = "color:#776b5d;margin-bottom:6px";
          root.append(subtitle);
        }
        const message = document.createElement("div");
        message.textContent = payload.message;
        root.append(message);
        document.documentElement.append(root);
        if (payload.max_age_ms > 0) {
          window.setTimeout(() => root.remove(), payload.max_age_ms);
        }
      }, {
        overlay_id: request.overlay_id,
        title: request.title,
        subtitle: request.subtitle ?? "",
        message: request.message,
        level: request.level ?? "info",
        max_age_ms: request.max_age_ms ?? 0,
      });
      this.logger.info(actionContext, step, "success", {
        overlay_id: request.overlay_id,
      });
      return { shown: true, overlay_id: request.overlay_id };
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        message: "页面提示暂时没显示出来",
      });
      this.logger.error(actionContext, step, "failed", normalized.details);
      throw normalized;
    }
  }

  /** close 关闭指定浮层。 */
  async close(
    request: OverlayCloseRequest,
    actionContext: ActionContext,
  ): Promise<JsonObject> {
    const page = await this.session.requirePage(
      actionContext,
      "close_overlay",
    );
    const removed = await page.evaluate((overlayID) => {
      const element = document.getElementById(
        `goodhr-overlay-${overlayID}`,
      );
      if (!element) {
        return false;
      }
      element.remove();
      return true;
    }, request.overlay_id);
    return { closed: removed, overlay_id: request.overlay_id };
  }
}
