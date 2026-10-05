// Package lifecycle 文件作用：验证发送按钮缺失及回车兜底结果进入岗位错误日志和上传数据。
package lifecycle

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
)

// TestMissingSendButtonLogKeepsDiagnostics 验证真实 Worker 的 find 错误字段不丢失，随后回车成功也不覆盖原错误。
func TestMissingSendButtonLogKeepsDiagnostics(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "send-task", PositionID: "send-position", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.CandidateActivity("send-task", "测试候选人", "正在发送打招呼消息", "running", nil)
	logger.WorkerLine([]byte(`{"trace_id":"send-task","action":"element.click","step":"find","status":"failed","level":"error","description":"Boss发送聊天消息","error_code":"ELEMENT_NOT_FOUND","error_message":"发送按钮暂时没找到","poll_attempts":18,"timeout_ms":5000,"click_started":false,"attempts":[{"selector_value":"新版按钮","matches":0},{"selector_value":"旧版按钮","matches":0}]}`))
	logger.WorkerLine([]byte(`{"trace_id":"send-task","action":"keyboard.press","step":"press_key","status":"success","level":"info","diagnostic":true,"target_description":"发送按钮未能点击，改用回车发送消息"}`))
	logs, err := store.ListPositionLogs(ctx, "send-position", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 || !logs[1].IsError || logs[1].Level != "error" || logs[1].Category != "错误" || !strings.Contains(logs[1].Message, "Boss发送聊天消息") {
		t.Fatalf("发送错误不可见：%+v", logs)
	}
	var input struct {
		Attempts []json.RawMessage `json:"attempts"`
		Target   string            `json:"target_description"`
	}
	var output struct {
		Code    string `json:"error_code"`
		Polls   int    `json:"poll_attempts"`
		Started *bool  `json:"click_started"`
	}
	if err := json.Unmarshal(logs[1].Input, &input); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(logs[1].Output, &output); err != nil {
		t.Fatal(err)
	}
	if len(input.Attempts) != 2 || input.Target == "" || output.Code != "ELEMENT_NOT_FOUND" || output.Polls != 18 || output.Started == nil || *output.Started {
		t.Fatalf("查找详情丢失：%+v %+v", input, output)
	}
	if logs[2].IsError || !strings.Contains(logs[2].Message, "改用回车") {
		t.Fatalf("回车兜底结果不清楚：%+v", logs[2])
	}
}
