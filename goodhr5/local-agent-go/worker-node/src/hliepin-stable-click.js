// 本文件负责猎聘弹层和抽屉动画期间的父子唯一定位、位置稳定复核与单次安全点击。

/** boxesApproximatelyEqual 判断两个元素位置是否在允许误差内保持稳定。 */
export function boxesApproximatelyEqual(left, right, tolerance = 2) {
  if (!left || !right) return false;
  const limit = Math.max(0, Number(tolerance || 0));
  return ["x", "y", "width", "height"].every(
    (key) => Math.abs(Number(left[key] || 0) - Number(right[key] || 0)) <= limit,
  );
}

/** pointInsideBox 判断鼠标最终落点是否仍在元素最新位置内部。 */
export function pointInsideBox(point, box, margin = 2) {
  if (!point || !box) return false;
  const safeMargin = Math.max(0, Number(margin || 0));
  return (
    Number(point.x) >= box.x + safeMargin &&
    Number(point.x) <= box.x + box.width - safeMargin &&
    Number(point.y) >= box.y + safeMargin &&
    Number(point.y) <= box.y + box.height - safeMargin
  );
}

/** normalizeComparableText 按配置整理目标文字，兼容猎聘按钮字间插入的展示空白。 */
export function normalizeComparableText(value, ignoreWhitespace = false) {
  const normalized = String(value || "").trim();
  return ignoreWhitespace ? normalized.replace(/\s+/g, "") : normalized;
}

/** createHLiepinStableClickAction 创建猎聘专用的稳定单次点击页面动作。 */
export function createHLiepinStableClickAction(dependencies) {
  const ensurePage = dependencies?.ensurePage;
  const moveMouseToElement = dependencies?.moveMouseToElement;
  const humanMouseClick = dependencies?.humanMouseClick;
  if (!ensurePage || !moveMouseToElement || !humanMouseClick) {
    throw new Error("猎聘稳定点击依赖不完整");
  }

  /** resolveUniqueTarget 在唯一可见父级内重新解析目标，防止跨弹层命中同类元素。 */
  async function resolveUniqueTarget(currentPage, payload) {
    const parentSelector = String(payload?.parent_selector || "").trim();
    const targetSelector = String(payload?.target_selector || "").trim();
    if (!parentSelector || !targetSelector) {
      throw new Error("猎聘稳定点击父级或目标选择器为空");
    }
    const parents = currentPage.locator(parentSelector);
    const parentCount = await parents.count();
    if (parentCount !== 1 || !(await parents.first().isVisible().catch(() => false))) {
      throw new Error(`猎聘稳定点击父级数量异常：${parentCount}`);
    }
    const parent = parents.first();
    const targets = parent.locator(targetSelector);
    const targetCount = await targets.count();
    const hasIndex = payload?.target_index !== undefined && payload?.target_index !== null;
    const targetIndex = hasIndex ? Math.max(0, Number(payload.target_index)) : 0;
    if ((!hasIndex && targetCount !== 1) || targetIndex >= targetCount) {
      throw new Error(`猎聘稳定点击目标数量异常：${targetCount}`);
    }
    let target = targets.nth(targetIndex);
    const nestedSelector = String(payload?.nested_selector || "").trim();
    if (nestedSelector) {
      const nested = target.locator(nestedSelector);
      const nestedCount = await nested.count();
      if (nestedCount !== 1) {
        throw new Error(`猎聘稳定点击项内目标数量异常：${nestedCount}`);
      }
      target = nested.first();
    }
    if (!(await target.isVisible().catch(() => false))) {
      throw new Error("猎聘稳定点击目标不可见");
    }
    const ignoreTextWhitespace = payload?.normalize_text_whitespace === true;
    const expectedText = normalizeComparableText(payload?.expected_text, ignoreTextWhitespace);
    if (expectedText) {
      const rawActualText = String(await target.innerText({ timeout: 800 }).catch(() => "")).trim();
      const actualText = normalizeComparableText(rawActualText, ignoreTextWhitespace);
      const matches = payload?.exact_text === true
        ? actualText === expectedText
        : actualText.includes(expectedText);
      if (!matches) {
        throw new Error(`猎聘稳定点击目标文字不匹配：${rawActualText}`);
      }
    }
    return { target, parentCount, targetCount, targetIndex };
  }

  /** waitForStableBox 连续读取元素位置，直到弹层动画结束并保持稳定。 */
  async function waitForStableBox(currentPage, target, payload) {
    const timeout = Math.max(500, Number(payload?.stability_timeout || 5000));
    const interval = Math.max(60, Number(payload?.stability_interval || 120));
    const requiredChecks = Math.max(2, Number(payload?.stable_checks || 3));
    const tolerance = Math.max(0, Number(payload?.position_tolerance || 2));
    const deadline = Date.now() + timeout;
    let previous = null;
    let stableCount = 0;
    while (Date.now() <= deadline) {
      const box = await target.boundingBox().catch(() => null);
      if (box && box.width > 0 && box.height > 0) {
        if (boxesApproximatelyEqual(previous, box, tolerance)) stableCount += 1;
        else stableCount = 1;
        previous = box;
        if (stableCount >= requiredChecks) return box;
      } else {
        previous = null;
        stableCount = 0;
      }
      await currentPage.waitForTimeout(interval);
    }
    throw new Error("猎聘稳定点击目标位置在超时前仍未稳定");
  }

  /** stableClick 等待父子目标唯一且位置稳定，移动后再次复核并只点击一次。 */
  async function stableClick(payload) {
    const currentPage = await ensurePage();
    const tolerance = Math.max(0, Number(payload?.position_tolerance || 2));
    const maxMoves = Math.max(1, Number(payload?.max_move_attempts || 3));
    let resolved = await resolveUniqueTarget(currentPage, payload || {});
    let stableBox = await waitForStableBox(currentPage, resolved.target, payload || {});
    let move = null;
    for (let attempt = 1; attempt <= maxMoves; attempt += 1) {
      move = await moveMouseToElement(currentPage, resolved.target, payload || {});
      resolved = await resolveUniqueTarget(currentPage, payload || {});
      const latestBox = await waitForStableBox(currentPage, resolved.target, payload || {});
      if (
        boxesApproximatelyEqual(stableBox, latestBox, tolerance) &&
        pointInsideBox(move, latestBox, Math.min(4, tolerance + 1))
      ) {
        stableBox = latestBox;
        break;
      }
      if (attempt >= maxMoves) {
        throw new Error("猎聘稳定点击目标在鼠标移动后发生位移，已取消点击");
      }
      stableBox = latestBox;
    }
    const finalResolved = await resolveUniqueTarget(currentPage, payload || {});
    const finalBox = await finalResolved.target.boundingBox().catch(() => null);
    if (
      !boxesApproximatelyEqual(stableBox, finalBox, tolerance) ||
      !pointInsideBox(move, finalBox, Math.min(4, tolerance + 1))
    ) {
      throw new Error("猎聘稳定点击前目标位置再次变化，已取消点击");
    }
    const click = await humanMouseClick(currentPage, payload || {});
    const waitForSelector = String(payload?.wait_for_selector || "").trim();
    if (waitForSelector) {
      const appeared = currentPage.locator(waitForSelector);
      await appeared.first().waitFor({ state: "visible", timeout: Math.max(500, Number(payload?.wait_timeout || 5000)) });
      const appearedCount = await appeared.count();
      if (appearedCount !== 1) throw new Error(`猎聘点击后目标父级数量异常：${appearedCount}`);
    }
    const waitForHiddenSelector = String(payload?.wait_for_hidden_selector || "").trim();
    if (waitForHiddenSelector) {
      await currentPage.locator(waitForHiddenSelector).first().waitFor({
        state: "hidden",
        timeout: Math.max(500, Number(payload?.wait_timeout || 5000)),
      });
    }
    return {
      clicked: true,
      click_count: 1,
      parent_count: finalResolved.parentCount,
      target_count: finalResolved.targetCount,
      target_index: finalResolved.targetIndex,
      stable_box: finalBox,
      mouse: move,
      click,
    };
  }

  return stableClick;
}
