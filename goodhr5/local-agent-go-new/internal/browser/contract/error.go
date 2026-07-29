// Package contract 定义 Go 与 TypeScript Browser Worker 之间唯一的强类型协议。
package contract

import (
	"encoding/json"
	"strconv"
	"strings"
)

// WorkerErrorBody 表示 Worker 返回的稳定错误。
type WorkerErrorBody struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Action    string          `json:"action"`
	Step      string          `json:"step"`
	TraceID   string          `json:"trace_id"`
	Retryable bool            `json:"retryable"`
	Details   json.RawMessage `json:"details"`
}

// WorkerError 包装 Worker 错误及 HTTP 状态。
type WorkerError struct {
	Status int
	Body   WorkerErrorBody
}

// Error 返回适合本地日志展示的错误文本。
func (e *WorkerError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Body.Message)
	if message == "" {
		message = "浏览器操作没有完成"
	}
	var details struct {
		Description       string `json:"description"`
		TargetDescription string `json:"target_description"`
		PollAttempts      int    `json:"poll_attempts"`
		TimeoutMS         int    `json:"timeout_ms"`
	}
	_ = json.Unmarshal(e.Body.Details, &details)
	target := strings.TrimSpace(details.TargetDescription)
	if target == "" {
		target = strings.TrimSpace(details.Description)
	}
	parts := []string{message}
	if target != "" && !strings.Contains(message, target) {
		parts = append(parts, "目标："+target)
	}
	if details.PollAttempts > 0 {
		parts = append(parts, "已尝试 "+pluralAttempts(details.PollAttempts))
	}
	parts = append(parts, workerErrorSuggestion(e.Body.Code))
	return strings.Join(parts, "；")
}

// pluralAttempts 返回适合用户查看的选择器尝试次数。
func pluralAttempts(attempts int) string {
	return strconv.Itoa(max(attempts, 1)) + " 次"
}

// workerErrorSuggestion 根据稳定错误码返回下一步建议。
func workerErrorSuggestion(code string) string {
	switch strings.TrimSpace(code) {
	case "VIEWPORT_TOO_SMALL", "MOUSE_TARGET_OUTSIDE_VIEWPORT":
		return "请把浏览器窗口放大后再试"
	case "ELEMENT_NOT_FOUND":
		return "页面可能还没加载好，或平台页面结构有变化"
	case "SCROLL_NO_PROGRESS":
		return "列表可能已经到底，或当前区域不能继续滚动"
	default:
		return "我已经记下具体步骤，可以直接重试"
	}
}
