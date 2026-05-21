// 本文件负责测试云端认证 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthCodeLogin 验证验证码发送、登录和登录态查询。
func TestAuthCodeLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	sendBody := bytes.NewBufferString(`{"email":"User@Example.com"}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", sendBody)
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)

	if sendResp.Code != http.StatusOK {
		t.Fatalf("send code status = %d, body = %s", sendResp.Code, sendResp.Body.String())
	}

	var sendPayload struct {
		DebugCode string `json:"debug_code"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(sendResp.Body).Decode(&sendPayload); err != nil {
		t.Fatal(err)
	}
	if sendPayload.Email != "user@example.com" {
		t.Fatalf("email was not normalized: %q", sendPayload.Email)
	}
	if len(sendPayload.DebugCode) != 4 {
		t.Fatalf("debug code length = %d", len(sendPayload.DebugCode))
	}

	loginBody := bytes.NewBufferString(`{"email":"user@example.com","code":"` + sendPayload.DebugCode + `"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatal(err)
	}
	if loginPayload.AccessToken == "" {
		t.Fatal("access token is empty")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginPayload.AccessToken)
	meResp := httptest.NewRecorder()
	routes.ServeHTTP(meResp, meReq)

	if meResp.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meResp.Code, meResp.Body.String())
	}

	var mePayload struct {
		User struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&mePayload); err != nil {
		t.Fatal(err)
	}
	if mePayload.User.Email != "user@example.com" {
		t.Fatalf("me email = %q", mePayload.User.Email)
	}
	if mePayload.User.Role != "admin" {
		t.Fatalf("me role = %q, want admin", mePayload.User.Role)
	}
}

// TestAuthRejectsWrongCode 验证错误验证码不能登录。
func TestAuthRejectsWrongCode(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"user@example.com"}`))
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"user@example.com","code":"0000"}`))
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want %d", loginResp.Code, http.StatusUnauthorized)
	}
}

// TestAuthMeRejectsMissingToken 验证未带 token 时不能读取登录态。
func TestAuthMeRejectsMissingToken(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("me status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}
