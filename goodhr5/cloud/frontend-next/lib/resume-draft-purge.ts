/** 文件作用说明：登录及恢复网页时，仅清理当前账号经过云端删除记录核实的简历摘要草稿。 */
import { cloudRequest, getToken } from "./admin-api";
import { summaryCacheKey } from "./resume-summary-api";

const candidateIDPattern = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;

/** reconcileResumeDrafts 分批只发送候选人编号，回包后复核账号、登录凭证和旧草稿原值。 */
export async function reconcileResumeDrafts(user: { id?: string; email?: string }, current: () => boolean): Promise<void> {
  const token = getToken();
  if (!token || !user.id && !user.email) return;
  const prefixes = [summaryCacheKey(user, "")];
  if (user.email && user.id && user.email !== user.id) prefixes.push(summaryCacheKey({ email: user.email }, ""));
  const keys = new Map<string, string[]>();
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (!key) continue;
    const prefix = prefixes.find((candidate) => key.startsWith(candidate) && candidateIDPattern.test(key.slice(candidate.length)));
    if (!prefix) continue;
    const id = key.slice(prefix.length).toLowerCase();
    keys.set(id, [...(keys.get(id) || []), key]);
  }
  const all = [...keys.keys()];
  for (let i = 0; i < all.length; i += 50) {
    if (!current() || token !== getToken()) return;
    const batch = all.slice(i, i + 50);
    const drafts = batch.flatMap((id) => keys.get(id)!.map((key) => ({ id, key, value: localStorage.getItem(key) })));
    const data = await cloudRequest("/api/candidates/deletions/check", { method: "POST", body: { candidate_ids: batch, source_hashes: [] } });
    if (!current() || token !== getToken() || data.ok !== true || !Array.isArray(data.candidate_ids)) return;
    const deleted = new Set(data.candidate_ids);
    for (const draft of drafts) {
      if (draft.value !== null && deleted.has(draft.id) && localStorage.getItem(draft.key) === draft.value) localStorage.removeItem(draft.key);
    }
  }
}
