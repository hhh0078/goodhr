/** 本文件负责新版后台邀请链接、邀请记录和奖励展示。 */
"use client";

import ContentCopyRoundedIcon from "@mui/icons-material/ContentCopyRounded";
import { Box, Button, Chip, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, FormActionRow, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** InvitationsPage 展示当前用户邀请链接和奖励记录。 */
export default function InvitationsPage() {
  const { notify } = useAdmin();
  const [data, setData] = useState<any>({});
  const [loading, setLoading] = useState(false);
  const inviteURL = useMemo(() => {
    const currentOrigin = typeof window === "undefined" ? "https://goodhr5.58it.cn" : window.location.origin;
    const url = new URL(process.env.NEXT_PUBLIC_SITE_URL || currentOrigin);
    if (data.invite_id) url.searchParams.set("invite", data.invite_id);
    return url.toString();
  }, [data.invite_id]);

  /** load 读取邀请汇总和被邀请用户列表。 */
  async function load() { setLoading(true); try { setData(await cloudRequest("/api/invitations/summary")); } catch (error) { notify(error instanceof Error ? error.message : "邀请记录读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { void load(); }, []);

  /** copyLink 复制当前用户邀请链接。 */
  async function copyLink() { try { await navigator.clipboard.writeText(inviteURL); notify("邀请链接已复制", "success"); } catch { notify("复制失败，请手动复制", "error"); } }

  const invitees = data.invitees || [];
  return <><PageHeader title="邀请奖励" description="好友通过邀请链接首次注册后，系统会记录邀请关系和奖励状态。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} /><SectionPanel sx={{ mb: 2 }}><Typography sx={{ fontWeight: 760 }}>专属邀请链接</Typography><Box sx={{ mt: 1.5 }}><FormActionRow field={<TextField value={inviteURL} fullWidth slotProps={{ input: { readOnly: true } }} />} action={<Button variant="contained" startIcon={<ContentCopyRoundedIcon />} onClick={() => void copyLink()}>复制链接</Button>} maxWidth={760} /></Box><Stack direction="row" spacing={3} sx={{ mt: 2, flexWrap: "wrap", rowGap: 1 }}><Typography color="text.secondary">已邀请 <strong>{invitees.length}</strong> 人</Typography><Typography color="text.secondary">累计奖励 <strong>{Number(data.reward_days || data.total_reward_days || 0)}</strong> 天</Typography></Stack></SectionPanel><SectionPanel>{invitees.length ? <Stack>{invitees.map((item: any) => <Stack key={item.id || item.email} direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ py: 2, borderBottom: "1px solid", borderColor: "divider", justifyContent: "space-between" }}><Stack><Typography sx={{ fontWeight: 700 }}>{item.email || "未显示邮箱"}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>注册：{formatDate(item.created_at)}</Typography></Stack><Chip size="small" color={item.invite_registered_rewarded_at ? "success" : "warning"} label={item.invite_registered_rewarded_at ? "已发奖励" : "等待奖励"} /></Stack>)}</Stack> : <EmptyState text="暂无邀请记录" />}</SectionPanel></>;
}
