// Package httpapi 文件作用：验证网页退出撤销自身凭证，离线本地程序重连被拒绝，并保护其他会话与存储失败重试。
package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// logoutRequest 通过实际路由提交指定凭证，不把凭证放入请求正文或地址。
func logoutRequest(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestWebLogoutRejectsOfflineAgentReconnect 验证退出后原凭证不能查询身份或重建本地命令通道，其他登录及插件保持有效。
func TestWebLogoutRejectsOfflineAgentReconnect(t *testing.T) {
	server := mustNewServer(t)
	store := NewMemoryAuthStore()
	server.auth.store = store
	for token, kind := range map[string]string{"web-current": "", "web-other": "", "plugin-other": extensionSessionKind} {
		if err := store.SaveSession(token, Session{Email: "logout@example.com", Kind: kind}, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	routes := server.Routes()
	if w := logoutRequest(routes, http.MethodPost, "/api/auth/logout", "web-current"); w.Code != http.StatusOK {
		t.Fatalf("网页退出未成功：%d", w.Code)
	}
	for _, path := range []string{"/api/auth/me", "/api/agents/ws", "/api/auth/logout"} {
		method := http.MethodGet
		if path == "/api/auth/logout" {
			method = http.MethodPost
		}
		if w := logoutRequest(routes, method, path, "web-current"); w.Code != http.StatusUnauthorized {
			t.Errorf("已退出凭证仍可访问 %s：%d", path, w.Code)
		}
	}
	for _, token := range []string{"web-other", "plugin-other"} {
		if _, err := store.GetSession(token); err != nil {
			t.Fatal("退出误伤其他登录或插件", err)
		}
	}
}

// TestWebLogoutRequiresOwnWebSession 验证请求方法、身份及会话类型均受限制，失败不删除其他会话。
func TestWebLogoutRequiresOwnWebSession(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("web-current", Session{Email: "logout@example.com"}, time.Hour)
	_ = store.SaveSession("plugin-current", Session{Email: "logout@example.com", Kind: extensionSessionKind}, time.Hour)
	auth := &AuthService{store: store}
	for _, item := range []struct {
		method, token string
		status        int
	}{
		{http.MethodGet, "web-current", http.StatusMethodNotAllowed},
		{http.MethodPost, "", http.StatusUnauthorized},
		{http.MethodPost, "unknown", http.StatusUnauthorized},
		{http.MethodPost, "plugin-current", http.StatusUnauthorized},
	} {
		if w := logoutRequest(http.HandlerFunc(auth.Logout), item.method, "/api/auth/logout", item.token); w.Code != item.status {
			t.Errorf("退出边界返回 %d，预期 %d", w.Code, item.status)
		}
	}
	for _, token := range []string{"web-current", "plugin-current"} {
		if _, err := store.GetSession(token); err != nil {
			t.Fatal("失败请求仍消费了有效会话", err)
		}
	}
}

// TestWebLogoutStorageFailureKeepsSession 验证暂时存储故障不会被报告成退出成功，重试后才撤销凭证。
func TestWebLogoutStorageFailureKeepsSession(t *testing.T) {
	store := NewMemoryAuthStore()
	_ = store.SaveSession("web-current", Session{Email: "logout@example.com"}, time.Hour)
	auth := &AuthService{store: failingSessionReplacementStore{store}}
	if w := logoutRequest(http.HandlerFunc(auth.Logout), http.MethodPost, "/api/auth/logout", "web-current"); w.Code != http.StatusInternalServerError {
		t.Fatalf("存储失败仍报告退出成功：%d", w.Code)
	}
	if _, err := store.GetSession("web-current"); err != nil {
		t.Fatal("失败时丢失了重试凭证", err)
	}
	auth.store = store
	if w := logoutRequest(http.HandlerFunc(auth.Logout), http.MethodPost, "/api/auth/logout", "web-current"); w.Code != http.StatusOK {
		t.Fatalf("重试退出失败：%d", w.Code)
	}
	if _, err := store.GetSession("web-current"); !errors.Is(err, ErrNotFound) {
		t.Fatal("重试后旧凭证仍然有效", err)
	}
}
