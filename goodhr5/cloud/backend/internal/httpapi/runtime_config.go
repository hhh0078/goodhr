// 本文件负责向已登录用户提供本地程序和必要运行组件配置。
package httpapi

import (
	"encoding/json"
	"net/http"
)

// RuntimeConfigService 处理本地程序与运行组件配置读取。
type RuntimeConfigService struct {
	auth          *AuthService
	systemConfigs SystemConfigStore
}

// NewRuntimeConfigService 创建运行组件配置服务。
func NewRuntimeConfigService(auth *AuthService, systemConfigs SystemConfigStore) *RuntimeConfigService {
	return &RuntimeConfigService{auth: auth, systemConfigs: systemConfigs}
}

// Current 返回当前可用的本地程序和运行组件配置。
func (s *RuntimeConfigService) Current(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := s.auth.SessionFromRequest(r); err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	config := map[string]any{
		"local_agent":        []any{},
		"runtime_components": map[string]any{},
	}
	if cfg, err := s.systemConfigs.Get("system.onboarding_config"); err == nil {
		_ = json.Unmarshal([]byte(cfg.ConfigValue), &config)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": config})
}
