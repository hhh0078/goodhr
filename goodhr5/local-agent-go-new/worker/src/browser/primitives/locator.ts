// 文件作用说明：提供只供封装能力调用的最小选择器解析、Locator 查询和元素状态读取能力。

import type {
  Locator,
  Page,
} from "playwright-core";
import type {
  ElementBox,
  ElementView,
  SelectorAttempt,
  SelectorCandidate,
  SelectorGroup,
  SelectorSpec,
} from "../../contracts/selector.js";
import { WorkerError } from "../../errors/worker-error.js";
import { safeURL } from "../session/navigation.js";
import type {
  FrameScope,
  LocatorScope,
  ResolvedElement,
} from "./locator-types.js";
import { ViewportPrimitive } from "./viewport.js";

export type { ResolvedElement } from "./locator-types.js";

const DEFAULT_SELECTOR_TIMEOUT_MS = 6_000;
const SELECTOR_RETRY_DELAY_MS = 300;

/** LocatorPrimitive 提供最小元素定位能力，不允许直接注册 HTTP 路由。 */
export class LocatorPrimitive {
  private readonly viewport = new ViewportPrimitive();

  /** resolve 按 iframe、父级和目标顺序解析一个元素。 */
  async resolve(
    page: Page,
    spec: SelectorSpec,
    context: {
      trace_id: string;
      action: string;
      step: string;
      require_unique: boolean;
    },
  ): Promise<ResolvedElement> {
    return this.resolveWithPolling(
      page,
      spec,
      context,
      (attempts) =>
        this.resolveOnce(
          page,
          spec,
          attempts,
          context.require_unique,
          context,
        ),
    );
  }

  /** resolveAll 查找目标层级的全部匹配元素。 */
  async resolveAll(
    page: Page,
    spec: SelectorSpec,
    maxItems: number,
    context: {
      trace_id: string;
      action: string;
      step: string;
    },
  ): Promise<ResolvedElement[]> {
    return this.resolveWithPolling(
      page,
      spec,
      context,
      (attempts) =>
        this.resolveAllOnce(
          page,
          spec,
          maxItems,
          attempts,
          context,
        ),
    );
  }

  /** resolveAllOnce 执行一次列表作用域解析、候选选择器匹配和状态过滤。 */
  private async resolveAllOnce(
    page: Page,
    spec: SelectorSpec,
    maxItems: number,
    attempts: SelectorAttempt[],
    context: {
      trace_id: string;
      action: string;
      step: string;
    },
  ): Promise<ResolvedElement[]> {
    const scope = await this.resolveScope(page, spec, attempts, context);
    for (const candidate of spec.target.selectors) {
      const locator = this.candidateLocator(scope, candidate, spec.target);
      const count = await locator.count().catch(() => 0);
      attempts.push({
        level: "target",
        selector_type: candidate.type,
        selector_value: candidate.value,
        matches: count,
        selected_index: spec.target.index ?? -1,
      });
      if (count === 0) {
        continue;
      }
      const start = spec.target.index ?? 0;
      const end =
        spec.target.index !== undefined
          ? Math.min(count, start + 1)
          : Math.min(count, Math.max(1, maxItems));
      const results: ResolvedElement[] = [];
      for (let index = start; index < end; index += 1) {
        const item = locator.nth(index);
        if (!(await this.matchesAttributes(item, spec.target))) {
          continue;
        }
        const view = await this.readView(item, spec);
        if (!(await this.matchesState(item, spec, view))) {
          continue;
        }
        results.push({
          page,
          locator: item,
          matched_selector: candidate,
          attempts: [...attempts],
          view,
        });
      }
      if (results.length > 0) {
        return results;
      }
    }
    throw this.notFound(page, spec, attempts, context);
  }

  /** resolveRelative 在已找到的父元素范围内继续解析子元素。 */
  async resolveRelative(
    page: Page,
    root: Locator,
    spec: SelectorSpec,
    context: {
      trace_id: string;
      action: string;
      step: string;
      require_unique: boolean;
    },
  ): Promise<ResolvedElement> {
    return this.resolveWithPolling(
      page,
      spec,
      context,
      (attempts) =>
        this.resolveRelativeOnce(
          page,
          root,
          spec,
          attempts,
          context,
        ),
    );
  }

  /** resolveRelativeOnce 在已找到的父元素范围内执行一次子元素解析。 */
  private async resolveRelativeOnce(
    page: Page,
    root: Locator,
    spec: SelectorSpec,
    attempts: SelectorAttempt[],
    context: {
      trace_id: string;
      action: string;
      step: string;
      require_unique: boolean;
    },
  ): Promise<ResolvedElement> {
    let scope: LocatorScope = root;
    for (const [index, parent] of (spec.parents ?? []).entries()) {
      const selectedParent = await this.selectFromGroup(
        scope,
        parent,
        `relative_parent[${index}]`,
        attempts,
        true,
        context,
      );
      scope = selectedParent.locator;
    }
    const selected = await this.selectFromGroup(
      scope,
      spec.target,
      "relative_target",
      attempts,
      context.require_unique,
      context,
    );
    const view = await this.readView(selected.locator, spec);
    if (!(await this.matchesState(selected.locator, spec, view))) {
      throw new WorkerError({
        code:
          spec.state === "enabled"
            ? "ELEMENT_NOT_ENABLED"
            : "ELEMENT_NOT_VISIBLE",
        message: `${spec.description} 找到了，但当前状态还不能使用`,
        action: context.action,
        step: context.step,
        trace_id: context.trace_id,
        retryable: true,
        details: { description: spec.description },
      });
    }
    return {
      page,
      locator: selected.locator,
      matched_selector: selected.candidate,
      attempts,
      view,
    };
  }

  /** resolveWithPolling 让全部选择器入口复用同一套超时、间隔和错误整理规则。 */
  private async resolveWithPolling<T>(
    page: Page,
    spec: SelectorSpec,
    context: { trace_id: string; action: string; step: string },
    resolveOnce: (attempts: SelectorAttempt[]) => Promise<T>,
  ): Promise<T> {
    const timeoutMS = spec.timeout_ms ?? DEFAULT_SELECTOR_TIMEOUT_MS;
    const deadline = Date.now() + timeoutMS;
    let lastAttempts: SelectorAttempt[] = [];
    let lastError: unknown;
    let pollAttempts = 0;
    while (true) {
      pollAttempts += 1;
      const attempts: SelectorAttempt[] = [];
      try {
        return await resolveOnce(attempts);
      } catch (error) {
        lastError = error;
        lastAttempts = attempts;
        if (
          error instanceof WorkerError &&
          error.code === "ELEMENT_AMBIGUOUS"
        ) {
          throw error;
        }
        const remainingMS = deadline - Date.now();
        if (remainingMS <= 0) {
          break;
        }
        await delay(Math.min(SELECTOR_RETRY_DELAY_MS, remainingMS));
      }
    }
    if (
      lastError instanceof WorkerError &&
      lastError.code !== "ELEMENT_NOT_FOUND"
    ) {
      throw new WorkerError({
        code: lastError.code,
        message: lastError.message,
        action: context.action,
        step: context.step,
        trace_id: context.trace_id,
        retryable: lastError.retryable,
        details: {
          ...lastError.details,
          description: spec.description,
          attempts: safeAttempts(lastAttempts),
          poll_attempts: pollAttempts,
          timeout_ms: timeoutMS,
          page_url: safeURL(page.url()),
        },
        cause: lastError,
      });
    }
    throw this.notFound(
      page,
      spec,
      lastAttempts,
      context,
      lastError,
      pollAttempts,
    );
  }

  /** view 读取元素位置和可见状态。 */
  async view(page: Page, locator: Locator): Promise<ElementView> {
    const visible = await locator.isVisible().catch(() => false);
    if (!visible) {
      return {
        box: { x: 0, y: 0, width: 0, height: 0 },
        viewport: { width: 0, height: 0 },
        visible: false,
        enabled: false,
        in_viewport: false,
        fully_in_viewport: false,
      };
    }
    const [box, viewport, enabled] = await Promise.all([
      locator.boundingBox().catch(() => null),
      this.viewport.size(page).catch(() => ({ width: 0, height: 0 })),
      locator.isEnabled().catch(() => false),
    ]);
    const normalizedBox: ElementBox = box
      ? {
          x: box.x,
          y: box.y,
          width: box.width,
          height: box.height,
        }
      : { x: 0, y: 0, width: 0, height: 0 };
    const right = normalizedBox.x + normalizedBox.width;
    const bottom = normalizedBox.y + normalizedBox.height;
    const inViewport =
      visible &&
      right > 0 &&
      bottom > 0 &&
      normalizedBox.x < viewport.width &&
      normalizedBox.y < viewport.height;
    const fullyInViewport =
      visible &&
      normalizedBox.x >= 0 &&
      normalizedBox.y >= 0 &&
      right <= viewport.width &&
      bottom <= viewport.height;
    return {
      box: normalizedBox,
      viewport,
      visible,
      enabled,
      in_viewport: inViewport,
      fully_in_viewport: fullyInViewport,
    };
  }

  /** box 只读取元素最新边界，供点击前的轻量稳定检查使用。 */
  async box(locator: Locator): Promise<ElementBox | null> {
    const box = await locator.boundingBox().catch(() => null);
    if (!box) {
      return null;
    }
    return {
      x: box.x,
      y: box.y,
      width: box.width,
      height: box.height,
    };
  }

  /** readView 为列表和相对字段只读取必要状态，不重复计算坐标和视口。 */
  private async readView(
    locator: Locator,
    spec: SelectorSpec,
  ): Promise<ElementView> {
    const state = spec.state ?? "visible";
    const visible =
      state === "attached"
        ? true
        : await locator.isVisible().catch(() => false);
    const enabled =
      state === "enabled"
        ? await locator.isEnabled().catch(() => false)
        : visible;
    return {
      box: { x: 0, y: 0, width: 0, height: 0 },
      viewport: { width: 0, height: 0 },
      visible,
      enabled,
      in_viewport: false,
      fully_in_viewport: false,
    };
  }

  /** resolveOnce 执行一次完整定位。 */
  private async resolveOnce(
    page: Page,
    spec: SelectorSpec,
    attempts: SelectorAttempt[],
    requireUnique: boolean,
    context: { trace_id: string; action: string; step: string },
  ): Promise<ResolvedElement> {
    const scope = await this.resolveScope(page, spec, attempts, context);
    const selected = await this.selectFromGroup(
      scope,
      spec.target,
      "target",
      attempts,
      requireUnique,
      context,
    );
    const view = await this.view(page, selected.locator);
    if (!(await this.matchesState(selected.locator, spec, view))) {
      throw new WorkerError({
        code:
          spec.state === "enabled"
            ? "ELEMENT_NOT_ENABLED"
            : "ELEMENT_NOT_VISIBLE",
        message:
          spec.state === "enabled"
            ? `${spec.description} 找到了，但现在还不能操作`
            : `${spec.description} 找到了，但现在看不见`,
        action: context.action,
        step: context.step,
        trace_id: context.trace_id,
        retryable: true,
        details: {
          description: spec.description,
          state: spec.state ?? "visible",
        },
      });
    }
    return {
      page,
      locator: selected.locator,
      matched_selector: selected.candidate,
      attempts: [...attempts],
      view,
    };
  }

  /** resolveScope 依次进入 iframe 和父级层级。 */
  private async resolveScope(
    page: Page,
    spec: SelectorSpec,
    attempts: SelectorAttempt[],
    context: { trace_id: string; action: string; step: string },
  ): Promise<LocatorScope> {
    let scope: LocatorScope = page;
    for (const [index, frame] of (spec.frames ?? []).entries()) {
      const selected = await this.selectFromGroup(
        scope,
        frame,
        `frame[${index}]`,
        attempts,
        true,
        context,
      );
      scope = selected.locator.contentFrame() as FrameScope;
    }
    for (const [index, parent] of (spec.parents ?? []).entries()) {
      const selected = await this.selectFromGroup(
        scope,
        parent,
        `parent[${index}]`,
        attempts,
        true,
        context,
      );
      scope = selected.locator;
    }
    return scope;
  }

  /** selectFromGroup 从一个层级的候选选择器中选中元素。 */
  private async selectFromGroup(
    scope: LocatorScope,
    group: SelectorGroup,
    level: string,
    attempts: SelectorAttempt[],
    requireUnique: boolean,
    context: { trace_id: string; action: string; step: string },
  ): Promise<{ locator: Locator; candidate: SelectorCandidate }> {
    for (const candidate of group.selectors) {
      const locator = this.candidateLocator(scope, candidate, group);
      const count = await locator.count().catch(() => 0);
      const selectedIndex = group.index ?? 0;
      attempts.push({
        level,
        selector_type: candidate.type,
        selector_value: candidate.value,
        matches: count,
        selected_index: selectedIndex,
      });
      if (count === 0 || selectedIndex >= count) {
        continue;
      }
      if (requireUnique && group.index === undefined && count > 1) {
        throw new WorkerError({
          code: "ELEMENT_AMBIGUOUS",
          message: "找到了多个相似元素，需要告诉我具体选第几个",
          action: context.action,
          step: context.step,
          trace_id: context.trace_id,
          retryable: false,
          details: {
            level,
            selector_type: candidate.type,
            selector_value: candidate.value,
            matches: count,
          },
        });
      }
      const selected = locator.nth(selectedIndex);
      if (!(await this.matchesAttributes(selected, group))) {
        continue;
      }
      return { locator: selected, candidate };
    }
    throw new WorkerError({
      code: "ELEMENT_NOT_FOUND",
      message: "页面里暂时没找到这个位置，我把尝试过程记下来了",
      action: context.action,
      step: context.step,
      trace_id: context.trace_id,
      retryable: true,
      details: { level },
    });
  }

  /** candidateLocator 根据选择器类型创建 Playwright Locator。 */
  private candidateLocator(
    scope: LocatorScope,
    candidate: SelectorCandidate,
    group: SelectorGroup,
  ): Locator {
    let locator: Locator;
    switch (candidate.type) {
      case "role":
        locator = scope.getByRole(
          candidate.value as Parameters<Page["getByRole"]>[0],
          group.text
            ? { name: group.text, exact: group.exact_text ?? false }
            : undefined,
        );
        break;
      case "text":
        locator = scope.getByText(candidate.value, {
          exact: group.exact_text ?? false,
        });
        break;
      case "test_id":
        locator = scope.getByTestId(candidate.value);
        break;
      case "css":
      default:
        locator = scope.locator(candidate.value);
        if (group.text) {
          locator = locator.filter({
            hasText: group.exact_text
              ? exactTextPattern(group.text)
              : group.text,
          });
        }
        break;
    }
    for (const text of group.texts ?? []) {
      locator = locator.filter({ hasText: text });
    }
    return locator;
  }

  /** matchesAttributes 检查元素属性约束。 */
  private async matchesAttributes(
    locator: Locator,
    group: SelectorGroup,
  ): Promise<boolean> {
    for (const [name, expected] of Object.entries(group.attributes ?? {})) {
      const actual = await locator.getAttribute(name).catch(() => null);
      if (actual !== expected) {
        return false;
      }
    }
    return true;
  }

  /** matchesState 检查目标状态要求。 */
  private async matchesState(
    locator: Locator,
    spec: SelectorSpec,
    view: ElementView,
  ): Promise<boolean> {
    switch (spec.state ?? "visible") {
      case "attached":
        return (await locator.count().catch(() => 0)) > 0;
      case "enabled":
        return view.visible && view.enabled;
      case "visible":
      default:
        return view.visible;
    }
  }

  /** notFound 创建带完整尝试记录的元素未找到错误。 */
  private notFound(
    page: Page,
    spec: SelectorSpec,
    attempts: SelectorAttempt[],
    context: { trace_id: string; action: string; step: string },
    cause?: unknown,
    pollAttempts?: number,
  ): WorkerError {
    return new WorkerError({
      code: "ELEMENT_NOT_FOUND",
      message: `${spec.description} 暂时没找到，我已经把尝试过程记下来了`,
      action: context.action,
      step: context.step,
      trace_id: context.trace_id,
      retryable: true,
      details: {
        description: spec.description,
        page_url: safeURL(page.url()),
        state: spec.state ?? "visible",
        timeout_ms: spec.timeout_ms ?? DEFAULT_SELECTOR_TIMEOUT_MS,
        poll_attempts: pollAttempts ?? 1,
        attempts: safeAttempts(attempts),
      },
      cause,
    });
  }
}

/** exactTextPattern 创建允许首尾空白、但不允许额外正文的精确文本表达式。 */
function exactTextPattern(value: string): RegExp {
  const escaped = value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`^\\s*${escaped}\\s*$`, "u");
}

/** safeAttempts 把内部选择器尝试记录转换成可安全返回的 JSON 字段。 */
function safeAttempts(attempts: SelectorAttempt[]) {
  return attempts.map((item) => ({
    level: item.level,
    selector_type: item.selector_type,
    selector_value: item.selector_value,
    matches: item.matches,
    selected_index: item.selected_index,
  }));
}

/** delay 使用 Node 定时器等待，避免发送浏览器内等待指令。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
