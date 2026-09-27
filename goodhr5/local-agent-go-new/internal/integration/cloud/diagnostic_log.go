// Package cloud 文件作用：上传岗位最近的脱敏结构化日志供云端排障分享。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PositionDiagnosticLog 表示云端可接收的一条脱敏诊断日志。
type PositionDiagnosticLog struct {
	Timestamp    string          `json:"timestamp"`
	Category     string          `json:"category"`
	IsError      bool            `json:"is_error"`
	ErrorMessage string          `json:"error_message"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output"`
	Message      string          `json:"message"`
	TaskID       string          `json:"task_id,omitempty"`
	Action       string          `json:"action,omitempty"`
	Step         string          `json:"step,omitempty"`
	Status       string          `json:"status,omitempty"`
	DurationMS   int64           `json:"duration_ms,omitempty"`
}

// UploadPositionLogs 批量替换云端岗位日志，失败时由调用方保留补传标记。
func (c *Client) UploadPositionLogs(ctx context.Context, token, positionID string, logs []PositionDiagnosticLog) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" || strings.TrimSpace(token) == "" {
		return fmt.Errorf("上传岗位日志缺少岗位编号或登录凭证")
	}
	if len(logs) == 0 {
		return nil
	}
	if len(logs) > 1000 {
		logs = logs[len(logs)-1000:]
	}
	request := struct {
		Logs []PositionDiagnosticLog `json:"logs"`
	}{Logs: logs}
	return c.do(ctx, http.MethodPut, "/api/positions/"+url.PathEscape(positionID)+"/logs", token, request, nil)
}
