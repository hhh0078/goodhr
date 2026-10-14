/** 文件作用说明：按账号保存标签最近使用顺序，并安全忽略已删除标签和损坏缓存。 */
import type { ResumeTag } from "./resume-types";

/** resumeTagPreferenceKey 用本人编号隔离共享浏览器上的标签偏好。 */
export function resumeTagPreferenceKey(identity: string): string {
  return identity.trim() ? `goodhr5_resume_tag_order:${identity.trim()}` : "";
}
/** parseResumeTagOrder 仅接受唯一字符串编号，坏缓存回退到服务端顺序。 */
export function parseResumeTagOrder(raw: string | null): string[] {
  try {
    const value: unknown = JSON.parse(raw || "[]");
    return Array.isArray(value) ? [...new Set(value.filter((id): id is string => typeof id === "string" && Boolean(id.trim())))] : [];
  } catch { return []; }
}
/** orderResumeTags 最近点击放最前，新标签继续按服务端顺序显示。 */
export function orderResumeTags(tags: ResumeTag[], order: string[]): ResumeTag[] {
  const ranks = new Map(order.map((id, index) => [id, index]));
  return [...tags].sort((first, second) => (ranks.get(first.id) ?? Number.MAX_SAFE_INTEGER) - (ranks.get(second.id) ?? Number.MAX_SAFE_INTEGER));
}
/** rememberResumeTag 将本次点击移到队首，去除已删除标签及重复记录。 */
export function rememberResumeTag(id: string, order: string[], tags: ResumeTag[]): string[] {
  const existing = new Set(tags.map((tag) => tag.id));
  return [...new Set([id, ...order])].filter((value) => existing.has(value));
}
