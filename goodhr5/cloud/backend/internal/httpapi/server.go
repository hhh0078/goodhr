// 本文件负责组装云端 HTTP 服务路由和公共响应工具。
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type Server struct {
	auth             *AuthService
	agent            *AgentService
	ai               *AIConfigService
	platformAccounts *PlatformAccountService
	positions        *PositionService
	tasks            *TaskService
	taskLogs         *TaskLogService
	systemConfigs    SystemConfigStore
	tenants          *TenantService
	cookies          *CookieService
}

// NewServer 创建云端 HTTP 服务实例，并完成认证和各业务模块依赖注入。
func NewServer() (*Server, error) {
	config := LoadConfigFromEnv()
	// 调用 PostgreSQL 初始化逻辑，供任务和平台账号映射在启用数据库时复用同一连接。
	db, err := config.PostgresDB()
	if err != nil {
		return nil, err
	}
	mailer, exposeDebugCode := config.Mailer()
	auth := NewAuthService(config.AuthStore(), mailer, exposeDebugCode)
	taskStore := config.TaskStore(db)
	taskLogs := NewTaskLogService(auth, taskStore, config.TaskLogStore(db))
	return &Server{
		auth:             auth,
		agent:            NewAgentService(auth, config.AgentStore(db)),
		ai:               NewAIConfigService(auth, config.AIConfigStore(db)),
		platformAccounts: NewPlatformAccountService(auth, config.PlatformAccountStore(db)),
		positions:        NewPositionService(auth, config.PositionStore(db)),
		tasks:            NewTaskService(auth, taskStore, config.SystemConfigStore(db), config.PositionStore(db), *taskLogs, config.AIConfigStore(db), config.TenantStore(db), config.CookieStore(db)),
		taskLogs:         taskLogs,
		systemConfigs:    config.SystemConfigStore(db),
		tenants:          NewTenantService(auth, config.TenantStore(db)),
		cookies:          NewCookieService(auth, config.CookieStore(db), config.TenantStore(db)),
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
	// 注册机器绑定接口，用于云端记录当前账号对应的本地 Agent。
	mux.HandleFunc("/api/agents/bind", s.agent.Bind)
	mux.HandleFunc("/api/agents/current", s.agent.Current)
	// 注册 AI 配置接口，用于读取系统默认、用户自定义和最终生效配置。
	mux.HandleFunc("/api/config/system-ai", s.ai.System)
	mux.HandleFunc("/api/admin/config/system-ai", s.ai.UpdateSystem)
	mux.HandleFunc("/api/config/user-ai", s.ai.User)
	mux.HandleFunc("/api/config/effective-ai", s.ai.Effective)
	// 注册平台账号映射接口，用于同一平台多账号/profile 管理。
	mux.HandleFunc("/api/platform-accounts", s.platformAccounts.List)
	mux.HandleFunc("/api/platform-accounts/create", s.platformAccounts.Create)
	mux.HandleFunc("/api/platform-accounts/", s.platformAccounts.Delete)
	// 注册岗位配置接口，用于复用岗位关键词和问候语模板。
	mux.HandleFunc("/api/positions", s.positions.Collection)
	mux.HandleFunc("/api/positions/", s.positions.Delete)
	// 注册任务接口，用于创建任务和展示任务统计摘要。
	mux.HandleFunc("/api/tasks", s.tasks.Collection)
	// 注册任务日志接口，用于展开任务卡片时查看运行摘要。
	mux.HandleFunc("/api/tasks/", s.taskOrLog)
	// 注册平台配置接口，用于读取平台选择器和行为配置供任务执行使用。
	mux.HandleFunc("/api/platforms/config/", s.ListPlatformConfigs)
	// 注册租户管理接口，用于管理员邀请成员和管理租户。
	mux.HandleFunc("/api/tenants/members", s.tenants.Members)
	mux.HandleFunc("/api/tenants/invite", s.tenants.Invite)
	mux.HandleFunc("/api/tenants/cookie-sharing", s.tenants.ToggleCookieSharing)
	mux.HandleFunc("/api/tenants/members/", s.tenantMember)
	mux.HandleFunc("/api/cookies", s.cookies.List)
	mux.HandleFunc("/api/cookies/create", s.cookies.Create)
	mux.HandleFunc("/api/cookies/", s.cookies.Delete)
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

// cors 为本地开发和云端前端调用添加基础跨域响应头。
// ListPlatformConfigs 返回所有已启用的平台配置。
func (s *Server) ListPlatformConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，限制只返回已登录用户可见的配置。
	_, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}

	// 调用 system_configs 存储读取平台配置，用于云端前端和任务执行。
	configs, err := s.systemConfigs.List("platform.")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load platform configs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"configs":  configs,
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
