/** 本文件负责新版后台团队成员的邀请、角色调整和移除。 */
"use client";

import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import PersonAddRoundedIcon from "@mui/icons-material/PersonAddRounded";
import { Button, MenuItem, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";

/** TeamPage 管理当前租户的团队成员。 */
export default function TeamPage() {
  const { notify, confirm } = useAdmin();
  const [members, setMembers] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("user");
  const [dialogOpen, setDialogOpen] = useState(false);

  /** load 读取团队成员列表。 */
  async function load() { setLoading(true); try { const data = await cloudRequest("/api/tenants/members"); setMembers(data.members || []); } catch (error) { notify(error instanceof Error ? error.message : "团队成员读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { void load(); }, []);

  /** invite 邀请邮箱加入当前团队。 */
  async function invite() { if (!email.trim()) return notify("请填写成员邮箱", "warning"); try { await cloudRequest("/api/tenants/invite", { method: "POST", body: { email: email.trim(), role } }); setEmail(""); setDialogOpen(false); notify("邀请已发送", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "邀请失败", "error"); } }

  /** updateRole 修改指定成员角色。 */
  async function updateRole(member: any, nextRole: string) { try { await cloudRequest(`/api/tenants/members/${encodeURIComponent(member.email)}`, { method: "PUT", body: { role: nextRole } }); notify("成员角色已更新", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "角色更新失败", "error"); } }

  /** remove 移除指定团队成员。 */
  async function remove(member: any) { if (!(await confirm("移除团队成员", `确认移除 ${member.email} 吗？`))) return; try { await cloudRequest(`/api/tenants/members/${encodeURIComponent(member.email)}`, { method: "DELETE" }); notify("成员已移除", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "移除失败", "error"); } }

  return <><PageHeader title="团队管理" description="邀请同事加入团队并管理成员角色。" actions={<><Button variant="contained" startIcon={<PersonAddRoundedIcon />} onClick={() => setDialogOpen(true)}>邀请成员</Button><RefreshButton loading={loading} onClick={() => void load()} /></>} /><SectionPanel>{members.length ? <Stack>{members.map((member) => <Stack key={member.email} direction={{ xs: "column", md: "row" }} spacing={2} sx={{ py: 2, borderBottom: "1px solid", borderColor: "divider", alignItems: { md: "center" } }}><Stack sx={{ flex: 1 }}><Typography sx={{ fontWeight: 760 }}>{member.email}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>加入时间：{formatDate(member.created_at)}</Typography></Stack><TextField select size="small" value={member.role || "user"} onChange={(event) => void updateRole(member, event.target.value)} sx={{ width: 160 }}><MenuItem value="user">普通成员</MenuItem><MenuItem value="admin">团队管理员</MenuItem><MenuItem value="owner">团队所有者</MenuItem></TextField><Button color="error" startIcon={<DeleteOutlineRoundedIcon />} onClick={() => void remove(member)}>移除</Button></Stack>)}</Stack> : <EmptyState text="暂无团队成员" />}</SectionPanel><AdminDialog open={dialogOpen} title="邀请团队成员" confirmText="发送邀请" confirmDisabled={!email.trim()} onClose={() => setDialogOpen(false)} onConfirm={() => void invite()}><Stack spacing={2.5}><TextField label="成员邮箱" type="email" value={email} onChange={(event) => setEmail(event.target.value)} fullWidth /><ChoiceCards label="成员角色" value={role} onChange={(value) => setRole(String(value))} options={[{ value: "user", label: "普通成员", description: "可以使用团队招聘数据和任务。" }, { value: "admin", label: "团队管理员", description: "可以管理团队成员和业务配置。" }]} /></Stack></AdminDialog></>;
}
