// 文件作用说明：等待目标连续稳定，避免弹框动画期间按旧坐标点击或聚焦输入框。
import type { FoundElement } from "./find.js";
import type { LocatorPrimitive } from "../primitives/locator.js";
import type { ElementBox } from "../../contracts/selector.js";
import type { ActionContext } from "../../contracts/common.js";
import { assertInputAllowed } from "../primitives/input-lifecycle.js";
import type { MoveResult } from "./move.js";

/** samePosition 按原有一像素容差核对完整目标边界。 */
function samePosition(a: ElementBox, b: ElementBox): boolean {
  return Math.abs(a.x - b.x) <= 1 && Math.abs(a.y - b.y) <= 1 &&
    Math.abs(a.width - b.width) <= 1 && Math.abs(a.height - b.height) <= 1;
}

/** waitForStablePosition 等待连续三次位置稳定，并把最后一次位置交给后续鼠标操作。 */
export async function waitForStablePosition(found: FoundElement, locator: Pick<LocatorPrimitive, "box">,
  context?: ActionContext, onSample?: (stableCount: number) => void): Promise<void> {
  let previous = found.resolved.view.box;
  let stableCount = 0;
  for (let check = 0; check < 10; check += 1) {
    await new Promise(resolve => setTimeout(resolve, 150));
    assertInputAllowed(context);
    const current = await locator.box(found.resolved.locator);
    assertInputAllowed(context);
    if (!current) throw new Error("元素在操作前消失了，已取消本次操作");
    stableCount = samePosition(current, previous) ? stableCount + 1 : 0;
    found.resolved.view.box = current;
    onSample?.(stableCount);
    if (stableCount >= 3) return;
    previous = current;
  }
  throw new Error("元素位置持续变化，已取消本次操作以避免点错");
}

/** moveToStablePosition 在第一段稳定后开始真实悬停，其余两段继续观测；按下前核对实际悬停目标，变化时仅重定位一次。 */
export async function moveToStablePosition(found: FoundElement, locator: Pick<LocatorPrimitive, "box">,
  context: ActionContext, move: () => Promise<MoveResult>): Promise<MoveResult> {
  for (let attempt = 0; attempt < 2; attempt++) {
    let motion: Promise<MoveResult> | undefined;
    let landing: MoveResult | undefined;
    try {
      await waitForStablePosition(found, locator, context, stableCount => {
        if (!motion && stableCount === (attempt === 0 ? 1 : 3)) {
          motion = move();
          // 观察仍在进行时原生移动可能已经拒绝，先接住拒绝，退出前一定等旧移动收尾。
          void motion.catch(() => undefined);
        }
      });
    } finally {
      landing = await motion;
    }
    assertInputAllowed(context);
    const current = await locator.box(found.resolved.locator);
    assertInputAllowed(context);
    if (!current) throw new Error("悬停后目标已经消失，已取消本次操作");
    found.resolved.view.box = current;
    // nth-child(n) 保持选择器在标准和兼容文档模式下都能核对非链接元素的实际悬停。
    if (landing && samePosition(landing.view.box, current) && await found.resolved.locator.locator(":scope:nth-child(n):hover").count() === 1) {
      assertInputAllowed(context);
      return landing;
    }
  }
  throw new Error("悬停后目标仍在移动或已被遮挡，鼠标没有按下");
}
