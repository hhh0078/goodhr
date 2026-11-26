/** 文件作用：只在展示层合并重复原因，将已知 Go 步骤技术行留在原始详情，保留不同原因。 */

/**
 * isGoStepLogMessage 识别 Go 步骤日志的完整固定格式，仅分类，不改写日志。
 * @param message - 已去除首尾空白的日志文本。
 * @returns 是否符合共享步骤日志的固定输出格式。
 */
function isGoStepLogMessage(message: string): boolean {
  return /^task_id=\S+ flow=\S+ step=\S+ status=\S+ duration_ms=\d+ error="(?:[^"\\\r\n]|\\.)*"$/.test(message);
}

/** diagnosticMessages 合并可见原因；已有具体原因时，Go 技术行交给原始详情保存，不改写原文。 */
export function diagnosticMessages(primary: unknown, secondary: unknown): string[] {
  const first = typeof primary === "string" ? primary.trim() : "";
  const second = typeof secondary === "string" ? secondary.trim() : "";
  if (!first) return second ? [second] : [];
  if (!second || first === second) return [first];
  if (!isGoStepLogMessage(first) && isGoStepLogMessage(second)) return [first];
  if (first.includes(second)) return [first];
  if (second.includes(first)) return [second];
  return [first, second];
}
