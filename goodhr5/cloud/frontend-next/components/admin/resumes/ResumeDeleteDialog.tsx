/** 文件作用说明：确认单份简历的永久删除，说明云端和离线设备的清理范围，核对云端回执后再关闭。 */
"use client";
import { Alert, Typography } from "antd";
import { useRef, useState } from "react";
import { APIRequestError } from "@/lib/admin-api";
import { deleteResume } from "@/lib/resume-api";
import { summaryCacheKey } from "@/lib/resume-summary-api";
import type { Resume } from "@/lib/resume-types";
import { useAdmin } from "../AdminApp";
import AdminDialog from "../AdminDialog";

/** deletionError 区分版本与权限变更，并在云端或网络结果不明时引导回到列表核对。 */
function deletionError(cause: unknown): string {
  if (cause instanceof APIRequestError) {
    if (cause.status === 409) return "这份简历刚被更新，本次没有删除。请重新查看后再决定。";
    if (cause.status === 403) return "当前账号没有删除这份简历的权限，请回简历库核对。";
    if (cause.status === 404) return "这份简历已不存在，或当前账号已无权查看。请回简历库核对。";
    if (cause.status >= 500) return "云端没有确认删除结果。请先回简历库核对，再决定是否重试。";
  }
  return cause instanceof Error ? cause.message : "删除结果没能确认，请回简历库核对。";
}

/** ResumeDeleteDialog 只在用户明确确认后删除打开时的候选人和版本；失败时先重新读取，不自动重试。 */
export default function ResumeDeleteDialog({ candidate, onClose, onDeleted, onRecheck }: {
  candidate: Resume; onClose: () => void; onDeleted: (id: string) => void; onRecheck: () => void;
}) {
  const { user, notify } = useAdmin();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submitting = useRef(false);
  const name = candidate.candidate_name || "未命名候选人";

  /** remove 以确认时的版本删除，并在收到同一份简历的成功回执后清掉本浏览器的摘要草稿。 */
  async function remove() {
    if (submitting.current || error) return;
    submitting.current = true;
    setBusy(true);
    try {
      await deleteResume(candidate);
    } catch (cause) {
      setError(deletionError(cause));
      submitting.current = false;
      setBusy(false);
      return;
    }
    try { localStorage.removeItem(summaryCacheKey(user, candidate.id)); }
    catch { notify("本浏览器没能清除摘要草稿，可在浏览器设置中清除此站点数据。", "warning"); }
    notify(`已永久删除「${name}」的简历。`, "success");
    onDeleted(candidate.id);
  }

  return <AdminDialog open title="永久删除简历" maxWidth="xs" confirmDanger
    onClose={error ? onRecheck : onClose} onConfirm={error ? undefined : remove} loading={busy}
    confirmText="确认永久删除" loadingText="正在删除" cancelText={error ? "返回简历库核对" : "取消"}>
    <Typography.Paragraph>永久删除 <strong>{name}</strong> 的简历？删除后无法恢复。</Typography.Paragraph>
    <Typography.Paragraph>这份简历的备注、岗位关联、推荐报告和面试记录会一起清除。分享链接和面试邀请立即失效。</Typography.Paragraph>
    <Typography.Paragraph type="secondary">离线电脑上的 GoodHR 托管文件和数据，会在本地程序联网并登录对应账号后清理。已发出的邮件无法撤回。</Typography.Paragraph>
    {error ? <Alert type="error" showIcon title={error} /> : null}
  </AdminDialog>;
}
