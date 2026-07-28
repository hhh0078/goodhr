// 文件作用说明：保存同一次短流程可复用的 Playwright Locator，并在页面变化后统一失效。

import { randomUUID } from "node:crypto";
import type { Locator, Page } from "playwright-core";
import { WorkerError } from "../../errors/worker-error.js";

interface ElementEntry {
  locator: Locator;
  page: Page;
  created_at: number;
}

const referenceMaxAgeMs = 30_000;

/** ElementRegistry 管理短生命周期元素引用，避免 Go 长期持有过期 Locator。 */
export class ElementRegistry {
  private readonly entries = new Map<string, ElementEntry>();

  /** remember 保存 Locator 并返回短生命周期引用。 */
  remember(page: Page, locator: Locator): string {
    const reference = `el_${randomUUID()}`;
    this.entries.set(reference, {
      locator,
      page,
      created_at: Date.now(),
    });
    return reference;
  }

  /** get 读取仍属于当前页面且未过期的 Locator。 */
  get(
    reference: string,
    page: Page,
    context: { trace_id: string; action: string; step: string },
  ): Locator {
    const entry = this.entries.get(reference);
    if (
      !entry ||
      entry.page !== page ||
      Date.now() - entry.created_at > referenceMaxAgeMs ||
      page.isClosed()
    ) {
      this.entries.delete(reference);
      throw new WorkerError({
        code: "ELEMENT_REF_EXPIRED",
        message: "这个页面元素已经变了，我需要重新找一下",
        action: context.action,
        step: context.step,
        trace_id: context.trace_id,
        retryable: true,
        details: { element_ref: reference },
      });
    }
    return entry.locator;
  }

  /** clearPage 清除指定页面的全部元素引用。 */
  clearPage(page: Page): void {
    for (const [reference, entry] of this.entries) {
      if (entry.page === page) {
        this.entries.delete(reference);
      }
    }
  }

  /** clear 清除全部元素引用。 */
  clear(): void {
    this.entries.clear();
  }
}
