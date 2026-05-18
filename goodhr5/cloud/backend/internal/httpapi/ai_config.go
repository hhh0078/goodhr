// 本文件负责提供系统默认和用户自定义 AI 配置的 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// AIConfigService 处理 AI 配置读取、保存和合并请求。
type AIConfigService struct {
	auth  *AuthService
	store AIConfigStore
}

type aiConfigRequest struct {
	BaseURL        string  `json:"base_url"`
	Model          string  `json:"model"`
	APIKey         string  `json:"api_key"`
	Temperature    float64 `json:"temperature"`
	PromptTemplate string  `json:"prompt_template"`
	Enabled        bool    `json:"enabled"`
}

// NewAIConfigService 创建 AI 配置 API 服务，并注入认证服务和配置存储。
func NewAIConfigService(auth *AuthService, store AIConfigStore) *AIConfigService {
	return &AIConfigService{
		auth:  auth,
		store: store,
	}
}

// System 返回系统默认 AI 配置。
func (s *AIConfigService) System(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务校验登录态，避免匿名用户读取系统配置。
	if _, ok := s.currentSession(w, r); !ok {
		return
	}

	// 调用 AIConfigStore 读取系统默认配置，用于任务配置兜底。
	config, err := s.store.SystemConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load system ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicAIConfig(config),
	})
}

// UpdateSystem 保存系统默认 AI 配置。
func (s *AIConfigService) UpdateSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务校验登录态；管理员权限后续在这里继续收紧。
	if _, ok := s.currentSession(w, r); !ok {
		return
	}

	req, ok := s.readConfigRequest(w, r)
	if !ok {
		return
	}

	// 调用 AIConfigStore 保存系统默认配置，供未配置用户使用。
	config, err := s.store.SaveSystemConfig(req.toConfig())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save system ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicAIConfig(config),
	})
}

// User 按请求方法读取或保存当前登录用户的自定义 AI 配置。
func (s *AIConfigService) User(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		// PUT 请求复用用户配置保存逻辑，让读取和更新保持同一个资源路径。
		s.UpdateUser(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于只返回自己的 AI 配置。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用 AIConfigStore 读取用户配置；未配置时返回空配置。
	config, err := s.store.UserConfig(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"config": nil,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicAIConfig(config),
	})
}

// UpdateUser 保存当前登录用户的自定义 AI 配置。
func (s *AIConfigService) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于将配置写入该用户名下。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	req, ok := s.readConfigRequest(w, r)
	if !ok {
		return
	}

	// 调用 AIConfigStore 保存用户配置，任务运行时会优先使用它。
	config, err := s.store.SaveUserConfig(session.Email, req.toConfig())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicAIConfig(config),
	})
}

// Effective 返回当前登录用户最终生效的 AI 配置。
func (s *AIConfigService) Effective(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 调用认证服务读取当前用户，用于合并该用户自己的配置。
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}

	// 调用 AIConfigStore 读取系统默认配置，作为最终配置的基础。
	system, err := s.store.SystemConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load system ai config")
		return
	}

	// 调用 AIConfigStore 读取用户配置；未配置时直接使用系统默认配置。
	user, err := s.store.UserConfig(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"config": publicAIConfig(system),
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user ai config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicAIConfig(EffectiveAIConfig(system, user)),
	})
}

// currentSession 从请求中解析登录会话。
func (s *AIConfigService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	// 调用认证服务解析请求会话，避免 AI 配置 API 自己重复处理 token。
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, false
	}
	return session, true
}

// readConfigRequest 读取并校验 AI 配置请求体。
func (s *AIConfigService) readConfigRequest(w http.ResponseWriter, r *http.Request) (aiConfigRequest, bool) {
	var req aiConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return aiConfigRequest{}, false
	}
	return req, true
}

// toConfig 将 HTTP 请求结构转换为内部 AI 配置模型。
func (r aiConfigRequest) toConfig() AIConfig {
	return AIConfig{
		BaseURL:        r.BaseURL,
		Model:          r.Model,
		APIKey:         r.APIKey,
		Temperature:    r.Temperature,
		PromptTemplate: r.PromptTemplate,
		Enabled:        r.Enabled,
	}
}

// publicAIConfig 返回可给前端展示的 AI 配置，并隐藏完整 API Key。
func publicAIConfig(config AIConfig) map[string]any {
	return map[string]any{
		"base_url":         config.BaseURL,
		"model":            config.Model,
		"api_key_set":      config.APIKey != "",
		"temperature":      config.Temperature,
		"prompt_template":  config.PromptTemplate,
		"enabled":          config.Enabled,
		"updated_at":       config.UpdatedAt,
		"api_key_masked":   maskedAPIKey(config.APIKey),
		"api_key_redacted": config.APIKey != "",
	}
}

// maskedAPIKey 返回脱敏后的 API Key，避免完整密钥进入前端。
func maskedAPIKey(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
