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
	for _, item := range logs {
		var output struct {
			AIResponse json.RawMessage `json:"ai_response_diagnostic"`
		}
		if json.Unmarshal(item.Output, &output) == nil && len(output.AIResponse) > 0 && string(output.AIResponse) != "null" {
			// AI 失败证据必须完整上传；超过明确协议上限时保留本地记录及补传标记。
			encoded, err := json.Marshal(request)
			if err != nil {
				return err
			}
			if len(encoded) > 64<<20 {
				return fmt.Errorf("AI 完整诊断日志超过 64 MiB 上传上限，原始记录仍保存在本地")
			}
			return c.do(ctx, http.MethodPut, "/api/positions/"+url.PathEscape(positionID)+"/logs", token, request, nil)
		}
	}
	// 云端请求上限为 8 MiB；详细诊断增大后保留最新完整记录，留出编码和协议余量。
	bytesUsed, start := 16, len(logs)
	for start > 0 {
		encoded, err := json.Marshal(logs[start-1])
		if err != nil {
			return err
		}
		if bytesUsed+len(encoded)+1 > 6<<20 {
			break
		}
		bytesUsed += len(encoded) + 1
		start--
	}
	if start == len(logs) {
		return fmt.Errorf("单条岗位日志超过上传大小限制")
	}
	logs = logs[start:]
	request.Logs = logs
	return c.do(ctx, http.MethodPut, "/api/positions/"+url.PathEscape(positionID)+"/logs", token, request, nil)
}
