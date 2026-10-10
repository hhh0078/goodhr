// Package api 文件作用：验证导入接口每次检查云端会话、拒绝插件受限凭证并保持草稿账号权限边界。
package api

import (
	resumeflow "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResumeImportRequiresWebSession 验证401、插件403、网页草稿404以及正确列表响应合同。
func TestResumeImportRequiresWebSession(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/auth/me" {
			t.Errorf("意外请求：%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer plugin-token" {
			_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"owner-a"},"session":{"kind":"browser_extension"}}`))
			return
		}
		if r.Header.Get("Authorization") == "Bearer web-token" {
			_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"owner-a"},"session":{"kind":"web"}}`))
			return
		}
		http.Error(w, `{"error":"登录已过期"}`, 401)
	}))
	defer backend.Close()
	server := &Server{cloud: cloud.New(backend.URL), resumeImports: &resumeflow.Manager{}}
	mux := http.NewServeMux()
	server.registerResumeImportRoutes(mux)
	for _, test := range []struct {
		path, token string
		status      int
	}{{"/api/v1/resume-import/tasks", "", 401}, {"/api/v1/resume-import/tasks", "plugin-token", 403}, {"/api/v1/resume-import/tasks", "bad-token", 401}, {"/api/v1/resume-import/tasks/missing", "web-token", 404}, {"/api/v1/resume-import/tasks", "web-token", 200}} {
		request := httptest.NewRequest("GET", test.path, nil)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Errorf("%s %s: HTTP=%d body=%s", test.path, test.token, response.Code, response.Body.String())
		}
	}
	if calls != 4 {
		t.Fatalf("请求未逐次验证云端：%d", calls)
	}
}
