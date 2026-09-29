// 文件作用：使用标准 Page、Frame 和 Locator 能力收集无正文的页面、定位和点击遮挡证据，供岗位日志上传。
import type { Locator } from "playwright-core";
import type { JsonObject } from "../../contracts/common.js";
import type { SelectorGroup, SelectorSpec } from "../../contracts/selector.js";
export { pageDiagnostic } from "../session/page-diagnostics.js";

/** selectorDiagnostic 记录定位层级和序号，文本过滤条件只保留数量，避免保存候选人姓名。 */
export function selectorDiagnostic(spec: SelectorSpec): JsonObject {
  return { description: spec.description, state: spec.state ?? "visible", timeout_ms: spec.timeout_ms ?? 6000,
    frames: (spec.frames ?? []).map(groupDiagnostic), frame_fallbacks: (spec.frame_fallbacks ?? []).map(path => path.map(groupDiagnostic)),
    parents: (spec.parents ?? []).map(groupDiagnostic), target: groupDiagnostic(spec.target) };
}

/** clickActionability 只读取可见和可用状态，原生浏览器的 trial 点击可能发送真实输入，诊断阶段禁止调用。 */
export async function clickActionability(locator: Locator): Promise<JsonObject> {
  const started = Date.now();
  try {
    const [visible, enabled] = await Promise.all([locator.isVisible(), locator.isEnabled({ timeout: 800 })]);
    return { actionable: visible && enabled ? null : false, visible, enabled, elapsed_ms: Date.now() - started,
      method: "readonly_locator_state", reason: !visible ? "not_visible" : !enabled ? "not_enabled" : "occlusion_not_checked" };
  } catch {
    return { actionable: null, elapsed_ms: Date.now() - started, method: "readonly_locator_state", reason: "element_state_unavailable" };
  }
}

/** groupDiagnostic 输出结构选择器，隐藏文字选择器及属性过滤的实际值。 */
function groupDiagnostic(group: SelectorGroup): JsonObject {
  return { selectors: group.selectors.map(selector => ({ type: selector.type, value: selector.type === "text" ? "[文字已隐藏]" : selector.value })),
    index: group.index ?? null, text_filter_count: (group.text ? 1 : 0) + (group.texts?.length ?? 0),
    attribute_filter_names: Object.keys(group.attributes ?? {}) };
}
