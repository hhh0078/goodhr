// Package cloud 文件作用：验证详细岗位日志按云端请求大小限制保留最新完整记录，避免整批上传失败。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDiagnosticUploadKeepsNewestWithinRequestLimit 验证大量详细记录仍可上传，最新故障证据不被截断。
func TestDiagnosticUploadKeepsNewestWithinRequestLimit(t *testing.T) {
	count, first, last := 0, "", ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if len(body) > 6<<20 {
			t.Errorf("请求超过安全大小：%d", len(body))
		}
		var request struct {
			Logs []PositionDiagnosticLog `json:"logs"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		count = len(request.Logs)
		first, last = request.Logs[0].TaskID, request.Logs[count-1].TaskID
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	payload := json.RawMessage(`{"evidence":"` + strings.Repeat("x", 14000) + `"}`)
	logs := make([]PositionDiagnosticLog, 1000)
	for index := range logs {
		logs[index] = PositionDiagnosticLog{TaskID: fmt.Sprintf("task-%d", index), Input: payload, Output: payload}
	}
	if err := New(server.URL).UploadPositionLogs(context.Background(), "test-token", "test-position", logs); err != nil {
		t.Fatal(err)
	}
	if count < 100 || count >= 1000 || first == "task-0" || last != "task-999" {
		t.Fatalf("没有保留大小限制内的最新完整证据：count=%d first=%s last=%s", count, first, last)
	}
}
