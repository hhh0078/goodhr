// 文件作用说明：实现 Go 可调用的封装查找能力，并统一生成元素引用、状态和详细查找日志。

import type { Locator } from "playwright-core";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import type { FindResult, SelectorSpec } from "../../contracts/selector.js";
import { normalizeWorkerError, WorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import {
  LocatorPrimitive,
  type ResolvedElement,
} from "../primitives/locator.js";
import { ReadPrimitive } from "../primitives/read.js";
import { BrowserSession } from "../session/browser-session.js";
import { safeURL } from "../session/navigation.js";
import { pageDiagnostic, selectorDiagnostic } from "./diagnostic-state.js";

/** FoundElement 保存封装查找结果和仅供后续封装能力复用的 Locator。 */
export interface FoundElement {
  resolved: ResolvedElement;
  result: FindResult;
}

/** FindAllItem 表示列表查找中的一个元素和字段。 */
export interface FindAllItem extends JsonObject {
  index: number;
  element_ref: string;
  text: string;
  fields: JsonObject;
}

/** FindAction 实现统一选择器查找能力。 */
export class FindAction {
  private readonly readPrimitive = new ReadPrimitive();

  /** 创建封装查找能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly primitive: LocatorPrimitive,
    private readonly logger: WorkerLogger,
  ) {}

  /** one 查找一个元素并生成短生命周期引用。 */
  async one(
    spec: SelectorSpec,
    actionContext: ActionContext,
    requireUnique = false,
    logFailure = true,
  ): Promise<FoundElement> {
    const step = "find";
    this.logger.info(actionContext, step, "start", {
      target_description: spec.description,
      candidates: spec.target.selectors.length,
      parent_levels: spec.parents?.length ?? 0,
      frame_levels: spec.frames?.length ?? 0,
    });
    try {
      const page = await this.session.requirePage(actionContext, step);
      const resolved = await this.primitive.resolve(page, spec, {
        trace_id: actionContext.trace_id,
        action: actionContext.action,
        step,
        require_unique: requireUnique,
      });
      const elementRef = this.session.elements.remember(
        page,
        resolved.locator,
      );
      const pages = await this.session.listPages();
      const current = pages.pages.find((item) => item.current);
      const result: FindResult = {
        element_ref: elementRef,
        description: spec.description,
        matched_selector: resolved.matched_selector,
        attempts: resolved.attempts,
        view: resolved.view,
        page_id: current?.page_id ?? "0",
        page_url: page.url(),
      };
      this.logger.info(actionContext, step, "success", {
        target_description: spec.description,
        matched_selector: resolved.matched_selector.value,
        matches_checked: resolved.attempts.length,
        in_viewport: resolved.view.in_viewport,
      });
      return { resolved, result };
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "ELEMENT_NOT_FOUND",
        message: `${spec.description} 暂时没找到，我已经把尝试过程记下来了`,
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { target_description: spec.description },
      });
      if (logFailure) {
        this.logger.failure(actionContext, normalized);
      }
      throw normalized;
    }
  }

  /** all 查找列表元素并按每个元素作用域提取强类型字段。 */
  async all(
    spec: SelectorSpec,
    maxItems: number,
    fields: Record<string, SelectorSpec>,
    actionContext: ActionContext,
    report = true,
    readConcurrency = 100,
    startIndex = 0,
    diagnostic = { diagnostic: false, attempt: 0, probe_only: false },
  ): Promise<FindAllItem[]> {
    const step = "find_all";
    const diagnosticInput: JsonObject = diagnostic.diagnostic ? { selector: selectorDiagnostic(spec), attempt: diagnostic.attempt,
      expected_missing: !report, probe_only: diagnostic.probe_only } : {};
    if (diagnostic.diagnostic) this.logger.info(actionContext, "selector_probe", "start", { diagnostic: true, input: diagnosticInput });
    if (report) {
      this.logger.info(actionContext, step, "start", {
        target_description: spec.description,
        max_items: maxItems,
        start_index: startIndex,
        field_count: Object.keys(fields).length,
      });
    }
    try {
      const page = await this.session.requirePage(actionContext, step);
      const resolvedItems = await this.primitive.resolveAll(
        page,
        spec,
        maxItems,
        {
          trace_id: actionContext.trace_id,
          action: actionContext.action,
          step,
        },
        startIndex,
      );
      if (diagnostic.diagnostic) this.logger.info(actionContext, "selector_probe", "success", {
        diagnostic: true, input: diagnosticInput, output: { count: resolvedItems.length,
          selector_attempts: this.logger.fromUnknown(resolvedItems[0]?.attempts ?? []).value ?? [], page_state: pageDiagnostic(page) },
      });
      if (diagnostic.probe_only) return resolvedItems.map((resolved, index) => ({
        index: resolved.list_index ?? index + startIndex, element_ref: "", text: "", fields: {},
      }));
      const diagnoseNames = report && Object.hasOwn(fields, "name");
      // 限制整批同时进行的页面读取，避免每张卡片的多个字段把浏览器命令队列挤满。
      const readWithSlot = createReadLimiter(Math.min(readConcurrency, 4));
      if (diagnoseNames) {
        this.logger.info(actionContext, "list_name_diagnostics", "resolved", {
          page_url: safeURL(page.url()),
          resolved_count: resolvedItems.length,
          field_count: Object.keys(fields).length,
          read_concurrency: readConcurrency,
          field_read_concurrency: Math.min(readConcurrency, 4),
          matched_selector: resolvedItems[0]?.matched_selector.value ?? "",
          selector_attempts: resolvedItems[0]?.attempts.map(attempt => ({
            level: attempt.level, selector_type: attempt.selector_type,
            selector_value: attempt.selector_value, matches: attempt.matches,
            selected_index: attempt.selected_index,
          })) ?? [],
        });
      }
      /** readItem 读取单张卡片，并在读取前后核对正文，避免列表变化后串人。 */
      const readItem = async (resolved: ResolvedElement, index: number): Promise<FindAllItem | null> => {
        const beforeText = await readWithSlot(() => this.readListText(resolved.locator, actionContext, index, "before", diagnoseNames));
        if (beforeText === null) return null;
        const fieldEntries = await Promise.all(
          Object.entries(fields).map(async ([fieldName, fieldSpec]) => [
            fieldName,
            await readWithSlot(() => this.readRelativeField(
              page,
              resolved.locator,
              fieldSpec,
              actionContext,
              fieldName,
              index,
              diagnoseNames,
            )),
          ] as const),
        );
        const values: JsonObject = Object.fromEntries(fieldEntries);
        const afterText = await readWithSlot(() => this.readListText(resolved.locator, actionContext, index, "after", diagnoseNames));
        if (afterText === null) return null;
        if (afterText !== beforeText) {
          this.logger.warn(actionContext, "read_list_item", "changed", {
            index, before_text_length: beforeText.length, after_text_length: afterText.length,
          });
          return null;
        }
        return {
          index: resolved.list_index ?? index + startIndex,
          element_ref: this.session.elements.remember(
            page,
            resolved.locator,
          ),
          text: afterText,
          fields: values,
        };
      };
      const items = await mapWithConcurrency(resolvedItems, readConcurrency, readItem);
      for (const [index, item] of items.entries()) {
        if (item) continue;
        for (let attempt = 1; attempt <= 2 && items[index] === null; attempt++) {
          items[index] = await readItem(resolvedItems[index]!, index);
          this.logger.info(actionContext, "read_list_item", items[index] ? "retry_read" : "retry_failed", { index, attempt });
        }
      }
      await this.retryMissingFields(page, resolvedItems, items, fields, actionContext, diagnoseNames, readWithSlot);
      for (const [index, item] of items.entries()) {
        if (item) continue;
        items[index] = await readItem(resolvedItems[index]!, index);
        this.logger.info(actionContext, "read_list_item", items[index] ? "identity_retry_read" : "identity_retry_failed", { index });
      }
      const unreadable = items.flatMap((item, index) => item === null ? [resolvedItems[index]?.list_index ?? index + startIndex] : []);
      if (unreadable.length > 0) {
        throw new WorkerError({ code: "ACTION_TIMEOUT", message: "列表元素重读后仍不完整，暂不跳过", action: actionContext.action,
          step: "read_list_item", trace_id: actionContext.trace_id, retryable: true, details: { unreadable_indices: unreadable } });
      }
      if (diagnoseNames) {
        this.logger.info(actionContext, "list_name_diagnostics", "summary", {
          resolved_count: resolvedItems.length,
          returned_count: items.filter(item => item !== null).length,
          dropped_indices: items.flatMap((item, index) => item === null ? [index] : []),
          missing_name_indices: items.flatMap(item => item && !String(item.fields.name ?? "").trim() ? [item.index] : []),
        });
      }
      if (report) {
        this.logger.info(actionContext, step, "success", {
          count: items.filter((item) => item !== null).length,
        });
      }
      return items.filter((item): item is FindAllItem => item !== null);
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "ELEMENT_NOT_FOUND",
        message: `${spec.description} 列表暂时没找到`,
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
      });
      if (diagnostic.diagnostic) {
        const page = await this.session.requirePage(actionContext, step).catch(() => undefined);
        const expected = !report && ["ELEMENT_NOT_FOUND", "ELEMENT_NOT_VISIBLE", "ELEMENT_NOT_ENABLED"].includes(normalized.code);
        this.logger.info(actionContext, "selector_probe", expected ? "missing" : "unavailable", {
          diagnostic: true, input: diagnosticInput, output: { count: 0, expected_missing: expected,
            error_code: normalized.code, selector_attempts: normalized.details.attempts ?? [],
            poll_attempts: normalized.details.poll_attempts ?? 0, page_state: page ? pageDiagnostic(page) : { page_unavailable: true } },
        });
      }
      if (report) {
        this.logger.failure(actionContext, normalized);
      }
      throw normalized;
    }
  }

  /** readListText 读取整张卡片文字；失败返回空结果供调用方逐项补读，不记录简历正文。 */
  private async readListText(root: Locator, context: ActionContext, index: number, phase: string, diagnose: boolean): Promise<string | null> {
    const started = Date.now();
    try {
      return await this.readPrimitive.text(root);
    } catch (error) {
      if (diagnose) this.logger.warn(context, "read_list_item", "read_failed", {
        index, phase, read_elapsed_ms: Date.now() - started, text_timeout_ms: 1500,
        ...readFailureDetails(error),
      });
      return null;
    }
  }

  /** readRelativeField 在列表项内部读取一个字段，找不到时返回空字符串。 */
  private async readRelativeField(
    page: ResolvedElement["page"],
    root: Locator,
    spec: SelectorSpec,
    actionContext: ActionContext,
    fieldName: string,
    index: number,
    diagnose: boolean,
  ): Promise<string> {
    const started = Date.now();
    let phase = "locate";
    let resolved: ResolvedElement | undefined;
    try {
      const fieldSpec: SelectorSpec = {
        ...spec,
        timeout_ms: 0,
      };
      resolved = await this.primitive.resolveRelative(
        page,
        root,
        fieldSpec,
        {
          trace_id: actionContext.trace_id,
          action: actionContext.action,
          step: `read_field:${fieldName}`,
          require_unique: false,
        },
      );
      phase = "read";
      if (spec.read_attribute) {
        return await this.readPrimitive.attribute(
          resolved.locator,
          spec.read_attribute,
        );
      }
      if (spec.read_property === "html") {
        return await this.readPrimitive.html(resolved.locator);
      }
      const value = await this.readPrimitive.text(resolved.locator);
      if (diagnose && (fieldName === "name" || fieldName === "card_text")) this.logger.info(actionContext, `read_field:${fieldName}`, value ? "read" : "empty", {
        index, phase, field_elapsed_ms: Date.now() - started,
        configured_timeout_ms: spec.timeout_ms ?? null, effective_locator_timeout_ms: 0, text_timeout_ms: 1500,
        field_state: spec.state ?? "visible", visible: resolved.view.visible,
        field_matches: resolved.attempts.at(-1)?.matches ?? null,
        text_length: value.length,
      });
      return value;
    } catch (error) {
      if (diagnose && (fieldName === "name" || fieldName === "card_text")) this.logger.warn(actionContext, `read_field:${fieldName}`, "failed", {
        index, phase, field_elapsed_ms: Date.now() - started,
        configured_timeout_ms: spec.timeout_ms ?? null, effective_locator_timeout_ms: 0, text_timeout_ms: 1500,
        field_state: spec.state ?? "visible", visible: resolved?.view.visible ?? (error instanceof WorkerError && error.code === "ELEMENT_NOT_VISIBLE" ? false : null),
        field_selectors: spec.target.selectors.map(selector => ({ type: selector.type, value: selector.value })),
        field_matches: resolved?.attempts.at(-1)?.matches ?? null,
        ...readFailureDetails(error),
      });
      return "";
    }
  }

  /** retryMissingFields 对关键字段逐项补读，并核对卡片正文，防止重读时串人。 */
  private async retryMissingFields(
    page: ResolvedElement["page"],
    resolvedItems: ResolvedElement[],
    items: Array<FindAllItem | null>,
    fields: Record<string, SelectorSpec>,
    actionContext: ActionContext,
    diagnose: boolean,
    readWithSlot: <T>(operation: () => Promise<T>) => Promise<T>,
  ): Promise<void> {
    for (const [index, item] of items.entries()) {
      if (!item) continue;
      const root = resolvedItems[index]?.locator;
      if (!root) continue;
      for (const fieldName of ["name", "card_text"]) {
        const fieldSpec = fields[fieldName];
        if (!fieldSpec || String(item.fields[fieldName] ?? "").trim()) continue;
        for (let attempt = 1; attempt <= 2 && !String(item.fields[fieldName] ?? "").trim(); attempt++) {
          const beforeText = await readWithSlot(() => this.readListText(root, actionContext, index, `${fieldName}_retry_before`, diagnose));
          if (beforeText === null || beforeText !== item.text) { items[index] = null; break; }
          const value = await readWithSlot(() => this.readRelativeField(page, root, fieldSpec, actionContext, fieldName, index, diagnose));
          const afterText = await readWithSlot(() => this.readListText(root, actionContext, index, `${fieldName}_retry_after`, diagnose));
          if (afterText === null || afterText !== beforeText) { items[index] = null; break; }
          item.fields[fieldName] = value;
          if (diagnose) this.logger.info(actionContext, `read_field:${fieldName}`, value ? "retry_read" : "retry_empty", {
            index, attempt, text_length: value.length,
          });
        }
        if (items[index] === null) break;
      }
    }
  }
}

/** createReadLimiter 为一次列表请求分配有限的浏览器读取槽位，避免并发字段读取排队超时。 */
function createReadLimiter(limit: number): <T>(operation: () => Promise<T>) => Promise<T> {
  let available = Math.max(1, limit);
  const waiting: Array<() => void> = [];
  return async <T>(operation: () => Promise<T>): Promise<T> => {
    if (available > 0) available--;
    else await new Promise<void>(resolve => waiting.push(resolve));
    try { return await operation(); }
    finally {
      const next = waiting.shift();
      if (next) next();
      else available++;
    }
  };
}

/** mapWithConcurrency 按指定并发数保持原顺序处理列表，避免把大量浏览器命令同时塞入队列。 */
async function mapWithConcurrency<T, R>(
  values: T[],
  concurrency: number,
  mapper: (value: T, index: number) => Promise<R>,
): Promise<R[]> {
  const results = new Array<R>(values.length);
  let nextIndex = 0;
  const workers = Array.from(
    { length: Math.min(Math.max(1, concurrency), values.length) },
    async () => {
      while (nextIndex < values.length) {
        const index = nextIndex;
        nextIndex += 1;
        results[index] = await mapper(values[index]!, index);
      }
    },
  );
  await Promise.all(workers);
  return results;
}

/** readFailureDetails 提取原始错误类别与匹配数量；不输出可能含姓名、正文或凭据的异常消息和调用栈。 */
function readFailureDetails(error: unknown): JsonObject {
  if (error instanceof WorkerError) {
    const attempts = Array.isArray(error.details.attempts) ? error.details.attempts : [];
    return {
      error_code: error.code, error_step: error.step, error_type: "WorkerError",
      visibility_error_type: error.details.visibility_error_type ?? null,
      cause_type: error.cause instanceof Error ? (["Error", "TimeoutError", "WorkerError"].includes(error.cause.name) ? error.cause.name : "UnknownError") : null,
      match_attempts: attempts.map(attempt => {
        if (!attempt || typeof attempt !== "object" || Array.isArray(attempt)) return {};
        return { level: attempt.level ?? null, matches: attempt.matches ?? null, selected_index: attempt.selected_index ?? null };
      }),
    };
  }
  const name = error instanceof Error ? error.name : "UnknownError";
  return { error_type: ["TimeoutError", "Error", "TypeError"].includes(name) ? name : "UnknownError", error_code: name === "TimeoutError" ? "READ_TIMEOUT" : "READ_ERROR" };
}
