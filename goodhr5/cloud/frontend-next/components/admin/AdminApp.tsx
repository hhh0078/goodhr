/** 本文件负责新版后台身份、悬浮布局、分类菜单、顶部状态和全局消息。 */
"use client";

import AdminPanelSettingsRoundedIcon from "@mui/icons-material/AdminPanelSettingsRounded";
import ArticleRoundedIcon from "@mui/icons-material/ArticleRounded";
import CalendarMonthRoundedIcon from "@mui/icons-material/CalendarMonthRounded";
import CreditCardRoundedIcon from "@mui/icons-material/CreditCardRounded";
import DashboardRoundedIcon from "@mui/icons-material/DashboardRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import EmailRoundedIcon from "@mui/icons-material/EmailRounded";
import GroupRoundedIcon from "@mui/icons-material/GroupRounded";
import HelpRoundedIcon from "@mui/icons-material/HelpRounded";
import KeyRoundedIcon from "@mui/icons-material/KeyRounded";
import LogoutRoundedIcon from "@mui/icons-material/LogoutRounded";
import MenuRoundedIcon from "@mui/icons-material/MenuRounded";
import PaidRoundedIcon from "@mui/icons-material/PaidRounded";
import PaletteRoundedIcon from "@mui/icons-material/PaletteRounded";
import PlayCircleRoundedIcon from "@mui/icons-material/PlayCircleRounded";
import PersonRoundedIcon from "@mui/icons-material/PersonRounded";
import QueryStatsRoundedIcon from "@mui/icons-material/QueryStatsRounded";
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
import {
	cloudRequest,
	detectLocalAgent,
	formatDate,
  openLocalPage,
} from "@/lib/admin-api";
import AdminDialog from "./AdminDialog";
import AdminSystemDialogs from "./AdminSystemDialogs";
import ChoiceCards from "./ChoiceCards";
import RequiredRuntimeInstaller from "./RequiredRuntimeInstaller";

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
const CHROMIUM_ICON_SRC = "/assets/platforms/chromium.png";
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
      ["/admin/positions", "岗位管理", WorkRoundedIcon],
      ["/admin/tasks", "任务列表", TaskAltRoundedIcon],
      ["/admin/resumes", "简历库", ArticleRoundedIcon],
    ],
  },
  {
    label: "团队与账户",
    items: [
      ["/admin/team", "团队管理", GroupRoundedIcon],
      ["/admin/team-stats", "团队统计", QueryStatsRoundedIcon],
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
      ["/admin/mail", "邮件群发", EmailRoundedIcon],
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
  const source = Array.isArray(appConfig?.admin_banners)
    ? appConfig.admin_banners
    : [appConfig?.admin_banner];
  const banners = source
    .filter(
      (item: any) => item?.enabled !== false && String(item?.text || "").trim(),
    )
    .slice(0, 3);
  if (!banners.length) return null;
  return (
    <Box
      sx={{
        mb: 1.5,
        mx: { xs: 1, md: 0 },
        display: "grid",
        gridTemplateColumns: {
          xs: "1fr",
          md: `repeat(${banners.length}, minmax(0, 1fr))`,
        },
        gap: 1,
      }}
    >
      {banners.map((banner: any, index: number) => (
        <Box
          key={`${banner.text}-${index}`}
          onClick={() => openExternalURL(banner.url)}
          sx={{
            minHeight: 46,
            height: "100%",
            display: "flex",
            alignItems: "center",
            px: { xs: 1.5, md: 2 },
            py: 1.15,
            borderRadius: "8px",
            bgcolor: banner.background_color || "#fff7df",
            color: banner.text_color || "#6b4a00",
            fontSize: 13,
            fontWeight: 720,
            lineHeight: 1.7,
            cursor: banner.url ? "pointer" : "default",
            border: "1px solid rgba(107, 74, 0, .12)",
            overflowWrap: "anywhere",
          }}
        >
          {banner.text}
        </Box>
      ))}
    </Box>
  );
}

/** openExternalURL 新开页面打开配置里的外部链接。 */
function openExternalURL(url?: string) {
  const value = String(url || "").trim();
  if (value) window.open(value, "_blank", "noopener,noreferrer");
}

/** formatAIBalance 格式化顶部栏 AI 余额。 */
function formatAIBalance(wallet: any) {
  return `￥${String(wallet?.balance || "0.00")}`;
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
  const [aiWallet, setAIWallet] = useState<any>({});
  const [appConfig, setAppConfig] = useState<any>({});
  const [onboardingConfig, setOnboardingConfig] = useState<any>({});
  const [onboarding, setOnboarding] = useState<any>({
    completed: false,
    steps: {},
  });
	const [agentBase, setAgentBase] = useState("");
	const [agentDetected, setAgentDetected] = useState(false);
	const agentBaseRef = useRef("");
	const initialPath = useRef(pathname);
	const agentChecking = useRef(false);
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
  const [trialWelcomeOpen, setTrialWelcomeOpen] = useState(false);
  const [localAgentInstallNoticeClosed, setLocalAgentInstallNoticeClosed] =
    useState(false);
  const localAgentInstallNoticeOpen = Boolean(
		user &&
		!loading &&
		!trialWelcomeOpen &&
		agentDetected &&
		!agentBase &&
		!localAgentInstallNoticeClosed,
	);

  /** refreshAgent 重新探测本地程序。 */
  const refreshAgent = useCallback(async () => {
    if (agentChecking.current) return;
    agentChecking.current = true;
    try {
      const nextBase = await detectLocalAgent(agentBaseRef.current);
			if (!nextBase) {
				agentBaseRef.current = "";
				setAgentBase("");
				return;
			}
			agentBaseRef.current = nextBase;
			setAgentBase(nextBase);
		} finally {
      setAgentDetected(true);
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

  /** openBingBrowser 通过本地程序打开浏览器并导航到必应。 */
  const openBingBrowser = useCallback(async () => {
    let baseURL = agentBaseRef.current || agentBase;
    if (!baseURL) {
      baseURL = await detectLocalAgent(agentBaseRef.current);
    }
    if (!baseURL) {
      notify("我没叫醒本地程序，你先确认它开着，再点我一次。", "warning");
      return;
    }

    const browserPayload = {
      persistent: true,
      user_data_dir: "default",
      headless: false,
      humanize: true,
    };

	try {
		agentBaseRef.current = baseURL;
		setAgentBase(baseURL);
		await openLocalPage(baseURL, {
			...browserPayload,
			url: "https://www.bing.com",
      });
      notify("浏览器已打开，我已经把它带到必应了。", "success");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "浏览器没打开成功，我再小声努力一次也行。",
        "error",
      );
    }
  }, [agentBase, notify]);

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

  /** ackTrialWelcome 确认新用户试用会员到账提醒。 */
  async function ackTrialWelcome() {
    const welcomeKey = `goodhr_trial_welcome_${user?.email || ""}`;
    localStorage.setItem(welcomeKey, "1");
    setTrialWelcomeOpen(false);
    try {
      await cloudRequest("/api/auth/trial-welcome/ack", { method: "POST" });
    } catch {
      notify("体验提醒已关闭，确认状态稍后会再同步。", "info");
    }
  }

  /** refreshSession 刷新用户、会员和系统公共配置。 */
  const refreshSession = useCallback(async () => {
    const results = await Promise.allSettled([
      cloudRequest("/api/auth/me"),
      cloudRequest("/api/subscription/status"),
      cloudRequest("/api/system/app-config", { auth: false }),
      cloudRequest("/api/onboarding/status"),
      cloudRequest("/api/ai-wallet"),
    ]);
    const authResult = results[0];
    if (authResult.status === "rejected") throw authResult.reason;
    const authPayload = authResult.value;
    const nextUser = authPayload.user || authPayload;
    setUser(nextUser);
    const welcomeKey = `goodhr_trial_welcome_${nextUser?.email || ""}`;
    setTrialWelcomeOpen(
      Boolean(authPayload.show_trial_welcome) &&
        !localStorage.getItem(welcomeKey),
    );
    if (results[1].status === "fulfilled")
      setSubscription(results[1].value.subscription || {});
    if (results[4].status === "fulfilled")
      setAIWallet(results[4].value.wallet || {});
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
    return () => {
      active = false;
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
      <Box component="nav" sx={{ flex: 1, px: 1.25, pb: 2, overflowY: "auto" }}>
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
          component="aside"
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
          position="fixed"
          color="inherit"
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
              aria-label="打开菜单"
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
              variant="outlined"
              startIcon={
                <Box
                  component="img"
                  src={CHROMIUM_ICON_SRC}
                  alt=""
                  sx={{ width: 18, height: 18, display: "block" }}
                />
              }
              onClick={() => void openBingBrowser()}
              sx={{
                ...topStatusButtonSx,
                display: { xs: "none", sm: "inline-flex" },
              }}
            >
              打开浏览器
            </Button>
            <Tooltip title="打开浏览器">
              <IconButton
                aria-label="打开浏览器"
                onClick={() => void openBingBrowser()}
                sx={{
                  display: { xs: "inline-flex", sm: "none" },
                  bgcolor: "#f2f7f4",
                }}
              >
                <Box
                  component="img"
                  src={CHROMIUM_ICON_SRC}
                  alt=""
                  sx={{ width: 22, height: 22, display: "block" }}
                />
              </IconButton>
            </Tooltip>
            <Button
              component={Link}
              href="/videos"
              variant="contained"
              startIcon={<PlayCircleRoundedIcon />}
              sx={{
                ...topStatusButtonSx,
                display: { xs: "none", sm: "inline-flex" },
                boxShadow: "0 8px 20px rgba(21,154,98,.2)",
              }}
            >
              视频教程
            </Button>
            <Tooltip title="视频教程">
              <IconButton
                component={Link}
                href="/videos"
                aria-label="视频教程"
                color="primary"
                sx={{
                  display: { xs: "inline-flex", sm: "none" },
                  bgcolor: "#e7f5ed",
                }}
              >
                <PlayCircleRoundedIcon />
              </IconButton>
            </Tooltip>
            <Button
              variant="outlined"
              color={subscription.active ? "success" : "warning"}
              startIcon={<CalendarMonthRoundedIcon />}
              onClick={() => router.push("/admin/subscription")}
              sx={{
                ...topStatusButtonSx,
                display: { xs: "none", lg: "inline-flex" },
              }}
            >
              {subscription.active
                ? `${subscription.member_type || "Plus"} · 到期 ${formatDate(subscription.expires_at)} · AI余额 ${formatAIBalance(aiWallet)}`
                : `免费版 · ${subscription.expires_at ? `已到期 ${formatDate(subscription.expires_at)}` : "未开通"} · AI余额 ${formatAIBalance(aiWallet)}`}
            </Button>
            <Button
              color={agentBase ? "success" : "error"}
              variant="outlined"
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
            <Tooltip title="选择主题">
              <IconButton
                aria-label="选择主题"
                onClick={() => setThemeOpen(true)}
              >
                <PaletteRoundedIcon />
              </IconButton>
            </Tooltip>
          </Toolbar>
        </AppBar>
        <Box
          component="main"
          sx={{
            ml: { md: `${drawerWidth + 16}px` },
            pt: { xs: "80px", md: "86px" },
            height: { xs: "100vh", md: "calc(100vh - 32px)" },
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
          <Alert severity={notice.severity} variant="filled">
            {notice.message}
          </Alert>
        </Snackbar>
        <AdminDialog
          open={trialWelcomeOpen}
          title="体验会员已到账"
          confirmText="我知道了"
          showCancel={false}
          onClose={() => void ackTrialWelcome()}
          onConfirm={() => void ackTrialWelcome()}
        >
          <Typography color="text.secondary">
            赠送的 3
            天体验会员已到账，请尽快体验。会员到期后，您可以选择续费，或者改用免费版。
          </Typography>
        </AdminDialog>{" "}
        <AdminDialog
          open={localAgentInstallNoticeOpen}
          title="请先安装本地程序"
          confirmText="去安装"
          showCancel={false}
          onClose={() => setLocalAgentInstallNoticeClosed(true)}
          onConfirm={() => router.push("/download")}
        >
          <Typography color="text.secondary">
            如果您是首次使用，请先安装本地程序。如果您已经安装，请尝试双击桌面上的图标。
          </Typography>
        </AdminDialog>
        <RequiredRuntimeInstaller
          agentBase={agentBase}
          onboardingConfig={onboardingConfig}
          notify={notify}
        />
        <AdminSystemDialogs
          appConfig={appConfig}
          onboardingConfig={onboardingConfig}
          agentBase={agentBase}
          refreshAgent={refreshAgent}
        />
        <AdminDialog
          open={confirmState.open}
          title={confirmState.title}
          confirmText="确认"
          onClose={() => closeConfirm(false)}
          onConfirm={() => closeConfirm(true)}
        >
          <Typography color="text.secondary">{confirmState.message}</Typography>
        </AdminDialog>
        <AdminDialog
          open={themeOpen}
          title="选择后台主题"
          description="选择后会立即生效，并保存在当前浏览器。"
          confirmText="完成"
          onClose={() => setThemeOpen(false)}
          onConfirm={() => setThemeOpen(false)}
        >
          <ChoiceCards
            label="主题色"
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
