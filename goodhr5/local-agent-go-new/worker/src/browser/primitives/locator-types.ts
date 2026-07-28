// 文件作用说明：定义选择器原子能力内部共享的定位作用域和解析结果类型。

import type {
  FrameLocator,
  Locator,
  Page,
} from "playwright-core";
import type {
  ElementView,
  SelectorAttempt,
  SelectorCandidate,
} from "../../contracts/selector.js";

/** LocatorScope 表示 Page、Locator 和 FrameLocator 共有的定位入口。 */
export interface LocatorScope {
  locator(selector: string): Locator;
  getByText(
    text: string | RegExp,
    options?: { exact?: boolean },
  ): Locator;
  getByTestId(testId: string | RegExp): Locator;
  getByRole(
    role: Parameters<Page["getByRole"]>[0],
    options?: Parameters<Page["getByRole"]>[1],
  ): Locator;
}

/** ResolvedElement 表示原子查找得到的 Locator 和完整诊断。 */
export interface ResolvedElement {
  page: Page;
  locator: Locator;
  matched_selector: SelectorCandidate;
  attempts: SelectorAttempt[];
  view: ElementView;
}

/** FrameScope 保留 FrameLocator 类型约束，避免作用域被宽化为未知对象。 */
export type FrameScope = FrameLocator;
