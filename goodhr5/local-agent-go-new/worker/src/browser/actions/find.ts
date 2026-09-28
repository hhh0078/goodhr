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
  ): Promise<FindAllItem[]> {
    const step = "find_all";
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
      const diagnoseNames = report && Object.hasOwn(fields, "name");
      if (diagnoseNames) {
        this.logger.info(actionContext, "list_name_diagnostics", "resolved", {
          page_url: safeURL(page.url()),
          resolved_count: resolvedItems.length,
          field_count: Object.keys(fields).length,
          read_concurrency: readConcurrency,
          matched_selector: resolvedItems[0]?.matched_selector.value ?? "",
          selector_attempts: resolvedItems[0]?.attempts.map(attempt => ({
            level: attempt.level, selector_type: attempt.selector_type,
            selector_value: attempt.selector_value, matches: attempt.matches,
            selected_index: attempt.selected_index,
          })) ?? [],
        });
      }
      const items = await mapWithConcurrency(
        resolvedItems,
        readConcurrency,
        async (resolved, index): Promise<FindAllItem | null> => {
          const beforeText = await this.readListText(resolved.locator, actionContext, index, "before", diagnoseNames);
          if (beforeText === null) return null;
          const fieldEntries = await Promise.all(
            Object.entries(fields).map(async ([fieldName, fieldSpec]) => [
              fieldName,
              await this.readRelativeField(
                page,
                resolved.locator,
                fieldSpec,
                actionContext,
                fieldName,
                index,
                diagnoseNames,
              ),
            ] as const),
          );
          const values: JsonObject = Object.fromEntries(fieldEntries);
          const afterText = await this.readListText(resolved.locator, actionContext, index, "after", diagnoseNames);
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
        },
      );
      if (fields.name) {
        await this.retryMissingNames(page, resolvedItems, items, fields.name, actionContext, diagnoseNames);
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
      if (report) {
        this.logger.failure(actionContext, normalized);
      }
      throw normalized;
    }
  }

  /** readListText 保留原有失败丢弃行为，只记录读取阶段、耗时和错误类别，不记录简历正文。 */
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
      if (diagnose && fieldName === "name") this.logger.info(actionContext, "read_field:name", value ? "read" : "empty", {
        index, phase, field_elapsed_ms: Date.now() - started,
        configured_timeout_ms: spec.timeout_ms ?? null, effective_locator_timeout_ms: 0, text_timeout_ms: 1500,
        field_state: spec.state ?? "visible", visible: resolved.view.visible,
        field_matches: resolved.attempts.at(-1)?.matches ?? null,
        text_length: value.length,
      });
      return value;
    } catch (error) {
      if (diagnose && fieldName === "name") this.logger.warn(actionContext, "read_field:name", "failed", {
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

  /** retryMissingNames 对已找到卡片但姓名读取超时的项目逐个重读，并用卡片正文前后校验防止串人。 */
  private async retryMissingNames(
    page: ResolvedElement["page"],
    resolvedItems: ResolvedElement[],
    items: Array<FindAllItem | null>,
    nameSpec: SelectorSpec,
    actionContext: ActionContext,
    diagnose: boolean,
  ): Promise<void> {
    for (const [index, item] of items.entries()) {
      if (!item || String(item.fields.name ?? "").trim()) continue;
      const root = resolvedItems[index]?.locator;
      if (!root) continue;
      const beforeText = await this.readListText(root, actionContext, index, "name_retry_before", diagnose);
      if (beforeText === null || beforeText !== item.text) {
        items[index] = null;
        continue;
      }
      const name = await this.readRelativeField(
        page, root, nameSpec, actionContext, "name", index, diagnose,
      );
      const afterText = await this.readListText(root, actionContext, index, "name_retry_after", diagnose);
      if (afterText === null || afterText !== beforeText) {
        items[index] = null;
        continue;
      }
      item.fields.name = name;
      if (diagnose) {
        this.logger.info(actionContext, "read_field:name", name ? "retry_read" : "retry_empty", {
          index, text_length: name.length,
        });
      }
    }
  }
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
