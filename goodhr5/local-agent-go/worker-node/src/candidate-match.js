// 本文件负责根据候选人姓名和卡片文本，在动态变化的列表中重新匹配目标候选人。

/**
 * normalizeCandidateMatchText 将候选人卡片文本整理为便于稳定比较的格式。
 * @param {string} value - 原始候选人文本。
 * @returns {string} 仅保留中英文和数字的标准文本。
 */
export function normalizeCandidateMatchText(value) {
  return String(value || "")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "");
}

/**
 * candidateCardLocator 兼容直接 Locator 与扫描结果包装对象，返回真正可操作的 Locator。
 * @param {any} item - 直接 Locator 或带 locator 字段的扫描结果。
 * @returns {any} 真正的候选人卡片 Locator。
 */
export function candidateCardLocator(item) {
  if (item && typeof item.locator !== "function" && item.locator) {
    return item.locator;
  }
  return item;
}

/**
 * candidateTextSimilarity 计算两段候选人卡片文本的双字符相似度。
 * @param {string} expected - 任务最初提取的候选人文本。
 * @param {string} actual - 页面当前候选人卡片文本。
 * @returns {number} 零到一之间的相似度。
 */
export function candidateTextSimilarity(expected, actual) {
  const left = normalizeCandidateMatchText(expected);
  const right = normalizeCandidateMatchText(actual);
  if (!left || !right) return 0;
  if (left === right || right.includes(left) || left.includes(right)) return 1;
  const leftPairs = textPairs(left);
  const rightPairs = textPairs(right);
  let overlap = 0;
  for (const pair of leftPairs) {
    if (rightPairs.has(pair)) overlap += 1;
  }
  return (2 * overlap) / Math.max(1, leftPairs.size + rightPairs.size);
}

/**
 * bestCandidateTextMatch 从当前页面卡片文本中找出最符合任务候选人的一项。
 * @param {string} expectedName - 任务候选人姓名。
 * @param {string} expectedText - 任务最初提取的候选人卡片文本。
 * @param {string[]} actualTexts - 页面当前全部候选人卡片文本。
 * @returns {{index:number,score:number}|null} 最佳匹配序号和分数。
 */
export function bestCandidateTextMatch(expectedName, expectedText, actualTexts) {
  const normalizedName = normalizeCandidateMatchText(expectedName);
  let best = null;
  for (let index = 0; index < actualTexts.length; index += 1) {
    const actualText = actualTexts[index];
    const normalizedActual = normalizeCandidateMatchText(actualText);
    if (normalizedName && !normalizedActual.includes(normalizedName)) continue;
    const similarity = candidateTextSimilarity(expectedText || expectedName, actualText);
    const score = Math.min(1, similarity + (normalizedName ? 0.2 : 0));
    if (!best || score > best.score) best = { index, score };
  }
  return best && best.score >= 0.45 ? best : null;
}

/**
 * textPairs 返回文本中用于相似度比较的双字符集合。
 * @param {string} value - 已标准化的文本。
 * @returns {Set<string>} 双字符集合。
 */
function textPairs(value) {
  if (value.length < 2) return new Set([value]);
  const pairs = new Set();
  for (let index = 0; index < value.length - 1; index += 1) {
    pairs.add(value.slice(index, index + 2));
  }
  return pairs;
}
