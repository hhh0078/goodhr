// Package lifecycle 文件作用：验证重要鼠标记录从 Worker 经离线缓存批量上传，完整保留服务器排障所需的原生证据。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestNativeMouseLogsUploadAndRetry 验证正常鼠标首尾不刷岗位日志，原生错误与重要诊断仍完整上传并支持补传。
func TestNativeMouseLogsUploadAndRetry(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SaveTask(ctx, storage.TaskRun{TaskID: "mouse-task", PositionID: "mouse-position", Status: "running", TaskType: "greeting"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	for _, event := range []string{
		`{"timestamp":"2026-09-29T05:19:35Z","trace_id":"mouse-task","request_id":"mouse-request","action":"element.scroll","step":"mouse_move","status":"start","level":"info","diagnostic":true,"input":{"x":300,"y":450,"camoufox_version":"152.0.4-beta.30"}}`,
		`{"timestamp":"2026-09-29T05:19:41Z","trace_id":"mouse-task","request_id":"mouse-request","action":"element.scroll","step":"mouse_move","status":"failed","level":"error","diagnostic":true,"error_message":"GOODHR_MOUSE_APZ_TIMEOUT:mousemove","input":{"x":300,"y":450},"output":{"step_duration_ms":6000,"native_mouse":{"stage":"apz_repaint","apz_flush_requested":true,"apz_repaint_received":false,"events_sent":0,"window_at_failure":{"window_minimized":true,"chrome_has_focus":false}}}}`,
		`{"timestamp":"2026-09-29T05:19:42Z","trace_id":"mouse-task","action":"element.find","step":"find","status":"success","level":"info"}`,
		`{"timestamp":"2026-09-29T05:19:43Z","trace_id":"mouse-task","request_id":"detail-request","action":"element.find_all","step":"selector_probe","status":"missing","level":"info","diagnostic":true,"input":{"attempt":2,"probe_only":true},"output":{"count":0,"selector_attempts":[{"level":"frame[1]","matches":0,"selected_index":0}],"page_state":{"frame_count":1,"recent_page_events":[{"type":"response","status":403}]}}}`,
		`{"timestamp":"2026-09-29T05:19:44Z","trace_id":"mouse-task","action":"element.click","step":"click_target","status":"checked","level":"info","diagnostic":true,"output":{"actionability":{"actionable":false,"reason":"pointer_intercepted","blockers":[{"css_class":"dialog-mask"}]}}}`,
	} {
		logger.WorkerLine([]byte(event))
	}
	items, err := store.ListPositionLogs(ctx, "mouse-position", 1000)
	if err != nil || len(items) != 3 {
		t.Fatalf("正常鼠标过程仍刷屏或重要诊断丢失：%+v %v", items, err)
	}
	if !strings.Contains(items[0].Message, "移动鼠标") {
		t.Fatalf("原子步骤不清楚：%s", items[0].Message)
	}
	fail := true
	var received []cloud.PositionDiagnosticLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/positions/mouse-position/logs" {
			t.Errorf("没有复用岗位日志上传接口：%s %s", r.Method, r.URL.Path)
		}
		if fail {
			http.Error(w, "temporarily offline", http.StatusServiceUnavailable)
			return
		}
		var body struct {
			Logs []cloud.PositionDiagnosticLog `json:"logs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		received = body.Logs
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	runner := &Runner{store: store, cloud: cloud.New(server.URL)}
	if err := store.MarkPositionLogUploadPending(ctx, "mouse-position"); err != nil {
		t.Fatal(err)
	}
	if err := runner.UploadPositionLogs(ctx, "test-token", "mouse-position"); err == nil {
		t.Fatal("失败的上传被当成成功")
	}
	pending, err := store.PendingPositionLogUploads(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("断网后没有保留补传标记：%v %v", pending, err)
	}
	fail = false
	if err := runner.UploadPositionLogs(ctx, "test-token", "mouse-position"); err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 || !received[0].IsError {
		t.Fatalf("上传记录不完整：%+v", received)
	}
	var output struct {
		RequestID   string `json:"request_id"`
		NativeMouse struct {
			Stage      string `json:"stage"`
			EventsSent int    `json:"events_sent"`
			Window     struct {
				Minimized bool `json:"window_minimized"`
			} `json:"window_at_failure"`
		} `json:"native_mouse"`
	}
	if err := json.Unmarshal(received[0].Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.RequestID != "mouse-request" || output.NativeMouse.Stage != "apz_repaint" || !output.NativeMouse.Window.Minimized || output.NativeMouse.EventsSent != 0 {
		t.Fatalf("服务器所需证据在上传时丢失：%+v", output)
	}
	if !strings.Contains(string(received[1].Output), `"frame[1]"`) || !strings.Contains(string(received[1].Output), `"status":403`) || !strings.Contains(string(received[2].Output), "dialog-mask") {
		t.Fatal("详情匹配、网络和遮挡证据在上传时丢失")
	}
	pending, err = store.PendingPositionLogUploads(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("上传成功没有清除补传标记：%v %v", pending, err)
	}
}
