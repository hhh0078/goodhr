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
const maximumReferences = 10_000;

/** ElementRegistry 管理短生命周期元素引用，避免 Go 长期持有过期 Locator。 */
export class ElementRegistry {
  private readonly entries = new Map<string, ElementEntry>();
  private cleanupTimer: ReturnType<typeof setTimeout> | undefined;

  /** remember 保存 Locator 并返回短生命周期引用。 */
  remember(page: Page, locator: Locator): string {
    this.removeExpired(Date.now());
    if (this.entries.size >= maximumReferences) {
      const oldest = this.entries.keys().next().value;
      if (oldest) this.entries.delete(oldest);
    }
    const reference = `el_${randomUUID()}`;
    this.entries.set(reference, {
      locator,
      page,
      created_at: Date.now(),
    });
    this.scheduleCleanup();
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
      Date.now() - entry.created_at >= referenceMaxAgeMs ||
      page.isClosed()
    ) {
      this.entries.delete(reference);
      if (this.entries.size === 0) this.clear();
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
    if (this.entries.size === 0) this.clear();
  }

  /** clear 清除全部元素引用及到期定时器。 */
  clear(): void {
    clearTimeout(this.cleanupTimer);
    this.cleanupTimer = undefined;
    this.entries.clear();
  }

  /** removeExpired 按插入顺序释放过期引用，普通查找不重复扫描整张表。 */
  private removeExpired(now: number): void {
    for (const [reference, entry] of this.entries) {
      if (now - entry.created_at < referenceMaxAgeMs) break;
      this.entries.delete(reference);
    }
  }

  /** scheduleCleanup 只保留一个最早到期定时器，空闲时也能释放引用，且不阻止 Worker 退出。 */
  private scheduleCleanup(): void {
    if (this.cleanupTimer || this.entries.size === 0) return;
    const first = this.entries.values().next().value;
    if (!first) return;
    this.cleanupTimer = setTimeout(() => {
      this.cleanupTimer = undefined;
      this.removeExpired(Date.now());
      this.scheduleCleanup();
    }, Math.max(1, first.created_at + referenceMaxAgeMs - Date.now()));
    this.cleanupTimer.unref();
  }
}
