// 文件作用说明：注册 Worker 通用封装能力路由，完成 unknown 校验、Trace ID 和统一响应。

import { randomUUID } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { ActionService } from "../browser/actions/action-service.js";
import type { ActionContext, JsonValue } from "../contracts/common.js";
import {
  normalizeWorkerError,
  safeJsonValue,
  WorkerError,
} from "../errors/worker-error.js";
import {
  parseBrowserStartRequest,
  parseCookieSetRequest,
  parseDownloadConfigureRequest,
  parseElementClickRequest,
  parseElementFindAllRequest,
  parseElementFindRequest,
  parseElementInputRequest,
  parseElementReadRequest,
  parseKeyboardPressRequest,
  parseLongScreenshotRequest,
  parseOverlayCloseRequest,
  parseOverlayShowRequest,
  parsePageOpenRequest,
  parsePageUseRequest,
  parseScreenshotRequest,
  parseScrollRequest,
} from "../validation/action-requests.js";
import { readJSONBody } from "./body.js";

type RouteHandler = (
  body: unknown,
  context: ActionContext,
) => Promise<unknown> | unknown;

interface Route {
  method: "GET" | "POST";
  path: string;
  action: string;
  handler: RouteHandler;
}

/** WorkerRouter 把固定 HTTP 路由映射到强类型封装能力。 */
export class WorkerRouter {
  private readonly routes: Route[];

  /** 创建 Worker 路由并绑定封装 Action，不暴露 Primitive。 */
  constructor(private readonly actions = new ActionService()) {
    this.routes = this.createRoutes();
  }

  /** handle 处理一次请求并统一捕获所有边界异常。 */
  async handle(
    request: IncomingMessage,
    response: ServerResponse,
  ): Promise<void> {
    const traceID = request.headers["x-trace-id"]?.toString() || randomUUID();
    const method = request.method === "GET" ? "GET" : "POST";
    const pathname = request.url ? new URL(request.url, "http://127.0.0.1").pathname : "/";
    const route = this.routes.find(
      (item) => item.method === method && item.path === pathname,
    );
    if (!route) {
      this.writeJSON(response, 404, {
        ok: false,
        error: {
          code: "INVALID_REQUEST",
          message: "这个 Worker 地址不存在",
          action: "route",
          step: "match_route",
          trace_id: traceID,
          retryable: false,
          details: { method, path: pathname },
        },
        trace_id: traceID,
      });
      return;
    }
    const context: ActionContext = {
      trace_id: traceID,
      action: route.action,
      started_at: Date.now(),
    };
    try {
      const body =
        route.method === "POST"
          ? await readJSONBody(request, traceID, route.action)
          : {};
      const result = await route.handler(body, context);
      this.writeJSON(response, 200, {
        ok: true,
        data: safeJsonValue(result),
        trace_id: traceID,
      });
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: route.action,
        step: "handle_request",
        trace_id: traceID,
      });
      this.writeJSON(response, statusForError(normalized), {
        ok: false,
        error: normalized.toBody(),
        trace_id: traceID,
      });
    }
  }

  /** createRoutes 创建全部通用 Worker 路由。 */
  private createRoutes(): Route[] {
    return [
      this.get("/health", "health", () => ({ status: "ok", version: "v1" })),
      this.post("/api/v1/browser/start", "browser.start", (body, context) =>
        this.actions.startBrowser(
          parseBrowserStartRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/browser/stop", "browser.stop", (_body, context) =>
        this.actions.stopBrowser(context),
      ),
      this.get("/api/v1/browser/status", "browser.status", () =>
        this.actions.browserStatus(),
      ),
      this.get("/api/v1/runtime/status", "runtime.status", () =>
        this.actions.runtimeStatus(),
      ),
      this.post("/api/v1/page/open", "page.open", (body, context) =>
        this.actions.openPage(
          parsePageOpenRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.get("/api/v1/page/list", "page.list", () =>
        this.actions.listPages(),
      ),
      this.post("/api/v1/page/use", "page.use", (body, context) =>
        this.actions.usePage(
          parsePageUseRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/page/close", "page.close", (_body, context) =>
        this.actions.closePage(context),
      ),
      this.get("/api/v1/page/url", "page.url", () =>
        this.actions.currentURL(),
      ),
      this.post("/api/v1/element/find", "element.find", (body, context) =>
        this.actions.findElement(
          parseElementFindRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/element/find-all", "element.find_all", (body, context) =>
        this.actions.findElements(
          parseElementFindAllRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/element/read", "element.read", (body, context) =>
        this.actions.readElement(
          parseElementReadRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/element/click", "element.click", (body, context) =>
        this.actions.clickElement(
          parseElementClickRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/element/input", "element.input", (body, context) =>
        this.actions.inputElement(
          parseElementInputRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/page/scroll", "page.scroll", (body, context) =>
        this.actions.scroll(
          parseScrollRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/element/scroll", "element.scroll", (body, context) =>
        this.actions.scroll(
          parseScrollRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/keyboard/press", "keyboard.press", (body, context) =>
        this.actions.pressKey(
          parseKeyboardPressRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/page/screenshot", "page.screenshot", (body, context) =>
        this.actions.screenshot(
          parseScreenshotRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post(
        "/api/v1/element/screenshot",
        "element.screenshot",
        (body, context) =>
          this.actions.screenshot(
            parseScreenshotRequest(body, context.trace_id, context.action),
            context,
          ),
      ),
      this.post(
        "/api/v1/element/screenshot-long",
        "element.screenshot_long",
        (body, context) =>
          this.actions.screenshotLong(
            parseLongScreenshotRequest(
              body,
              context.trace_id,
              context.action,
            ),
            context,
          ),
      ),
      this.get("/api/v1/cookies", "cookies.list", (_body, context) =>
        this.actions.listCookies(context),
      ),
      this.post("/api/v1/cookies", "cookies.set", (body, context) =>
        this.actions.setCookies(
          parseCookieSetRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.get("/api/v1/downloads", "downloads.list", () =>
        this.actions.listDownloads(),
      ),
      this.post("/api/v1/downloads/configure", "downloads.configure", (body, context) =>
        this.actions.configureDownloads(
          parseDownloadConfigureRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/downloads/clear", "downloads.clear", () =>
        this.actions.clearDownloads(),
      ),
      this.post("/api/v1/overlay/show", "overlay.show", (body, context) =>
        this.actions.showOverlay(
          parseOverlayShowRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
      this.post("/api/v1/overlay/close", "overlay.close", (body, context) =>
        this.actions.closeOverlay(
          parseOverlayCloseRequest(body, context.trace_id, context.action),
          context,
        ),
      ),
    ];
  }

  /** get 创建 GET 路由。 */
  private get(path: string, action: string, handler: RouteHandler): Route {
    return { method: "GET", path, action, handler };
  }

  /** post 创建 POST 路由。 */
  private post(path: string, action: string, handler: RouteHandler): Route {
    return { method: "POST", path, action, handler };
  }

  /** writeJSON 写入统一 JSON 响应。 */
  private writeJSON(
    response: ServerResponse,
    status: number,
    payload: JsonValue,
  ): void {
    response.writeHead(status, {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
    });
    response.end(JSON.stringify(payload));
  }
}

/** statusForError 把稳定错误码映射为 HTTP 状态码。 */
function statusForError(error: WorkerError): number {
  if (error.code === "INVALID_REQUEST") {
    return 400;
  }
  if (
    error.code === "BROWSER_NOT_RUNNING" ||
    error.code === "PAGE_NOT_AVAILABLE" ||
    error.code === "PAGE_CLOSED"
  ) {
    return 409;
  }
  if (error.code === "ELEMENT_NOT_FOUND") {
    return 404;
  }
  if (error.code === "ACTION_TIMEOUT") {
    return 504;
  }
  return 500;
}
