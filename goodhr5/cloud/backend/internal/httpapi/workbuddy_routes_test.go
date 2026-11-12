// 本文件验证WorkBuddy公开入口与现有/api反代入口等价，并保留连接器会话的访问边界。
package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestWorkBuddyProxyRoutes 验证发现元数据、MCP鉴权和连接器会话在两个入口保持一致。
func TestWorkBuddyProxyRoutes(t *testing.T) {
	s, _ := newWechatOfficialTestServer(t, "safe")
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/api/workbuddy/oauth/protected-resource", "/.well-known/oauth-authorization-server", "/api/workbuddy/oauth/authorization-server"} {
		response := wechatTestRequest(s.Routes(), http.MethodGet, path, "", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"mcp"`) {
			t.Fatalf("发现入口%s失败：%d %s", path, response.Code, response.Body.String())
		}
	}
	token := "synthetic-workbuddy-session"
	if err := s.auth.store.SaveSession(token, Session{Kind: workbuddySessionKind, Email: "connector@example.invalid"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/mcp", "/api/workbuddy/mcp"} {
		response := wechatTestRequest(s.Routes(), http.MethodPost, path, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Header().Get("WWW-Authenticate"), "oauth-protected-resource") {
			t.Fatal(path, response.Code, response.Body.String())
		}
		response = wechatTestRequest(s.Routes(), http.MethodPost, path, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "goodhr-connector") {
			t.Fatal(path, response.Code, response.Body.String())
		}
	}
	response := wechatTestRequest(s.Routes(), http.MethodGet, "/api/auth/me", token, "")
	if response.Code != http.StatusForbidden {
		t.Fatal("连接器会话越权", response.Code)
	}
}
