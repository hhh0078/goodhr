// 文件作用说明：实现封装好的元素读取能力，统一查找后读取文本、HTML 或属性。

import type { ElementReadRequest } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { ReadPrimitive } from "../primitives/read.js";
import { FindAction } from "./find.js";

/** ReadResult 表示元素读取结果。 */
export interface ReadResult extends JsonObject {
  value: string;
  property: string;
  element_ref: string;
}

/** ReadAction 实现查找后读取元素内容的封装能力。 */
export class ReadAction {
  private readonly primitive = new ReadPrimitive();

  /** 创建元素读取封装能力。 */
  constructor(
    private readonly find: FindAction,
    private readonly logger: WorkerLogger,
  ) {}

  /** execute 平铺执行查找和读取。 */
  async execute(
    request: ElementReadRequest,
    actionContext: ActionContext,
  ): Promise<ReadResult> {
    const step = "read";
    this.logger.info(actionContext, step, "start", {
      target_description: request.selector.description,
      property: request.attribute
        ? `attribute:${request.attribute}`
        : request.property ?? "text",
    });
    try {
      const found = await this.find.one(
        request.selector,
        actionContext,
        false,
      );
      let value: string;
      let property: string;
      if (request.attribute) {
        value = await this.primitive.attribute(
          found.resolved.locator,
          request.attribute,
        );
        property = `attribute:${request.attribute}`;
      } else if (request.property === "html") {
        value = await this.primitive.html(found.resolved.locator);
        property = "html";
      } else {
        value = await this.primitive.text(found.resolved.locator);
        property = "text";
      }
      const result: ReadResult = {
        value,
        property,
        element_ref: found.result.element_ref,
      };
      this.logger.info(actionContext, step, "success", {
        property,
        value_length: value.length,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        message: `${request.selector.description} 暂时没读到`,
      });
      this.logger.failure(actionContext, normalized);
      throw normalized;
    }
  }
}
