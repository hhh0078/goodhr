// 文件作用：保留有界页面事件、标签页和 iframe 结构，诊断详情加载和浏览器故障，不保存响应正文或截图。
import type { Page } from "playwright-core";
import type { JsonObject } from "../../contracts/common.js";
import { safeURL } from "./navigation.js";
const records = new WeakMap<Page, { started_at: string; events: JsonObject[]; failures: JsonObject[] }>();

/** trackPageDiagnostics 为每个页面只订阅一次标准事件，限制保存数量，避免长期运行积累。 */
export function trackPageDiagnostics(page: Page): void {
  if (records.has(page)) return;
  const record = { started_at: new Date().toISOString(), events: [] as JsonObject[], failures: [] as JsonObject[] };
  records.set(page, record);
  /** remember 保存事件类型和无正文摘要，最多保留最近十二条。 */
  const remember = (fields: JsonObject): void => {
    const item = { timestamp: new Date().toISOString(), ...fields };
    record.events.push(item);
    if (record.events.length > 12) record.events.shift();
    if (fields.type === "request_failed" || fields.type === "page_error" || fields.type === "page_crash" ||
        (typeof fields.status === "number" && fields.status >= 400)) {
      record.failures.push(item);
      if (record.failures.length > 8) record.failures.shift();
    }
  };
  page.on("requestfailed", request => remember({ type: "request_failed", url: diagnosticURL(request.url()),
    resource_type: request.resourceType(), reason: request.failure()?.errorText.match(/(?:NS_|ERR_|net::)[A-Z0-9_:]+/)?.[0] ?? "request_failed" }));
  page.on("response", response => {
    const resource = response.request().resourceType();
    if (response.status() >= 400 || ["document", "xhr", "fetch"].includes(resource)) remember({
      type: "response", url: diagnosticURL(response.url()), status: response.status(), resource_type: resource,
    });
  });
  page.on("pageerror", error => {
    const source = error.stack?.match(/(https?:\/\/[^\s()]+):(\d+):(\d+)/);
    remember({ type: "page_error", error_type: ["Error", "TypeError", "SyntaxError", "ReferenceError"].includes(error.name) ? error.name : "UnknownError",
      source_url: source?.[1] ? diagnosticURL(source[1]) : null, source_line: source ? Number(source[2]) : null,
      source_column: source ? Number(source[3]) : null });
  });
  page.on("crash", () => remember({ type: "page_crash" }));
  page.on("framenavigated", frame => remember({ type: "frame_navigated", url: diagnosticURL(frame.url()) }));
}

/** pageDiagnostic 只读取标准 Page、Frame 缓存状态，页面卡住或关闭时仍能取得最后证据。 */
export function pageDiagnostic(page: Page): JsonObject {
  try {
    const frames = page.frames();
    const record = records.get(page);
    const result: JsonObject = {
      page_closed: page.isClosed(), page_url: diagnosticURL(page.url()),
      pages: page.context().pages().slice(0, 6).map(item => ({ current: item === page, closed: item.isClosed(), url: diagnosticURL(item.url()) })),
      frame_count: frames.length, frames_truncated: frames.length > 12,
      frames: frames.slice(0, 12).map(frame => ({ name: frame.name().slice(0, 48), url: diagnosticURL(frame.url()),
        parent_index: frame.parentFrame() ? frames.indexOf(frame.parentFrame()!) : -1 })),
      recording_started_at: record?.started_at ?? null, recent_page_events: record?.events.slice() ?? [],
      recent_page_failures: record?.failures.slice() ?? [],
    };
    // 上传单个 JSON 字段有大小限制，优先减少普通事件，保留失败证据和定位结构。
    for (const [key, minimum] of [["recent_page_events", 0], ["pages", 1], ["frames", 4], ["recent_page_failures", 2]] as const) {
      const values = result[key];
      if (!Array.isArray(values)) continue;
      while (Buffer.byteLength(JSON.stringify(result)) > 6000 && values.length > minimum) {
        if (key === "frames" || key === "pages") values.pop(); else values.shift();
        result.evidence_truncated = true;
      }
    }
    result.frames_truncated = frames.length > (Array.isArray(result.frames) ? result.frames.length : 0);
    return result;
  } catch { return { page_state_unavailable: true, recent_page_events: records.get(page)?.events.slice() ?? [],
    recent_page_failures: records.get(page)?.failures.slice() ?? [] }; }
}

/** diagnosticURL 只保存普通页面地址，移除敏感查询参数和内嵌页面数据。 */
function diagnosticURL(value: string): string {
  try { const url = new URL(value); url.username = ""; url.password = "";
    return /^https?:$/.test(url.protocol) ? safeURL(url.toString()).slice(0, 180) : url.protocol; }
  catch { return "unavailable"; }
}
