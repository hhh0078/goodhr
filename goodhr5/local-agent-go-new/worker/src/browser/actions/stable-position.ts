// 文件作用说明：等待目标连续稳定，避免弹框动画期间按旧坐标点击或聚焦输入框。
import type { FoundElement } from "./find.js";
import type { LocatorPrimitive } from "../primitives/locator.js";

/** waitForStablePosition 等待连续三次位置稳定，并把最后一次位置交给后续鼠标操作。 */
export async function waitForStablePosition(found: FoundElement, locator: Pick<LocatorPrimitive, "box">): Promise<void> {
  let previous = found.resolved.view.box;
  let stableCount = 0;
  for (let check = 0; check < 10; check += 1) {
    await new Promise(resolve => setTimeout(resolve, 150));
    const current = await locator.box(found.resolved.locator);
    if (!current) throw new Error("元素在操作前消失了，已取消本次操作");
    const stable = ["x", "y", "width", "height"] as const;
    stableCount = stable.every(key => Math.abs(current[key] - previous[key]) <= 1) ? stableCount + 1 : 0;
    found.resolved.view.box = current;
    if (stableCount >= 3) return;
    previous = current;
  }
  throw new Error("元素位置持续变化，已取消本次操作以避免点错");
}
