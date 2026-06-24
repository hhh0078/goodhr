/** 本文件负责超级管理员用户搜索、用户画像标签、分页、会员调整和程序解绑。 */
"use client";

import LinkOffRoundedIcon from "@mui/icons-material/LinkOffRounded";
import SearchRoundedIcon from "@mui/icons-material/SearchRounded";
import { Box, Button, Chip, MenuItem, Pagination, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";

type NotificationProfile = {
  completed?: boolean;
  user_type?: string;
  gender?: string;
  platforms?: string[];
  os?: string;
  browser?: string;
};

type AdminUserItem = {
  email: string;
  role: string;
  status: string;
  inviter_email?: string;
  created_at?: string;
  last_login_at?: string;
  subscription?: { member_type?: string; expires_at?: string; active?: boolean };
  agent?: { machine_id?: string; agent_version?: string };
  notification_profile?: NotificationProfile;
};

/** UsersPage 提供超级管理员用户列表和用户操作。 */
export default function UsersPage() {
  const { user, notify, confirm } = useAdmin();
  const [items, setItems] = useState<AdminUserItem[]>([]);
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
    try {
      const params = new URLSearchParams({ page: String(nextPage), page_size: String(nextPageSize) });
      if (nextQuery.trim()) params.set("q", nextQuery.trim());
      const data = await cloudRequest(`/api/admin/users?${params}`);
      setItems(data.users || []);
      setStats(data.stats || {});
      setTotal(Number(data.total || 0));
      setPage(Number(data.page || nextPage));
    } catch (error) {
      notify(error instanceof Error ? error.message : "用户列表读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (user?.role === "super_admin") void load(1);
  }, [user]);

  /** openAdjust 打开会员天数调整弹框。 */
  function openAdjust(item: AdminUserItem, days: number) {
    setForm({ email: item.email, days, reason: days > 0 ? "补偿会员天数" : "扣减会员天数" });
    setDialogOpen(true);
  }

  /** adjust 提交会员天数调整。 */
  async function adjust() {
    try {
      await cloudRequest("/api/admin/users", { method: "PUT", body: form });
      notify("会员时间已调整", "success");
      setDialogOpen(false);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "调整失败", "error");
    }
  }

  /** unbind 解除用户本地程序绑定。 */
  async function unbind(item: AdminUserItem) {
    const ok = await confirm("确认解绑本地程序", `要解绑 ${item.email} 的本地程序吗？`);
    if (!ok) return;
    try {
      await cloudRequest("/api/admin/users/unbind-agent", { method: "POST", body: { email: item.email } });
      notify("本地程序已解绑", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "解绑失败", "error");
    }
  }

  /** resetSearch 清空搜索条件并返回第一页。 */
  function resetSearch() {
    setQuery("");
    setPage(1);
    void load(1, pageSize, "");
  }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;

  return <>
    <PageHeader title="用户管理" description="查看注册、登录、会员、用户画像和本地程序绑定情况。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} />
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", md: "repeat(2, 220px)" }, gap: 1.5, mb: 2 }}>
      <Metric label="今日注册" value={Number(stats.today_registered_count || 0)} />
      <Metric label="绑定程序" value={Number(stats.agent_binding_count || 0)} />
    </Box>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "minmax(260px, 520px) 110px auto auto" }, gap: 1.25, mb: 2, alignItems: "center" }}>
      <TextField size="small" label="搜索用户" value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void load(1); }} placeholder="邮箱、角色、状态或邀请人" />
      <TextField select size="small" label="每页" value={pageSize} onChange={(event) => { const size = Number(event.target.value); setPageSize(size); void load(1, size); }}>
        <MenuItem value={10}>10</MenuItem>
        <MenuItem value={20}>20</MenuItem>
        <MenuItem value={50}>50</MenuItem>
        <MenuItem value={100}>100</MenuItem>
      </TextField>
      <Button variant="contained" startIcon={<SearchRoundedIcon />} onClick={() => void load(1)}>搜索</Button>
      <Button color="secondary" onClick={resetSearch}>重置</Button>
    </Box>

    <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
      {items.length ? <>
        <TableContainer sx={{ display: { xs: "none", md: "block" } }}>
          <Table size="small" sx={{ tableLayout: "fixed" }}>
            <TableHead>
              <TableRow sx={{ bgcolor: "#f6faf7" }}>
                <TableCell sx={{ width: "32%" }}>用户</TableCell>
                <TableCell sx={{ width: "16%" }}>会员</TableCell>
                <TableCell sx={{ width: "18%" }}>时间</TableCell>
                <TableCell sx={{ width: "18%" }}>本地程序</TableCell>
                <TableCell sx={{ width: "16%" }}>操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {items.map((item) => <TableRow key={item.email} hover sx={{ "& td": { py: 1.5, verticalAlign: "top" } }}>
                <TableCell><UserIdentity item={item} /></TableCell>
                <TableCell><SubscriptionInfo item={item} /></TableCell>
                <TableCell><TimeInfo item={item} /></TableCell>
                <TableCell><AgentInfo item={item} /></TableCell>
                <TableCell><UserActions item={item} openAdjust={openAdjust} unbind={unbind} /></TableCell>
              </TableRow>)}
            </TableBody>
          </Table>
        </TableContainer>
        <Stack spacing={1.25} sx={{ display: { xs: "flex", md: "none" }, p: 1.25 }}>
          {items.map((item) => <UserCard key={item.email} item={item} openAdjust={openAdjust} unbind={unbind} />)}
        </Stack>
      </> : <EmptyState text={loading ? "正在读取用户" : "暂无用户"} />}
    </SectionPanel>

    <Stack direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ mt: 2, justifyContent: "space-between", alignItems: "center" }}>
      <Typography color="text.secondary">共 {total} 个用户，第 {page} / {Math.max(1, Math.ceil(total / pageSize))} 页</Typography>
      <Pagination page={page} count={Math.max(1, Math.ceil(total / pageSize))} onChange={(_, value) => void load(value)} />
    </Stack>

    <AdminDialog open={dialogOpen} title="调整会员时间" description={`当前用户：${form.email || "--"}`} confirmText="确认调整" loading={loading} onClose={() => setDialogOpen(false)} onConfirm={() => void adjust()}>
      <Stack spacing={2}>
        <TextField label="用户邮箱" value={form.email} disabled fullWidth />
        <TextField label="调整天数" type="number" value={form.days} onChange={(event) => setForm({ ...form, days: Number(event.target.value) })} fullWidth helperText="正数增加，负数减少，不能为 0。" />
        <TextField label="调整原因" value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} fullWidth />
      </Stack>
    </AdminDialog>
  </>;
}

/** UserCard 展示移动端用户卡片。 */
function UserCard({ item, openAdjust, unbind }: { item: AdminUserItem; openAdjust: (item: AdminUserItem, days: number) => void; unbind: (item: AdminUserItem) => Promise<void> }) {
  return <Box sx={{ p: 1.5, border: "1px solid", borderColor: "divider", borderRadius: "8px", bgcolor: "#fff" }}>
    <UserIdentity item={item} />
    <Box sx={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 1.5, mt: 1.5 }}>
      <SubscriptionInfo item={item} />
      <TimeInfo item={item} />
    </Box>
    <Box sx={{ mt: 1.5 }}><AgentInfo item={item} /></Box>
    <Box sx={{ mt: 1.5 }}><UserActions item={item} openAdjust={openAdjust} unbind={unbind} /></Box>
  </Box>;
}

/** UserIdentity 展示邮箱、角色和通知画像标签。 */
function UserIdentity({ item }: { item: AdminUserItem }) {
  return <Stack spacing={0.75} sx={{ minWidth: 0 }}>
    <Typography noWrap title={item.email} sx={{ fontFamily: "monospace", fontSize: 13, fontWeight: 760 }}>{item.email}</Typography>
    <Stack direction="row" spacing={0.75} sx={{ flexWrap: "wrap", rowGap: 0.75 }}>
      <SmallTag label={roleText(item.role)} />
      <SmallTag label={statusText(item.status)} />
      {profileTags(item.notification_profile).map((tag) => <SmallTag key={tag} label={tag} />)}
    </Stack>
    {item.inviter_email ? <Typography noWrap title={item.inviter_email} sx={{ color: "text.secondary", fontSize: 11 }}>邀请：{item.inviter_email}</Typography> : null}
  </Stack>;
}

/** SubscriptionInfo 展示会员状态和到期时间。 */
function SubscriptionInfo({ item }: { item: AdminUserItem }) {
  return <Stack spacing={0.5}>
    <Chip size="small" color={item.subscription?.active ? "success" : "default"} label={item.subscription?.active ? "会员有效" : "已过期"} sx={{ width: "fit-content" }} />
    <Typography sx={{ fontSize: 12 }}>{item.subscription?.member_type || "免费版"}</Typography>
    <Typography sx={{ color: "text.secondary", fontSize: 11 }}>{formatDate(item.subscription?.expires_at)}</Typography>
  </Stack>;
}

/** TimeInfo 展示注册时间和最近登录时间。 */
function TimeInfo({ item }: { item: AdminUserItem }) {
  return <Stack spacing={0.5}>
    <Typography sx={{ fontSize: 12, fontWeight: 720 }}>注册 / 最近登录</Typography>
    <Typography sx={{ color: "text.secondary", fontSize: 11 }}>注册：{formatDate(item.created_at)}</Typography>
    <Typography sx={{ color: "text.secondary", fontSize: 11 }}>登录：{formatDate(item.last_login_at) || "暂无"}</Typography>
  </Stack>;
}

/** AgentInfo 展示本地程序绑定状态。 */
function AgentInfo({ item }: { item: AdminUserItem }) {
  return <Stack spacing={0.5}>
    <Typography sx={{ fontSize: 12, fontWeight: 720 }}>本地程序</Typography>
    <Typography noWrap sx={{ fontSize: 12 }}>{item.agent?.machine_id ? String(item.agent.machine_id).slice(0, 14) : "未绑定"}</Typography>
    <Typography noWrap sx={{ color: "text.secondary", fontSize: 11 }}>{item.agent?.agent_version || "暂无版本"}</Typography>
  </Stack>;
}

/** UserActions 展示用户管理操作按钮。 */
function UserActions({ item, openAdjust, unbind }: { item: AdminUserItem; openAdjust: (item: AdminUserItem, days: number) => void; unbind: (item: AdminUserItem) => Promise<void> }) {
  return <Stack direction="row" spacing={0.5} sx={{ flexWrap: "wrap", rowGap: 0.75 }}>
    <Button size="small" onClick={() => openAdjust(item, 7)}>加天数</Button>
    <Button size="small" onClick={() => openAdjust(item, -7)}>减天数</Button>
    <Button size="small" color="error" startIcon={<LinkOffRoundedIcon />} onClick={() => void unbind(item)}>解绑</Button>
  </Stack>;
}

/** Metric 展示用户管理顶部统计。 */
function Metric({ label, value }: { label: string; value: number }) {
  return <Box sx={{ p: 2, bgcolor: "#f7faf8", border: "1px solid", borderColor: "divider", borderRadius: "8px" }}><Typography color="text.secondary" sx={{ fontSize: 13 }}>{label}</Typography><Typography sx={{ mt: 0.5, fontSize: 30, fontWeight: 800 }}>{value}</Typography></Box>;
}

/** SmallTag 展示用户画像小标签。 */
function SmallTag({ label }: { label: string }) {
  return <Box component="span" sx={{ px: 0.75, py: 0.25, borderRadius: "6px", bgcolor: "#eef6f0", color: "#2f6f4f", fontSize: 11, lineHeight: 1.55 }}>{label}</Box>;
}

/** profileTags 将通知画像转换为用户标签。 */
function profileTags(profile?: NotificationProfile) {
  if (!profile) return ["未填画像"];
  const tags = [userTypeText(profile.user_type), genderText(profile.gender), profile.os, profile.browser, ...(profile.platforms || [])].filter(Boolean) as string[];
  return tags.length ? tags.slice(0, 8) : ["未填画像"];
}

/** roleText 将后端角色转换为中文。 */
function roleText(role: string) {
  return role === "super_admin" ? "超管" : role === "admin" ? "管理员" : "成员";
}

/** statusText 将用户状态转换为中文。 */
function statusText(status: string) {
  return status === "pending" ? "待激活" : status === "disabled" ? "已停用" : "正常";
}

/** userTypeText 将用户身份画像转换为中文。 */
function userTypeText(value?: string) {
  return value === "headhunter" ? "猎头" : value === "hr" ? "企业HR" : value === "recruiting_manager" ? "招聘负责人" : value === "owner" ? "老板/管理者" : "";
}

/** genderText 将性别画像转换为中文。 */
function genderText(value?: string) {
  return value === "female" ? "女" : value === "male" ? "男" : value === "unknown" ? "不方便说" : "";
}
