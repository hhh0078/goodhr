// Package diagnostics 文件作用：使用真实 SQLite 和 HTTP 客户端验证无岗位崩溃、旧日志与离线补传。
package diagnostics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestDetailedIncidentsSurviveOfflineAndUpload 验证故障时保留队列、恢复后传完整错误且不会被旧类别阻断。
func TestDetailedIncidentsSurviveOfflineAndUpload(t *testing.T) {
	var offline atomic.Bool
	offline.Store(true)
	accepted := make(chan []storage.BrowserEvent, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "temporarily offline", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/api/browser-events" || r.Header.Get("Authorization") != "Bearer session-fixture" {
			http.Error(w, "invalid request", 400)
			return
		}
		var request struct {
			Events []storage.BrowserEvent `json:"events"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		for _, event := range request.Events {
			if len(event.Payload) > 16<<10 || event.PositionID != "" || event.CreatedAt.IsZero() {
				http.Error(w, "invalid event", 400)
				return
			}
		}
		accepted <- request.Events
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recorder := NewBrowserIncidentRecorder(store, cloud.New(server.URL))
	for _, line := range []string{
		`{"action":"worker.crash","category":"异常退出","is_error":true,"error_message":"original crash token=private-secret","stack":"Error: original crash\n at Mouse.down","trace_id":"original-trace"}`,
		`{"action":"worker.exit","category":"异常退出","is_error":true,"exit_code":1}`,
		`{"action":"worker.exit","category":"worker","is_error":false,"exit_code":0}`,
		`{"action":"ocr.exit","category":"异常退出","is_error":true,"exit_code":7}`,
	} {
		recorder.WorkerLine(Normalize([]byte(line)))
	}
	legacy, _ := json.Marshal(map[string]any{"action": "worker.exit", "category": "错误", "is_error": true, "stack": "legacy original stack", "input": strings.Repeat("x", 30000)})
	if err := store.SaveBrowserEvent(context.Background(), storage.BrowserEvent{EventID: strings.Repeat("a", 32), Payload: legacy, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	recorder.Bind("user-fixture", "session-fixture")
	recorder.Flush(context.Background())
	pending, err := store.PendingBrowserEvents(context.Background(), "user-fixture")
	if err != nil || len(pending) != 5 {
		t.Fatalf("离线时记录丢失：%v count=%d", err, len(pending))
	}
	offline.Store(false)
	recorder.Flush(context.Background())
	pending, err = store.PendingBrowserEvents(context.Background(), "user-fixture")
	if err != nil || len(pending) != 0 {
		t.Fatalf("补传失败，队列未清空：%v count=%d", err, len(pending))
	}
	select {
	case events := <-accepted:
		if len(events) != 5 {
			t.Fatalf("上传数量错误：%d", len(events))
		}
		for _, event := range events {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if payload["action"] == "worker.crash" && (!strings.Contains(string(event.Payload), "Mouse.down") || strings.Contains(string(event.Payload), "private-secret")) {
				t.Fatal("原始栈丢失或凭据未隐藏")
			}
			if event.EventID == strings.Repeat("a", 32) && (payload["category"] != "异常退出" || payload["stack"] != "legacy original stack") {
				t.Fatalf("旧记录未修正：%s", event.Payload)
			}
		}
	default:
		t.Fatal("真实 HTTP 客户端未完成上传")
	}
}
