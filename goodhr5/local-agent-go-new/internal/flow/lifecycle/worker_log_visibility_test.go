// Package lifecycle 文件作用：验证正常鼠标细节只进入本地诊断，不刷岗位日志、不覆盖关键动作，真实故障仍可见。
package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// TestRoutineMouseLogsRemainLocal 验证连续正常鼠标动作完整留在诊断文件，岗位只保留候选人关键动作。
func TestRoutineMouseLogsRemainLocal(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "compact-task", PositionID: "compact-position", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.CandidateActivity("compact-task", "测试候选人", "正在读取详情", "running", nil)
	var raw bytes.Buffer
	sink := diagnostics.WorkerLine(&raw, logger.WorkerLine)
	for repeat := 0; repeat < 20; repeat++ {
		for _, step := range []string{"mouse_move", "mouse_down", "mouse_up", "mouse_wheel"} {
			for _, status := range []string{"start", "success"} {
				line := fmt.Sprintf(`{"trace_id":"compact-task","request_id":"mouse-%d","action":"element.screenshot_long","step":%q,"status":%q,"level":"info","diagnostic":true,"input":{"x":300,"y":450},"output":{"step_duration_ms":20}}`, repeat, step, status)
				sink([]byte(line))
			}
		}
	}
	logs, err := store.ListPositionLogs(ctx, "compact-position", 1000)
	if err != nil || len(logs) != 1 || !strings.Contains(logs[0].Message, "正在读取详情") {
		t.Fatalf("正常鼠标过程淹没了关键动作：logs=%+v err=%v", logs, err)
	}
	task, err := store.Task(ctx, "compact-task")
	if err != nil || !strings.Contains(task.CurrentStep, "正在读取详情") {
		t.Fatalf("正常鼠标细节覆盖了悬浮状态：task=%+v err=%v", task, err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw.Bytes()), []byte{'\n'})
	if len(lines) != 160 {
		t.Fatalf("本地正常鼠标诊断丢失，实际 %d 条", len(lines))
	}
	for _, line := range lines {
		var item workerLogLine
		if err := json.Unmarshal(line, &item); err != nil || !item.Diagnostic || item.RequestID == "" || !bytes.Contains(item.Input, []byte(`"x":300`)) || !bytes.Contains(item.Output, []byte(`"step_duration_ms":20`)) {
			t.Fatalf("本地诊断字段丢失：%s，错误：%v", line, err)
		}
	}
}

// TestMouseVisibilityKeepsAbnormalEvidence 验证失败、恢复、警告和未知诊断都不受正常鼠标精简规则影响。
func TestMouseVisibilityKeepsAbnormalEvidence(t *testing.T) {
	for _, item := range []workerLogLine{
		{Diagnostic: true, Step: "mouse_move", Status: "failed", Level: "error"},
		{Diagnostic: true, Step: "mouse_down", Status: "success", Level: "info", IsError: true},
		{Diagnostic: true, Step: "mouse_up", Status: "success", Level: "info", ErrorCode: "WORKER_UNRESPONSIVE"},
		{Diagnostic: true, Step: "mouse_wheel", Status: "success", Level: "info", ErrorMessage: "输入结果不确定"},
		{Diagnostic: true, Step: "mouse_move", Status: "success", Level: "warn"},
		{Diagnostic: true, Step: "mouse_move", Status: "success", Level: "info", Category: "错误"},
		{Diagnostic: true, Step: "mouse_move", Status: "success", Level: "info", Output: json.RawMessage(`{"error_code":"GOODHR_MOUSE_WINDOW_NOT_READY"}`)},
		{Diagnostic: true, Step: "mouse_move", Status: "success", Level: "info", Output: json.RawMessage(`{"input_wait":{"status":"failed"}}`)},
		{Diagnostic: true, Step: "mouse_move", Status: "success", Level: "info", Output: json.RawMessage(`{"native_mouse":{"recovery_required":true}}`)},
		{Diagnostic: true, Step: "mouse_move", Status: "recovered", Level: "info"},
		{Diagnostic: true, Step: "window_recovery", Status: "success", Level: "info"},
		{Diagnostic: true, Step: "selector_probe", Status: "missing", Level: "info"},
	} {
		if isRoutineMouseDiagnostic(item) || !keepWorkerLog(item) {
			t.Fatalf("异常或关键诊断被误隐藏：%+v", item)
		}
	}
}

// TestBrowserStderrWarningDoesNotCreateCandidateFailure 验证底层对象失效只留本地，而同文案的真实动作失败仍显示。
func TestBrowserStderrWarningDoesNotCreateCandidateFailure(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "stderr-task", PositionID: "stderr-position", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.CandidateActivity("stderr-task", "测试候选人", "正在读取详情", "running", nil)
	var raw bytes.Buffer
	sink := diagnostics.WorkerLine(&raw, logger.WorkerLine)
	sink([]byte(`2026-10-08T16:44:53.636Z pw:browser [pid=27348][out] ERROR: error in channel "content::2/3/2": exception while running method "callFunction" in namespace "page": Cannot find object with id = id-1448 evaluateFunction/args<@chrome://juggler/content/content/Runtime.js:747:17`))
	sink([]byte(`{"trace_id":"stderr-task","request_id":"actual-read","action":"element.read","step":"read","status":"failed","level":"error","error_code":"BROWSER_OPERATION_FAILED","error_message":"Cannot find object with id = id-1448","target_description":"候选人详情"}`))
	logs, err := store.ListPositionLogs(ctx, "stderr-position", 1000)
	if err != nil || len(logs) != 2 || !logs[1].IsError || !strings.Contains(logs[1].Message, "候选人详情") || !strings.Contains(logs[1].Message, "Cannot find object") {
		t.Fatalf("内部警告被误写为失败或真正动作失败丢失：logs=%+v err=%v", logs, err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw.Bytes()), []byte{'\n'})
	if len(lines) != 2 || !bytes.Contains(lines[0], []byte(`"stderr_kind":"browser_internal_stale_object"`)) || !bytes.Contains(lines[0], []byte(`"is_error":false`)) || !bytes.Contains(lines[1], []byte(`"status":"failed"`)) {
		t.Fatalf("本地原始诊断或真实动作错误丢失：%s", raw.String())
	}
}
