// Package download 文件作用：验证 Worker 下载终态会保存一次，处理中记录不会提前落库。
package download

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestSync 验证下载同步只保存成功和失败终态。
func TestSync(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"downloads": []map[string]any{
					{"id": "pending", "status": "pending", "created_at": "2026-07-29T01:00:00Z"},
					{"id": "saved", "status": "saved", "file_path": "/tmp/resume.pdf", "file_name": "resume.pdf", "created_at": "2026-07-29T01:01:00Z"},
					{"id": "failed", "status": "failed", "error": "网络中断", "created_at": "2026-07-29T01:02:00Z"},
				},
				"count":     3,
				"pending":   1,
				"directory": "/tmp",
			},
			"trace_id": "download_test",
		})
	}))
	defer worker.Close()

	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()

	monitor := &Monitor{Browser: client.New(worker.URL), Store: store}
	if err := monitor.Sync(context.Background()); err != nil {
		t.Fatalf("同步下载失败：%v", err)
	}
	if err := monitor.Sync(context.Background()); err != nil {
		t.Fatalf("重复同步下载失败：%v", err)
	}
	records, err := store.ListDownloads(context.Background())
	if err != nil {
		t.Fatalf("读取下载历史失败：%v", err)
	}
	if len(records) != 2 {
		t.Fatalf("只应保存两个终态记录，实际为 %d", len(records))
	}
}

// TestSyncQuietlyBeforeWorkerStarts 验证 Worker 首次连接成功前的连接失败会安静等待。
func TestSyncQuietlyBeforeWorkerStarts(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()

	stoppedWorker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	stoppedWorkerURL := stoppedWorker.URL
	stoppedWorker.Close()
	monitor := &Monitor{Browser: client.New(stoppedWorkerURL), Store: store}
	monitor.syncQuietly(context.Background())
	if monitor.connected || monitor.lastError != "" {
		t.Fatalf("Worker 启动前应该安静等待：connected=%t last_error=%q", monitor.connected, monitor.lastError)
	}

	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"downloads": []map[string]any{},
				"count":     0,
				"pending":   0,
				"directory": "/tmp",
			},
			"trace_id": "download_ready_test",
		})
	}))
	monitor.Browser = client.New(worker.URL)
	monitor.syncQuietly(context.Background())
	if !monitor.connected {
		t.Fatal("Worker 首次连接成功后应该记录已连接状态")
	}
	worker.Close()
	monitor.syncQuietly(context.Background())
	if monitor.lastError == "" {
		t.Fatal("Worker 曾经连接成功后意外断开应该保留错误提醒")
	}
}
