/** 文件作用说明：合并岗位话术草稿与系统默认，保留继承、覆盖、删除和恢复的区别。 */
export type ReplyPhrase = { id: string; category: string; title: string; scene: string; content: string };
export type ReplyPhraseOverride = ReplyPhrase | { id: string; deleted: true };

/** effectiveReplyPhrases 按系统顺序显示继承或已修改的话术，随后列出岗位独有的条目。 */
export function effectiveReplyPhrases(defaults: ReplyPhrase[], overrides: ReplyPhraseOverride[]): ReplyPhrase[] {
  const rows = new Map(defaults.map(row => [row.id, row]));
  for (const row of overrides) {
    if ("deleted" in row) rows.delete(row.id);
    else rows.set(row.id, row);
  }
  return [...rows.values()];
}

/** replacePhraseOverride 清除某一项的旧覆盖，再按需写入新的修改或删除标记。 */
export function replacePhraseOverride(rows: ReplyPhraseOverride[], id: string, replacement?: ReplyPhraseOverride) {
  if (!replacement) return rows.filter(row => row.id !== id);
  if (rows.some(row => row.id === id)) return rows.map(row => row.id === id ? replacement : row);
  return [...rows, replacement];
}
