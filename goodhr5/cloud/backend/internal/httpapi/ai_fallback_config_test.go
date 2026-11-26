// 本文件验证备用AI配置的超管权限、密钥保留、并发保护及公开接口不泄露新字段。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fallbackConfigCall 通过真实路由发起配置请求，返回HTTP回执和脱敏配置版本。
func fallbackConfigCall(t *testing.T, server *Server, token, method, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, "/api/admin/system/ai-fallback", strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	var data map[string]any
	if json.Unmarshal(response.Body.Bytes(), &data) != nil {
		t.Fatal("配置接口未返回JSON")
	}
	return response, data
}

// TestAIFallbackConfigPermissionsAndPreservation 验证表单不回显密钥、留空保留并保持主用与其它配置不变。
func TestAIFallbackConfigPermissionsAndPreservation(t *testing.T) {
	t.Setenv("GOODHR_SUPER_ADMINS", "backup-admin@example.com")
	server := mustNewServer(t)
	routes := server.Routes()
	admin := loginForTest(t, routes, "backup-admin@example.com")
	ordinary := loginForTest(t, routes, "ordinary@example.com")
	for _, item := range []struct {
		token  string
		status int
	}{{"", 401}, {ordinary, 403}} {
		resp, _ := fallbackConfigCall(t, server, item.token, http.MethodGet, "")
		if resp.Code != item.status {
			t.Fatalf("权限错误status=%d", resp.Code)
		}
	}
	cfg := builtinAIPolicyConfigForTest()
	saveBuiltinAIConfigForTest(t, server.systemConfigs, cfg)
	response, view := fallbackConfigCall(t, server, admin, http.MethodGet, "")
	if response.Code != 200 {
		t.Fatalf("读取失败status=%d", response.Code)
	}
	revision := view["revision"].(string)
	payload := map[string]any{"enabled": true, "base_url": "https://backup.example.com/v1", "api_key": "fixture-backup-secret", "model": "backup-model", "revision": revision}
	raw, _ := json.Marshal(payload)
	response, view = fallbackConfigCall(t, server, admin, http.MethodPut, string(raw))
	if response.Code != 200 || strings.Contains(response.Body.String(), "fixture-backup-secret") {
		t.Fatalf("保存或脱敏错误status=%d", response.Code)
	}
	if view["config"].(map[string]any)["api_key_set"] != true {
		t.Fatal("已配置密钥未正确标记")
	}
	stale, _ := fallbackConfigCall(t, server, admin, http.MethodPut, string(raw))
	if stale.Code != 409 {
		t.Fatal("旧版本覆盖未被拦截")
	}
	payload["revision"] = view["revision"]
	payload["api_key"] = ""
	payload["model"] = "next-backup-model"
	raw, _ = json.Marshal(payload)
	response, _ = fallbackConfigCall(t, server, admin, http.MethodPut, string(raw))
	if response.Code != 200 {
		t.Fatal("留空保留密钥失败")
	}
	loaded, err := server.aiWallet.loadBuiltinAIConfig()
	if err != nil || loaded.Fallback == nil || loaded.Fallback.APIKey != "fixture-backup-secret" || loaded.Fallback.Model != "next-backup-model" {
		t.Fatal("备用密钥或模型未保留")
	}
	if loaded.UpstreamBaseURL != cfg.UpstreamBaseURL || loaded.UpstreamAPIKey != cfg.UpstreamAPIKey || len(loaded.Models) != len(cfg.Models) || loaded.Models[0] != cfg.Models[0] {
		t.Fatal("主用模型或价格被改动")
	}
	public := httptest.NewRecorder()
	routes.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/system/app-config", nil))
	for _, value := range []string{"fixture-backup-secret", "backup.example.com", "next-backup-model", "fallback"} {
		if strings.Contains(public.Body.String(), value) {
			t.Fatal("公开配置泄露备用配置")
		}
	}
}

// TestAIFallbackConfigValidation 验证完整三项要求、地址和模型边界，不通过时原配置不变。
func TestAIFallbackConfigValidation(t *testing.T) {
	for _, cfg := range []BuiltinAIFallbackConfig{
		{Enabled: true, BaseURL: "https://backup.example.com", Model: "backup"},
		{Enabled: true, BaseURL: "https://backup.example.com", APIKey: "fixture"},
		{BaseURL: "file:///tmp/anything"},
		{BaseURL: "https://user:pass@backup.example.com"},
		{BaseURL: "https://backup.example.com?api_key=fixture"},
		{BaseURL: "http://goodhr.transit.ai.58it.cn/api/ai-compatible/v1/chat/completions"},
		{BaseURL: "https://goodhr.transit.ai.58it.cn:443/api/ai-compatible/v1/chat/completions"},
		{BaseURL: "https://goodhr.transit.ai.58it.cn./v1"},
		{Model: "model\nheader"},
		{APIKey: "key\r\nheader"},
	} {
		if validateAIFallbackConfig(cfg) == nil {
			t.Fatal("坏配置被接受")
		}
	}
	if validateAIFallbackConfig(BuiltinAIFallbackConfig{}) != nil {
		t.Fatal("未启用的空配置不应报错")
	}
}

// TestMalformedAIFallbackDoesNotBreakPrimary 验证旧手写备用的坏类型不拖累主用模型配置。
func TestMalformedAIFallbackDoesNotBreakPrimary(t *testing.T) {
	for _, fallback := range []string{`"bad"`, `{"enabled":"yes"}`, `{"api_key":[]}`} {
		store := NewMemorySystemConfigStore()
		raw := `{"builtin_ai":{"upstream_base_url":"https://primary.example.com","upstream_api_key":"fixture-primary","models":[{"id":"primary"}],"fallback":` + fallback + "}}"
		if err := store.Save(SystemConfig{ConfigKey: "system.app_config", ConfigValue: raw, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadBuiltinAIConfig(store)
		if err != nil || cfg.Fallback != nil || cfg.Models[0].ID != "primary" || cfg.UpstreamAPIKey != "fixture-primary" {
			t.Fatal("坏备用影响了主用")
		}
	}
}
