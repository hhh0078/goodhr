// Package diagnostics 文件作用：验证日志在线上传、账号隔离、停止本地落盘和排障字段保留。
package diagnostics

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
)

// captureOnlineUpload 记录真实传给云端客户端的账号与载荷。
type captureOnlineUpload struct {
	tokens []string
	events []storage.BrowserEvent
}

// UploadBrowserEvents 接收在线上传事件供断言使用。
func (c *captureOnlineUpload) UploadBrowserEvents(_ context.Context, token string, events []storage.BrowserEvent) error {
	c.tokens = append(c.tokens, token)
	c.events = append(c.events, events...)
	return nil
}

// TestOnlineWriterUploadsStartupReadErrorAndShutdown 验证启动缓存、读取错误与退出日志进入云端且不创建日志文件。
func TestOnlineWriterUploadsStartupReadErrorAndShutdown(t *testing.T) {
	client := &captureOnlineUpload{}
	recorder := NewBrowserIncidentRecorder(nil, client)
	writer := NewOnlineWriter()
	_, _ = writer.Write([]byte(`{"action":"agent.start","message":"启动"}`))
	writer.SetUploadSink(recorder.WorkerLine, recorder.Flush)
	recorder.Bind("user-a", "session-a")
	_, _ = writer.Write([]byte(`{"action":"element.read","step":"read","is_error":true,"error_message":"读取超时","error_code":"READ_TIMEOUT","input":{"text":"private-chat","token":"private-session"},"output":{"duration_ms":45000,"stack":"at Read.text"}}`))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if writer.file != nil || writer.path != "" || len(client.events) != 2 || recorder.Status().Pending != 0 {
		t.Fatalf("日志没有完全在线上传：%+v events=%d", recorder.Status(), len(client.events))
	}
	var found bool
	for _, event := range client.events {
		var data map[string]any
		_ = json.Unmarshal(event.Payload, &data)
		if data["source_action"] == "element.read" {
			found = true
			text := string(event.Payload)
			if data["action"] != "runtime.log" || !strings.Contains(text, "READ_TIMEOUT") || !strings.Contains(text, "读取超时") || !strings.Contains(text, "45000") || !strings.Contains(text, "Read.text") || strings.Contains(text, "private-chat") || strings.Contains(text, "private-session") {
				t.Fatalf("错误证据丢失或正文泄露：%s", text)
			}
		}
	}
	if !found {
		t.Fatal("缺少读取失败日志")
	}
}

// TestOnlineLogsKeepAccountOwnership 验证退出后停止上传且旧账号日志不会发送给新账号。
func TestOnlineLogsKeepAccountOwnership(t *testing.T) {
	client := &captureOnlineUpload{}
	recorder := NewBrowserIncidentRecorder(nil, client)
	recorder.Bind("a", "token-a")
	recorder.WorkerLine([]byte(`{"action":"browser.exit","is_error":true,"message":"a-error"}`))
	recorder.Bind("", "")
	recorder.Flush(context.Background())
	if len(client.events) != 0 {
		t.Fatal("退出后仍在上传")
	}
	recorder.Bind("b", "token-b")
	recorder.WorkerLine([]byte(`{"action":"worker.stderr","message":"b-info"}`))
	recorder.Flush(context.Background())
	if len(client.events) != 1 || strings.Contains(string(client.events[0].Payload), "a-error") || client.tokens[0] != "token-b" {
		t.Fatalf("日志跨账号：%+v", client)
	}
	recorder.Bind("a", "token-a")
	recorder.Flush(context.Background())
	if len(client.events) != 2 || recorder.Status().Pending != 0 {
		t.Fatal("原账号日志未上传")
	}
}

// TestRuntimeBufferProtectsErrorsAndReportsOverflow 验证大量正常字段读取不会优先挤掉错误，溢出数量可见。
func TestRuntimeBufferProtectsErrorsAndReportsOverflow(t *testing.T) {
	client := &captureOnlineUpload{}
	recorder := NewBrowserIncidentRecorder(nil, client)
	recorder.WorkerLine([]byte(`{"action":"element.read","is_error":true,"error_message":"keep-error"}`))
	for index := 0; index < 2100; index++ {
		recorder.WorkerLine([]byte(`{"action":"element.find_all","message":"normal"}`))
	}
	status := recorder.Status()
	if status.Pending != 2000 || status.Dropped != 101 {
		t.Fatalf("内存日志未受限：%+v", status)
	}
	if !strings.Contains(string(recorder.pending[0].Payload), "keep-error") {
		t.Fatal("关键错误被正常日志挤掉")
	}
	recorder.Bind("owner", "session")
	recorder.Flush(context.Background())
	if len(client.events) == 0 || !strings.Contains(string(client.events[0].Payload), "LOG_UPLOAD_BUFFER_OVERFLOW") {
		t.Fatal("缓冲溢出没有上传到云端")
	}
}

// TestOnlineCrashKeepsAbnormalCategory 验证统一运行日志类型不会把真正崩溃降为普通错误。
func TestOnlineCrashKeepsAbnormalCategory(t *testing.T) {
	client := &captureOnlineUpload{}
	recorder := NewBrowserIncidentRecorder(nil, client)
	recorder.Bind("owner", "session")
	recorder.WorkerLine([]byte(`{"action":"worker.crash","category":"browser","is_error":true,"error_message":"crash"}`))
	recorder.Flush(context.Background())
	var item map[string]any
	if len(client.events) != 1 {
		t.Fatal("崩溃未上传")
	}
	_ = json.Unmarshal(client.events[0].Payload, &item)
	if item["category"] != "异常退出" || item["source_action"] != "worker.crash" {
		t.Fatalf("崩溃类别丢失：%s", client.events[0].Payload)
	}
}
