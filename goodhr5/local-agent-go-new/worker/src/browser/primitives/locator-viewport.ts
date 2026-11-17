// 文件作用说明：在一次只读请求内定位纵向列表首张完整卡片，仅调用标准Locator边界和截图测量，不滚动或注入脚本。
import type { Locator, Page } from "playwright-core";
import type { ViewportStartOptions } from "../../contracts/actions.js";
import type { ElementBox, ElementView } from "../../contracts/selector.js";
import { WorkerError } from "../../errors/worker-error.js";
import { traceOperation } from "../../logging/operation.js";
import { assertRequestActive } from "./request-lifecycle.js";
import { ViewportPrimitive } from "./viewport.js";

/** readBounds 读取当前序号的真实几何；未知几何必须停止，不能猜测起点或跳过候选人。 */
async function readBounds(locator: Locator, index: number, traceId: string): Promise<ElementBox> {
  assertRequestActive();
  const box = await traceOperation("viewport_start_bounds", () => locator.nth(index).boundingBox());
  assertRequestActive();
  if (!box || box.width <= 0 || box.height <= 0) throw new WorkerError({ code: "ACTION_TIMEOUT", message: "候选人位置还没读完整，请等页面稳定后再开始", action: "element.find_all", step: "viewport_start", trace_id: traceId, retryable: true });
  return box;
}

/** findViewportStart 二分定位当前视口，复用一次窗口测量，返回前复核数量和几何；不读取卡片正文或改变页面。 */
export async function findViewportStart(page: Page, locator: Locator, count: number, insets: ViewportStartOptions, measurement: ViewportPrimitive, traceId: string): Promise<{ index: number; view: ElementView } | null> {
  const viewport = await measurement.size(page);
  assertRequestActive();
  const bottom = viewport.height - insets.bottom_inset;
  if (viewport.width <= 0 || bottom <= insets.top_inset) return null;
  let first = 0, last = count;
  while (first < last) {
    const middle = first + Math.floor((last - first) / 2);
    const box = await readBounds(locator, middle, traceId);
    if (box.y < insets.top_inset) first = middle + 1;
    else last = middle;
  }
  for (let index = first; index < count; index++) {
    const box = await readBounds(locator, index, traceId);
    if (box.y >= bottom) return null;
    if (box.x < 0 || box.x + box.width > viewport.width || box.y + box.height > bottom) continue;
    const after = await readBounds(locator, index, traceId);
    const currentCount = await locator.count();
    assertRequestActive();
    if (currentCount !== count || box.x !== after.x || box.y !== after.y || box.width !== after.width || box.height !== after.height) {
      throw new WorkerError({ code: "ACTION_TIMEOUT", message: "当前候选人位置刚发生变化，请等页面稳定后再开始", action: "element.find_all", step: "viewport_start", trace_id: traceId, retryable: true });
    }
    const [visible, enabled] = await Promise.all([locator.nth(index).isVisible(), locator.nth(index).isEnabled()]);
    assertRequestActive();
    if (!visible) throw new WorkerError({ code: "ACTION_TIMEOUT", message: "当前候选人位置刚发生变化，请等页面稳定后再开始", action: "element.find_all", step: "viewport_start", trace_id: traceId, retryable: true });
    return { index, view: { box: after, viewport, visible, enabled, in_viewport: true, fully_in_viewport: true } };
  }
  return null;
}
