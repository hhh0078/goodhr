// 文件作用说明：校验封装点击完成后的 URL、元素显示和元素隐藏验证条件。

import type { ClickVerification } from "../contracts/actions.js";
import { parseSelectorSpec } from "./selector.js";
import {
  asRecord,
  optionalNumber,
  optionalString,
} from "./value.js";

/** parseClickVerification 校验点击后的 URL 和元素状态验证。 */
export function parseClickVerification(
  value: unknown,
  traceId: string,
  action: string,
): ClickVerification {
  const record = asRecord(value, traceId, action, "verify");
  const result: ClickVerification = {};
  const urlContains = optionalString(record, "url_contains");
  if (urlContains !== undefined) {
    result.url_contains = urlContains;
  }
  if (record.target_hidden !== undefined) {
    result.target_hidden = parseSelectorSpec(
      record.target_hidden,
      traceId,
      action,
    );
  }
  if (record.target_visible !== undefined) {
    result.target_visible = parseSelectorSpec(
      record.target_visible,
      traceId,
      action,
    );
  }
  const timeout = optionalNumber(record, "timeout_ms", {
    min: 100,
    max: 120_000,
  });
  if (timeout !== undefined) {
    result.timeout_ms = timeout;
  }
  return result;
}
