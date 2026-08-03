/** 本文件负责按固定轮询间隔等待候选人详情容器出现。 */

/** positiveMilliseconds 将等待参数转换为有效的正毫秒数。 */
function positiveMilliseconds(value, fallback) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

/** waitForDetailContainer 每隔指定时间查找一次详情容器，找到后立即返回。 */
export async function waitForDetailContainer(options = {}) {
  const findVisible = options.findVisible;
  const wait = options.wait;
  const now = typeof options.now === "function" ? options.now : Date.now;
  const timeoutMs = positiveMilliseconds(options.timeoutMs, 5000);
  const pollIntervalMs = positiveMilliseconds(options.pollIntervalMs, 100);
  const startedAt = now();
  let attempts = 0;
  if (typeof findVisible !== "function" || typeof wait !== "function") {
    return {
      ready: false,
      attempts,
      elapsed_ms: 0,
      containers: 0,
      reason: "详情查找方法不可用",
    };
  }
  while (now() - startedAt < timeoutMs) {
    attempts += 1;
    const containers = (await findVisible()) || [];
    if (containers.length > 0) {
      return {
        ready: true,
        attempts,
        elapsed_ms: now() - startedAt,
        containers: containers.length,
        matched_selector: String(containers[0]?.targetSelector || ""),
        reason: "ready",
      };
    }
    const remainingMs = timeoutMs - (now() - startedAt);
    if (remainingMs <= 0) break;
    await wait(Math.min(pollIntervalMs, remainingMs));
  }
  return {
    ready: false,
    attempts,
    elapsed_ms: now() - startedAt,
    containers: 0,
    reason: "未找到可见详情容器",
  };
}
