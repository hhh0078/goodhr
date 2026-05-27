// 本文件负责测试云端 AI 配置 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAIConfigEffectiveUserOnly 验证最终生效 AI 配置只来自用户配置。
func TestAIConfigEffectiveUserOnly(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "1224299352@qq.com")

	// 调用用户配置接口，保存当前用户自己的 AI 服务参数。
	updateUser := httptest.NewRequest(
		http.MethodPut,
		"/api/config/user-ai",
		bytes.NewBufferString(`{"base_url":"https://user.example.com","model":"user-model","api_key":"user-secret-key","temperature":0.7,"prompt_template":"user prompt","enabled":true}`),
	)
	updateUser.Header.Set("Authorization", "Bearer "+token)
	updateUserResp := httptest.NewRecorder()
	routes.ServeHTTP(updateUserResp, updateUser)
	if updateUserResp.Code != http.StatusOK {
		t.Fatalf("update user status = %d, body = %s", updateUserResp.Code, updateUserResp.Body.String())
	}

	// 调用最终生效配置接口，确认返回用户配置。
	effectiveReq := httptest.NewRequest(http.MethodGet, "/api/config/effective-ai", nil)
	effectiveReq.Header.Set("Authorization", "Bearer "+token)
	effectiveResp := httptest.NewRecorder()
	routes.ServeHTTP(effectiveResp, effectiveReq)
	if effectiveResp.Code != http.StatusOK {
		t.Fatalf("effective status = %d, body = %s", effectiveResp.Code, effectiveResp.Body.String())
	}

	var payload struct {
		Config struct {
			BaseURL   string  `json:"base_url"`
			Model     string  `json:"model"`
			KeySet    bool    `json:"api_key_set"`
			Temp      float64 `json:"temperature"`
			Prompt    string  `json:"prompt_template"`
			KeyMasked string  `json:"api_key_masked"`
		} `json:"config"`
	}
	if err := json.NewDecoder(effectiveResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Config.BaseURL != "https://user.example.com" {
		t.Fatalf("base_url = %q", payload.Config.BaseURL)
	}
	if payload.Config.Model != "user-model" {
		t.Fatalf("model = %q", payload.Config.Model)
	}
	if payload.Config.Prompt != "user prompt" {
		t.Fatalf("prompt = %q", payload.Config.Prompt)
	}
	if !payload.Config.KeySet || payload.Config.KeyMasked != "user****-key" {
		t.Fatalf("api key masking failed: set=%v masked=%q", payload.Config.KeySet, payload.Config.KeyMasked)
	}
}

// TestAIConfigRejectsAnonymous 验证未登录用户不能读取 AI 配置。
func TestAIConfigRejectsAnonymous(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/config/effective-ai", nil)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("effective status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}
