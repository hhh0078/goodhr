/** 本文件负责新版后台平台账号的创建、打开、重新登录和删除。 */
"use client";

import AddRoundedIcon from "@mui/icons-material/AddRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import LaunchRoundedIcon from "@mui/icons-material/LaunchRounded";
import LoginRoundedIcon from "@mui/icons-material/LoginRounded";
import { Button, Chip, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate, localRequest } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";

const defaultURLs: Record<string, string> = { boss: "https://www.zhipin.com/web/chat/recommend", zhaopin: "https://rd6.zhaopin.com", liepin: "https://lpt.liepin.com" };

/** AccountsPage 管理云端账号信息和本地浏览器资料目录。 */
export default function AccountsPage() {
  const { agentBase, notify, confirm } = useAdmin();
  const [accounts, setAccounts] = useState<any[]>([]);
  const [platforms, setPlatforms] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ platform_id: "boss", display_name: "" });

  /** load 读取平台账号和平台公开配置。 */
  async function load() {
    setLoading(true);
    try {
      const [accountData, platformData] = await Promise.all([cloudRequest("/api/platform-accounts"), cloudRequest("/api/platforms/config/", { auth: false })]);
      setAccounts(accountData.accounts || []);
      setPlatforms(platformData.platforms || platformData.configs || []);
    } catch (error) { notify(error instanceof Error ? error.message : "平台账号加载失败", "error"); }
    finally { setLoading(false); }
  }

  useEffect(() => { void load(); }, []);

  /** create 创建云端账号并立即打开本地登录页。 */
  async function create() {
    if (!form.display_name.trim()) return notify("请填写账号名称", "warning");
    if (!agentBase) return notify("请先启动本地程序", "error");
    setLoading(true);
    let created: any = null;
    try {
      const data = await cloudRequest("/api/platform-accounts/create", { method: "POST", body: form });
      created = data.account || data;
      await openAccount(created);
      setForm({ ...form, display_name: "" }); setShowForm(false); notify("账号已创建，请在浏览器中完成登录", "success"); await load();
    } catch (error) {
      if (created?.id) await cloudRequest(`/api/platform-accounts/${created.id}`, { method: "DELETE" }).catch(() => undefined);
      notify(error instanceof Error ? error.message : "创建账号失败", "error");
    } finally { setLoading(false); }
  }

  /** openAccount 使用云端账号 ID 作为本地浏览器资料目录打开平台。 */
  async function openAccount(account: any) {
    if (!agentBase) throw new Error("请先启动本地程序");
    const config = platforms.find((item) => String(item.platform_id || item.id) === account.platform_id) || {};
    const url = String(config.entry_url || config.login_url || config.url || defaultURLs[account.platform_id] || "");
    await localRequest(agentBase, "/api/v1/browser/start", { method: "POST", body: { url, persistent: true, platform_account_id: account.id, user_data_dir: account.id, headless: false, humanize: true } });
  }

  /** remove 删除指定平台账号。 */
  async function remove(account: any) {
    if (!(await confirm("删除平台账号", `确认删除“${account.display_name || account.id}”吗？本地浏览器目录不会自动删除。`))) return;
    try { await cloudRequest(`/api/platform-accounts/${account.id}`, { method: "DELETE" }); notify("账号已删除", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "删除失败", "error"); }
  }

  return <><PageHeader title="平台账号" description="云端保存账号名称，本地程序保存招聘平台登录状态。" actions={<><Button variant="contained" startIcon={<AddRoundedIcon />} onClick={() => setShowForm(true)}>新增账号</Button><RefreshButton loading={loading} onClick={() => void load()} /></>} /><SectionPanel>{accounts.length ? <Stack divider={<span style={{ borderTop: "1px solid #dce5e0" }} />}>{accounts.map((account) => <Stack key={account.id} direction={{ xs: "column", md: "row" }} spacing={2} sx={{ py: 2, alignItems: { md: "center" } }}><Stack sx={{ flex: 1 }}><Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><Typography sx={{ fontWeight: 760 }}>{account.display_name || "未命名账号"}</Typography><Chip size="small" label={platformLabel(account.platform_id)} /></Stack><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>状态：{account.status === "available" ? "已登录" : "需要登录"} · 更新：{formatDate(account.updated_at)}</Typography></Stack><Stack direction="row" spacing={1} sx={{ flexWrap: "wrap" }}><Button variant="outlined" startIcon={<LaunchRoundedIcon />} onClick={() => void openAccount(account).then(() => notify("浏览器已打开", "success")).catch((error) => notify(error.message, "error"))}>打开</Button><Button startIcon={<LoginRoundedIcon />} onClick={() => void openAccount(account).then(() => notify("请在浏览器中重新登录", "info")).catch((error) => notify(error.message, "error"))}>重新登录</Button><Button color="error" startIcon={<DeleteOutlineRoundedIcon />} onClick={() => void remove(account)}>删除</Button></Stack></Stack>)}</Stack> : <EmptyState text="暂无平台账号" />}</SectionPanel><AdminDialog open={showForm} title="新增平台账号" description="创建后将立即打开浏览器，请在招聘平台中完成登录。" confirmText="创建并登录" loading={loading} confirmDisabled={!form.display_name.trim()} onClose={() => setShowForm(false)} onConfirm={() => void create()}><Stack spacing={2.5}><ChoiceCards label="招聘平台" value={form.platform_id} columns={3} onChange={(value) => setForm({ ...form, platform_id: String(value) })} options={[{ value: "boss", label: "Boss直聘", description: "当前主要支持的平台。" }, { value: "zhaopin", label: "智联招聘", description: "平台适配开发中。", disabled: true }, { value: "liepin", label: "猎聘", description: "平台适配开发中。", disabled: true }]} /><TextField label="账号名称" value={form.display_name} onChange={(event) => setForm({ ...form, display_name: event.target.value })} fullWidth placeholder="例如：成都招聘账号" helperText="用于在任务和控制台中区分不同招聘账号。" /></Stack></AdminDialog></>;
}

/** platformLabel 返回平台中文名称。 */
function platformLabel(platformID: string) {
  return platformID === "boss" ? "Boss直聘" : platformID === "zhaopin" ? "智联招聘" : platformID === "liepin" ? "猎聘" : platformID || "未知平台";
}
