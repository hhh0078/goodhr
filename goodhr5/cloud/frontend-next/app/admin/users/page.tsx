/** 本文件负责超级管理员用户搜索、稳定表格、分页、会员调整和程序解绑。 */
"use client";

import LinkOffRoundedIcon from "@mui/icons-material/LinkOffRounded";
import SearchRoundedIcon from "@mui/icons-material/SearchRounded";
import { Box, Button, Chip, MenuItem, Pagination, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";

/** UsersPage 提供超级管理员用户列表和用户操作。 */
export default function UsersPage() {
  const { user, notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [stats, setStats] = useState<any>({});
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [form, setForm] = useState({ email: "", days: 7, reason: "" });
  const [dialogOpen, setDialogOpen] = useState(false);
  const [loading, setLoading] = useState(false);

  /** load 按分页和关键词读取用户。 */
  async function load(nextPage = page, nextPageSize = pageSize, nextQuery = query) {
    setLoading(true);
    try { const params = new URLSearchParams({ page: String(nextPage), page_size: String(nextPageSize) }); if (nextQuery.trim()) params.set("q", nextQuery.trim()); const data = await cloudRequest(`/api/admin/users?${params}`); setItems(data.users || []); setStats(data.stats || {}); setTotal(Number(data.total || 0)); setPage(Number(data.page || nextPage)); }
    catch (error) { notify(error instanceof Error ? error.message : "用户列表读取失败", "error"); }
    finally { setLoading(false); }
  }

  useEffect(() => { if (user?.role === "super_admin") void load(1); }, [user]);

  /** openAdjust 打开会员天数调整弹框。 */
  function openAdjust(item: any, days: number) {
    setForm({ email: item.email || "", days, reason: days > 0 ? "超级管理员增加会员天数" : "超级管理员减少会员天数" });
    setDialogOpen(true);
  }

  /** adjust 调整指定用户会员天数。 */
  async function adjust() {
    if (!form.email.trim() || !Number(form.days)) return notify("用户邮箱不能为空，调整天数不能为 0", "warning");
    setLoading(true);
    try { await cloudRequest("/api/admin/users", { method: "POST", body: form }); notify("会员天数已调整", "success"); setDialogOpen(false); await load(); }
    catch (error) { notify(error instanceof Error ? error.message : "调整失败", "error"); }
    finally { setLoading(false); }
  }

  /** unbind 解除指定用户本地程序绑定。 */
  async function unbind(item: any) {
    if (!(await confirm("解绑本地程序", `确认解除 ${item.email} 的本地程序绑定吗？`))) return;
    try { await cloudRequest("/api/admin/users/unbind-agent", { method: "POST", body: { email: item.email } }); notify("本地程序已解绑", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "解绑失败", "error"); }
  }

  /** resetSearch 清空搜索条件并返回第一页。 */
  function resetSearch() { setQuery(""); setPage(1); void load(1, pageSize, ""); }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;
  return <><PageHeader title="用户管理" description="查看注册、会员和本地程序绑定情况。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} />
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", md: "repeat(2, 220px)" }, gap: 1.5, mb: 2 }}><Metric label="今日注册" value={Number(stats.today_registered_count || 0)} /><Metric label="绑定程序" value={Number(stats.agent_binding_count || 0)} /></Box>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "minmax(260px, 520px) 110px auto auto" }, gap: 1.25, mb: 2, alignItems: "center" }}><TextField size="small" label="搜索用户" value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void load(1); }} placeholder="邮箱、角色、状态或邀请人" /><TextField select size="small" label="每页" value={pageSize} onChange={(event) => { const size = Number(event.target.value); setPageSize(size); void load(1, size); }}><MenuItem value={10}>10</MenuItem><MenuItem value={20}>20</MenuItem><MenuItem value={50}>50</MenuItem><MenuItem value={100}>100</MenuItem></TextField><Button variant="contained" startIcon={<SearchRoundedIcon />} onClick={() => void load(1)}>搜索</Button><Button color="secondary" onClick={resetSearch}>重置</Button></Box>
    <SectionPanel>{items.length ? <TableContainer sx={{ overflowX: "auto" }}><Table size="small" sx={{ minWidth: 1180, tableLayout: "fixed" }}><TableHead><TableRow><TableCell sx={{ width: 220 }}>用户</TableCell><TableCell sx={{ width: 92 }}>角色</TableCell><TableCell sx={{ width: 180 }}>会员</TableCell><TableCell sx={{ width: 92 }}>状态</TableCell><TableCell sx={{ width: 180 }}>本地程序</TableCell><TableCell sx={{ width: 190 }}>邀请人</TableCell><TableCell sx={{ width: 180 }}>注册时间</TableCell><TableCell sx={{ width: 280 }}>操作</TableCell></TableRow></TableHead><TableBody>{items.map((item) => <TableRow key={item.email} hover><TableCell><Typography noWrap title={item.email} sx={{ fontFamily: "monospace", fontSize: 13 }}>{item.email}</Typography></TableCell><TableCell>{roleText(item.role)}</TableCell><TableCell><Typography sx={{ fontSize: 13 }}>{item.subscription?.member_type || "免费版"}</Typography><Typography sx={{ color: "text.secondary", fontSize: 11 }}>{formatDate(item.subscription?.expires_at)}</Typography></TableCell><TableCell><Chip size="small" color={item.subscription?.active ? "success" : "default"} label={item.subscription?.active ? "有效" : "已过期"} /></TableCell><TableCell><Typography noWrap sx={{ fontSize: 13 }}>{item.agent?.machine_id ? String(item.agent.machine_id).slice(0, 14) : "未绑定"}</Typography><Typography noWrap sx={{ color: "text.secondary", fontSize: 11 }}>{item.agent?.agent_version || ""}</Typography></TableCell><TableCell><Typography noWrap title={item.inviter_email || "--"} sx={{ fontSize: 13 }}>{item.inviter_email || "--"}</Typography></TableCell><TableCell sx={{ fontSize: 12 }}>{formatDate(item.created_at)}</TableCell><TableCell><Stack direction="row" spacing={0.5} sx={{ whiteSpace: "nowrap" }}><Button size="small" onClick={() => openAdjust(item, 7)}>加天数</Button><Button size="small" onClick={() => openAdjust(item, -7)}>减天数</Button><Button size="small" color="error" startIcon={<LinkOffRoundedIcon />} onClick={() => void unbind(item)}>解绑</Button></Stack></TableCell></TableRow>)}</TableBody></Table></TableContainer> : <EmptyState text={loading ? "正在读取用户" : "暂无用户"} />}</SectionPanel>
    <Stack direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ mt: 2, justifyContent: "space-between", alignItems: "center" }}><Typography color="text.secondary">共 {total} 个用户，第 {page} / {Math.max(1, Math.ceil(total / pageSize))} 页</Typography><Pagination page={page} count={Math.max(1, Math.ceil(total / pageSize))} onChange={(_, value) => void load(value)} /></Stack>
    <AdminDialog open={dialogOpen} title="调整会员时间" description={`当前用户：${form.email || "--"}`} confirmText="确认调整" loading={loading} onClose={() => setDialogOpen(false)} onConfirm={() => void adjust()}><Stack spacing={2}><TextField label="用户邮箱" value={form.email} disabled fullWidth /><TextField label="调整天数" type="number" value={form.days} onChange={(event) => setForm({ ...form, days: Number(event.target.value) })} fullWidth helperText="正数增加，负数减少，不能为 0。" /><TextField label="调整原因" value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} fullWidth /></Stack></AdminDialog>
  </>;
}

/** Metric 展示用户管理顶部统计。 */
function Metric({ label, value }: { label: string; value: number }) { return <Box sx={{ p: 2, bgcolor: "#f7faf8", border: "1px solid", borderColor: "divider", borderRadius: "8px" }}><Typography color="text.secondary" sx={{ fontSize: 13 }}>{label}</Typography><Typography sx={{ mt: 0.5, fontSize: 30, fontWeight: 800 }}>{value}</Typography></Box>; }

/** roleText 将后端角色转换为中文。 */
function roleText(role: string) { return role === "super_admin" ? "超管" : role === "admin" ? "管理员" : "成员"; }
