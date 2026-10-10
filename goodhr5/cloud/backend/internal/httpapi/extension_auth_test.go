// 本文件验证插件会话最小权限、退出失效和公开下载配置的安全边界。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestExtensionSessionGuardBlocksWebSocketQueryToken 验证查询参数和混合凭证均不能绕过插件权限限制。
func TestExtensionSessionGuardBlocksWebSocketQueryToken(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("plugin-token", Session{Email: "test@example.com", Kind: extensionSessionKind}, time.Hour)
	_ = store.SaveSession("web-token", Session{Email: "test@example.com"}, time.Hour)
	server := &Server{auth: &AuthService{store: store}}
	handler := server.extensionSessionGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, item := range []struct {
		path, header string
		want         int
	}{
		{"/api/agents/ws?token=plugin-token", "", http.StatusForbidden},
		{"/api/agents/ws?token=plugin-token", "Bearer web-token", http.StatusForbidden},
		{"/api/agents/ws?token=web-token", "Bearer plugin-token", http.StatusForbidden},
		{"/api/agents/ws?token=web-token", "", http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodGet, item.path, nil)
		r.Header.Set("Authorization", item.header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != item.want {
			t.Errorf("path=%s header=%s: %d, want %d", item.path, item.header, w.Code, item.want)
		}
	}
}

// TestExtensionSessionCannotReplaceAgentConnection 验证直接调用 WebSocket handler 也会拒绝插件且保留原连接。
func TestExtensionSessionCannotReplaceAgentConnection(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("plugin-token", Session{Email: "test@example.com", Kind: extensionSessionKind}, time.Hour)
	_ = store.SaveSession("web-token", Session{Email: "test@example.com"}, time.Hour)
	hub := NewAgentWSHub(&AuthService{store: store})
	original := NewAgentWSClient("test@example.com", nil, hub)
	hub.clients["test@example.com"] = original
	for _, item := range []struct{ path, header string }{
		{"/api/agents/ws?token=plugin-token", ""},
		{"/api/agents/ws?token=plugin-token", "Bearer web-token"},
		{"/api/agents/ws", "Bearer plugin-token"},
	} {
		r := httptest.NewRequest(http.MethodGet, item.path, nil)
		r.Header.Set("Authorization", item.header)
		w := httptest.NewRecorder()
		hub.ServeWS(w, r)
		if w.Code != http.StatusForbidden || hub.clients["test@example.com"] != original || original.isClosed() {
			t.Fatalf("plugin replaced agent or reached upgrade: status=%d", w.Code)
		}
	}
}

// TestExtensionRefreshAndLogoutConsumeOldSession 验证续期旧凭证只能使用一次，退出后不能复活且网页凭证不会被消费。
func TestExtensionRefreshAndLogoutConsumeOldSession(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("old-token", Session{Email: "test@example.com", Kind: extensionSessionKind}, time.Hour)
	_ = store.SaveSession("web-token", Session{Email: "test@example.com"}, time.Hour)
	auth := &AuthService{store: store}
	w := requestExtensionSessionOperation(auth.ExtensionRefresh, "old-token")
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: status=%d body=%s", w.Code, w.Body.String())
	}
	var response extensionLoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.AccessToken == "" || response.AccessToken == "old-token" {
		t.Fatalf("replacement token missing: %+v err=%v", response, err)
	}
	if _, err := store.GetSession("old-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token still usable: %v", err)
	}
	for _, operation := range []http.HandlerFunc{auth.ExtensionRefresh, auth.ExtensionLogout} {
		for _, token := range []string{"old-token", "web-token", ""} {
			if w := requestExtensionSessionOperation(operation, token); w.Code != http.StatusUnauthorized {
				t.Fatalf("replayed or wrong-kind token was accepted: status=%d", w.Code)
			}
		}
	}
	if _, err := store.GetSession("web-token"); err != nil {
		t.Fatalf("web session was consumed: %v", err)
	}
	if w := requestExtensionSessionOperation(auth.ExtensionLogout, response.AccessToken); w.Code != http.StatusOK {
		t.Fatalf("logout: status=%d", w.Code)
	}
	if w := requestExtensionSessionOperation(auth.ExtensionRefresh, response.AccessToken); w.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token was renewed: status=%d", w.Code)
	}
}

// failingSessionReplacementStore 模拟 Redis 暂时不可用，验证不会提前返回成功或签发凭证。
type failingSessionReplacementStore struct{ AuthStore }

// ReplaceSession 返回存储故障，不触碰原会话。
func (s failingSessionReplacementStore) ReplaceSession(_, _ string, _ time.Duration, _ string) (Session, error) {
	return Session{}, errors.New("test store unavailable")
}

// TestExtensionSessionStorageFailureDoesNotReportSuccess 验证续期和退出都必须等原子存储操作成功。
func TestExtensionSessionStorageFailureDoesNotReportSuccess(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("old-token", Session{Email: "test@example.com", Kind: extensionSessionKind}, time.Hour)
	auth := &AuthService{store: failingSessionReplacementStore{store}}
	for _, operation := range []http.HandlerFunc{auth.ExtensionRefresh, auth.ExtensionLogout} {
		w := requestExtensionSessionOperation(operation, "old-token")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("storage failure reported success: status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if _, err := store.GetSession("old-token"); err != nil || len(store.sessions) != 1 {
		t.Fatalf("failed operation consumed or issued a session: err=%v count=%d", err, len(store.sessions))
	}
}

// requestExtensionSessionOperation 发起一个携带插件凭证的续期或退出请求，返回完整响应。
func requestExtensionSessionOperation(operation http.HandlerFunc, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/extension-refresh", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	operation(w, r)
	return w
}

// TestExtensionSessionGuardBlocksPrivilegedRoutes 验证真实插件会话不能调用管理和招聘任务操作。
func TestExtensionSessionGuardBlocksPrivilegedRoutes(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("plugin-token", Session{Email: "test@example.com", Kind: extensionSessionKind}, time.Hour)
	server := &Server{auth: &AuthService{store: store}}
	handler := server.extensionSessionGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, item := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/positions/p/start", 403}, {http.MethodGet, "/api/admin/users", 403},
		{http.MethodGet, "/api/config/effective", 403}, {http.MethodDelete, "/api/candidates", 403},
		{http.MethodPost, "/api/auth/extension-session", 403}, {http.MethodPost, "/api/candidates", 204},
		{http.MethodPost, "/api/candidates/c/notes", 204}, {http.MethodGet, "/api/positions", 204},
	} {
		r := httptest.NewRequest(item.method, item.path, nil)
		r.Header.Set("Authorization", "Bearer plugin-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != item.want {
			t.Errorf("%s %s: %d, want %d", item.method, item.path, w.Code, item.want)
		}
	}
	_ = store.SaveSession("plugin-token", Session{}, -time.Second)
	if _, err := store.GetSession("plugin-token"); err == nil {
		t.Fatal("revoked session is still active")
	}
}

// TestExtensionDownloadURLRejectsActiveContent 验证后台下载地址不允许脚本、伪协议与外部凭证。
func TestExtensionDownloadURLRejectsActiveContent(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "//evil.example/x", "/\\evil.example/x", "https://name:password@example.com/x", "file:///tmp/a"} {
		if safeExtensionDownloadURL(value) != "" {
			t.Errorf("unsafe URL accepted: %q", value)
		}
	}
	if safeExtensionDownloadURL("/downloads/plugin.zip") == "" || safeExtensionDownloadURL("https://example.com/plugin.zip") == "" {
		t.Fatal("safe downloads rejected")
	}
}
