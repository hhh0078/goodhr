// 本文件负责测试帮助中心系统指南和权限校验。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHelpGuideRequiresLogin 验证帮助中心指南必须登录后才能读取。
func TestHelpGuideRequiresLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/help/guide", nil)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("guide status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

// TestHelpGuideReturnsSystemGuide 验证登录用户可以读取系统指南。
func TestHelpGuideReturnsSystemGuide(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "help@example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/help/guide", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("guide status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Guide struct {
			Title string `json:"title"`
			Cards []any  `json:"cards"`
		} `json:"guide"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Guide.Title == "" || len(payload.Guide.Cards) == 0 {
		t.Fatalf("unexpected guide payload: %+v", payload.Guide)
	}
}
