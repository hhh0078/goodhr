// 本文件通过真实路由验证 OAuth 回调安全、认证前预检、明确授权、PKCE 交换和刷新令牌轮换。
package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// workBuddyOAuthTestRequest 发送合成 JSON 或 OAuth 表单请求，不使用真实账号凭据。
func workBuddyOAuthTestRequest(t *testing.T, routes http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	contentType := "application/json"
	if form, ok := body.(url.Values); ok {
		data = []byte(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", contentType)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

// workBuddyOAuthTestJSON 解出成功响应，仅在失败时报告状态而不打印令牌正文。
func workBuddyOAuthTestJSON(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("OAuth status=%d want=200", response.Code)
	}
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// TestWorkBuddyOAuthRedirectRegistration 验证只允许 HTTPS 或明确的本机回环 HTTP 回调。
func TestWorkBuddyOAuthRedirectRegistration(t *testing.T) {
	routes := mustNewServer(t).Routes()
	for _, test := range []struct {
		uri   string
		valid bool
	}{
		{"https://third-party.example/callback", true},
		{"http://localhost:18765/callback", true},
		{"http://127.0.0.1:18765/callback", true},
		{"http://[::1]:18765/callback", true},
		{"javascript://example.com/callback", false},
		{"file://example.com/callback", false},
		{"workbuddy://callback", false},
		{"http://example.com/callback", false},
		{"http://localhost.example.com/callback", false},
		{"https:///callback", false},
		{"https://user:password@example.com/callback", false},
		{"https://example.com/callback#fragment", false},
		{"https://example.com/callback#", false},
		{"//example.com/callback", false},
		{"data:text/plain,callback", false},
	} {
		t.Run(test.uri, func(t *testing.T) {
			response := workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/register", "", map[string]any{"client_name": "客户端自报名称", "redirect_uris": []string{test.uri}})
			want := http.StatusBadRequest
			if test.valid {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("registration status=%d want=%d", response.Code, want)
			}
		})
	}
}

// TestWorkBuddyOAuthLifecycle 验证预检不需要登录，明确同意后 PKCE、MCP 和刷新轮换保持可用。
func TestWorkBuddyOAuthLifecycle(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	callback := "http://localhost:18765/callback?original=keep"
	registered := workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/register", "", map[string]any{"client_name": "第三方测试招聘助手", "redirect_uris": []string{callback}}))
	clientID := registered["client_id"].(string)
	verifier := strings.Repeat("v", 64)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	query := url.Values{"client_id": {clientID}, "redirect_uri": {callback}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"mcp"}, "state": {"original-state"}}
	preview := workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodGet, "/api/workbuddy/authorize?"+query.Encode(), "", nil))
	if preview["client_name"] != "第三方测试招聘助手" || preview["callback_origin"] != "http://localhost:18765" || preview["redirect_uri"] != callback || preview["access_token"] != nil || preview["code"] != nil {
		t.Fatal("preview omitted real client metadata or exposed credentials")
	}
	for _, change := range []struct{ field, value string }{
		{"client_id", "not-registered"}, {"redirect_uri", callback + "&changed=true"}, {"redirect_uri", " " + callback},
		{"code_challenge_method", ""}, {"code_challenge_method", "plain"}, {"code_challenge_method", "S512"},
		{"code_challenge", "short"}, {"code_challenge", strings.Repeat("B", 43)},
	} {
		invalid, err := url.ParseQuery(query.Encode())
		if err != nil {
			t.Fatal(err)
		}
		invalid.Set(change.field, change.value)
		response := workBuddyOAuthTestRequest(t, routes, http.MethodGet, "/api/workbuddy/authorize?"+invalid.Encode(), "", nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid preview %s status=%d", change.field, response.Code)
		}
	}
	consent := map[string]any{"client_id": clientID, "redirect_uri": callback, "code_challenge": challenge, "code_challenge_method": "S256", "scope": "mcp", "state": "original-state", "decision": "approve"}
	if response := workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/api/workbuddy/authorize", "", consent); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous consent status=%d", response.Code)
	}
	webToken := loginForTest(t, routes, "oauth-lifecycle@example.com")
	for _, decision := range []string{"", "anything", "APPROVE"} {
		consent["decision"] = decision
		if response := workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/api/workbuddy/authorize", webToken, consent); response.Code != http.StatusBadRequest {
			t.Fatalf("implicit consent status=%d", response.Code)
		}
	}
	consent["decision"] = "deny"
	denied := workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/api/workbuddy/authorize", webToken, consent))
	deniedURL, err := url.Parse(denied["redirect_url"].(string))
	if err != nil || deniedURL.Query().Get("error") != "access_denied" || deniedURL.Query().Get("code") != "" || deniedURL.Query().Get("state") != "original-state" {
		t.Fatal("denial issued a code or lost state")
	}
	consent["decision"] = "approve"
	approved := workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/api/workbuddy/authorize", webToken, consent))
	approvedURL, err := url.Parse(approved["redirect_url"].(string))
	if err != nil || approvedURL.Query().Get("code") == "" || approvedURL.Query().Get("original") != "keep" || approvedURL.Query().Get("state") != "original-state" {
		t.Fatal("approval callback is incomplete")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "redirect_uri": {callback}, "code": {approvedURL.Query().Get("code")}, "code_verifier": {verifier}}
	tokens := workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/token", "", form))
	access, refresh := tokens["access_token"].(string), tokens["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatal("token pair missing")
	}
	if response := workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/token", "", form); response.Code != http.StatusBadRequest {
		t.Fatal("authorization code was reusable")
	}
	workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/mcp", access, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}))
	if response := workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/api/workbuddy/authorize", access, consent); response.Code != http.StatusForbidden {
		t.Fatal("connector session approved a new grant")
	}
	refreshForm := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}}
	rotated := workBuddyOAuthTestJSON(t, workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/token", "", refreshForm))
	if rotated["access_token"] == access || rotated["refresh_token"] == refresh {
		t.Fatal("refresh did not rotate credentials")
	}
	if response := workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/token", "", refreshForm); response.Code != http.StatusUnauthorized {
		t.Fatal("refresh token was reusable")
	}
}

// TestWorkBuddyOAuthUnsafeHistoricalClient 验证旧库中的危险回调不能通过预检、授权、授权码或刷新令牌绕过。
func TestWorkBuddyOAuthUnsafeHistoricalClient(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	webToken := loginForTest(t, routes, "oauth-history@example.com")
	verifier := strings.Repeat("v", 64)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	for _, callback := range []string{"javascript://example.com/callback", "file://example.com/callback", "https://user@example.com/callback", "http://example.com/callback"} {
		clientID := "historical-" + callback
		if err := server.workBuddyOAuth.store.SaveClient(WorkBuddyOAuthClient{ClientID: clientID, RedirectURI: []string{callback}, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		code, refresh := "synthetic-code-"+callback, "synthetic-refresh-"+callback
		if err := server.workBuddyOAuth.store.SaveCode(WorkBuddyOAuthCode{Code: code, ClientID: clientID, RedirectURI: callback, CodeChallenge: challenge, UserEmail: "oauth-history@example.com", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		if err := server.workBuddyOAuth.store.SaveRefreshToken(WorkBuddyRefreshToken{Token: refresh, ClientID: clientID, UserEmail: "oauth-history@example.com", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		query := url.Values{"client_id": {clientID}, "redirect_uri": {callback}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
		for _, response := range []*httptest.ResponseRecorder{
			workBuddyOAuthTestRequest(t, routes, http.MethodGet, "/api/workbuddy/authorize?"+query.Encode(), "", nil),
			workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/api/workbuddy/authorize", webToken, map[string]any{"client_id": clientID, "redirect_uri": callback, "code_challenge": challenge, "code_challenge_method": "S256", "decision": "approve"}),
			workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/token", "", url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "redirect_uri": {callback}, "code": {code}, "code_verifier": {verifier}}),
			workBuddyOAuthTestRequest(t, routes, http.MethodPost, "/oauth/token", "", url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}}),
		} {
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "access_token") || strings.Contains(response.Body.String(), "redirect_url") {
				t.Fatalf("unsafe historical client accepted status=%d", response.Code)
			}
		}
	}
}

// TestWorkBuddyPKCEVerifierFormat 验证摘要吻合也不能接受带空格或非法字符的 verifier。
func TestWorkBuddyPKCEVerifierFormat(t *testing.T) {
	for _, verifier := range []string{strings.Repeat("v", 64), strings.Repeat("v", 42), strings.Repeat("v", 129), strings.Repeat("v", 63) + " ", strings.Repeat("v", 63) + "/"} {
		digest := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(digest[:])
		if actual := workBuddyVerifyPKCE(challenge, verifier); actual != (verifier == strings.Repeat("v", 64)) {
			t.Fatal("verifier format validation failed")
		}
	}
}
