// Package contract 定义 Go 与 TypeScript Browser Worker 之间唯一的强类型协议。
package contract

import (
	"encoding/json"
	"fmt"
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
	return fmt.Sprintf("%s：%s（action=%s step=%s trace=%s）", e.Body.Code, e.Body.Message, e.Body.Action, e.Body.Step, e.Body.TraceID)
}
