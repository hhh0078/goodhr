// 文件作用说明：校验并构造 Browser Worker 各封装能力的强类型请求，禁止 unknown 直接进入动作层。

import type { Cookie } from "playwright-core";
import type {
  BrowserStartRequest,
  CookieSetRequest,
  ElementClickRequest,
  ElementFindAllRequest,
  ElementFindRequest,
  ElementInputRequest,
  ElementReadRequest,
  KeyboardPressRequest,
  OverlayCloseRequest,
  OverlayShowRequest,
  PageOpenRequest,
  PageUseRequest,
  ScreenshotRequest,
  ScrollRequest,
} from "../contracts/actions.js";
import type { SelectorSpec } from "../contracts/selector.js";
import { parseClickVerification } from "./click-verification.js";
import { parseSelectorSpec } from "./selector.js";
import {
  asRecord,
  invalidRequest,
  optionalBoolean,
  optionalNumber,
  optionalString,
  requiredString,
  stringArray,
} from "./value.js";

/** parseBrowserStartRequest 校验浏览器启动请求。 */
export function parseBrowserStartRequest(
  value: unknown,
  traceId: string,
  action: string,
): BrowserStartRequest {
  const record = asRecord(value, traceId, action);
  const request: BrowserStartRequest = {};
  assignString(request, "user_data_dir", optionalString(record, "user_data_dir"));
  assignString(request, "downloads_path", optionalString(record, "downloads_path"));
  assignString(request, "url", optionalString(record, "url"));
  assignString(request, "locale", optionalString(record, "locale"));
  assignString(request, "timezone", optionalString(record, "timezone"));
  assignBoolean(request, "headless", optionalBoolean(record, "headless"));
  assignBoolean(request, "humanize", optionalBoolean(record, "humanize"));
  const args = stringArray(record.args);
  if (args.length > 0) {
    request.args = args;
  }
  if (typeof record.proxy === "string" && record.proxy.trim()) {
    request.proxy = record.proxy.trim();
  } else if (
    record.proxy &&
    typeof record.proxy === "object" &&
    !Array.isArray(record.proxy)
  ) {
    const proxy = record.proxy as Record<string, unknown>;
    const server = requiredString(proxy, "server", traceId, action);
    const parsedProxy: Exclude<BrowserStartRequest["proxy"], string | undefined> = {
      server,
    };
    const bypass = optionalString(proxy, "bypass");
    const username = optionalString(proxy, "username");
    const password = optionalString(proxy, "password");
    if (bypass !== undefined) {
      parsedProxy.bypass = bypass;
    }
    if (username !== undefined) {
      parsedProxy.username = username;
    }
    if (password !== undefined) {
      parsedProxy.password = password;
    }
    request.proxy = parsedProxy;
  }
  return request;
}

/** parsePageOpenRequest 校验打开页面请求。 */
export function parsePageOpenRequest(
  value: unknown,
  traceId: string,
  action: string,
): PageOpenRequest {
  const record = asRecord(value, traceId, action);
  const request: PageOpenRequest = {
    url: requiredString(record, "url", traceId, action),
  };
  const waitUntil = optionalString(record, "wait_until");
  if (
    waitUntil === "load" ||
    waitUntil === "domcontentloaded" ||
    waitUntil === "networkidle" ||
    waitUntil === "commit"
  ) {
    request.wait_until = waitUntil;
  }
  assignNumber(
    request,
    "timeout_ms",
    optionalNumber(record, "timeout_ms", { min: 100, max: 120_000 }),
  );
  assignBoolean(request, "new_tab", optionalBoolean(record, "new_tab"));
  return request;
}

/** parsePageUseRequest 校验切换页面请求。 */
export function parsePageUseRequest(
  value: unknown,
  traceId: string,
  action: string,
): PageUseRequest {
  const record = asRecord(value, traceId, action);
  return {
    page_id: requiredString(record, "page_id", traceId, action),
  };
}

/** parseElementFindRequest 校验单元素查找请求。 */
export function parseElementFindRequest(
  value: unknown,
  traceId: string,
  action: string,
): ElementFindRequest {
  const record = asRecord(value, traceId, action);
  return {
    selector: parseSelectorSpec(record.selector, traceId, action),
  };
}

/** parseElementFindAllRequest 校验列表查找和字段请求。 */
export function parseElementFindAllRequest(
  value: unknown,
  traceId: string,
  action: string,
): ElementFindAllRequest {
  const record = asRecord(value, traceId, action);
  const request: ElementFindAllRequest = {
    selector: parseSelectorSpec(record.selector, traceId, action),
  };
  assignNumber(
    request,
    "max_items",
    optionalNumber(record, "max_items", { min: 1, max: 500 }),
  );
  const fields = parseFieldSelectors(record.fields, traceId, action);
  if (Object.keys(fields).length > 0) {
    request.fields = fields;
  }
  return request;
}

/** parseElementReadRequest 校验元素读取请求。 */
export function parseElementReadRequest(
  value: unknown,
  traceId: string,
  action: string,
): ElementReadRequest {
  const record = asRecord(value, traceId, action);
  const request: ElementReadRequest = {
    selector: parseSelectorSpec(record.selector, traceId, action),
  };
  if (record.property === "html" || record.property === "text") {
    request.property = record.property;
  }
  assignString(request, "attribute", optionalString(record, "attribute"));
  return request;
}

/** parseElementClickRequest 校验封装点击请求。 */
export function parseElementClickRequest(
  value: unknown,
  traceId: string,
  action: string,
): ElementClickRequest {
  const record = asRecord(value, traceId, action);
  const request: ElementClickRequest = {
    selector: parseSelectorSpec(record.selector, traceId, action),
  };
  if (
    record.button === "left" ||
    record.button === "right" ||
    record.button === "middle"
  ) {
    request.button = record.button;
  }
  assignNumber(
    request,
    "click_count",
    optionalNumber(record, "click_count", { min: 1, max: 3 }),
  );
  assignNumber(
    request,
    "viewport_margin",
    optionalNumber(record, "viewport_margin", { min: 0, max: 500 }),
  );
  if (record.verify !== undefined) {
    request.verify = parseClickVerification(
      record.verify,
      traceId,
      action,
    );
  }
  return request;
}

/** parseElementInputRequest 校验封装输入请求。 */
export function parseElementInputRequest(
  value: unknown,
  traceId: string,
  action: string,
): ElementInputRequest {
  const record = asRecord(value, traceId, action);
  const request: ElementInputRequest = {
    selector: parseSelectorSpec(record.selector, traceId, action),
    text:
      typeof record.text === "string"
        ? record.text
        : (() => {
            throw invalidRequest(traceId, action, "text 必须是字符串");
          })(),
  };
  assignBoolean(request, "clear", optionalBoolean(record, "clear"));
  assignBoolean(request, "verify", optionalBoolean(record, "verify"));
  assignNumber(
    request,
    "min_delay_ms",
    optionalNumber(record, "min_delay_ms", { min: 0, max: 5_000 }),
  );
  assignNumber(
    request,
    "max_delay_ms",
    optionalNumber(record, "max_delay_ms", { min: 0, max: 5_000 }),
  );
  return request;
}

/** parseKeyboardPressRequest 校验按键请求。 */
export function parseKeyboardPressRequest(
  value: unknown,
  traceId: string,
  action: string,
): KeyboardPressRequest {
  const record = asRecord(value, traceId, action);
  const request: KeyboardPressRequest = {
    key: requiredString(record, "key", traceId, action),
  };
  assignNumber(
    request,
    "delay_ms",
    optionalNumber(record, "delay_ms", { min: 0, max: 10_000 }),
  );
  return request;
}

/** parseScrollRequest 校验真实滚轮请求。 */
export function parseScrollRequest(
  value: unknown,
  traceId: string,
  action: string,
): ScrollRequest {
  const record = asRecord(value, traceId, action);
  const distance = optionalNumber(record, "distance", {
    min: -10_000,
    max: 10_000,
  });
  if (distance === undefined || distance === 0) {
    throw invalidRequest(traceId, action, "distance 不能为 0");
  }
  const request: ScrollRequest = { distance };
  if (record.target !== undefined) {
    request.target = parseSelectorSpec(record.target, traceId, action);
  }
  if (record.wheel_anchor !== undefined) {
    request.wheel_anchor = parseSelectorSpec(
      record.wheel_anchor,
      traceId,
      action,
    );
  }
  assignNumber(
    request,
    "max_attempts",
    optionalNumber(record, "max_attempts", { min: 1, max: 100 }),
  );
  assignNumber(
    request,
    "wait_ms",
    optionalNumber(record, "wait_ms", { min: 0, max: 30_000 }),
  );
  assignNumber(
    request,
    "viewport_margin",
    optionalNumber(record, "viewport_margin", { min: 0, max: 500 }),
  );
  assignBoolean(
    request,
    "require_full",
    optionalBoolean(record, "require_full"),
  );
  return request;
}

/** parseScreenshotRequest 校验截图请求。 */
export function parseScreenshotRequest(
  value: unknown,
  traceId: string,
  action: string,
): ScreenshotRequest {
  const record = asRecord(value, traceId, action);
  const request: ScreenshotRequest = {
    directory: requiredString(record, "directory", traceId, action),
    filename: requiredString(record, "filename", traceId, action),
  };
  if (record.target !== undefined) {
    request.target = parseSelectorSpec(record.target, traceId, action);
  }
  assignBoolean(
    request,
    "full_page",
    optionalBoolean(record, "full_page"),
  );
  return request;
}

/** parseCookieSetRequest 校验 Cookie 导入请求。 */
export function parseCookieSetRequest(
  value: unknown,
  traceId: string,
  action: string,
): CookieSetRequest {
  const record = asRecord(value, traceId, action);
  if (!Array.isArray(record.cookies)) {
    throw invalidRequest(traceId, action, "cookies 必须是数组");
  }
  return {
    cookies: record.cookies.map((item, index) =>
      parseCookie(item, traceId, action, index),
    ),
  };
}

/** parseOverlayShowRequest 校验浮层显示请求。 */
export function parseOverlayShowRequest(
  value: unknown,
  traceId: string,
  action: string,
): OverlayShowRequest {
  const record = asRecord(value, traceId, action);
  const request: OverlayShowRequest = {
    overlay_id: requiredString(record, "overlay_id", traceId, action),
    title: requiredString(record, "title", traceId, action),
    message: requiredString(record, "message", traceId, action),
  };
  assignString(request, "subtitle", optionalString(record, "subtitle"));
  if (
    record.level === "info" ||
    record.level === "success" ||
    record.level === "warning" ||
    record.level === "error"
  ) {
    request.level = record.level;
  }
  assignNumber(
    request,
    "max_age_ms",
    optionalNumber(record, "max_age_ms", { min: 0, max: 600_000 }),
  );
  return request;
}

/** parseOverlayCloseRequest 校验关闭浮层请求。 */
export function parseOverlayCloseRequest(
  value: unknown,
  traceId: string,
  action: string,
): OverlayCloseRequest {
  const record = asRecord(value, traceId, action);
  return {
    overlay_id: requiredString(record, "overlay_id", traceId, action),
  };
}

/** parseFieldSelectors 读取列表项字段选择器。 */
function parseFieldSelectors(
  value: unknown,
  traceId: string,
  action: string,
): Record<string, SelectorSpec> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  const result: Record<string, SelectorSpec> = {};
  for (const [fieldName, fieldSelector] of Object.entries(value)) {
    if (fieldName.trim()) {
      result[fieldName] = parseSelectorSpec(
        fieldSelector,
        traceId,
        action,
      );
    }
  }
  return result;
}

/** parseCookie 校验一个 Playwright Cookie。 */
function parseCookie(
  value: unknown,
  traceId: string,
  action: string,
  index: number,
): Cookie {
  const record = asRecord(
    value,
    traceId,
    action,
    `cookies[${index}]`,
  );
  const cookie: Cookie = {
    name: requiredString(record, "name", traceId, action),
    value:
      typeof record.value === "string"
        ? record.value
        : (() => {
            throw invalidRequest(
              traceId,
              action,
              `cookies[${index}].value 必须是字符串`,
            );
          })(),
    domain: optionalString(record, "domain") ?? "",
    path: optionalString(record, "path") ?? "/",
    expires: optionalNumber(record, "expires") ?? -1,
    httpOnly: optionalBoolean(record, "httpOnly") ?? false,
    secure: optionalBoolean(record, "secure") ?? false,
    sameSite:
      record.sameSite === "Strict" ||
      record.sameSite === "Lax" ||
      record.sameSite === "None"
        ? record.sameSite
        : "Lax",
  };
  return cookie;
}

/** assignString 只在值存在时写入可选字符串。 */
function assignString<T extends object, K extends keyof T>(
  target: T,
  key: K,
  value: T[K] | undefined,
): void {
  if (value !== undefined) {
    target[key] = value;
  }
}

/** assignBoolean 只在值存在时写入可选布尔值。 */
function assignBoolean<T extends object, K extends keyof T>(
  target: T,
  key: K,
  value: T[K] | undefined,
): void {
  if (value !== undefined) {
    target[key] = value;
  }
}

/** assignNumber 只在值存在时写入可选数字。 */
function assignNumber<T extends object, K extends keyof T>(
  target: T,
  key: K,
  value: T[K] | undefined,
): void {
  if (value !== undefined) {
    target[key] = value;
  }
}
