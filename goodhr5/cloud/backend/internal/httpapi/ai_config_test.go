// 本文件负责测试云端 AI 配置 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 执行测试自定义 HTTP 响应逻辑。
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

	// 调用个人配置接口，确认后台表单可直接显示明文 Key。
	userReq := httptest.NewRequest(http.MethodGet, "/api/config/user-ai", nil)
	userReq.Header.Set("Authorization", "Bearer "+token)
	userResp := httptest.NewRecorder()
	routes.ServeHTTP(userResp, userReq)
	if userResp.Code != http.StatusOK {
		t.Fatalf("user ai status = %d, body = %s", userResp.Code, userResp.Body.String())
	}
	var userPayload struct {
		Config struct {
			APIKey string `json:"api_key"`
		} `json:"config"`
	}
	if err := json.NewDecoder(userResp.Body).Decode(&userPayload); err != nil {
		t.Fatal(err)
	}
	if userPayload.Config.APIKey != "user-secret-key" {
		t.Fatalf("user api_key = %q", userPayload.Config.APIKey)
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
			APIKey    string  `json:"api_key"`
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
	if payload.Config.APIKey != "" {
		t.Fatalf("普通读取不应返回明文 api_key")
	}

	revealReq := httptest.NewRequest(http.MethodGet, "/api/config/effective-ai?reveal_api_key=1", nil)
	revealReq.Header.Set("Authorization", "Bearer "+token)
	revealResp := httptest.NewRecorder()
	routes.ServeHTTP(revealResp, revealReq)
	if revealResp.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, body = %s", revealResp.Code, revealResp.Body.String())
	}
	var revealPayload struct {
		Config struct {
			APIKey string `json:"api_key"`
		} `json:"config"`
	}
	if err := json.NewDecoder(revealResp.Body).Decode(&revealPayload); err != nil {
		t.Fatal(err)
	}
	if revealPayload.Config.APIKey != "user-secret-key" {
		t.Fatalf("reveal api_key = %q", revealPayload.Config.APIKey)
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

// TestAIConfigTestProxy 验证 AI 配置测试由云端后端代发并返回成功结果。
func TestAIConfigTestProxy(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "1224299352@qq.com")

	server.ai.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://token-plan.example.com/compatible-mode/v1/chat/completions" {
			t.Fatalf("AI request URL = %q", req.URL.String())
		}
		if req.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"成功"}}]}`)),
		}, nil
	})}

	req := httptest.NewRequest(http.MethodPost, "/api/config/test-ai", bytes.NewBufferString(`{"base_url":"https://token-plan.example.com/compatible-mode/v1/chat/completions","model":"test-model","api_key":"test-secret"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("test AI status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Content != "成功" {
		t.Fatalf("test AI response = %+v", payload)
	}
}

// TestAIConfigTestProxyNormalizesBaseURL 验证 AI 测试会补全 OpenAI 兼容调用地址。
func TestAIConfigTestProxyNormalizesBaseURL(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "1224299352@qq.com")
	server.ai.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://token-plan.example.com/compatible-mode/v1/chat/completions" {
			t.Fatalf("AI request URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"成功"}}]}`)),
		}, nil
	})}
	req := httptest.NewRequest(http.MethodPost, "/api/config/test-ai", bytes.NewBufferString(`{"base_url":"https://token-plan.example.com/compatible-mode/v1","model":"test-model","api_key":"test-secret"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("test AI status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

// TestAIConfigTestRejectsAnonymous 验证匿名用户不能借助云端测试任意 AI 地址。
func TestAIConfigTestRejectsAnonymous(t *testing.T) {
	server := mustNewServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/config/test-ai", bytes.NewBufferString(`{"base_url":"https://example.com/v1/chat/completions","model":"test-model","api_key":"test-secret"}`))
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("test AI anonymous status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}
