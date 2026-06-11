// 本文件负责测试云端认证 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// TestAuthSessionsKeepSeparateUsers 验证多个用户同时登录时 token 不会串号。
func TestAuthSessionsKeepSeparateUsers(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	userToken := loginForTest(t, routes, "normal-user@example.com")

	adminEmail := currentUserEmailForTest(t, routes, adminToken)
	if adminEmail != "1224299352@qq.com" {
		t.Fatalf("admin token email = %q", adminEmail)
	}

	userEmail := currentUserEmailForTest(t, routes, userToken)
	if userEmail != "normal-user@example.com" {
		t.Fatalf("user token email = %q", userEmail)
	}
}

// TestUniversalLoginCode 验证动态万能验证码按当前时间加 3 分钟计算。
func TestUniversalLoginCode(t *testing.T) {
	china := chinaLocation()
	now := time.Date(2026, 6, 1, 18, 15, 0, 0, china)
	if !isUniversalLoginCode("1818", now) {
		t.Fatal("universal code 1818 should match 18:15 + 3 minutes")
	}

	carryNow := time.Date(2026, 6, 1, 18, 58, 0, 0, china)
	if !isUniversalLoginCode("1901", carryNow) {
		t.Fatal("universal code 1901 should match 18:58 + 3 minutes")
	}

	utcNow := time.Date(2026, 6, 1, 10, 15, 0, 0, time.UTC)
	if !isUniversalLoginCode("1818", utcNow) {
		t.Fatal("universal code should use China timezone when server time is UTC")
	}

	if isUniversalLoginCode("1858", carryNow) {
		t.Fatal("original time should not match universal code")
	}
}

// TestAuthLoginSendsInitialSubscriptionRewardOnce 验证新用户首次登录会收到试用会员赠送邮件且不会重复发送。
func TestAuthLoginSendsInitialSubscriptionRewardOnce(t *testing.T) {
	server := mustNewServer(t)
	mailer := &recordingMailer{}
	server.auth.mailer = mailer
	routes := server.Routes()

	loginForTest(t, routes, "trial-reward@example.com")
	if len(mailer.rewards) != 1 {
		t.Fatalf("reward email count = %d, want 1", len(mailer.rewards))
	}
	if mailer.rewards[0].email != "trial-reward@example.com" {
		t.Fatalf("reward email sent to %q", mailer.rewards[0].email)
	}
	if mailer.rewards[0].notice.Reason != "新用户注册赠送会员" {
		t.Fatalf("reward reason = %q", mailer.rewards[0].notice.Reason)
	}
	if mailer.rewards[0].notice.Days != 3 {
		t.Fatalf("reward days = %d, want 3", mailer.rewards[0].notice.Days)
	}

	loginForTest(t, routes, "trial-reward@example.com")
	if len(mailer.rewards) != 1 {
		t.Fatalf("reward email repeated, count = %d", len(mailer.rewards))
	}
}

// currentUserEmailForTest 使用指定 token 调用 /api/auth/me 并返回邮箱。
func currentUserEmailForTest(t *testing.T, routes http.Handler, token string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.User.Email
}

// TestAuthRejectsWrongCode 验证错误验证码不能登录。
func TestAuthRejectsWrongCode(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"user@example.com"}`))
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)

	wrongCode := "0000"
	if isUniversalLoginCode(wrongCode, time.Now()) {
		wrongCode = "9999"
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"user@example.com","code":"`+wrongCode+`"}`))
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want %d", loginResp.Code, http.StatusUnauthorized)
	}
}

type recordingMailer struct {
	loginCodes []struct {
		email string
		code  string
	}
	rewards []struct {
		email  string
		notice SubscriptionRewardNotice
	}
}

// SendLoginCode 记录验证码邮件发送请求。
func (m *recordingMailer) SendLoginCode(email string, code string) error {
	m.loginCodes = append(m.loginCodes, struct {
		email string
		code  string
	}{email: email, code: code})
	return nil
}

// SendSubscriptionReward 记录会员时间变动邮件发送请求。
func (m *recordingMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	m.rewards = append(m.rewards, struct {
		email  string
		notice SubscriptionRewardNotice
	}{email: email, notice: notice})
	return nil
}

// SendTaskStatus 忽略任务状态邮件发送请求。
func (m *recordingMailer) SendTaskStatus(email string, notice TaskStatusNotice) error {
	return nil
}

// TestAuthRejectsEmailDomainNotAllowed 验证发送验证码时会拦截非白名单邮箱域名。
func TestAuthRejectsEmailDomainNotAllowed(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"temp@mailto.plus"}`))
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)

	if sendResp.Code != http.StatusForbidden {
		t.Fatalf("send code status = %d, want %d, body = %s", sendResp.Code, http.StatusForbidden, sendResp.Body.String())
	}
	if !strings.Contains(sendResp.Body.String(), "该邮箱不在白名单内，请联系站长") {
		t.Fatalf("unexpected body: %s", sendResp.Body.String())
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
