/** 本文件负责新版后台登录状态、侧栏、消息提醒和页面框架。 */
"use client";

import AdminPanelSettingsRoundedIcon from "@mui/icons-material/AdminPanelSettingsRounded";
import ArticleRoundedIcon from "@mui/icons-material/ArticleRounded";
import BadgeRoundedIcon from "@mui/icons-material/BadgeRounded";
import CreditCardRoundedIcon from "@mui/icons-material/CreditCardRounded";
import DashboardRoundedIcon from "@mui/icons-material/DashboardRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import GroupRoundedIcon from "@mui/icons-material/GroupRounded";
import HelpRoundedIcon from "@mui/icons-material/HelpRounded";
import KeyRoundedIcon from "@mui/icons-material/KeyRounded";
import LogoutRoundedIcon from "@mui/icons-material/LogoutRounded";
import MenuRoundedIcon from "@mui/icons-material/MenuRounded";
import PaidRoundedIcon from "@mui/icons-material/PaidRounded";
import PersonRoundedIcon from "@mui/icons-material/PersonRounded";
import SettingsRoundedIcon from "@mui/icons-material/SettingsRounded";
import StorageRoundedIcon from "@mui/icons-material/StorageRounded";
import TaskAltRoundedIcon from "@mui/icons-material/TaskAltRounded";
import WorkRoundedIcon from "@mui/icons-material/WorkRounded";
import { Alert, AppBar, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Drawer, IconButton, Snackbar, Stack, Toolbar, Typography } from "@mui/material";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import BrandMark from "@/components/BrandMark";
import { TOKEN_KEY } from "@/lib/api";
import { cloudRequest, detectLocalAgent } from "@/lib/admin-api";

type AdminContextValue = {
  user: any;
  agentBase: string;
  refreshAgent: () => Promise<void>;
  notify: (message: string, severity?: "success" | "error" | "warning" | "info") => void;
  confirm: (title: string, message: string) => Promise<boolean>;
};

const AdminContext = createContext<AdminContextValue | null>(null);
const drawerWidth = 232;

const baseMenu = [
  ["/admin", "控制台", DashboardRoundedIcon], ["/admin/accounts", "平台账号", BadgeRoundedIcon], ["/admin/positions", "岗位管理", WorkRoundedIcon], ["/admin/tasks", "任务列表", TaskAltRoundedIcon], ["/admin/resumes", "简历库", ArticleRoundedIcon], ["/admin/team", "团队管理", GroupRoundedIcon], ["/admin/invitations", "邀请奖励", KeyRoundedIcon], ["/admin/personal-config", "个人配置", SettingsRoundedIcon], ["/admin/subscription", "订阅会员", CreditCardRoundedIcon], ["/admin/local-data", "本地数据", StorageRoundedIcon], ["/admin/help", "常见问题", HelpRoundedIcon], ["/admin/agent-download", "组件信息", DownloadRoundedIcon],
] as const;

const superMenu = [
  ["/admin/users", "用户管理", PersonRoundedIcon], ["/admin/activation-codes", "激活码", AdminPanelSettingsRoundedIcon], ["/admin/payment-records", "支付记录", PaidRoundedIcon], ["/admin/system-config", "系统配置", SettingsRoundedIcon],
] as const;

/** useAdmin 返回后台全局状态。 */
export function useAdmin() {
  const value = useContext(AdminContext);
  if (!value) throw new Error("后台上下文尚未初始化");
  return value;
}

/** AdminApp 输出后台统一布局并完成用户身份校验。 */
export default function AdminApp({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<any>(null);
  const [agentBase, setAgentBase] = useState("");
  const [notice, setNotice] = useState({ open: false, message: "", severity: "info" as "success" | "error" | "warning" | "info" });
  const [confirmState, setConfirmState] = useState<{ open: boolean; title: string; message: string; resolve?: (value: boolean) => void }>({ open: false, title: "", message: "" });

  /** refreshAgent 重新探测本地程序。 */
  async function refreshAgent() {
    setAgentBase(await detectLocalAgent());
  }

  /** notify 显示统一右上角轻提示。 */
  function notify(message: string, severity: "success" | "error" | "warning" | "info" = "info") {
    setNotice({ open: true, message, severity });
  }

  /** confirm 显示需要用户确认的中间弹框。 */
  function confirm(title: string, message: string) {
    return new Promise<boolean>((resolve) => setConfirmState({ open: true, title, message, resolve }));
  }

  /** closeConfirm 关闭确认弹框并返回选择结果。 */
  function closeConfirm(value: boolean) {
    confirmState.resolve?.(value);
    setConfirmState({ open: false, title: "", message: "" });
  }

  useEffect(() => {
    let active = true;
    const token = localStorage.getItem(TOKEN_KEY) || "";
    if (!token) {
      router.replace(`/login?next=${encodeURIComponent(pathname)}`);
      return;
    }
    cloudRequest("/api/auth/me").then((data) => { if (active) setUser(data.user || data); }).catch(() => { localStorage.removeItem(TOKEN_KEY); router.replace("/login"); }).finally(() => { if (active) setLoading(false); });
    void refreshAgent();
    return () => { active = false; };
  }, [pathname, router]);

  const contextValue = useMemo(() => ({ user, agentBase, refreshAgent, notify, confirm }), [user, agentBase]);
  const menu = user?.role === "super_admin" ? [...baseMenu, ...superMenu] : baseMenu;

  /** logout 清除登录状态并返回登录页。 */
  function logout() {
    localStorage.removeItem(TOKEN_KEY);
    router.replace("/login");
  }

  const drawer = <Box sx={{ height: "100%", display: "flex", flexDirection: "column", bgcolor: "#ffffff" }}><Box sx={{ px: 2.25, py: 2.5 }}><BrandMark /></Box><Stack component="nav" spacing={0.5} sx={{ px: 1.25, overflowY: "auto" }}>{menu.map(([href, label, Icon]) => { const active = href === "/admin" ? pathname === href : pathname.startsWith(href); return <Button key={href} component={Link} href={href} color={active ? "primary" : "secondary"} startIcon={<Icon />} onClick={() => setMobileOpen(false)} sx={{ justifyContent: "flex-start", minHeight: 42, px: 1.5, bgcolor: active ? "#edf7f1" : "transparent" }}>{label}</Button>; })}</Stack><Box sx={{ mt: "auto", p: 1.5, borderTop: "1px solid", borderColor: "divider" }}><Button color="secondary" startIcon={<LogoutRoundedIcon />} onClick={logout} fullWidth sx={{ justifyContent: "flex-start" }}>退出登录</Button></Box></Box>;

  if (loading) return <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center" }}><CircularProgress /></Box>;

  return <AdminContext.Provider value={contextValue}><Box sx={{ minHeight: "100vh", bgcolor: "#f4f7f5" }}><AppBar position="fixed" color="inherit" elevation={0} sx={{ ml: { md: `${drawerWidth}px` }, width: { md: `calc(100% - ${drawerWidth}px)` }, borderBottom: "1px solid", borderColor: "divider" }}><Toolbar sx={{ gap: 1.5 }}><IconButton aria-label="打开菜单" onClick={() => setMobileOpen(true)} sx={{ display: { md: "none" } }}><MenuRoundedIcon /></IconButton><Box sx={{ flex: 1 }}><Typography sx={{ fontWeight: 750 }}>{user?.email || "GoodHR 控制台"}</Typography><Typography sx={{ color: agentBase ? "primary.main" : "error.main", fontSize: 12 }}>{agentBase ? `本地程序已连接 · ${agentBase.replace("http://127.0.0.1:", "端口 ")}` : "本地程序未连接"}</Typography></Box><Button onClick={() => void refreshAgent()} variant="outlined" size="small">重新检测</Button></Toolbar></AppBar><Box component="aside" sx={{ display: { xs: "none", md: "block" }, width: drawerWidth, position: "fixed", inset: "0 auto 0 0", borderRight: "1px solid", borderColor: "divider" }}>{drawer}</Box><Drawer open={mobileOpen} onClose={() => setMobileOpen(false)} sx={{ display: { md: "none" }, "& .MuiDrawer-paper": { width: drawerWidth } }}>{drawer}</Drawer><Box component="main" sx={{ ml: { md: `${drawerWidth}px` }, pt: "64px", minHeight: "100vh" }}><Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1440, mx: "auto" }}>{children}</Box></Box><Snackbar open={notice.open} autoHideDuration={3000} onClose={() => setNotice((value) => ({ ...value, open: false }))} anchorOrigin={{ vertical: "top", horizontal: "right" }}><Alert severity={notice.severity} variant="filled">{notice.message}</Alert></Snackbar><Dialog open={confirmState.open} onClose={() => closeConfirm(false)}><DialogTitle>{confirmState.title}</DialogTitle><DialogContent><Typography color="text.secondary">{confirmState.message}</Typography></DialogContent><DialogActions><Button onClick={() => closeConfirm(false)} color="secondary">取消</Button><Button onClick={() => closeConfirm(true)} variant="contained">确认</Button></DialogActions></Dialog></Box></AdminContext.Provider>;
}
