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

	loginBody := bytes.NewBufferString(`{"email":"user@example.com","code":"` + sendPayload.DebugCode + `","password":"test-password-123","agreement_accepted":true}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	var loginPayload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatal(err)
	}
	if loginPayload.AccessToken == "" {
		t.Fatal("access token is empty")
	}
	if loginPayload.ExpiresIn != int((30 * 24 * time.Hour).Seconds()) {
		t.Fatalf("session expires_in = %d, want 30 days", loginPayload.ExpiresIn)
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

// TestNewAccountRequiresPassword 验证新邮箱必须同时提交验证码和合格密码才能注册。
func TestNewAccountRequiresPassword(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"new-password@example.com"}`)))
	var sent struct {
		DebugCode string `json:"debug_code"`
	}
	if err := json.NewDecoder(sendResp.Body).Decode(&sent); err != nil {
		t.Fatal(err)
	}
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"new-password@example.com","code":"`+sent.DebugCode+`","agreement_accepted":true}`)))
	if loginResp.Code != http.StatusBadRequest || !strings.Contains(loginResp.Body.String(), "密码至少需要 8 位") {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}
}

// TestPasswordLogin 验证完成注册的用户可以使用邮箱和密码登录。
func TestPasswordLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	loginForTest(t, routes, "password-login@example.com")

	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"password-login@example.com","password":"test-password-123"}`)))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "access_token") {
		t.Fatalf("password login status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

// TestPasswordLoginRateLimit 验证连续输错密码后会短暂限制继续尝试。
func TestPasswordLoginRateLimit(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	loginForTest(t, routes, "password-limit@example.com")
	for attempt := 0; attempt < maxPasswordLoginAttempts; attempt++ {
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"password-limit@example.com","password":"wrong-password"}`)))
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, body = %s", attempt+1, resp.Code, resp.Body.String())
		}
	}
	limited := httptest.NewRecorder()
	routes.ServeHTTP(limited, httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"password-limit@example.com","password":"test-password-123"}`)))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d, body = %s", limited.Code, limited.Body.String())
	}
}

// TestLegacyAccountMustSetPassword 验证历史账号用验证码登录后必须补设密码，随后可以密码登录。
func TestLegacyAccountMustSetPassword(t *testing.T) {
	server := mustNewServer(t)
	passwords, ok := server.auth.passwords.(*MemoryPasswordStore)
	if !ok {
		t.Fatal("test password store is not memory store")
	}
	passwords.AddLegacyAccount("legacy@example.com")
	routes := server.Routes()

	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"legacy@example.com"}`)))
	var sent struct {
		DebugCode string `json:"debug_code"`
	}
	if err := json.NewDecoder(sendResp.Body).Decode(&sent); err != nil {
		t.Fatal(err)
	}
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"legacy@example.com","code":"`+sent.DebugCode+`","agreement_accepted":true}`)))
	var login struct {
		AccessToken           string `json:"access_token"`
		RequiresPasswordSetup bool   `json:"requires_password_setup"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}
	if loginResp.Code != http.StatusOK || login.AccessToken == "" || !login.RequiresPasswordSetup {
		t.Fatalf("legacy login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	setupReq := httptest.NewRequest(http.MethodPost, "/api/auth/password/setup", bytes.NewBufferString(`{"password":"legacy-password-123"}`))
	setupReq.Header.Set("Authorization", "Bearer "+login.AccessToken)
	setupResp := httptest.NewRecorder()
	routes.ServeHTTP(setupResp, setupReq)
	if setupResp.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", setupResp.Code, setupResp.Body.String())
	}

	passwordResp := httptest.NewRecorder()
	routes.ServeHTTP(passwordResp, httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"legacy@example.com","password":"legacy-password-123"}`)))
	if passwordResp.Code != http.StatusOK {
		t.Fatalf("password login status = %d, body = %s", passwordResp.Code, passwordResp.Body.String())
	}
}

// TestAuthMeRefreshesLastLogin 验证用户复用有效登录态进入后台时也会刷新最近登录时间。
func TestAuthMeRefreshesLastLogin(t *testing.T) {
	store := NewMemoryAuthStore()
	activity := &recordingUserActivityStore{}
	auth := NewAuthService(store, NewMemoryPasswordStore(), DevMailer{}, true, nil, nil, nil, nil, activity, nil, nil, 0)
	if err := store.SaveSession("active-token", Session{Email: "active@example.com", CreatedAt: time.Now()}, time.Hour); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer active-token")
	resp := httptest.NewRecorder()
	auth.Me(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if activity.email != "active@example.com" || activity.at.IsZero() {
		t.Fatalf("recorded activity = %q at %v", activity.email, activity.at)
	}
}

// recordingUserActivityStore 记录认证测试中的最近登录写入。
type recordingUserActivityStore struct {
	email string
	at    time.Time
}

// RecordLogin 保存测试捕获到的用户和时间。
func (s *recordingUserActivityStore) RecordLogin(email string, at time.Time) error {
	s.email = email
	s.at = at
	return nil
}

// HasAcceptedAgreement 让测试账号默认需要本次请求显式同意协议。
func (s *recordingUserActivityStore) HasAcceptedAgreement(email string) (bool, error) {
	return false, nil
}

// AcceptAgreement 忽略本测试未涉及的协议确认落库。
func (s *recordingUserActivityStore) AcceptAgreement(email string, at time.Time) error {
	return nil
}

// ShouldShowTrialWelcome 让本测试不展示试用欢迎弹框。
func (s *recordingUserActivityStore) ShouldShowTrialWelcome(email string) (bool, error) {
	return false, nil
}

// AckTrialWelcome 忽略本测试未涉及的欢迎弹框确认。
func (s *recordingUserActivityStore) AckTrialWelcome(email string, at time.Time) error {
	return nil
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

// TestAuthLoginKeepsPreviousSession 验证同一账号多处登录不会互相踢下线。
func TestAuthLoginKeepsPreviousSession(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	oldToken := loginForTest(t, routes, "single-login@example.com")
	newToken := loginForTest(t, routes, "single-login@example.com")

	oldReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	oldReq.Header.Set("Authorization", "Bearer "+oldToken)
	oldResp := httptest.NewRecorder()
	routes.ServeHTTP(oldResp, oldReq)
	if oldResp.Code != http.StatusOK {
		t.Fatalf("old token status = %d, body = %s", oldResp.Code, oldResp.Body.String())
	}

	newReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	newReq.Header.Set("Authorization", "Bearer "+newToken)
	newResp := httptest.NewRecorder()
	routes.ServeHTTP(newResp, newReq)
	if newResp.Code != http.StatusOK {
		t.Fatalf("new token status = %d, body = %s", newResp.Code, newResp.Body.String())
	}
}

// TestUniversalLoginCode 验证动态万能验证码只在配置偏移分钟后生效。
func TestUniversalLoginCode(t *testing.T) {
	china := chinaLocation()
	now := time.Date(2026, 6, 1, 18, 15, 0, 0, china)
	if isUniversalLoginCode("1818", now, 0) {
		t.Fatal("universal code should be disabled when offset is 0")
	}
	if isUniversalLoginCode("1818", now, -1) {
		t.Fatal("universal code should be disabled when offset is negative")
	}
	if !isUniversalLoginCode("1818", now, 3) {
		t.Fatal("universal code 1818 should match 18:15 + 3 minutes when configured")
	}

	carryNow := time.Date(2026, 6, 1, 18, 58, 0, 0, china)
	if !isUniversalLoginCode("1901", carryNow, 3) {
		t.Fatal("universal code 1901 should match 18:58 + 3 minutes when configured")
	}

	utcNow := time.Date(2026, 6, 1, 10, 15, 0, 0, time.UTC)
	if !isUniversalLoginCode("1818", utcNow, 3) {
		t.Fatal("universal code should use China timezone when server time is UTC")
	}
	if isUniversalLoginCode("1858", carryNow, 3) {
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
	if isUniversalLoginCode(wrongCode, time.Now(), 3) {
		wrongCode = "9999"
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"user@example.com","code":"`+wrongCode+`","password":"test-password-123","agreement_accepted":true}`))
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
	resetCodes []struct {
		email string
		code  string
	}
	rewards []struct {
		email  string
		notice SubscriptionRewardNotice
	}
	balanceNotices []struct {
		email  string
		notice AIBalanceNotice
	}
	positionNotices []struct {
		email  string
		notice PositionStatusNotice
	}
	subscriptionRewardErr error
	positionStatusErr     error
}

// SendLoginCode 记录验证码邮件发送请求。
func (m *recordingMailer) SendLoginCode(email string, code string) error {
	m.loginCodes = append(m.loginCodes, struct {
		email string
		code  string
	}{email: email, code: code})
	return nil
}

// SendPasswordResetCode 记录重置密码验证码邮件发送请求。
func (m *recordingMailer) SendPasswordResetCode(email string, code string) error {
	m.resetCodes = append(m.resetCodes, struct {
		email string
		code  string
	}{email: email, code: code})
	return nil
}

// SendSubscriptionReward 记录会员时间变动邮件发送请求。
func (m *recordingMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	if m.subscriptionRewardErr != nil {
		return m.subscriptionRewardErr
	}
	m.rewards = append(m.rewards, struct {
		email  string
		notice SubscriptionRewardNotice
	}{email: email, notice: notice})
	return nil
}

// SendAIBalanceNotice 记录 AI 余额变动邮件发送请求。
func (m *recordingMailer) SendAIBalanceNotice(email string, notice AIBalanceNotice) error {
	m.balanceNotices = append(m.balanceNotices, struct {
		email  string
		notice AIBalanceNotice
	}{email: email, notice: notice})
	return nil
}

// SendPositionStatus 记录岗位状态邮件发送请求，并可模拟发送失败。
func (m *recordingMailer) SendPositionStatus(email string, notice PositionStatusNotice) error {
	if m.positionStatusErr != nil {
		return m.positionStatusErr
	}
	m.positionNotices = append(m.positionNotices, struct {
		email  string
		notice PositionStatusNotice
	}{email: email, notice: notice})
	return nil
}

// SendCustomHTML 忽略自定义邮件发送请求。
func (m *recordingMailer) SendCustomHTML(email string, subject string, htmlBody string, plainText string) error {
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
	if !strings.Contains(sendResp.Body.String(), "该邮箱域名不在白名单内，请使用qq邮箱、163 等等常见邮箱域名") {
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

// TestChangePasswordVerifiesOldPassword 验证修改密码必须校验旧密码，且新密码立即生效。
func TestChangePasswordVerifiesOldPassword(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "change-password@example.com"
	token := loginForTest(t, routes, email)

	wrongReq := httptest.NewRequest(http.MethodPost, "/api/auth/password/change", bytes.NewBufferString(`{"old_password":"wrong-password-1","new_password":"brand-new-pass-1"}`))
	wrongReq.Header.Set("Authorization", "Bearer "+token)
	wrongResp := httptest.NewRecorder()
	routes.ServeHTTP(wrongResp, wrongReq)
	if wrongResp.Code != http.StatusUnauthorized {
		t.Fatalf("旧密码错误应返回 401, 实际 %d, body=%s", wrongResp.Code, wrongResp.Body.String())
	}

	changeReq := httptest.NewRequest(http.MethodPost, "/api/auth/password/change", bytes.NewBufferString(`{"old_password":"test-password-123","new_password":"brand-new-pass-1"}`))
	changeReq.Header.Set("Authorization", "Bearer "+token)
	changeResp := httptest.NewRecorder()
	routes.ServeHTTP(changeResp, changeReq)
	if changeResp.Code != http.StatusOK {
		t.Fatalf("修改密码 status = %d, body = %s", changeResp.Code, changeResp.Body.String())
	}

	oldLogin := httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"`+email+`","password":"test-password-123"}`))
	oldLoginResp := httptest.NewRecorder()
	routes.ServeHTTP(oldLoginResp, oldLogin)
	if oldLoginResp.Code == http.StatusOK {
		t.Fatalf("旧密码修改后不应再能登录")
	}
	newLogin := httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"`+email+`","password":"brand-new-pass-1"}`))
	newLoginResp := httptest.NewRecorder()
	routes.ServeHTTP(newLoginResp, newLogin)
	if newLoginResp.Code != http.StatusOK {
		t.Fatalf("新密码应能登录, status = %d, body = %s", newLoginResp.Code, newLoginResp.Body.String())
	}
}

// TestResetPasswordWithCode 验证忘记密码可以用邮箱验证码重设，且验证码一次性有效。
func TestResetPasswordWithCode(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "reset-password@example.com"
	loginForTest(t, routes, email)

	codeReq := httptest.NewRequest(http.MethodPost, "/api/auth/password/reset-code", bytes.NewBufferString(`{"email":"`+email+`"}`))
	codeResp := httptest.NewRecorder()
	routes.ServeHTTP(codeResp, codeReq)
	if codeResp.Code != http.StatusOK {
		t.Fatalf("发送重置验证码 status = %d, body = %s", codeResp.Code, codeResp.Body.String())
	}
	var codePayload struct {
		DebugCode string `json:"debug_code"`
	}
	if err := json.NewDecoder(codeResp.Body).Decode(&codePayload); err != nil {
		t.Fatal(err)
	}
	if codePayload.DebugCode == "" {
		t.Fatalf("开发模式应返回 debug_code, body = %s", codeResp.Body.String())
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/api/auth/password/reset", bytes.NewBufferString(`{"email":"`+email+`","code":"`+codePayload.DebugCode+`","password":"reset-new-pass-1"}`))
	resetResp := httptest.NewRecorder()
	routes.ServeHTTP(resetResp, resetReq)
	if resetResp.Code != http.StatusOK {
		t.Fatalf("重置密码 status = %d, body = %s", resetResp.Code, resetResp.Body.String())
	}

	reuseReq := httptest.NewRequest(http.MethodPost, "/api/auth/password/reset", bytes.NewBufferString(`{"email":"`+email+`","code":"`+codePayload.DebugCode+`","password":"another-pass-123"}`))
	reuseResp := httptest.NewRecorder()
	routes.ServeHTTP(reuseResp, reuseReq)
	if reuseResp.Code != http.StatusUnauthorized {
		t.Fatalf("验证码重复使用应返回 401, 实际 %d", reuseResp.Code)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/password-login", bytes.NewBufferString(`{"email":"`+email+`","password":"reset-new-pass-1"}`))
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("重置后的新密码应能登录, status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}
}

// TestSetupPasswordRejectsAlreadySet 验证已经设置过密码的账号不能再走补设接口，避免假成功。
func TestSetupPasswordRejectsAlreadySet(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "setup-twice@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/password/setup", bytes.NewBufferString(`{"password":"another-pass-123"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict {
		t.Fatalf("已设置密码的账号补设应返回 409, 实际 %d, body = %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "个人配置") {
		t.Fatalf("应提示去个人配置修改, body = %s", resp.Body.String())
	}
}
