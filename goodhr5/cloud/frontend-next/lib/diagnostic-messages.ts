/** 文件作用：只在展示层合并重复的日志正文和错误原因，保留完整文本及不同原因。 */

/** diagnosticMessages 返回需要展示的完整原因，包含关系保留较长文本，不改写或截断原始日志。 */
export function diagnosticMessages(primary: unknown, secondary: unknown): string[] {
  const first = typeof primary === "string" ? primary.trim() : "";
  const second = typeof secondary === "string" ? secondary.trim() : "";
  if (!first) return second ? [second] : [];
  if (!second || first === second) return [first];
  if (first.includes(second)) return [first];
  if (second.includes(first)) return [second];
  return [first, second];
}
