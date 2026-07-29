// 文件作用说明：实现 Go 可调用的封装查找能力，并统一生成元素引用、状态和详细查找日志。

import type { Locator } from "playwright-core";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import type { FindResult, SelectorSpec } from "../../contracts/selector.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import {
  LocatorPrimitive,
  type ResolvedElement,
} from "../primitives/locator.js";
import { ReadPrimitive } from "../primitives/read.js";
import { BrowserSession } from "../session/browser-session.js";

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
  ): Promise<FindAllItem[]> {
    const step = "find_all";
    if (report) {
      this.logger.info(actionContext, step, "start", {
        target_description: spec.description,
        max_items: maxItems,
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
      );
      const items = await Promise.all(
        resolvedItems.map(async (resolved, index): Promise<FindAllItem> => {
          const fieldEntries = await Promise.all(
            Object.entries(fields).map(async ([fieldName, fieldSpec]) => [
              fieldName,
              await this.readRelativeField(
                page,
                resolved.locator,
                fieldSpec,
                actionContext,
                fieldName,
              ),
            ] as const),
          );
          const values: JsonObject = Object.fromEntries(fieldEntries);
          return {
            index,
            element_ref: this.session.elements.remember(
              page,
              resolved.locator,
            ),
            text: await this.readPrimitive
              .text(resolved.locator)
              .catch(() => ""),
            fields: values,
          };
        }),
      );
      if (report) {
        this.logger.info(actionContext, step, "success", {
          count: items.length,
        });
      }
      return items;
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

  /** readRelativeField 在列表项内部读取一个字段，找不到时返回空字符串。 */
  private async readRelativeField(
    page: ResolvedElement["page"],
    root: Locator,
    spec: SelectorSpec,
    actionContext: ActionContext,
    fieldName: string,
  ): Promise<string> {
    try {
      const fieldSpec: SelectorSpec = {
        ...spec,
        timeout_ms: Math.min(spec.timeout_ms ?? 300, 300),
      };
      const resolved = await this.primitive.resolveRelative(
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
      if (spec.read_attribute) {
        return this.readPrimitive.attribute(
          resolved.locator,
          spec.read_attribute,
        );
      }
      if (spec.read_property === "html") {
        return this.readPrimitive.html(resolved.locator);
      }
      return this.readPrimitive.text(resolved.locator);
    } catch {
      return "";
    }
  }
}
