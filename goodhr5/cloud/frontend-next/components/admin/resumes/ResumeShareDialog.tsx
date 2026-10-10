/** 文件作用说明：创建免登录简历分享链接和本地二维码，并提供撤销全部分享链接的入口。 */
"use client";
import { Alert, Button, Flex, Input, QRCode, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";

/** ResumeShareDialog 仅由用户打开时生成链接，展示内容范围并允许立即取消分享。 */
export default function ResumeShareDialog({ open, candidateID, onClose }: { open: boolean; candidateID: string; onClose: () => void }) {
  const { notify } = useAdmin();
  const [shareURL, setShareURL] = useState("");
  const [busy, setBusy] = useState(false);
  const [revoked, setRevoked] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);

  /** createLink 生成不可猜测链接，保留已发送的其它链接，重复点击不会叠加请求。 */
  async function createLink() {
    if (!candidateID || inFlight.current) return;
    inFlight.current = true; setBusy(true); setError("");
    try {
      const data = await cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}/share`, { method: "POST" });
      if (!/^[a-f0-9]{64}$/.test(data.token || "")) throw new Error("链接没有生成完整，请再试一次");
      if (alive.current) { setShareURL(new URL(`/resumes/share/${data.token}`, window.location.origin).href); setRevoked(false); }
    } catch (reason) {
      if (alive.current) setError(reason instanceof Error ? reason.message : "分享没生成成功，请再试一次");
    } finally { inFlight.current = false; if (alive.current) setBusy(false); }
  }
  useEffect(() => { if (open && !shareURL && !revoked && !inFlight.current) void createLink(); }, [open]);

  /** revokeShare 明确撤销这份简历的全部链接，已发送的链接和二维码同步失效。 */
  async function revokeShare() {
    if (inFlight.current) return;
    inFlight.current = true; setBusy(true); setError("");
    try {
      await cloudRequest(`/api/candidates/${encodeURIComponent(candidateID)}/share`, { method: "DELETE" });
      if (alive.current) { setShareURL(""); setRevoked(true); notify("分享已取消，之前的链接和二维码都打不开了。", "success"); }
    } catch (reason) {
      if (alive.current) setError(reason instanceof Error ? reason.message : "分享还没取消成功，请再试一次");
    } finally { inFlight.current = false; if (alive.current) setBusy(false); }
  }

  /** copyLink 复制当前分享地址，浏览器不允许复制时保留可手动选择的地址。 */
  async function copyLink() {
    try { await navigator.clipboard.writeText(shareURL); notify("链接已复制，发给对方就能看。", "success"); }
    catch { notify("这次没复制成功，可以直接选中上面的链接复制。", "info"); }
  }

  return <AdminDialog open={open} title="分享简历" onClose={onClose} cancelText="收好" loading={busy} onConfirm={shareURL ? () => void copyLink() : () => void createLink()} confirmText={shareURL ? "复制链接" : revoked ? "重新开启分享" : "重新生成"} extraActions={!revoked ? <Button danger onClick={() => void revokeShare()} disabled={busy}>取消分享</Button> : null}>
    <Flex vertical gap={12}>
      <Typography.Text type="secondary">对方无需登录。链接持续有效，取消分享或删除简历后失效。</Typography.Text>
      <Alert type="info" showIcon title="只分享简历资料" description="包含简历中的联系方式；内部备注、推荐记录、AI评分、完整原文和AI摘要不对外展示。" />
      {error ? <Alert type="warning" showIcon title={error} /> : null}
      {busy && !shareURL ? <Flex justify="center" style={{ padding: 24 }}><Spin tip="我正在生成链接" /></Flex> : null}
      {shareURL ? <>
        <Input value={shareURL} readOnly aria-label="简历分享链接" onFocus={(event) => event.target.select()} />
        <Flex vertical align="center" gap={8}><QRCode value={shareURL} size={180} color="#183d31" /><Typography.Text type="secondary">扫一扫，也能打开这份简历</Typography.Text></Flex>
        <Typography.Text type="secondary">取消分享会让这份简历的所有分享链接立即失效。</Typography.Text>
      </> : revoked ? <Alert type="success" showIcon title="分享已取消" description="之前发出的链接和二维码已失效，需要时可以重新开启。" /> : null}
    </Flex>
  </AdminDialog>;
}
