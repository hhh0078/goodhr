// Package lifecycle 文件作用：在任务结束、分享或下次登录时上传岗位最近一千条脱敏日志。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// UploadPositionLogs 读取岗位最近日志并批量上传，成功后清除补传标记。
func (r *Runner) UploadPositionLogs(ctx context.Context, token, positionID string) error {
	if r == nil || r.store == nil || r.cloud == nil {
		return fmt.Errorf("岗位日志上传服务尚未准备好")
	}
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return fmt.Errorf("岗位编号不能为空")
	}
	items, err := r.store.ListPositionLogs(ctx, positionID, 1000)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return r.store.ClearPositionLogUploadPending(ctx, positionID)
	}
	logs := make([]cloud.PositionDiagnosticLog, 0, len(items))
	for _, item := range items {
		category := item.Category
		if category == "" {
			category = item.Flow
		}
		if category == "" {
			category = "agent"
		}
		message := uploadLogText(item.Message)
		errorMessage := uploadLogText(item.ErrorMessage)
		input := uploadLogObject(item.Input)
		output := uploadLogObject(item.Output)
		logs = append(logs, cloud.PositionDiagnosticLog{
			Timestamp: item.CreatedAt, Category: uploadLogText(category), IsError: item.IsError,
			ErrorMessage: errorMessage, Input: input, Output: output,
			Message: message, TaskID: item.TaskID,
			Action: item.Flow, Step: item.Step, Status: item.Status, DurationMS: item.DurationMS,
		})
	}
	if err := r.cloud.UploadPositionLogs(ctx, token, positionID, logs); err != nil {
		return err
	}
	return r.store.ClearPositionLogUploadPending(ctx, positionID)
}

// uploadLogText 对上传自由文本再次脱敏并限制字节数，避免单条超长错误挡住整批日志。
func uploadLogText(value string) string {
	value = strings.TrimSpace(string(diagnostics.Redact([]byte(value))))
	if len(value) <= 1000 {
		return value
	}
	boundary := 0
	for index := range value {
		if index > 996 {
			break
		}
		boundary = index
	}
	return value[:boundary] + "…"
}

// uploadLogObject 只上传脱敏且大小受限的 JSON 对象，避免敏感内容及超大响应进入云端。
func uploadLogObject(value json.RawMessage) json.RawMessage {
	if !json.Valid(value) {
		return json.RawMessage(`{}`)
	}
	safe := json.RawMessage(strings.TrimSpace(string(diagnostics.Redact(value))))
	if len(safe) > 12<<10 || len(safe) == 0 || safe[0] != '{' {
		return json.RawMessage(`{"summary":"内容过长或格式不适合分享"}`)
	}
	return safe
}

// retryPendingLogUploads 在获取新登录凭证后补传异常退出或离线期间积压的岗位日志。
func (r *Runner) retryPendingLogUploads(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	positions, err := r.store.PendingPositionLogUploads(ctx)
	if err != nil {
		return
	}
	for _, positionID := range positions {
		if err := r.UploadPositionLogs(ctx, token, positionID); err != nil {
			// 当前账号可能不拥有这个岗位，保留标记并等原账号下次登录。
			continue
		}
	}
}
