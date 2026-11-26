// Package contract 文件作用：验证错误正文只保留实际原因，完整结构化诊断和暂停判定不受文案精简影响。
package contract

import (
	"encoding/json"
	"testing"
)

// TestWorkerErrorKeepsReasonAndDiagnostics 验证不同错误码都不会追加固定建议，且详细诊断原样保留。
func TestWorkerErrorKeepsReasonAndDiagnostics(t *testing.T) {
	for _, code := range []string{"WORKER_UNRESPONSIVE", "ELEMENT_NOT_FOUND", "VIEWPORT_TOO_SMALL", "SCROLL_NO_PROGRESS", "BROWSER_WINDOW_NOT_READY", "UNKNOWN_ERROR"} {
		t.Run(code, func(t *testing.T) {
			err := &WorkerError{Status: 500, Body: WorkerErrorBody{
				Code: code, Message: "实际错误原因", Action: "element.find_all", Step: "queue", TraceID: "trace-example",
				Details: json.RawMessage(`{"request_id":"request-example","original_error":"TimeoutError: read field\n  at readValue","stack":"Error stack","recovery_required":true,"browser_retained":true}`),
			}}
			before, _ := json.Marshal(err.Body)
			if got := err.Error(); got != "实际错误原因" {
				t.Fatalf("错误原因被固定话术改写：%q", got)
			}
			after, _ := json.Marshal(err.Body)
			if string(before) != string(after) {
				t.Fatalf("格式化正文修改了详细错误：%s", after)
			}
			if code == "WORKER_UNRESPONSIVE" && !IsSessionUnavailable(err) {
				t.Fatal("精简正文后丢失会话暂停判定")
			}
		})
	}
}

// TestWorkerErrorKeepsTargetAndAttempts 验证查找目标和实际尝试次数仍保留在正文，空错误可安全处理。
func TestWorkerErrorKeepsTargetAndAttempts(t *testing.T) {
	err := &WorkerError{Body: WorkerErrorBody{
		Code: "ELEMENT_NOT_FOUND", Message: "元素未找到",
		Details: json.RawMessage(`{"target_description":"候选人卡片","poll_attempts":7,"timeout_ms":2000}`),
	}}
	if got := err.Error(); got != "元素未找到；目标：候选人卡片；已尝试 7 次" {
		t.Fatalf("丢失查找证据：%q", got)
	}
	var empty *WorkerError
	if empty.Error() != "" || (&WorkerError{}).Error() != "浏览器操作没有完成" {
		t.Fatal("空错误格式化不正确")
	}
}
