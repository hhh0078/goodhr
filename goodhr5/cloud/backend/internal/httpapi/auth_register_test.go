// 本文件验证仅注册模式的邮箱验证、重复账号拒绝、账号初始化及并发创建保护。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestRegisterOnlyNewAccount 验证新邮箱注册仍完成会话、协议、试用和邀请初始化。
func TestRegisterOnlyNewAccount(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	loginForTest(t, routes, "registration-inviter@example.com")
	inviteID, err := server.auth.invitations.InviteID("registration-inviter@example.com")
	if err != nil {
		t.Fatal(err)
	}
	email := "registration-new@example.com"
	code := sendLoginCodeForTest(t, routes, email)
	body, _ := json.Marshal(loginRequest{Email: "Registration-New@Example.com", Code: code, Password: "new-password", AgreementAccepted: true, RegisterOnly: true, InviterID: inviteID})
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(string(body))))
	var result struct {
		AccessToken           string `json:"access_token"`
		RequiresPasswordSetup bool   `json:"requires_password_setup"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.AccessToken == "" || result.RequiresPasswordSetup {
		t.Fatalf("registration status=%d", response.Code)
	}
	if currentUserEmailForTest(t, routes, result.AccessToken) != email {
		t.Fatal("registered session does not belong to the normalized email")
	}
	hash, err := server.auth.passwords.PasswordHash(email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte("new-password")) != nil {
		t.Fatal("registered password was not saved")
	}
	accepted, err := server.auth.userActivity.HasAcceptedAgreement(email)
	if err != nil || !accepted {
		t.Fatal("registration did not preserve agreement acceptance")
	}
	subscription, err := server.auth.subscriptions.UserSubscription(email)
	if err != nil || subscription.ExpiresAt.After(time.Now()) {
		t.Fatal("registration with zero gift days must not create active trial")
	}
	inviter, err := server.auth.invitations.InviterEmailByInvitee(email)
	if err != nil || inviter != "registration-inviter@example.com" {
		t.Fatal("registration did not preserve invitation initialization")
	}
}

// TestRegisterOnlyRejectsExisting 验证有密码和无密码的已有账号均拒绝注册，且原验证码登录仍有效。
func TestRegisterOnlyRejectsExisting(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			server := mustNewServer(t)
			routes := server.Routes()
			passwords := server.auth.passwords.(*MemoryPasswordStore)
			email := "registration-existing@example.com"
			if legacy {
				passwords.AddLegacyAccount(email)
			} else {
				loginForTest(t, routes, email)
			}
			before, _ := passwords.PasswordHash(email)
			code := sendLoginCodeForTest(t, routes, email)
			body := fmt.Sprintf(`{"email":%q,"code":%q,"password":"replacement-password","agreement_accepted":true,"register_only":true}`, email, code)
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body)))
			if response.Code != http.StatusConflict || strings.Contains(response.Body.String(), "access_token") || !strings.Contains(response.Body.String(), "已有账号") {
				t.Fatalf("existing registration status=%d body=%s", response.Code, response.Body.String())
			}
			after, _ := passwords.PasswordHash(email)
			if before != after {
				t.Fatal("duplicate registration modified existing password")
			}
			// 注册冲突不能消耗验证码或悄悄登录；显式选择原登录入口后才签发会话。
			body = fmt.Sprintf(`{"email":%q,"code":%q,"agreement_accepted":true}`, email, code)
			login := httptest.NewRecorder()
			routes.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body)))
			if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "access_token") {
				t.Fatalf("legacy login regression status=%d", login.Code)
			}
		})
	}
}

// TestRegisterOnlyRequiresVerification 验证仅注册不能绕过邮箱验证码、密码和协议要求。
func TestRegisterOnlyRequiresVerification(t *testing.T) {
	for _, test := range []struct {
		name, code, password string
		agreement            bool
		status               int
	}{
		{name: "wrong-code", code: "2222", password: "new-password", agreement: true, status: http.StatusUnauthorized},
		{name: "missing-code", password: "new-password", agreement: true, status: http.StatusBadRequest},
		{name: "missing-password", code: "1111", agreement: true, status: http.StatusBadRequest},
		{name: "missing-agreement", code: "1111", password: "new-password", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := mustNewServer(t)
			server.auth.universalLoginCodeOffsetMin = 0
			email := "registration-invalid@example.com"
			if err := server.auth.store.SaveLoginCode(email, "1111", codeTTL); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(loginRequest{Email: email, Code: test.code, Password: test.password, AgreementAccepted: test.agreement, RegisterOnly: true})
			response := httptest.NewRecorder()
			server.auth.Login(response, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(string(body))))
			if response.Code != test.status || strings.Contains(response.Body.String(), "access_token") {
				t.Fatalf("registration status=%d want=%d", response.Code, test.status)
			}
			exists, _, err := server.auth.passwords.AccountStatus(email)
			if err != nil || exists {
				t.Fatal("rejected registration created an account")
			}
		})
	}
}

// insertedLegacyPasswordStore 模拟读取账号快照后，被另一个请求创建了无密码老账号。
type insertedLegacyPasswordStore struct{ *MemoryPasswordStore }

// AccountStatus 返回原快照并模拟并发插入，检验真正创建时仍须防止覆盖。
func (s *insertedLegacyPasswordStore) AccountStatus(email string) (bool, bool, error) {
	exists, passwordSet, err := s.MemoryPasswordStore.AccountStatus(email)
	if !exists && err == nil {
		s.AddLegacyAccount(email)
	}
	return exists, passwordSet, err
}

// TestRegisterOnlyConcurrentLegacyCreation 验证注册与老账号创建竞争时不补设密码、不签发会话。
func TestRegisterOnlyConcurrentLegacyCreation(t *testing.T) {
	server := mustNewServer(t)
	email := "registration-raced@example.com"
	passwords := NewMemoryPasswordStore()
	server.auth.passwords = &insertedLegacyPasswordStore{passwords}
	if err := server.auth.store.SaveLoginCode(email, "1111", codeTTL); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"email":%q,"code":"1111","password":"unwanted-password","agreement_accepted":true,"register_only":true}`, email)
	response := httptest.NewRecorder()
	server.auth.Login(response, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body)))
	if response.Code != http.StatusConflict || strings.Contains(response.Body.String(), "access_token") {
		t.Fatalf("concurrent registration status=%d", response.Code)
	}
	exists, passwordSet, err := passwords.AccountStatus(email)
	if err != nil || !exists || passwordSet {
		t.Fatal("registration changed the concurrently created legacy account")
	}
}

// TestCreateAccountConcurrent 验证并发注册同一邮箱只有一个成功，后到请求不能覆盖赢家密码。
func TestCreateAccountConcurrent(t *testing.T) {
	passwords := NewMemoryPasswordStore()
	var group sync.WaitGroup
	results := make(chan string, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			hash := fmt.Sprintf("synthetic-hash-%d", i)
			err := passwords.CreateAccount("registration-concurrent@example.com", hash)
			if err == nil {
				results <- hash
			} else if !errors.Is(err, ErrAccountAlreadyExists) {
				t.Errorf("create account err=%v", err)
			}
		}(i)
	}
	group.Wait()
	close(results)
	if len(results) != 1 {
		t.Fatalf("successful registrations=%d want=1", len(results))
	}
	winner := <-results
	actual, err := passwords.PasswordHash("registration-concurrent@example.com")
	if err != nil || actual != winner {
		t.Fatal("a losing registration replaced the winning password")
	}
}
