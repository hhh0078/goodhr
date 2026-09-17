// 本文件验证公开系统配置不会泄露内置 AI 的上游密钥和内部配置。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPublicAppConfigHidesBuiltinAISecrets 验证公开接口仅返回内置 AI 的展示字段，且不修改原始配置。
func TestPublicAppConfigHidesBuiltinAISecrets(t *testing.T) {
	server := mustNewServer(t)
	raw := `{"local_agent_version":"6","builtin_ai":{"public_base_url":"https://example.com/api/ai-compatible/v1/chat/completions","default_model":"test-model","signup_bonus_cents":50,"models":[{"id":"test-model"}],"upstream_api_key":"test-secret-key","upstream_base_url":"https://internal.example.com","future_secret":"test-future-secret"}}`
	if err := server.systemConfigs.Save(SystemConfig{ConfigKey: "system.app_config", ConfigValue: raw, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/system/app-config", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	for _, sensitive := range []string{"upstream_api_key", "upstream_base_url", "future_secret", "test-secret-key", "test-future-secret"} {
		if strings.Contains(resp.Body.String(), sensitive) {
			t.Fatalf("公开配置包含内部字段 %q", sensitive)
		}
	}
	var payload struct {
		Config struct {
			Version string         `json:"local_agent_version"`
			Builtin map[string]any `json:"builtin_ai"`
		} `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Config.Version != "6" || len(payload.Config.Builtin) != 4 || payload.Config.Builtin["default_model"] != "test-model" {
		t.Fatalf("公开展示字段不完整: %+v", payload.Config)
	}
	stored, err := server.systemConfigs.Get("system.app_config")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConfigValue != raw {
		t.Fatal("公开接口修改了原始系统配置")
	}
}
