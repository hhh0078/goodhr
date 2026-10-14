/** 本文件负责新版后台身份、悬浮布局、分类菜单、顶部状态和全局消息。 */
"use client";

import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Flex,
  Layout,
  Menu,
  Spin,
  Tooltip,
  Typography,
} from "antd";
import {
  SafetyOutlined as AdminPanelSettingsRoundedIcon,
  FileTextOutlined as ArticleRoundedIcon,
  CalendarOutlined as CalendarMonthRoundedIcon,
  CreditCardOutlined as CreditCardRoundedIcon,
  DashboardOutlined as DashboardRoundedIcon,
  DownloadOutlined as DownloadRoundedIcon,
  MailOutlined as EmailRoundedIcon,
  TeamOutlined as GroupRoundedIcon,
  QuestionCircleOutlined as HelpRoundedIcon,
  FileSearchOutlined as ManageSearchRoundedIcon,
  KeyOutlined as KeyRoundedIcon,
  LogoutOutlined as LogoutRoundedIcon,
  MenuOutlined as MenuRoundedIcon,
  DollarOutlined as PaidRoundedIcon,
  SkinOutlined as PaletteRoundedIcon,
  PlayCircleOutlined as PlayCircleRoundedIcon,
  UserOutlined as PersonRoundedIcon,
  SettingOutlined as SettingsRoundedIcon,
  ApiOutlined as SensorsRoundedIcon,
  SolutionOutlined as WorkRoundedIcon,
} from "@ant-design/icons";
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
import BrowserOpenControl from "./BrowserOpenControl";
import MessageCenter from "./MessageCenter";
import BrandMark from "@/components/BrandMark";
import PasswordSetupDialog from "@/components/PasswordSetupDialog";
import { useUserTheme } from "@/app/providers";
import { normalizeThemePreset } from "@/app/theme";
import { TOKEN_KEY } from "@/lib/api";
import {
  bindDetectedLocalAgent,
  captureLocalAgentPortFromURL,
  cloudRequest,
  detectLocalAgent,
  formatDate,
  localRequest,
} from "@/lib/admin-api";
import { requiredRuntimeComponents } from "@/lib/admin-runtime";
import {
  EMPTY_SUBSCRIPTION,
  normalizeSubscription,
  type SubscriptionStatus,
} from "@/lib/subscription";
import { reportUserFlow } from "@/lib/user-flow";
import AdminDialog from "./AdminDialog";
import AdminSystemDialogs from "./AdminSystemDialogs";
import ChoiceCards from "./ChoiceCards";
import ClickableImagePreview from "./ClickableImagePreview";
import RequiredRuntimeInstaller from "./RequiredRuntimeInstaller";
import { AdminOverlayProvider, AdminOverlayActivity } from "./AdminOverlay";
import {
  normalizeWechatStatus,
  type WechatOfficialStatus,
} from "@/lib/wechat-official";

type AdminContextValue = {
  user: any;
  subscription: SubscriptionStatus;
  appConfig: any;
  onboardingConfig: any;
  agentBase: string;
  refreshAgent: () => Promise<void>;
  refreshSession: () => Promise<void>;
  wechatOfficialStatus: WechatOfficialStatus | null;
  refreshWechatOfficialStatus: () => Promise<void>;
  openWechatOfficial: () => void;
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
const LOCAL_AGENT_PERMISSION_IMAGE_SRC =
  "/assets/help/local-agent-device-app-permission.png";

const menuGroups: MenuGroup[] = [
  { label: "工作台", items: [["/admin", "控制台", DashboardRoundedIcon]] },
  {
    label: "招聘管理",
    items: [
      ["/admin/positions", "岗位管理", WorkRoundedIcon],
      ["/admin/resumes", "简历库", ArticleRoundedIcon],
      ["/admin/recruitment", "企业招聘", WorkRoundedIcon],
      ["/admin/recommendations", "猎头推荐", GroupRoundedIcon],
      ["/admin/recruitment/settings", "招聘设置", SettingsRoundedIcon],
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
      ["/admin/agent-download", "组件信息", DownloadRoundedIcon],
      ["/admin/help", "常见问题", HelpRoundedIcon],
    ],
  },
  {
    label: "系统管理",
    superOnly: true,
    items: [
      ["/admin/users", "用户管理", PersonRoundedIcon],
      ["/admin/logs", "运行日志", ManageSearchRoundedIcon],
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

/** AdminBanners 将后台广告压成一行等宽文字，长文案只显示单行省略号。 */
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
    <nav className="goodhr-admin-banners" aria-label="GoodHR 公告与推荐">
      {banners.map((banner: any, index: number) => {
        const text = String(banner.text || "").trim();
        const url = String(banner.url || "").trim();
        const common = {
          className: "goodhr-admin-banner-text",
          title: text,
          style: { color: banner.text_color || undefined },
        };
        return url ? (
          <a key={index} {...common} href={url} target="_blank" rel="noopener noreferrer">{text}</a>
        ) : (
          <span key={index} {...common}>{text}</span>
        );
      })}
    </nav>
  );
}

/** formatAIBalance 格式化顶部栏 AI 余额。 */
function formatAIBalance(wallet: any) {
  return `￥${String(wallet?.balance || "0.00")}`;
}

/** AdminApp 输出后台悬浮三卡布局并完成用户身份校验。 */
export default function AdminApp({ children }: { children: ReactNode }) {
  const { notification } = App.useApp();
  const pathname = usePathname();
  const router = useRouter();
  const { themePreset, setThemePreset, setThemeAccount } = useUserTheme();
  const sessionActiveRef = useRef(true);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [themeOpen, setThemeOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<any>(null);
  const [subscription, setSubscription] =
    useState<SubscriptionStatus>(EMPTY_SUBSCRIPTION);
  const [aiWallet, setAIWallet] = useState<any>({});
  const [appConfig, setAppConfig] = useState<any>({});
  const [onboardingConfig, setOnboardingConfig] = useState<any>({});
  const [wechatOfficialStatus, setWechatOfficialStatus] =
    useState<WechatOfficialStatus | null>(null);
  const [wechatOpenSignal, setWechatOpenSignal] = useState(0);
  const wechatStatusRequestRef = useRef<AbortController | null>(null);
  const [agentBase, setAgentBase] = useState("");
  const [agentVersion, setAgentVersion] = useState("");
  const [agentDetected, setAgentDetected] = useState(false);
  const agentBaseRef = useRef("");
  const initialPath = useRef(pathname);
  const agentChecking = useRef<Promise<void> | null>(null);
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
  const [passwordSetupRequired, setPasswordSetupRequired] = useState(false);
  const [teamInvitations, setTeamInvitations] = useState<any[]>([]);
  const [teamInvitationLoading, setTeamInvitationLoading] = useState(false);
  const [localAgentInstallNoticeClosed, setLocalAgentInstallNoticeClosed] =
    useState(false);
  const localAgentInstallNoticeOpen = Boolean(
    user &&
      !loading &&
      teamInvitations.length === 0 &&
      !trialWelcomeOpen &&
      agentDetected &&
      !agentBase &&
      !localAgentInstallNoticeClosed,
  );

  /** refreshAgent 合并正在进行的探测，让更新完成等调用等待实际绑定和状态同步。 */
  const refreshAgent = useCallback(() => {
    if (agentChecking.current) return agentChecking.current;
    const request = (async () => {
      try {
        const nextBase = await detectLocalAgent(agentBaseRef.current);
        if (!nextBase) {
          agentBaseRef.current = "";
          setAgentBase("");
          setAgentVersion("");
          return;
        }
        agentBaseRef.current = nextBase;
        setAgentBase(nextBase);
        setAgentVersion("");
        await bindDetectedLocalAgent(nextBase);
        void reportUserFlow({
          step: "agent_detected",
          source: "frontend_agent_probe",
        });
        const [runtimeResult, healthResult] = await Promise.allSettled([
          localRequest(nextBase, "/api/v1/runtime/status"),
          localRequest(nextBase, "/health"),
        ]);
        const health =
          healthResult.status === "fulfilled" ? healthResult.value : {};
        const runtime =
          runtimeResult.status === "fulfilled" ? runtimeResult.value : {};
        const detectedVersion = String(
          health?.version ||
            health?.agent_version ||
            runtime?.version ||
            runtime?.agent_version ||
            runtime?.runtime?.version ||
            "",
        );
        if (!detectedVersion)
          throw new Error("本地程序版本暂时没读到，请稍后重试");
        setAgentVersion(detectedVersion);
        if (runtimeResult.status === "fulfilled") {
          const missing = requiredRuntimeComponents(runtime).filter(
            (item) => !item.installed,
          );
          void reportUserFlow(
            missing.length
              ? {
                  step: "runtime_ready",
                  status: "blocked",
                  reason_code: "runtime_missing",
                  message: `缺少运行组件：${missing.map((item) => item.name).join("、")}`,
                  source: "frontend_agent_probe",
                }
              : { step: "runtime_ready", source: "frontend_agent_probe" },
          );
        } else {
          void reportUserFlow({
            step: "runtime_ready",
            status: "blocked",
            reason_code: "runtime_status_unavailable",
            message: "运行组件状态读取失败",
            source: "frontend_agent_probe",
          });
        }
      } finally {
        setAgentDetected(true);
        agentChecking.current = null;
      }
    })();
    agentChecking.current = request;
    return request;
  }, []);

  /** refreshAgentAfterUpdate 确认安装后的本地连接、绑定和版本均已恢复，避免断线瞬间提前解除更新锁。 */
  const refreshAgentAfterUpdate = useCallback(async () => {
    await refreshAgent();
    if (!agentBaseRef.current) throw new Error("本地程序还没重新连上，请稍等");
  }, [refreshAgent]);

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

  /** acceptTeamInvitation 接受当前团队邀请并刷新账号所属团队。 */
  async function acceptTeamInvitation() {
    const invitation = teamInvitations[0];
    if (!invitation || teamInvitationLoading) return;
    setTeamInvitationLoading(true);
    try {
      await cloudRequest(
        `/api/tenants/invitations/${encodeURIComponent(invitation.id)}/accept`,
        { method: "POST" },
      );
      setTeamInvitations((items) => items.slice(1));
      await refreshSession();
      router.refresh();
      notify("已经加入团队，岗位和简历也搬好了。我这次没敢偷懒。", "success");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "团队暂时没加入成功，请稍后再试",
        "error",
      );
    } finally {
      setTeamInvitationLoading(false);
    }
  }

  /** rejectTeamInvitation 拒绝当前邀请，拒绝后管理员仍可重新邀请。 */
  async function rejectTeamInvitation() {
    const invitation = teamInvitations[0];
    if (!invitation || teamInvitationLoading) return;
    setTeamInvitationLoading(true);
    try {
      await cloudRequest(
        `/api/tenants/invitations/${encodeURIComponent(invitation.id)}/reject`,
        { method: "POST" },
      );
      setTeamInvitations((items) => items.slice(1));
      notify("这次先不加入也没关系，我已经替你婉拒了。", "info");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "邀请暂时没处理成功，请稍后再试",
        "error",
      );
    } finally {
      setTeamInvitationLoading(false);
    }
  }

  /** refreshWechatOfficialStatus 读取本人公众号状态，失败时不凭空提示关注或奖励。 */
  const refreshWechatOfficialStatus = useCallback(async () => {
    const token = localStorage.getItem(TOKEN_KEY) || "";
    wechatStatusRequestRef.current?.abort();
    const controller = new AbortController();
    wechatStatusRequestRef.current = controller;
    const timeout = window.setTimeout(() => controller.abort(), 5000);
    try {
      const status = await cloudRequest("/api/wechat-official/status", {
        signal: controller.signal,
      });
      if (
        controller.signal.aborted ||
        wechatStatusRequestRef.current !== controller ||
        token !== localStorage.getItem(TOKEN_KEY)
      )
        return;
      setWechatOfficialStatus(normalizeWechatStatus(status));
    } catch (error) {
      if (
        wechatStatusRequestRef.current !== controller ||
        token !== localStorage.getItem(TOKEN_KEY)
      )
        return;
      setWechatOfficialStatus(null);
      throw error;
    } finally {
      window.clearTimeout(timeout);
      if (wechatStatusRequestRef.current === controller)
        wechatStatusRequestRef.current = null;
    }
  }, []);
  useEffect(
    () => () => {
      wechatStatusRequestRef.current?.abort();
      wechatStatusRequestRef.current = null;
    },
    [],
  );

  /** openWechatOfficial 主动打开关注入口，取消自动提醒后仍可重新领取或绑定。 */
  const openWechatOfficial = useCallback(
    () => setWechatOpenSignal((value) => value + 1),
    [],
  );

  /** refreshSession 刷新用户、会员和系统公共配置。 */
  const refreshSession = useCallback(async () => {
    const requestToken = localStorage.getItem(TOKEN_KEY) || "";
    // 关注状态独立查询，慢接口不拖住账号和岗位；失败时保持无提醒状态。
    void refreshWechatOfficialStatus().catch(() => undefined);
    const results = await Promise.allSettled([
      cloudRequest("/api/auth/me"),
      cloudRequest("/api/subscription/status"),
      cloudRequest("/api/system/app-config", { auth: false }),
      cloudRequest("/api/runtime/config"),
      cloudRequest("/api/ai-wallet"),
      cloudRequest("/api/tenants/invitations/pending"),
    ]);
    // 退出或换号后的旧响应不能重新加载旧账号主题或身份。
    if (!sessionActiveRef.current || requestToken !== (localStorage.getItem(TOKEN_KEY) || "")) return;
    const authResult = results[0];
    if (authResult.status === "rejected") throw authResult.reason;
    const authPayload = authResult.value;
    const nextUser = authPayload.user || authPayload;
    setThemeAccount(String(nextUser?.email || nextUser?.id || ""));
    setUser(nextUser);
    const nextPasswordSetupRequired = Boolean(
      authPayload.requires_password_setup,
    );
    setPasswordSetupRequired(nextPasswordSetupRequired);
    const welcomeKey = `goodhr_trial_welcome_${nextUser?.email || ""}`;
    setTrialWelcomeOpen(
      !nextPasswordSetupRequired &&
        Boolean(authPayload.show_trial_welcome) &&
        !localStorage.getItem(welcomeKey),
    );
    if (results[1].status === "fulfilled") {
      const nextSubscription = normalizeSubscription(
        results[1].value.subscription || {},
      );
      setSubscription(nextSubscription);
    }
    if (results[4].status === "fulfilled")
      setAIWallet(results[4].value.wallet || results[4].value || {});
    if (results[5].status === "fulfilled")
      setTeamInvitations(
        Array.isArray(results[5].value.invitations)
          ? results[5].value.invitations
          : [],
      );
    if (results[2].status === "fulfilled") {
      const payload = results[2].value;
      setAppConfig(payload.config || payload.app_config || payload || {});
    }
    if (results[3].status === "fulfilled") {
      setOnboardingConfig(results[3].value.config || {});
    }
  }, [refreshWechatOfficialStatus, setThemeAccount]);

  useEffect(() => {
    sessionActiveRef.current = true;
    return () => {
      sessionActiveRef.current = false;
      setThemeAccount("");
    };
  }, [setThemeAccount]);

  useEffect(() => {
    let active = true;
    captureLocalAgentPortFromURL();
    const token = localStorage.getItem(TOKEN_KEY) || "";
    if (!token) {
      router.replace(`/login?next=${encodeURIComponent(initialPath.current)}`);
      return () => {
        active = false;
      };
    }
    Promise.all([refreshSession(), refreshAgent()])
      .catch((error) => {
        if (!localStorage.getItem(TOKEN_KEY)) {
          router.replace("/login");
          return;
        }
        notify(
          error instanceof Error ? error.message : "后台初始化失败，请稍后重试",
          "error",
        );
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
      agentBase,
      refreshAgent,
      refreshSession,
      wechatOfficialStatus,
      refreshWechatOfficialStatus,
      openWechatOfficial,
      notify,
      confirm,
    }),
    [
      user,
      subscription,
      appConfig,
      onboardingConfig,
      agentBase,
      refreshAgent,
      refreshSession,
      wechatOfficialStatus,
      refreshWechatOfficialStatus,
      openWechatOfficial,
      notify,
      confirm,
    ],
  );
  const visibleGroups = menuGroups.filter(
    (group) => !group.superOnly || user?.role === "super_admin",
  );

  /** logout 清除登录状态并返回登录页。 */
  function logout() {
    wechatStatusRequestRef.current?.abort();
    wechatStatusRequestRef.current = null;
    setWechatOfficialStatus(null);
    localStorage.removeItem(TOKEN_KEY);
    setThemeAccount("");
    router.replace("/login");
  }

  useEffect(() => {
    if (!notice.open) return;
    notification[notice.severity]({
      title: notice.message,
      duration: 3,
      placement: "topRight",
    });
    setNotice((current) => ({ ...current, open: false }));
  }, [notice, notification]);

  const selectedMenu = visibleGroups
    .flatMap((group) => group.items).sort(([first], [second]) => second.length - first.length)
    .find(([href]) =>
      href === "/admin" ? pathname === href : pathname.startsWith(href),
    )?.[0];
  const drawer = (
    <Flex vertical style={{ height: "100%" }}>
      <div style={{ padding: 18 }}>
        <BrandMark />
      </div>
      <Menu
        mode="inline"
        selectedKeys={selectedMenu ? [selectedMenu] : []}
        style={{ flex: 1, overflowY: "auto", border: 0 }}
        items={visibleGroups.map((group) => ({
          type: "group" as const,
          label: group.label,
          key: group.label,
          children: group.items.map(([href, label, Icon]) => ({
            key: href,
            icon: <Icon aria-hidden="true" />,
            label,
          })),
        }))}
        onClick={({ key }) => {
          router.push(key);
          setMobileOpen(false);
        }}
      />
      <Button
        type="text"
        icon={<LogoutRoundedIcon aria-hidden="true" />}
        onClick={logout}
        block
        style={{ marginBlock: 12 }}
      >
        退出登录
      </Button>
    </Flex>
  );

  if (loading)
    return (
      <Flex justify="center" align="center" style={{ minHeight: "100vh" }}>
        <Spin size="large" aria-label="正在读取账号" />
      </Flex>
    );
  return (
    <AdminContext.Provider value={contextValue}>
      <AdminOverlayProvider>
        <Layout data-admin-root className="goodhr-admin-layout">
          <AdminOverlayActivity open={mobileOpen} />
          <aside className="goodhr-admin-sidebar">{drawer}</aside>
          <Drawer
            title="GoodHR 菜单"
            placement="left"
            open={mobileOpen}
            onClose={() => setMobileOpen(false)}
            size={drawerWidth}
            styles={{ body: { padding: 0 } }}
          >
            {drawer}
          </Drawer>
          <header className="goodhr-admin-toolbar">
            <Flex gap={10} align="center" style={{ minWidth: 0 }}>
              <Button
                className="goodhr-mobile-menu"
                type="text"
                icon={<MenuRoundedIcon aria-hidden="true" />}
                aria-label="打开菜单"
                onClick={() => setMobileOpen(true)}
              />
              <div className="goodhr-toolbar-identity">
                <Typography.Text strong ellipsis>
                  {user?.email || "GoodHR 控制台"}
                </Typography.Text>
                <Typography.Text
                  type="secondary"
                  ellipsis
                  style={{ display: "block", fontSize: 12 }}
                >
                  {user?.role_label ||
                    (user?.role === "super_admin" ? "超级管理员" : "用户")}
                </Typography.Text>
              </div>
              <BrowserOpenControl
                agentBase={agentBase}
                notify={notify}
                onAgentDetected={(base) => {
                  agentBaseRef.current = base;
                  setAgentBase(base);
                }}
              />
              <Button
                className="goodhr-desktop-sm"
                href="/videos"
                type="primary"
                icon={<PlayCircleRoundedIcon aria-hidden="true" />}
              >
                视频教程
              </Button>
              <Tooltip title="视频教程">
                <Button
                  className="goodhr-mobile-sm"
                  href="/videos"
                  type="text"
                  icon={<PlayCircleRoundedIcon aria-hidden="true" />}
                  aria-label="视频教程"
                />
              </Tooltip>
              <Button
                className="goodhr-desktop-lg"
                icon={<CalendarMonthRoundedIcon aria-hidden="true" />}
                onClick={() => router.push("/admin/subscription")}
              >
                {subscription.active
                  ? `${subscription.member_name} · 剩${subscription.remaining_days}天 · AI余额 ${formatAIBalance(aiWallet)}`
                  : `${subscription.expires_at ? `已过期 ${formatDate(subscription.expires_at)}` : "未开通"} · AI余额 ${formatAIBalance(aiWallet)}`}
              </Button>
              <Button
                className="goodhr-desktop-sm"
                icon={<SensorsRoundedIcon aria-hidden="true" />}
                onClick={() =>
                  void refreshAgent().catch((error) =>
                    notify(
                      error instanceof Error
                        ? error.message
                        : "本地程序没连上，请稍后重试",
                      "error",
                    ),
                  )
                }
                style={{ color: agentBase ? "#238653" : "#c83f49" }}
              >
                {agentBase
                  ? agentBase.replace(
                      "http://127.0.0.1:",
                      `${agentVersion || "--"} · 端口 `,
                    )
                  : "本地程序未连接"}
              </Button>
              <div style={{ marginLeft: "auto" }}><MessageCenter userKey={user?.id || user?.email || ""} /></div>
              <Tooltip title="切换主题">
                <Button
                  type="text"
                  icon={<PaletteRoundedIcon aria-hidden="true" />}
                  aria-label="切换主题"
                  onClick={() => setThemeOpen(true)}
                />
              </Tooltip>
            </Flex>
          </header>
          <Layout.Content className="goodhr-admin-main">
            <AdminBanners appConfig={appConfig} />
            <Card
              className="goodhr-admin-content"
              styles={{ body: { padding: "clamp(16px, 2vw, 24px)" } }}
            >
              {children}
            </Card>
          </Layout.Content>
          <AdminDialog
            open={trialWelcomeOpen && teamInvitations.length === 0}
            title={`${subscription.member_name || "Max 全能体验版"}已到账`}
            confirmText="我知道了"
            showCancel={false}
            onClose={() => void ackTrialWelcome()}
            onConfirm={() => void ackTrialWelcome()}
          >
            <Typography.Paragraph type="secondary">
              赠送的 {subscription.remaining_days || 3} 天{" "}
              {subscription.member_name || "Max 全能版"} 已到账。支持：
              {subscription.features.length
                ? subscription.features.join("、")
                : "AI 筛选、自动打招呼和自动回复"}
              。
            </Typography.Paragraph>
            <Typography.Text type="secondary">
              到期时间：{formatDate(subscription.expires_at) || "--"}
              。到期后可以续费，也可以继续使用免费版。
            </Typography.Text>
          </AdminDialog>
          <AdminDialog
            open={teamInvitations.length > 0}
            title="有人想拉你进团队"
            description="这次得你亲自点头，我不敢擅自替你答应。"
            confirmText="同意加入"
            showCancel={false}
            hideClose
            loading={teamInvitationLoading}
            loadingText="正在搬家"
            onClose={() => undefined}
            onConfirm={() => void acceptTeamInvitation()}
            extraActions={
              <Button
                disabled={teamInvitationLoading}
                onClick={() => void rejectTeamInvitation()}
              >
                拒绝邀请
              </Button>
            }
          >
            <Card size="small" style={{ marginBottom: 12 }}>
              <Typography.Paragraph strong>
                {teamInvitations[0]?.tenant_name ||
                  `${teamInvitations[0]?.tenant_owner || "对方"} 的团队`}
              </Typography.Paragraph>
              <Typography.Text type="secondary">
                邀请人：{teamInvitations[0]?.invited_by_email || "团队管理员"} ·
                加入后身份：
                {teamInvitations[0]?.role === "admin"
                  ? "团队管理员"
                  : "普通成员"}
              </Typography.Text>
            </Card>
            <Alert
              type="warning"
              showIcon
              title="同意后，你现在的岗位、简历和平台账号会一起搬进这个团队，简历也会进入团队统一管理范围。如果你当前还有岗位在运行，我会先拦一下，请停掉后再加入。"
            />
          </AdminDialog>
          <AdminDialog
            open={localAgentInstallNoticeOpen}
            title="请先安装本地程序"
            confirmText="去安装"
            showCancel={false}
            onClose={() => setLocalAgentInstallNoticeClosed(true)}
            onConfirm={() => router.push("/download")}
          >
            <Typography.Paragraph type="secondary">
              如果您是首次使用，请先安装本地程序。如果您已经安装，请尝试双击桌面上的图标。
            </Typography.Paragraph>
            <Alert
              type="warning"
              title="本地程序开着，还是连不上？"
              description={
                <>
                  <Typography.Paragraph>
                    也可能是浏览器没有允许“设备上的应用”。点击地址栏左侧的网站设置按钮，打开“设备上的应用”开关后再试。
                  </Typography.Paragraph>
                  <ClickableImagePreview
                    src={LOCAL_AGENT_PERMISSION_IMAGE_SRC}
                    alt="Chrome 设备上的应用权限开关位置"
                  />
                </>
              }
            />
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
            refreshAgent={refreshAgentAfterUpdate}
            user={user}
            wechatStatus={wechatOfficialStatus}
            wechatOpenSignal={wechatOpenSignal}
            onWechatStatus={setWechatOfficialStatus}
            refreshSession={refreshSession}
            notify={notify}
            promotionBlocked={
              passwordSetupRequired ||
              trialWelcomeOpen ||
              teamInvitations.length > 0 ||
              localAgentInstallNoticeOpen
            }
          />
          <AdminDialog
            open={confirmState.open}
            title={confirmState.title}
            confirmText="确认"
            onClose={() => closeConfirm(false)}
            onConfirm={() => closeConfirm(true)}
          >
            <Typography.Paragraph type="secondary">
              {confirmState.message}
            </Typography.Paragraph>
          </AdminDialog>
          <AdminDialog
            open={themeOpen}
            title="切换主题"
            description="选你喜欢的颜色，点一下就换好。这个浏览器会记住你的选择。"
            confirmText="完成"
            showCancel={false}
            onClose={() => setThemeOpen(false)}
            onConfirm={() => setThemeOpen(false)}
          >
            <ChoiceCards
              label="主题颜色"
              value={themePreset}
              columns={3}
              onChange={(value) => {
                if (!setThemePreset(normalizeThemePreset(value))) {
                  notify("颜色换好了，但浏览器暂时没记住；下次打开可以再选一次。", "warning");
                }
              }}
              options={[
                {
                  value: "green",
                  label: "松绿色（默认）",
                  description: "默认主题，安静清晰，久看也不累。",
                },
                {
                  value: "ink",
                  label: "深墨黑",
                  description: "克制稳重，用黑色标记重点内容。",
                },
                {
                  value: "gold",
                  label: "黑金色",
                  description: "低调黑金，只在重点位置使用金色。",
                },
              ]}
            />
          </AdminDialog>
          <PasswordSetupDialog
            open={passwordSetupRequired}
            email={user?.email}
            onSubmit={async (password) => {
              await cloudRequest("/api/auth/password/setup", {
                method: "POST",
                body: { password },
              });
              setPasswordSetupRequired(false);
              notify("密码设置好了，这次记住它就行。", "success");
            }}
          />
        </Layout>
      </AdminOverlayProvider>
    </AdminContext.Provider>
  );
}
