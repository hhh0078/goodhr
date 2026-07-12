// Package browser 负责测试 Node Browser Worker 固定端口和重启逻辑。
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSetAgentBaseURLUsesNextPort 验证 Worker 固定使用本地程序端口加一。
// t 为测试对象。
func TestSetAgentBaseURLUsesNextPort(t *testing.T) {
	manager := NewWorkerManager(nil)
	manager.SetAgentBaseURL("http://127.0.0.1:55271")
	if manager.baseURL != "http://127.0.0.1:55272" {
		t.Fatalf("baseURL = %s", manager.baseURL)
	}
}

// TestStatusReadsOnlyFixedWorker 验证状态只读取固定 Worker 地址。
// t 为测试对象。
func TestStatusReadsOnlyFixedWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"code": 200,
			"msg":  "成功",
			"data": map[string]any{
				"worker": "node",
				"pid":    12345,
			},
		})
	}))
	defer server.Close()

	manager := NewWorkerManager(nil)
	manager.baseURL = server.URL
	status := manager.Status()
	if !status.Running || status.Managed || status.PID != 12345 || status.BaseURL != server.URL {
		t.Fatalf("status = %+v", status)
	}
}

// TestCanceledCallDoesNotRestartWorker 验证任务停止导致的请求取消不会触发 Worker 重启。
func TestCanceledCallDoesNotRestartWorker(t *testing.T) {
	err := normalizeCallError(fmt.Errorf("Post http://127.0.0.1:55272/api: %w", context.Canceled))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if isRestartableCallError(err) {
		t.Fatal("任务取消错误不应触发 Worker 重启")
	}
}

// TestDeadlineCallDoesNotRestartWorker 验证请求超时不会触发 Worker 重启。
func TestDeadlineCallDoesNotRestartWorker(t *testing.T) {
	err := normalizeCallError(fmt.Errorf("Post http://127.0.0.1:55272/api: %w", context.DeadlineExceeded))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if isRestartableCallError(err) {
		t.Fatal("请求超时错误不应触发 Worker 重启")
	}
}
