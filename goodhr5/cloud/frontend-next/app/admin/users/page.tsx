/** 本文件负责超级管理员用户搜索、分页、会员调整和程序解绑。 */
"use client";

import LinkOffRoundedIcon from "@mui/icons-material/LinkOffRounded";
import SearchRoundedIcon from "@mui/icons-material/SearchRounded";
import { Box, Button, Chip, MenuItem, Pagination, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** UsersPage 提供超级管理员用户管理功能。 */
export default function UsersPage() {
  const { user, notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [stats, setStats] = useState<any>({});
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [form, setForm] = useState({ email: "", days: 7, reason: "" });
  const [loading, setLoading] = useState(false);

  /** load 按分页和关键词读取用户。 */
  async function load(nextPage = page) { setLoading(true); try { const params = new URLSearchParams({ page: String(nextPage), page_size: String(pageSize) }); if (query.trim()) params.set("q", query.trim()); const data = await cloudRequest(`/api/admin/users?${params}`); setItems(data.users || []); setStats(data.stats || {}); setTotal(Number(data.total || 0)); setPage(Number(data.page || nextPage)); } catch (error) { notify(error instanceof Error ? error.message : "用户列表读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { if (user?.role === "super_admin") void load(1); }, [user]);

  /** adjust 调整指定用户会员天数。 */
  async function adjust() { if (!form.email.trim() || !Number(form.days)) return notify("请选择用户并填写调整天数", "warning"); try { await cloudRequest("/api/admin/users", { method: "POST", body: form }); notify("会员天数已调整", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "调整失败", "error"); } }

  /** unbind 解除指定用户本地程序绑定。 */
  async function unbind(item: any) { if (!(await confirm("解绑本地程序", `确认解除 ${item.email} 的本地程序绑定吗？`))) return; try { await cloudRequest("/api/admin/users/unbind-agent", { method: "POST", body: { email: item.email } }); notify("本地程序已解绑", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "解绑失败", "error"); } }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;
  return <><PageHeader title="用户管理" description="搜索用户、查看注册和绑定情况，并调整会员天数。" /><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", md: "repeat(2, 220px)" }, gap: 2, mb: 2 }}><SectionPanel><Typography color="text.secondary" sx={{ fontSize: 13 }}>今日注册</Typography><Typography sx={{ mt: 1, fontSize: 30, fontWeight: 800 }}>{Number(stats.today_registered_count || 0)}</Typography></SectionPanel><SectionPanel><Typography color="text.secondary" sx={{ fontSize: 13 }}>绑定程序</Typography><Typography sx={{ mt: 1, fontSize: 30, fontWeight: 800 }}>{Number(stats.agent_binding_count || 0)}</Typography></SectionPanel></Box><SectionPanel sx={{ mb: 2 }}><Stack direction={{ xs: "column", md: "row" }} spacing={1.5}><TextField label="搜索用户" value={query} onChange={(event) => setQuery(event.target.value)} fullWidth /><TextField select label="每页" value={pageSize} onChange={(event) => setPageSize(Number(event.target.value))} sx={{ minWidth: 100 }}><MenuItem value={10}>10</MenuItem><MenuItem value={20}>20</MenuItem><MenuItem value={50}>50</MenuItem></TextField><Button variant="contained" startIcon={<SearchRoundedIcon />} onClick={() => void load(1)}>搜索</Button></Stack><Stack direction={{ xs: "column", md: "row" }} spacing={1.5} sx={{ mt: 2 }}><TextField label="用户邮箱" value={form.email} onChange={(event) => setForm({ ...form, email: event.target.value })} fullWidth /><TextField label="调整天数" type="number" value={form.days} onChange={(event) => setForm({ ...form, days: Number(event.target.value) })} /><TextField label="调整原因" value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} fullWidth /><Button variant="outlined" onClick={() => void adjust()}>确认调整</Button></Stack></SectionPanel><SectionPanel>{items.length ? <Stack>{items.map((item) => <Stack key={item.email} direction={{ xs: "column", lg: "row" }} spacing={2} sx={{ py: 1.75, borderBottom: "1px solid", borderColor: "divider", alignItems: { lg: "center" } }}><Box sx={{ flex: 1 }}><Typography sx={{ fontWeight: 760 }}>{item.email}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>注册：{formatDate(item.created_at)} · 邀请人：{item.inviter_email || "--"}</Typography></Box><Chip label={item.role_label || item.role || "用户"} /><Chip color={item.subscription?.active ? "success" : "default"} label={`${item.subscription?.member_type || "免费"} · ${formatDate(item.subscription?.expires_at)}`} /><Typography sx={{ fontSize: 13 }}>{item.agent?.machine_id ? `已绑定 ${String(item.agent.machine_id).slice(0, 8)}` : "未绑定"}</Typography><Stack direction="row"><Button onClick={() => setForm({ email: item.email, days: 7, reason: "超级管理员增加会员天数" })}>加天数</Button><Button onClick={() => setForm({ email: item.email, days: -7, reason: "超级管理员减少会员天数" })}>减天数</Button><Button color="error" startIcon={<LinkOffRoundedIcon />} onClick={() => void unbind(item)}>解绑</Button></Stack></Stack>)}</Stack> : <EmptyState text={loading ? "正在读取用户" : "暂无用户"} />}<Stack sx={{ mt: 2, alignItems: "center" }}><Pagination page={page} count={Math.max(1, Math.ceil(total / pageSize))} onChange={(_, value) => void load(value)} /></Stack></SectionPanel></>;
}
