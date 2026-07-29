// 文件作用说明：把 Worker 外部传入的 unknown 选择器数据校验并转换为统一 SelectorSpec。

import type {
  SelectorCandidate,
  SelectorCandidateType,
  SelectorGroup,
  SelectorSpec,
  SelectorState,
} from "../contracts/selector.js";
import { asRecord, invalidRequest, optionalNumber } from "./value.js";

const candidateTypes: SelectorCandidateType[] = [
  "css",
  "role",
  "text",
  "test_id",
];
const selectorStates: SelectorState[] = ["attached", "visible", "enabled"];

/** parseSelectorSpec 校验完整通用选择器。 */
export function parseSelectorSpec(
  value: unknown,
  traceId: string,
  action: string,
): SelectorSpec {
  const record = asRecord(value, traceId, action, "selector");
  const description =
    typeof record.description === "string" &&
    record.description.trim() !== ""
      ? record.description.trim()
      : "未命名页面元素";
  const result: SelectorSpec = {
    target: parseSelectorGroup(
      record.target,
      traceId,
      action,
      "selector.target",
    ),
    description,
  };
  const frames = parseGroupArray(
    record.frames,
    traceId,
    action,
    "selector.frames",
  );
  if (frames.length > 0) {
    result.frames = frames;
  }
  const parents = parseGroupArray(
    record.parents,
    traceId,
    action,
    "selector.parents",
  );
  if (parents.length > 0) {
    result.parents = parents;
  }
  if (
    typeof record.state === "string" &&
    selectorStates.includes(record.state as SelectorState)
  ) {
    result.state = record.state as SelectorState;
  }
  const timeout = optionalNumber(record, "timeout_ms", {
    min: 100,
    max: 120_000,
  });
  if (timeout !== undefined) {
    result.timeout_ms = timeout;
  }
  if (record.read_property === "text" || record.read_property === "html") {
    result.read_property = record.read_property;
  }
  if (
    typeof record.read_attribute === "string" &&
    record.read_attribute.trim() !== ""
  ) {
    result.read_attribute = record.read_attribute.trim();
  }
  return result;
}

/** parseGroupArray 校验 iframe 或父级选择器层级数组。 */
function parseGroupArray(
  value: unknown,
  traceId: string,
  action: string,
  field: string,
): SelectorGroup[] {
  if (value === undefined || value === null) {
    return [];
  }
  if (!Array.isArray(value)) {
    throw invalidRequest(traceId, action, `${field} 必须是数组`);
  }
  return value.map((item, index) =>
    parseSelectorGroup(item, traceId, action, `${field}[${index}]`),
  );
}

/** parseSelectorGroup 校验单个选择器层级。 */
function parseSelectorGroup(
  value: unknown,
  traceId: string,
  action: string,
  field: string,
): SelectorGroup {
  const record = asRecord(value, traceId, action, field);
  if (!Array.isArray(record.selectors) || record.selectors.length === 0) {
    throw invalidRequest(
      traceId,
      action,
      `${field}.selectors 至少需要一个候选选择器`,
    );
  }
  const group: SelectorGroup = {
    selectors: record.selectors.map((item, index) =>
      parseCandidate(
        item,
        traceId,
        action,
        `${field}.selectors[${index}]`,
      ),
    ),
  };
  const index = optionalNumber(record, "index", {
    min: 0,
    max: 100_000,
  });
  if (index !== undefined) {
    group.index = Math.floor(index);
  }
  if (typeof record.text === "string" && record.text.trim() !== "") {
    group.text = record.text.trim();
  }
  if (Array.isArray(record.texts)) {
    const texts = record.texts
      .filter((item): item is string => typeof item === "string")
      .map((item) => item.trim())
      .filter((item) => item !== "");
    if (texts.length > 0) {
      group.texts = texts;
    }
  }
  if (typeof record.exact_text === "boolean") {
    group.exact_text = record.exact_text;
  }
  const attributes = parseAttributes(record.attributes);
  if (Object.keys(attributes).length > 0) {
    group.attributes = attributes;
  }
  return group;
}

/** parseCandidate 校验一个候选选择器。 */
function parseCandidate(
  value: unknown,
  traceId: string,
  action: string,
  field: string,
): SelectorCandidate {
  if (typeof value === "string" && value.trim() !== "") {
    return { type: "css", value: value.trim() };
  }
  const record = asRecord(value, traceId, action, field);
  const type =
    typeof record.type === "string" ? record.type.trim() : "css";
  if (!candidateTypes.includes(type as SelectorCandidateType)) {
    throw invalidRequest(
      traceId,
      action,
      `${field}.type 暂不支持 ${type}`,
    );
  }
  if (typeof record.value !== "string" || record.value.trim() === "") {
    throw invalidRequest(traceId, action, `${field}.value 不能为空`);
  }
  const candidate: SelectorCandidate = {
    type: type as SelectorCandidateType,
    value: record.value.trim(),
  };
  if (typeof record.name === "string" && record.name.trim() !== "") {
    candidate.name = record.name.trim();
  }
  return candidate;
}

/** parseAttributes 读取可选的元素属性约束。 */
function parseAttributes(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  const result: Record<string, string> = {};
  for (const [key, item] of Object.entries(value)) {
    if (typeof item === "string" && key.trim() !== "") {
      result[key.trim()] = item;
    }
  }
  return result;
}
