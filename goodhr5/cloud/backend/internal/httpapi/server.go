package httpapi

import (
	"encoding/json"
	"net/http"
)

type Server struct {
	auth  *AuthService
	agent *AgentService
}

// NewServer 创建云端 HTTP 服务实例，并完成认证和 Agent 模块依赖注入。
func NewServer() *Server {
	config := LoadConfigFromEnv()
	mailer, exposeDebugCode := config.Mailer()
	auth := NewAuthService(config.AuthStore(), mailer, exposeDebugCode)
	return &Server{
		auth:  auth,
		agent: NewAgentService(auth, config.AgentStore()),
	}
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
	return cors(mux)
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
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
