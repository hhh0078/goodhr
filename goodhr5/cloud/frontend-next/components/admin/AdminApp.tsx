/** 本文件负责新版后台身份、悬浮布局、分类菜单、顶部状态和全局消息。 */
"use client";

import AdminPanelSettingsRoundedIcon from "@mui/icons-material/AdminPanelSettingsRounded";
import ArticleRoundedIcon from "@mui/icons-material/ArticleRounded";
import BadgeRoundedIcon from "@mui/icons-material/BadgeRounded";
import CalendarMonthRoundedIcon from "@mui/icons-material/CalendarMonthRounded";
import CreditCardRoundedIcon from "@mui/icons-material/CreditCardRounded";
import DashboardRoundedIcon from "@mui/icons-material/DashboardRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import GroupRoundedIcon from "@mui/icons-material/GroupRounded";
import HelpRoundedIcon from "@mui/icons-material/HelpRounded";
import KeyRoundedIcon from "@mui/icons-material/KeyRounded";
import LogoutRoundedIcon from "@mui/icons-material/LogoutRounded";
import MenuRoundedIcon from "@mui/icons-material/MenuRounded";
import PaidRoundedIcon from "@mui/icons-material/PaidRounded";
import PaletteRoundedIcon from "@mui/icons-material/PaletteRounded";
import PlayCircleRoundedIcon from "@mui/icons-material/PlayCircleRounded";
import PersonRoundedIcon from "@mui/icons-material/PersonRounded";
import SettingsRoundedIcon from "@mui/icons-material/SettingsRounded";
import StorageRoundedIcon from "@mui/icons-material/StorageRounded";
import SensorsRoundedIcon from "@mui/icons-material/SensorsRounded";
import TaskAltRoundedIcon from "@mui/icons-material/TaskAltRounded";
import WorkRoundedIcon from "@mui/icons-material/WorkRounded";
import {
  Alert,
  AppBar,
  Box,
  Button,
  CircularProgress,
  Drawer,
  IconButton,
  Paper,
  Snackbar,
  Stack,
  Toolbar,
  Tooltip,
  Typography,
} from "@mui/material";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import BrandMark from "@/components/BrandMark";
import { useThemePreference } from "@/app/providers";
import { TOKEN_KEY } from "@/lib/api";
import { bindLocalAgent, cloudRequest, detectLocalAgent, formatDate } from "@/lib/admin-api";
import AdminDialog from "./AdminDialog";
import AdminSystemDialogs from "./AdminSystemDialogs";
import ChoiceCards from "./ChoiceCards";

type AdminContextValue = {
  user: any;
  subscription: any;
  appConfig: any;
  onboardingConfig: any;
  onboarding: any;
  agentBase: string;
  refreshAgent: () => Promise<void>;
  refreshSession: () => Promise<void>;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
  confirm: (title: string, message: string) => Promise<boolean>;
};

type MenuItem = readonly [string, string, typeof DashboardRoundedIcon];
type MenuGroup = {
  label: string;
  items: readonly MenuItem[];
  superOnly?: boolean;
};

const AdminContext = createContext<AdminContextValue | null>(null);
const drawerWidth = 248;
const topStatusButtonSx = {
  minHeight: 38,
  height: 38,
  px: 1.6,
  borderRadius: "999px",
  flexShrink: 0,
  whiteSpace: "nowrap",
  fontSize: 13,
  fontWeight: 700,
};

const menuGroups: MenuGroup[] = [
  { label: "工作台", items: [["/admin", "控制台", DashboardRoundedIcon]] },
  {
    label: "招聘管理",
    items: [
      ["/admin/accounts", "平台账号", BadgeRoundedIcon],
      ["/admin/positions", "岗位管理", WorkRoundedIcon],
      ["/admin/tasks", "任务列表", TaskAltRoundedIcon],
      ["/admin/resumes", "简历库", ArticleRoundedIcon],
    ],
  },
  {
    label: "团队与账户",
    items: [
      ["/admin/team", "团队管理", GroupRoundedIcon],
      ["/admin/invitations", "邀请奖励", KeyRoundedIcon],
      ["/admin/personal-config", "个人配置", SettingsRoundedIcon],
      ["/admin/subscription", "订阅会员", CreditCardRoundedIcon],
    ],
  },
  {
    label: "本地与帮助",
    items: [
      ["/admin/local-data", "本地数据", StorageRoundedIcon],
      ["/admin/agent-download", "组件信息", DownloadRoundedIcon],
      ["/admin/help", "常见问题", HelpRoundedIcon],
    ],
  },
  {
    label: "系统管理",
    superOnly: true,
    items: [
      ["/admin/users", "用户管理", PersonRoundedIcon],
      ["/admin/activation-codes", "激活码管理", AdminPanelSettingsRoundedIcon],
      ["/admin/payment-records", "支付记录", PaidRoundedIcon],
      ["/admin/system-config", "系统配置", SettingsRoundedIcon],
    ],
  },
];

/** useAdmin 返回后台全局状态和统一交互方法。 */
export function useAdmin() {
  const value = useContext(AdminContext);
  if (!value) throw new Error("后台上下文尚未初始化");
  return value;
}

/** AdminBanners 展示后台全局常驻广告位，最多显示三条。 */
function AdminBanners({ appConfig }: { appConfig: any }) {
 const source = Array.isArray(appConfig?.admin_banners) ? appConfig.admin_banners : [appConfig?.admin_banner];
 const banners = source.filter((item: any) => item?.enabled !== false && String(item?.text || "").trim()).slice(0, 3);
 if (!banners.length) return null;
 return <Box sx={{ mb: 1.5, mx: { xs: 1, md: 0 }, display: "grid", gridTemplateColumns: { xs: "1fr", md: `repeat(${banners.length}, minmax(0, 1fr))` }, gap: 1 }}>
  {banners.map((banner: any, index: number) => <Box key={`${banner.text}-${index}`} onClick={() => openExternalURL(banner.url)} sx={{ minHeight: 46, height: "100%", display: "flex", alignItems: "center", px: { xs: 1.5, md: 2 }, py: 1.15, borderRadius: "8px", bgcolor: banner.background_color || "#fff7df", color: banner.text_color || "#6b4a00", fontSize: 13, fontWeight: 720, lineHeight: 1.7, cursor: banner.url ? "pointer" : "default", border: "1px solid rgba(107, 74, 0, .12)", overflowWrap: "anywhere" }}>{banner.text}</Box>)}
 </Box>;
}

/** openExternalURL 新开页面打开配置里的外部链接。 */
function openExternalURL(url?: string) {
  const value = String(url || "").trim();
  if (value) window.open(value, "_blank", "noopener,noreferrer");
}

/** AdminApp 输出后台悬浮三卡布局并完成用户身份校验。 */
export default function AdminApp({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { preference, setPreference } = useThemePreference();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [themeOpen, setThemeOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<any>(null);
  const [subscription, setSubscription] = useState<any>({});
  const [appConfig, setAppConfig] = useState<any>({});
  const [onboardingConfig, setOnboardingConfig] = useState<any>({});
  const [onboarding, setOnboarding] = useState<any>({
    completed: false,
    steps: {},
  });
  const [agentBase, setAgentBase] = useState("");
  const agentBaseRef = useRef("");
  const initialPath = useRef(pathname);
  const agentChecking = useRef(false);
  const agentBindNoticeShown = useRef(false);
  const [notice, setNotice] = useState({
    open: false,
    message: "",
    severity: "info" as "success" | "error" | "warning" | "info",
  });
  const [confirmState, setConfirmState] = useState<{
    open: boolean;
    title: string;
    message: string;
    resolve?: (value: boolean) => void;
  }>({ open: false, title: "", message: "" });

  /** refreshAgent 重新探测本地程序。 */
  const refreshAgent = useCallback(async () => {
    if (agentChecking.current) return;
    agentChecking.current = true;
    try {
      const nextBase = await detectLocalAgent(agentBaseRef.current);
      agentBaseRef.current = nextBase;
      setAgentBase(nextBase);
      if (nextBase) void bindLocalAgent(nextBase).catch((error) => {
        const message = error instanceof Error ? error.message : "";
        if (message.includes("已经绑定") && !agentBindNoticeShown.current) {
          agentBindNoticeShown.current = true;
          notify("我小声提醒一下：这个账号已经绑定过另一台电脑。要换电脑的话，先去用户管理里解绑一下。", "warning");
        }
      });
    } finally {
      agentChecking.current = false;
    }
  }, []);

  /** notify 显示统一右上角轻提示。 */
  const notify = useCallback(
    (
      message: string,
      severity: "success" | "error" | "warning" | "info" = "info",
    ) => {
      setNotice({ open: true, message, severity });
    },
    [],
  );

  /** confirm 显示需要用户确认的中间弹框。 */
  const confirm = useCallback(
    (title: string, message: string) =>
      new Promise<boolean>((resolve) =>
        setConfirmState({ open: true, title, message, resolve }),
      ),
    [],
  );

  /** closeConfirm 关闭确认弹框并返回选择结果。 */
  function closeConfirm(value: boolean) {
    confirmState.resolve?.(value);
    setConfirmState({ open: false, title: "", message: "" });
  }

  /** refreshSession 刷新用户、会员和系统公共配置。 */
  const refreshSession = useCallback(async () => {
    const results = await Promise.allSettled([
      cloudRequest("/api/auth/me"),
      cloudRequest("/api/subscription/status"),
      cloudRequest("/api/system/app-config", { auth: false }),
      cloudRequest("/api/onboarding/status"),
    ]);
    const authResult = results[0];
    if (authResult.status === "rejected") throw authResult.reason;
    setUser(authResult.value.user || authResult.value);
    if (results[1].status === "fulfilled")
      setSubscription(results[1].value.subscription || {});
    if (results[2].status === "fulfilled") {
      const payload = results[2].value;
      setAppConfig(payload.config || payload.app_config || payload || {});
    }
    if (results[3].status === "fulfilled") {
      setOnboardingConfig(results[3].value.config || {});
      setOnboarding(
        results[3].value.progress || results[3].value.onboarding || {},
      );
    }
  }, []);

  useEffect(() => {
    let active = true;
    const token = localStorage.getItem(TOKEN_KEY) || "";
    if (!token) {
      router.replace(`/login?next=${encodeURIComponent(initialPath.current)}`);
      return () => {
        active = false;
      };
    }
    Promise.all([refreshSession(), refreshAgent()])
      .catch(() => {
        if (!localStorage.getItem(TOKEN_KEY)) {
          router.replace("/login");
          return;
        }
        notify("后台初始化失败，请检查网络后刷新页面", "error");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    const timer = window.setInterval(() => void refreshAgent(), 10000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [notify, refreshAgent, refreshSession, router]);

  const contextValue = useMemo(
    () => ({
      user,
      subscription,
      appConfig,
      onboardingConfig,
      onboarding,
      agentBase,
      refreshAgent,
      refreshSession,
      notify,
      confirm,
    }),
    [
      user,
      subscription,
      appConfig,
      onboardingConfig,
      onboarding,
      agentBase,
      refreshAgent,
      refreshSession,
      notify,
      confirm,
    ],
  );
  const visibleGroups = menuGroups.filter(
    (group) => !group.superOnly || user?.role === "super_admin",
  );

  /** logout 清除登录状态并返回登录页。 */
  function logout() {
    localStorage.removeItem(TOKEN_KEY);
    router.replace("/login");
  }

  const drawer = (
    <Box sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <Box sx={{ px: 2.25, py: 2.25 }}>
        <BrandMark />
      </Box>
      <Box component='nav' sx={{ flex: 1, px: 1.25, pb: 2, overflowY: "auto" }}>
        {visibleGroups.map((group) => (
          <Box key={group.label} sx={{ mt: 1.25 }}>
            <Typography
              sx={{
                px: 1.5,
                mb: 0.5,
                color: "#89958f",
                fontSize: 11,
                fontWeight: 760,
              }}
            >
              {group.label}
            </Typography>
            <Stack spacing={0.35}>
              {group.items.map(([href, label, Icon]) => {
                const active =
                  href === "/admin"
                    ? pathname === href
                    : pathname.startsWith(href);
                return (
                  <Button
                    key={href}
                    component={Link}
                    href={href}
                    startIcon={<Icon />}
                    onClick={() => setMobileOpen(false)}
                    sx={{
                      justifyContent: "flex-start",
                      minHeight: 40,
                      px: 1.5,
                      borderRadius: "8px",
                      color: active ? "primary.dark" : "#718078",
                      bgcolor: active ? "action.selected" : "transparent",
                      "& .MuiButton-startIcon": {
                        color: active ? "primary.main" : "#97a39d",
                      },
                      "&:hover": {
                        color: active ? "primary.dark" : "#4f5e56",
                        bgcolor: active ? "action.selected" : "action.hover",
                      },
                    }}
                  >
                    {label}
                  </Button>
                );
              })}
            </Stack>
          </Box>
        ))}
      </Box>
      <Box sx={{ p: 1.5, borderTop: "1px solid", borderColor: "divider" }}>
        <Button
          startIcon={<LogoutRoundedIcon />}
          onClick={logout}
          fullWidth
          sx={{
            justifyContent: "flex-start",
            borderRadius: "8px",
            color: "#718078",
            "& .MuiButton-startIcon": { color: "#97a39d" },
          }}
        >
          退出登录
        </Button>
      </Box>
    </Box>
  );

  if (loading)
    return (
      <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center" }}>
        <CircularProgress />
      </Box>
    );

  return (
    <AdminContext.Provider value={contextValue}>
      <Box
        data-admin-root
        sx={{
          minHeight: "100vh",
          bgcolor: "#eef3f0",
          p: { xs: 0, md: 2 },
          "& .MuiButton-root": { minHeight: 38, px: 1.75 },
          "& .MuiIconButton-root": { width: 38, height: 38 },
          "& .MuiOutlinedInput-root": { minHeight: 46, borderRadius: "8px" },
          "& .MuiOutlinedInput-root.MuiInputBase-multiline": {
            minHeight: "unset",
          },
          "& .MuiInputLabel-root": { fontSize: 14 },
        }}
      >
        <Paper
          component='aside'
          elevation={0}
          sx={{
            display: { xs: "none", md: "block" },
            width: drawerWidth,
            position: "fixed",
            inset: "16px auto 16px 16px",
            border: "1px solid",
            borderColor: "divider",
            borderRadius: "8px",
            boxShadow: "0 16px 42px rgba(31,54,42,.08)",
            overflow: "hidden",
            zIndex: 1200,
          }}
        >
          {drawer}
        </Paper>
        <Drawer
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          sx={{
            display: { md: "none" },
            "& .MuiDrawer-paper": { width: drawerWidth },
          }}
        >
          {drawer}
        </Drawer>
        <AppBar
          position='fixed'
          color='inherit'
          elevation={0}
          sx={{
            top: { xs: 0, md: 16 },
            left: { md: drawerWidth + 32 },
            right: { md: 16 },
            width: { xs: "100%", md: `calc(100% - ${drawerWidth + 48}px)` },
            border: 0,
            borderRadius: { xs: 0, md: "8px" },
            boxShadow: "0 12px 34px rgba(31,54,42,.07)",
            overflow: "hidden",
          }}
        >
          <Toolbar sx={{ minHeight: { xs: 64, md: 70 }, gap: 1.25 }}>
            <IconButton
              aria-label='打开菜单'
              onClick={() => setMobileOpen(true)}
              sx={{ display: { md: "none" } }}
            >
              <MenuRoundedIcon />
            </IconButton>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography noWrap sx={{ fontWeight: 780 }}>
                {user?.email || "GoodHR 控制台"}
              </Typography>
              <Typography noWrap sx={{ color: "text.secondary", fontSize: 12 }}>
                {user?.role_label ||
                  (user?.role === "super_admin" ? "超级管理员" : "用户")}
              </Typography>
            </Box>
            <Button
              component={Link}
              href='/videos'
              variant='contained'
              startIcon={<PlayCircleRoundedIcon />}
              sx={{
                ...topStatusButtonSx,
                display: { xs: "none", sm: "inline-flex" },
                boxShadow: "0 8px 20px rgba(21,154,98,.2)",
              }}
            >
              视频教程
            </Button>
            <Tooltip title='视频教程'>
              <IconButton
                component={Link}
                href='/videos'
                aria-label='视频教程'
                color='primary'
                sx={{
                  display: { xs: "inline-flex", sm: "none" },
                  bgcolor: "#e7f5ed",
                }}
              >
                <PlayCircleRoundedIcon />
              </IconButton>
            </Tooltip>
            <Button
              variant='outlined'
              color={subscription.active ? "success" : "warning"}
              startIcon={<CalendarMonthRoundedIcon />}
              onClick={() => router.push("/admin/subscription")}
              sx={{
                ...topStatusButtonSx,
                display: { xs: "none", lg: "inline-flex" },
              }}
            >
              {subscription.active
                ? `${subscription.member_type || "Plus"} · 到期 ${formatDate(subscription.expires_at)}`
                : `免费版 · ${subscription.expires_at ? `已到期 ${formatDate(subscription.expires_at)}` : "未开通"}`}
            </Button>
            <Button
              color={agentBase ? "success" : "error"}
              variant='outlined'
              startIcon={<SensorsRoundedIcon />}
              onClick={() => void refreshAgent()}
              sx={{
                ...topStatusButtonSx,
                display: { xs: "none", sm: "inline-flex" },
              }}
            >
              {agentBase
                ? agentBase.replace("http://127.0.0.1:", "已连接 · 端口 ")
                : "本地程序未连接"}
            </Button>
            <Tooltip title='选择主题'>
              <IconButton
                aria-label='选择主题'
                onClick={() => setThemeOpen(true)}
              >
                <PaletteRoundedIcon />
              </IconButton>
            </Tooltip>
          </Toolbar>
        </AppBar>
        <Box
          component='main'
          sx={{
            ml: { md: `${drawerWidth + 16}px` },
            pt: { xs: "80px", md: "86px" },
            height: "100vh",
            boxSizing: "border-box",
            display: "flex",
            flexDirection: "column",
          }}
        >
          <AdminBanners appConfig={appConfig} />
          <Paper
            elevation={0}
            sx={{
              flex: 1,
              minHeight: 0,
              p: { xs: 2, md: 3 },
              border: "1px solid",
              borderColor: "divider",
              borderRadius: { xs: "8px 8px 0 0", md: "8px" },
              boxShadow: "0 16px 42px rgba(31,54,42,.06)",
              overflow: "auto",
            }}
          >
            {children}
          </Paper>
        </Box>
        <Snackbar
          open={notice.open}
          autoHideDuration={3000}
          onClose={() => setNotice((value) => ({ ...value, open: false }))}
          anchorOrigin={{ vertical: "top", horizontal: "right" }}
        >
          <Alert severity={notice.severity} variant='filled'>
            {notice.message}
          </Alert>
        </Snackbar>
        <AdminSystemDialogs appConfig={appConfig} onboardingConfig={onboardingConfig} agentBase={agentBase} refreshAgent={refreshAgent} />
        <AdminDialog
          open={confirmState.open}
          title={confirmState.title}
          confirmText='确认'
          onClose={() => closeConfirm(false)}
          onConfirm={() => closeConfirm(true)}
        >
          <Typography color='text.secondary'>{confirmState.message}</Typography>
        </AdminDialog>
        <AdminDialog
          open={themeOpen}
          title='选择后台主题'
          description='选择后会立即生效，并保存在当前浏览器。'
          confirmText='完成'
          onClose={() => setThemeOpen(false)}
          onConfirm={() => setThemeOpen(false)}
        >
          <ChoiceCards
            label='主题色'
            value={preference}
            columns={3}
            onChange={(value) =>
              setPreference(value as "green" | "rose" | "amber")
            }
            options={[
              {
                value: "green",
                label: "松绿色",
                description: "安静、清晰，适合长时间工作。",
              },
              {
                value: "rose",
                label: "莓果红",
                description: "柔和暖色，重点更醒目。",
              },
              {
                value: "amber",
                label: "琥珀色",
                description: "自然稳重，信息层级清楚。",
              },
            ]}
          />
        </AdminDialog>
      </Box>
    </AdminContext.Provider>
  );
}
