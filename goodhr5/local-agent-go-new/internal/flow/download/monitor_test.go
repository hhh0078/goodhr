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
