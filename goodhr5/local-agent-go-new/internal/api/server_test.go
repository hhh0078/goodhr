// Package api 文件作用：验证控制台跨域方法和健康信息兼容字段。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestMiddlewareAllowsDeletePreflight 验证岗位日志清空请求可以通过跨域预检。
func TestMiddlewareAllowsDeletePreflight(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/local/positions/1/logs", nil)
	request.Header.Set("Origin", "https://goodhr5.58it.cn")
	response := httptest.NewRecorder()
	server.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d", response.Code)
	}
	if methods := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, "DELETE") {
		t.Fatalf("Access-Control-Allow-Methods = %q", methods)
	}
}

// TestPositionLogsIncludeLatestTaskStatus 验证三秒日志轮询响应同时返回岗位最新任务状态。
func TestPositionLogsIncludeLatestTaskStatus(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SaveTask(context.Background(), storage.TaskRun{
		TaskID: "task-1", PositionID: "position-1", PlatformID: "zhaopin",
		TaskType: "greeting", Status: "failed", ErrorMessage: "连续三次操作失败",
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/local/positions/position-1/logs", nil)
	response := httptest.NewRecorder()
	server.handleLocalPositionLogs(response, request, "position-1")
	if response.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data struct {
			Task *storage.TaskRun `json:"task"`
		} `json:"data"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Task == nil || payload.Data.Task.Status != "failed" || payload.Data.Task.ErrorMessage != "连续三次操作失败" {
		t.Fatalf("unexpected task payload: %+v", payload.Data.Task)
	}
	clearRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/local/positions/position-1/logs", nil)
	clearResponse := httptest.NewRecorder()
	server.handleLocalPositionLogs(clearResponse, clearRequest, "position-1")
	if clearResponse.Code != http.StatusOK {
		t.Fatalf("clear logs status = %d, body = %s", clearResponse.Code, clearResponse.Body.String())
	}
	payload.Data.Task = nil
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/local/positions/position-1/logs", nil)
	server.handleLocalPositionLogs(response, request, "position-1")
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Task == nil || payload.Data.Task.Status != "failed" || payload.Data.Task.ErrorMessage != "" || payload.Data.Task.ErrorCode != "" {
		t.Fatalf("清空日志后仍返回旧任务错误：%+v", payload.Data.Task)
	}
}

// TestAllowedOriginRejectsLookalikeHosts 验证本机和 GoodHR 域名使用精确 URL 判断。
func TestAllowedOriginRejectsLookalikeHosts(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:3000",
		"http://127.0.0.1:55271",
		"https://goodhr5.58it.cn",
	} {
		if !allowedOrigin(origin) {
			t.Fatalf("合法 Origin 被拒绝：%s", origin)
		}
	}
	for _, origin := range []string{
		"http://localhost.evil.com",
		"http://127.0.0.1.evil.com",
		"https://goodhr5.58it.cn.evil.com",
		"https://goodhr5.58it.cn:443",
		"http://goodhr5.58it.cn",
	} {
		if allowedOrigin(origin) {
			t.Fatalf("伪造 Origin 被放行：%s", origin)
		}
	}
}

// TestHealthContainsMachineIdentityPaths 验证健康接口保留控制台生成机器码需要的数据目录字段。
func TestHealthContainsMachineIdentityPaths(t *testing.T) {
	server := &Server{cfg: config.Config{
		Port: 43129, DataDir: "/tmp/goodhr-data", LogsDir: "/tmp/goodhr-logs",
		ProfilesDir: "/tmp/goodhr-profiles", DownloadsDir: "/tmp/goodhr-downloads",
		ScreenshotsDir: "/tmp/goodhr-screenshots", DatabasePath: "/tmp/goodhr.db",
	}}
	response := httptest.NewRecorder()
	server.handleHealth(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	var payload struct {
		Data struct {
			DataDir      string `json:"dataDir"`
			DataDirAlias string `json:"data_dir"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析健康响应失败：%v", err)
	}
	if payload.Data.DataDir == "" || payload.Data.DataDirAlias != payload.Data.DataDir {
		t.Fatalf("健康响应缺少兼容数据目录：%s", response.Body.String())
	}
}

// TestBrowserStartIsNotPublicRoute 验证对外只保留统一的页面打开接口。
func TestBrowserStartIsNotPublicRoute(t *testing.T) {
	server := NewServer(config.Config{Host: "127.0.0.1", Port: 43129}, Dependencies{})
	response := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/api/v1/browser/start", strings.NewReader("{}")),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("POST /api/v1/browser/start status = %d", response.Code)
	}
}

// TestRequestedNewTabSupportsLegacyField 验证新增标签页优先使用 new_tab 并兼容 new_page。
func TestRequestedNewTabSupportsLegacyField(t *testing.T) {
	legacyTrue := true
	if result := requestedNewTab(pageOpenRequest{NewPage: &legacyTrue}); result == nil || !*result {
		t.Fatal("new_page=true 应该继续创建新标签页")
	}
	newFalse := false
	if result := requestedNewTab(pageOpenRequest{NewTab: &newFalse, NewPage: &legacyTrue}); result == nil || *result {
		t.Fatal("new_tab 应该优先于旧版 new_page")
	}
}

// TestSafeDownloadFilePath 验证文件打开接口不能越过配置的下载目录。
func TestSafeDownloadFilePath(t *testing.T) {
	downloadsDir := filepath.Join(t.TempDir(), "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(downloadsDir, "resume.pdf")
	if err := os.WriteFile(inside, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{DownloadsDir: downloadsDir}}
	if resolved, err := server.safeDownloadFilePath(inside); err != nil || resolved != inside {
		t.Fatalf("下载目录内文件应该允许打开：path=%s err=%v", resolved, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outside, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := server.safeDownloadFilePath(outside); err == nil {
		t.Fatal("下载目录外文件不应允许打开")
	}
	customDir := filepath.Join(t.TempDir(), "custom-downloads")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customFile := filepath.Join(customDir, "candidate.docx")
	if err := os.WriteFile(customFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	server.rememberDownloadRoot(customDir)
	if resolved, err := server.safeDownloadFilePath(customFile); err != nil || resolved != customFile {
		t.Fatalf("切换后的下载目录内文件应该允许打开：path=%s err=%v", resolved, err)
	}
	linkPath := filepath.Join(downloadsDir, "outside-link.pdf")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := server.safeDownloadFilePath(linkPath); err == nil {
		t.Fatal("下载目录内指向外部文件的软链接不应允许打开")
	}
}
