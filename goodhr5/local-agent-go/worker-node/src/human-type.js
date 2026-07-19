/** 本文件提供浏览器输入框共用的分段拟人输入能力。 */

/**
 * 将输入参数限制在安全整数范围内。
 * @param {unknown} value - 原始参数。
 * @param {number} fallback - 参数无效时的默认值。
 * @param {number} minimum - 最小值。
 * @param {number} maximum - 最大值。
 * @returns {number} 安全整数。
 */
function boundedInteger(value, fallback, minimum, maximum) {
  const parsed = value === null || value === undefined || value === ""
    ? Number.NaN
    : Number(value);
  const normalized = Number.isFinite(parsed) ? Math.trunc(parsed) : fallback;
  return Math.max(minimum, Math.min(normalized, maximum));
}

/**
 * 返回闭区间内的随机整数。
 * @param {number} minimum - 最小值。
 * @param {number} maximum - 最大值。
 * @param {() => number} random - 随机数生成器。
 * @returns {number} 随机整数。
 */
function randomInteger(minimum, maximum, random) {
  if (maximum <= minimum) return minimum;
  return minimum + Math.floor(random() * (maximum - minimum + 1));
}

/**
 * 等待指定毫秒数，用于模拟真人分段输入时的停顿。
 * @param {number} milliseconds - 等待毫秒数。
 * @returns {Promise<void>} 等待完成。
 */
function waitMilliseconds(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/**
 * 把文字随机拆成小段输入，并在字符及段落之间加入随机延时。
 * @param {{type: (text: string, options: {delay: number}) => Promise<void>}} keyboard - Playwright 键盘对象。
 * @param {string} text - 要输入的完整文字。
 * @param {Record<string, any>} options - 分段大小、延时及测试依赖配置。
 * @returns {Promise<{chars: number, chunks: number}>} 输入字符数和分段数。
 */
export async function humanTypeText(keyboard, text, options = {}) {
  const characters = Array.from(String(text || ""));
  const chunkMin = boundedInteger(options.chunk_min, 1, 1, 10);
  const chunkMax = boundedInteger(options.chunk_max, 2, chunkMin, 10);
  const pauseMin = boundedInteger(options.delay_min_ms, 80, 0, 5000);
  const pauseMax = boundedInteger(options.delay_max_ms, 220, pauseMin, 5000);
  const hasFixedTypingDelay = options.typing_delay_ms !== null
    && options.typing_delay_ms !== undefined
    && String(options.typing_delay_ms).trim() !== "";
  const fixedTypingDelay = hasFixedTypingDelay
    ? Number(options.typing_delay_ms)
    : Number.NaN;
  const typingMin = Number.isFinite(fixedTypingDelay)
    ? boundedInteger(fixedTypingDelay, 25, 0, 5000)
    : 25;
  const typingMax = Number.isFinite(fixedTypingDelay) ? typingMin : 90;
  const random = typeof options.random === "function" ? options.random : Math.random;
  const wait = typeof options.wait === "function" ? options.wait : waitMilliseconds;

  let offset = 0;
  let chunks = 0;
  while (offset < characters.length) {
    const chunkSize = randomInteger(chunkMin, chunkMax, random);
    const chunk = characters.slice(offset, offset + chunkSize).join("");
    await keyboard.type(chunk, {
      delay: randomInteger(typingMin, typingMax, random),
    });
    offset += Array.from(chunk).length;
    chunks += 1;
    if (offset < characters.length && pauseMax > 0) {
      await wait(randomInteger(pauseMin, pauseMax, random));
    }
  }
  return { chars: characters.length, chunks };
}
