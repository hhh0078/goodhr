// 本文件负责组装云端 HTTP 服务路由和公共响应工具。
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

type updateAdminSystemConfigRequest struct {
	ConfigValue string `json:"config_value"`
}

type Server struct {
	auth                *AuthService
	agent               *AgentService
	agentWS             *AgentWSHub
	ai                  *AIConfigService
	userPreferences     *UserPreferencesService
	notificationProfile *NotificationProfileService
	platformAccounts    *PlatformAccountService
	positions           *PositionService
	tasks               *TaskService
	taskLogs            *TaskLogService
	candidates          *CandidateService
	subscriptions       *SubscriptionService
	payments            *PaymentService
	onboarding          *OnboardingService
	invitations         *InvitationService
	activationCodes     *ActivationCodeService
	adminUsers          *AdminUserService
	publicStats         *PublicStatsService
	teamStats           *TeamStatsService
	dailyStats          SystemDailyStatsStore
	help                *HelpService
	systemConfigs       SystemConfigStore
	tenants             *TenantService
	cookies             *CookieService
}

// NewServer 创建云端 HTTP 服务实例，并完成认证和各业务模块依赖注入。
func NewServer() (*Server, error) {
	config := LoadConfigFromEnv()
	// 调用 PostgreSQL 初始化逻辑，供任务和平台账号映射在启用数据库时复用同一连接。
	db, err := config.PostgresDB()
	if err != nil {
		return nil, err
	}
	if db != nil {
		log.Print("PostgreSQL 连接检查成功")
	}
	mailer, exposeDebugCode := config.Mailer()
	tenantStore := config.TenantStore(db)
	onboardingStore := config.OnboardingStore(db)
	systemConfigStore := config.SystemConfigStore(db)
	subscriptionStore := config.SubscriptionStore(db)
	adminUserStore := config.AdminUserStore(db, subscriptionStore)
	invitationStore := config.InvitationStore(db)
	activationCodeStore := config.ActivationCodeStore(db)
	authStore, err := config.AuthStore()
	if err != nil {
		return nil, err
	}
	if config.RedisAddr != "" {
		log.Print("Redis 连接检查成功")
	}
	auth := NewAuthService(authStore, mailer, exposeDebugCode, tenantStore, onboardingStore, invitationStore, subscriptionStore, systemConfigStore, config.UserActivityStore(db), config.SuperAdmins)
	agentWS := NewAgentWSHub(auth)
	taskStore := config.TaskStore(db)
	dailyStatsStore := config.SystemDailyStatsStore(db)
	candidateStore := config.CandidateStore(db)
	agentStore := config.AgentStore(db)
	cookieStore := config.CookieStore(db)
	platformAccountStore := config.PlatformAccountStore(db)
	positionStore := config.PositionStore(db)
	aiConfigStore := config.AIConfigStore(db)
	userPreferencesStore := config.UserPreferencesStore(db)
	notificationProfileStore := config.NotificationProfileStore(db)
	paymentStore := config.PaymentStore(db)
	taskLogs := NewTaskLogService(auth, taskStore, config.TaskLogStore(db), tenantStore)
	paymentService := NewPaymentService(auth, paymentStore, subscriptionStore, systemConfigStore, invitationStore, mailer, NewHaoshoumiProvider(config))
	return &Server{
		auth:                auth,
		agent:               NewAgentService(auth, agentStore, systemConfigStore),
		agentWS:             agentWS,
		ai:                  NewAIConfigService(auth, aiConfigStore),
		userPreferences:     NewUserPreferencesService(auth, userPreferencesStore),
		notificationProfile: NewNotificationProfileService(auth, notificationProfileStore),
		platformAccounts:    NewPlatformAccountService(auth, platformAccountStore, tenantStore),
		positions:           NewPositionService(auth, positionStore, systemConfigStore, aiConfigStore),
		tasks:               NewTaskService(auth, taskStore, positionStore, *taskLogs, tenantStore, platformAccountStore, candidateStore, subscriptionStore, mailer, dailyStatsStore),
		taskLogs:            taskLogs,
		candidates:          NewCandidateService(auth, candidateStore, tenantStore),
		subscriptions:       NewSubscriptionService(auth, subscriptionStore, systemConfigStore),
		payments:            paymentService,
		onboarding:          NewOnboardingService(auth, onboardingStore, systemConfigStore),
		invitations:         NewInvitationService(auth, invitationStore, systemConfigStore),
		activationCodes:     NewActivationCodeService(auth, activationCodeStore, subscriptionStore, mailer),
		adminUsers:          NewAdminUserService(auth, adminUserStore, subscriptionStore, mailer, agentStore),
		publicStats:         NewPublicStatsService(adminUserStore, taskStore, agentStore, dailyStatsStore),
		teamStats:           NewTeamStatsService(auth, db, tenantStore),
		dailyStats:          dailyStatsStore,
		help:                NewHelpService(auth, systemConfigStore, aiConfigStore),
		systemConfigs:       systemConfigStore,
		tenants:             NewTenantService(auth, tenantStore),
		cookies:             NewCookieService(auth, cookieStore, tenantStore, agentStore, agentWS),
	}, nil
}

// Routes 注册云端 HTTP 路由，并包装基础 CORS 中间件。
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// 注册健康检查接口，用于部署和本地开发确认服务在线。
	mux.HandleFunc("/health", s.health)
	// 注册认证接口，用于邮箱验证码登录和登录态校验。
	mux.HandleFunc("/api/auth/send-code", s.auth.SendCode)
	mux.HandleFunc("/api/auth/login", s.auth.Login)
	mux.HandleFunc("/api/auth/me", s.auth.Me)
	mux.HandleFunc("/api/public/stats/today", s.publicStats.Today)
	// 注册机器绑定接口，用于云端记录当前账号对应的本地 Agent。
	mux.HandleFunc("/api/agents/bind", s.agent.Bind)
	mux.HandleFunc("/api/agents/current", s.agent.Current)
	mux.HandleFunc("/api/agents/ws", s.agentWS.ServeWS)
	mux.HandleFunc("/api/agents/ws-status", s.agentWS.Status)
	// 注册 AI 配置接口，用于读取用户自定义和最终生效配置。
	mux.HandleFunc("/api/config/user-ai", s.ai.User)
	mux.HandleFunc("/api/config/effective-ai", s.ai.Effective)
	mux.HandleFunc("/api/config/test-ai", s.ai.Test)
	mux.HandleFunc("/api/config/user-preferences", s.userPreferences.User)
	mux.HandleFunc("/api/config/notification-profile", s.notificationProfile.User)
	mux.HandleFunc("/api/subscription/status", s.subscriptions.Status)
	mux.HandleFunc("/api/subscription/plans", s.subscriptions.Plans)
	mux.HandleFunc("/api/payment/orders", s.payments.Orders)
	mux.HandleFunc("/api/payment/orders/", s.payments.OrderDetail)
	mux.HandleFunc("/api/payment/notify/haoshoumi", s.payments.HaoshoumiNotify)
	mux.HandleFunc("/api/admin/payment/orders", s.payments.ListAdminOrders)
	mux.HandleFunc("/api/onboarding/status", s.onboarding.Status)
	mux.HandleFunc("/api/onboarding/complete", s.onboarding.Complete)
	mux.HandleFunc("/api/invitations/summary", s.invitations.Summary)
	mux.HandleFunc("/api/team/stats", s.teamStats.Summary)
	mux.HandleFunc("/api/activation-codes/redeem", s.activationCodes.Redeem)
	mux.HandleFunc("/api/admin/activation-codes", s.activationCodes.AdminCollection)
	mux.HandleFunc("/api/admin/users", s.adminUsers.Collection)
	mux.HandleFunc("/api/admin/users/unbind-agent", s.adminUsers.UnbindAgent)
	mux.HandleFunc("/api/help/guide", s.help.Guide)
	mux.HandleFunc("/api/help/chat", s.help.Chat)
	// 注册平台账号信息接口，只保存名称和本地 profile 标识。
	mux.HandleFunc("/api/platform-accounts", s.platformAccounts.List)
	mux.HandleFunc("/api/platform-accounts/create", s.platformAccounts.Create)
	mux.HandleFunc("/api/platform-accounts/", s.platformAccounts.Delete)
	// 注册岗位配置接口，用于复用岗位关键词和问候语模板。
	mux.HandleFunc("/api/positions", s.positions.Collection)
	mux.HandleFunc("/api/positions/optimize-requirement", s.positions.OptimizeRequirement)
	mux.HandleFunc("/api/positions/", s.positions.Delete)
	// 注册任务接口，用于创建任务和展示任务统计摘要。
	mux.HandleFunc("/api/tasks", s.tasks.Collection)
	// 注册任务日志接口，用于展开任务卡片时查看运行摘要。
	mux.HandleFunc("/api/fail-notice", s.tasks.FailNotice)
	mux.HandleFunc("/api/tasks/", s.taskOrLog)
	// 注册简历库接口，用于查看当前团队或指定任务下的候选人。
	mux.HandleFunc("/api/candidates", s.candidates.Collection)
	mux.HandleFunc("/api/candidates/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/notes") {
			s.candidates.Notes(w, r)
			return
		}
		s.candidates.Detail(w, r)
	})
	// 注册平台配置接口，用于读取平台选择器和行为配置供任务执行使用。
	mux.HandleFunc("/api/platforms/config/", s.ListPlatformConfigs)
	mux.HandleFunc("/api/system/app-config", s.GetAppConfig)
	mux.HandleFunc("/api/system/default-prompts", s.GetDefaultPrompts)
	mux.HandleFunc("/api/admin/system/configs/", s.adminSystemConfigs)
	mux.HandleFunc("/api/admin/platforms/config/", s.adminPlatformConfigs)
	// 注册租户管理接口，用于管理员邀请成员和管理租户。
	mux.HandleFunc("/api/tenants/members", s.tenants.Members)
	mux.HandleFunc("/api/tenants/invite", s.tenants.Invite)
	mux.HandleFunc("/api/tenants/cookie-sharing", s.tenants.ToggleCookieSharing)
	mux.HandleFunc("/api/tenants/members/", s.tenantMember)
	mux.HandleFunc("/api/cookies", s.cookies.List)
	mux.HandleFunc("/api/cookies/create", s.cookies.Create)
	mux.HandleFunc("/api/cookies/", s.cookieRoute)
	return cors(mux)
}

// taskOrLog 根据路径分发任务详情和任务日志请求。
func (s *Server) taskOrLog(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/logs") {
		// 调用任务日志服务处理日志读写，供前端展开任务卡片。
		s.taskLogs.Collection(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/run") {
		// 调用任务服务异步执行任务。
		s.tasks.Run(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/stop") {
		// 调用任务服务停止正在运行的任务。
		s.tasks.Stop(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/candidates") {
		// 接收本地程序回传的候选人 JSON，并写入云端简历库。
		s.tasks.SaveLocalCandidate(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/processed-resumes") {
		// 接收本地程序上报的已处理简历数量，供官网公开统计展示。
		s.tasks.AddProcessedResumes(w, r)
		return
	}
	// 调用任务服务处理任务详情读取。
	s.tasks.Detail(w, r)
}

// health 返回云端 API 的健康状态。
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"name":    "GoodHR 5 Cloud API",
		"version": "0.1.0",
	})
}

// writeJSON 将响应对象序列化为 JSON。
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Vary", "Authorization")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError 按统一格式返回 API 错误。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": message,
	})
}

// GetAppConfig 返回前端公共系统配置。
func (s *Server) GetAppConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg, err := s.systemConfigs.Get("system.app_config")
	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			writeError(w, http.StatusNotFound, "system app config not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load system app config")
		return
	}

	var value any
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &value); err != nil {
		writeError(w, http.StatusInternalServerError, "system app config is invalid")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": value,
	})
}

// ListPlatformConfigs 返回业务流程可读取的已启用平台配置。
func (s *Server) ListPlatformConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用 system_configs 存储读取平台配置，用于云端前端和任务执行。
	configs, err := s.systemConfigs.List("platform.")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load platform configs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"configs": configs,
	})
}

// ListAdminPlatformConfigs 返回管理员可查看的原始系统配置 JSON。
func (s *Server) ListAdminPlatformConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}

	configs, err := s.systemConfigs.List("platform.")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load system configs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"configs": configs,
	})
}

func (s *Server) adminPlatformConfigs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminPlatformConfigs(w, r)
	case http.MethodPut:
		s.UpdateAdminPlatformConfig(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) adminSystemConfigs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ListAdminSystemConfigs(w, r)
	case http.MethodPut:
		s.UpdateAdminPlatformConfig(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ListAdminSystemConfigs 返回管理员可查看的全部系统配置 JSON。
func (s *Server) ListAdminSystemConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}

	configs, err := s.systemConfigs.List("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load system configs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"configs": configs,
	})
}

// UpdateAdminPlatformConfig 允许超管直接保存系统原始 JSON。
func (s *Server) UpdateAdminPlatformConfig(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}

	configKey := strings.TrimPrefix(r.URL.Path, "/api/admin/system/configs/")
	if configKey == r.URL.Path {
		configKey = strings.TrimPrefix(r.URL.Path, "/api/admin/platforms/config/")
	}
	configKey = strings.TrimSpace(strings.Trim(configKey, "/"))
	if configKey == "" {
		writeError(w, http.StatusBadRequest, "config key is required")
		return
	}

	existing, err := s.systemConfigs.Get(configKey)
	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			writeError(w, http.StatusNotFound, "system config not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load system config")
		return
	}

	var req updateAdminSystemConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	raw := strings.TrimSpace(req.ConfigValue)
	if raw == "" {
		writeError(w, http.StatusBadRequest, "config_value is required")
		return
	}
	if !json.Valid([]byte(raw)) {
		writeError(w, http.StatusBadRequest, "config_value must be valid json")
		return
	}

	existing.ConfigValue = raw
	if err := s.systemConfigs.Save(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save system config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": existing,
	})
}

func (s *Server) tenantMember(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		s.tenants.UpdateMember(w, r)
	case http.MethodDelete:
		s.tenants.DeleteMember(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) cookieRoute(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/claim") {
		s.cookies.Claim(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/release") {
		s.cookies.Release(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/status") {
		s.cookies.Status(w, r)
		return
	}
	if r.Method == http.MethodPut {
		s.cookies.Update(w, r)
		return
	}
	s.cookies.Delete(w, r)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-GoodHR-Agent-BaseURL")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
