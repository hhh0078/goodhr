// 文件作用说明：定义所有元素查找、点击、输入、滚动和截图共用的强类型选择器协议。

/** SelectorCandidateType 表示支持的选择器种类。 */
export type SelectorCandidateType = "css" | "role" | "text" | "test_id";

/** SelectorCandidate 表示一个可按顺序尝试的候选选择器。 */
export interface SelectorCandidate {
  type: SelectorCandidateType;
  value: string;
  name?: string;
}

/** SelectorGroup 表示一个层级的候选选择器、序号和附加匹配条件。 */
export interface SelectorGroup {
  selectors: SelectorCandidate[];
  index?: number;
  text?: string;
  exact_text?: boolean;
  attributes?: Record<string, string>;
}

/** SelectorState 表示目标元素必须满足的状态。 */
export type SelectorState = "attached" | "visible" | "enabled";

/** SelectorSpec 表示从 iframe、父级到目标元素的完整通用定位方案。 */
export interface SelectorSpec {
  frames?: SelectorGroup[];
  parents?: SelectorGroup[];
  target: SelectorGroup;
  state?: SelectorState;
  timeout_ms?: number;
  description: string;
}

/** ElementBox 表示元素在当前页面视口内的位置。 */
export interface ElementBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** ViewportSize 表示浏览器内容视口尺寸。 */
export interface ViewportSize {
  width: number;
  height: number;
}

/** ElementView 表示元素的可见、可用和视口状态。 */
export interface ElementView {
  box: ElementBox;
  viewport: ViewportSize;
  visible: boolean;
  enabled: boolean;
  in_viewport: boolean;
  fully_in_viewport: boolean;
}

/** SelectorAttempt 表示一次选择器尝试的诊断结果。 */
export interface SelectorAttempt {
  level: string;
  selector_type: SelectorCandidateType;
  selector_value: string;
  matches: number;
  selected_index: number;
}

/** FindResult 表示封装查找能力返回的元素引用和诊断信息。 */
export interface FindResult {
  element_ref: string;
  description: string;
  matched_selector: SelectorCandidate;
  attempts: SelectorAttempt[];
  view: ElementView;
  page_id: string;
  page_url: string;
}
