// Package api 文件作用：验证导入接口每次检查云端会话、拒绝插件受限凭证并保持草稿账号权限边界。
package api

import (
	"goodhr5/local-agent-go-new/internal/config"
	resumeflow "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// TestDraftRemovalChecksWebOwner 验证查询版本和确认移除均逐次验证网页登录，不把别人的条目作为可操作结果。
func TestDraftRemovalChecksWebOwner(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer plugin" {
			_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"mine"},"session":{"kind":"browser_extension"}}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"mine"},"session":{"kind":"web"}}`))
		}
	}))
	defer backend.Close()
	s := &Server{cloud: cloud.New(backend.URL), resumeImports: &resumeflow.Manager{}}
	mux := http.NewServeMux()
	s.registerResumeImportRoutes(mux)
	for _, method := range []string{"GET", "POST"} {
		for token, want := range map[string]int{"": 401, "plugin": 403, "web": 404} {
			r := httptest.NewRequest(method, "/api/v1/resume-import/tasks/missing/items/missing/discard", strings.NewReader(`{"revision":"old"}`))
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != want {
				t.Errorf("%s %s: got %d want %d", method, token, w.Code, want)
			}
		}
	}
}

// TestPrivateDownloadPathRejectsPersonalFolder 只授权应用私有下载根，即使用户配置其他下载目录或私有子目录被换成符号链接。
func TestPrivateDownloadPathRejectsPersonalFolder(t *testing.T) {
	data := t.TempDir()
	personal := t.TempDir()
	s := &Server{cfg: config.Config{DataDir: data, DownloadsDir: personal}}
	s.rememberDownloadRoot(personal)
	managed := filepath.Join(data, "downloads")
	if err := os.Mkdir(managed, 0700); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(managed)
	if err != nil {
		t.Fatal(err)
	}
	if !s.privateDownloadPath(filepath.Join(real, "file.pdf")) || s.privateDownloadPath(filepath.Join(personal, "file.pdf")) {
		t.Fatal("个人下载与应用私有根范围错误")
	}
	if err = os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(personal, managed); err != nil {
		t.Skip("当前环境不允许测试符号链接", err)
	}
	if s.privateDownloadPath(filepath.Join(personal, "file.pdf")) {
		t.Fatal("不能把链接目标作为私有根")
	}
}
