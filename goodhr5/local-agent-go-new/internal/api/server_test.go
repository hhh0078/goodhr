// Package api 文件作用：验证控制台跨域方法和健康信息兼容字段。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/config"
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

// TestHealthContainsMachineIdentityPaths 验证健康接口保留控制台生成机器码需要的数据目录字段。
func TestHealthContainsMachineIdentityPaths(t *testing.T) {
	server := &Server{cfg: config.Config{
		Port: 55271, DataDir: "/tmp/goodhr-data", LogsDir: "/tmp/goodhr-logs",
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
