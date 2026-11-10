// Package api 本文件验证所有可争用浏览器的公开入口与旧岗位入口都遵守同一设备互斥边界。
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCloudRunBlocksAllBrowserMutationRoutes 验证云端执行期间旧岗位、开窗、关窗和组件更新不能绕过互斥保护。
func TestCloudRunBlocksAllBrowserMutationRoutes(t *testing.T) {
	s := newExtensionPairTestServer(t, http.NotFoundHandler())
	s.cloudReplyActive = true
	for _, path := range []string{
		"/api/v1/tasks/start", "/api/v1/local/positions/position/run",
		"/api/v1/local/auto-reply/start", "/api/v1/page/open",
		"/api/v1/browser/stop", "/api/v1/worker/stop", "/api/v1/runtime/install",
	} {
		result := extensionPairTestRequest(s, path, map[string]any{})
		if result.Code != 409 || !strings.Contains(result.Body.String(), "CLOUD_DEVICE_BUSY") {
			t.Fatalf("%s 绕过云任务保护：%d %s", path, result.Code, result.Body.String())
		}
	}
}

// TestBrowserOperationReservationIsAtomic 验证启动尚未完成时其他操作立即返回冲突，运行标记建立后仍不能争用。
func TestBrowserOperationReservationIsAtomic(t *testing.T) {
	s := &Server{}
	if !s.beginIdleBrowserOperation(httptest.NewRecorder()) {
		t.Fatal("首次空闲操作未取得保留")
	}
	result := httptest.NewRecorder()
	if s.beginIdleBrowserOperation(result) || result.Code != 409 || !strings.Contains(result.Body.String(), "BROWSER_OPERATION_PENDING") {
		t.Fatal("并发启动没有被立即拒绝", result.Body.String())
	}
	s.cloudReplyMu.Lock()
	s.cloudReplyActive = true
	s.cloudReplyMu.Unlock()
	s.browserOperationMu.Unlock()
	if s.beginIdleBrowserOperation(httptest.NewRecorder()) {
		t.Fatal("运行任务建立后仍能取得浏览器操作权")
	}
}

// TestLegacyPositionCannotStartLocalAutoReply 验证旧岗位兼容路由也无法恢复已下线的本机自动回复流程。
func TestLegacyPositionCannotStartLocalAutoReply(t *testing.T) {
	s := newExtensionPairTestServer(t, http.NotFoundHandler())
	for _, path := range []string{"/api/v1/local/positions/position/run", "/api/v1/tasks/start"} {
		result := extensionPairTestRequest(s, path, map[string]string{"task_type": " Auto_Reply "})
		if result.Code != 409 || !strings.Contains(result.Body.String(), "CLOUD_AUTO_REPLY_REQUIRED") {
			t.Fatalf("%s 仍接受本机自动回复：%s", path, result.Body.String())
		}
	}
}
