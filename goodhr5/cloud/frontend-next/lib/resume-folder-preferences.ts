/** 文件作用说明：按账号保存收藏夹最近使用顺序，并安全忽略已删除收藏夹和损坏缓存。 */
import type { ResumeFolder } from "./resume-types";

/** resumeFolderPreferenceKey 用本人编号隔离共享浏览器上的收藏夹偏好。 */
export function resumeFolderPreferenceKey(identity: string): string {
  return identity.trim() ? `goodhr5_resume_folder_order:${identity.trim()}` : "";
}
/** parseResumeFolderOrder 仅接受唯一字符串编号，坏缓存回退到服务端顺序。 */
export function parseResumeFolderOrder(raw: string | null): string[] {
  try {
    const value: unknown = JSON.parse(raw || "[]");
    return Array.isArray(value) ? [...new Set(value.filter((id): id is string => typeof id === "string" && Boolean(id.trim())))] : [];
  } catch { return []; }
}
/** orderResumeFolders 最近点击放最前，新收藏夹继续按服务端顺序显示。 */
export function orderResumeFolders(folders: ResumeFolder[], order: string[]): ResumeFolder[] {
  const ranks = new Map(order.map((id, index) => [id, index]));
  return [...folders].sort((first, second) => (ranks.get(first.id) ?? Number.MAX_SAFE_INTEGER) - (ranks.get(second.id) ?? Number.MAX_SAFE_INTEGER));
}
/** rememberResumeFolder 将本次点击移到队首，去除已删除收藏夹及重复记录。 */
export function rememberResumeFolder(id: string, order: string[], folders: ResumeFolder[]): string[] {
  const existing = new Set(folders.map((folder) => folder.id));
  return [...new Set([id, ...order])].filter((value) => existing.has(value));
}
