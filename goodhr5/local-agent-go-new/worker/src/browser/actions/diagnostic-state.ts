// 文件作用：使用标准 Page、Frame 和 Locator 能力收集无正文的页面、定位和点击遮挡证据，供岗位日志上传。
import type { Locator } from "playwright-core";
import type { JsonObject } from "../../contracts/common.js";
import type { SelectorGroup, SelectorSpec } from "../../contracts/selector.js";
export { pageDiagnostic } from "../session/page-diagnostics.js";

/** selectorDiagnostic 记录定位层级和序号，文本过滤条件只保留数量，避免保存候选人姓名。 */
export function selectorDiagnostic(spec: SelectorSpec): JsonObject {
  return { description: spec.description, state: spec.state ?? "visible", timeout_ms: spec.timeout_ms ?? 6000,
    frames: (spec.frames ?? []).map(groupDiagnostic), parents: (spec.parents ?? []).map(groupDiagnostic), target: groupDiagnostic(spec.target) };
}

/** clickActionability 通过标准 trial 点击检查遮挡，不发送实际点击，也不返回包含简历正文的错误消息。 */
export async function clickActionability(locator: Locator): Promise<JsonObject> {
  const started = Date.now();
  try {
    await locator.click({ trial: true, timeout: 800 });
    return { actionable: true, elapsed_ms: Date.now() - started };
  } catch (error) {
    const message = error instanceof Error ? error.message : "";
    const intercepted = /intercepts pointer events/i.test(message);
    const blockers = message.split("\n").filter(line => /intercepts pointer events/i.test(line)).slice(-3).map(line => {
      const tag = line.match(/<([a-z][a-z0-9-]*)[\s>]/i)?.[1] ?? "unknown";
      const cssClass = line.match(/class=["']([a-z0-9_\-\s]+)["']/i)?.[1] ?? "";
      return { tag, css_class: cssClass.slice(0, 200) };
    });
    return { actionable: intercepted || /not visible|not enabled|not stable/i.test(message) ? false : null, elapsed_ms: Date.now() - started,
      reason: intercepted ? "pointer_intercepted" : /not visible/i.test(message) ? "not_visible" :
        /not enabled/i.test(message) ? "not_enabled" : /not stable/i.test(message) ? "not_stable" : "trial_timeout_or_page_unavailable",
      blockers };
  }
}

/** groupDiagnostic 输出结构选择器，隐藏文字选择器及属性过滤的实际值。 */
function groupDiagnostic(group: SelectorGroup): JsonObject {
  return { selectors: group.selectors.map(selector => ({ type: selector.type, value: selector.type === "text" ? "[文字已隐藏]" : selector.value })),
    index: group.index ?? null, text_filter_count: (group.text ? 1 : 0) + (group.texts?.length ?? 0),
    attribute_filter_names: Object.keys(group.attributes ?? {}) };
}
