// Package lifecycle 文件作用：在任务结束、分享或下次登录时上传岗位最近一千条脱敏日志。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/ai"
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
	localAutoTasks := map[string]bool{}
	checkedTasks := map[string]bool{}
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
		localReplyDetail := item.Flow == "auto_reply" || strings.Contains(item.Step, "auto_reply")
		if !checkedTasks[item.TaskID] {
			checkedTasks[item.TaskID] = true
			if task, taskErr := r.store.Task(ctx, item.TaskID); taskErr == nil {
				localAutoTasks[item.TaskID] = task.TaskType == "auto_reply"
			}
		}
		localReplyDetail = localReplyDetail || localAutoTasks[item.TaskID]
		if item.Flow == "browser_worker" && (strings.Contains(item.Step, "input") || strings.Contains(item.Step, "read") || strings.Contains(string(item.Input), `"action":"element.input"`) || strings.Contains(string(item.Input), `"action":"element.read"`)) {
			localReplyDetail = true
		}

		if localReplyDetail {
			message = "浏览器步骤已记录，正文已隐藏"
			input = uploadPrivateDiagnostic(input)
			output = uploadPrivateDiagnostic(output)
		}
		if item.Flow == "ai" && item.Step == "ai_billing" {
			var billing ai.Billing
			if json.Unmarshal(item.Output, &billing) == nil {
				// 费用可展示，自动回复的候选人姓名和正文仍留在本机。
				message = "AI 调用，" + aiBillingCostMessage(billing)
				output, _ = json.Marshal(billing)
			}
		}
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

// uploadLogObject 完整保留经授权的 AI 失败诊断，普通出参继续脱敏并限制大小。
func uploadLogObject(value json.RawMessage) json.RawMessage {
	if !json.Valid(value) {
		return json.RawMessage(`{}`)
	}
	safe := json.RawMessage(strings.TrimSpace(string(diagnostics.Redact(value))))
	if len(safe) == 0 || safe[0] != '{' {
		return json.RawMessage(`{"summary":"内容过长或格式不适合分享"}`)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(safe, &fields) != nil {
		return json.RawMessage(`{}`)
	}
	aiResponse := fields["ai_response_diagnostic"]
	delete(fields, "ai_response_diagnostic")
	ordinary, _ := json.Marshal(fields)
	if len(ordinary) > 12<<10 {
		fields = map[string]json.RawMessage{"summary": json.RawMessage(`"内容过长或格式不适合分享"`)}
	}
	if len(aiResponse) > 0 && aiResponse[0] == '{' {
		fields["ai_response_diagnostic"] = aiResponse
	}
	safe, _ = json.Marshal(fields)
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

// uploadPrivateDiagnostic 隐藏自动回复正文，同时保留失败步骤、错误码、超时与原生诊断。
func uploadPrivateDiagnostic(value json.RawMessage) json.RawMessage {
	var fields map[string]any
	if json.Unmarshal(value, &fields) != nil {
		return json.RawMessage(`{}`)
	}
	allowed := map[string]any{}
	for _, key := range []string{"action", "step", "status", "error_code", "error_message", "original_error", "timeout_ms", "duration_ms", "request_id", "trace_id", "reason", "input_wait", "page_state", "diagnostics", "queue", "native_mouse", "stack"} {
		if v, ok := fields[key]; ok {
			allowed[key] = v
		}
	}
	safe, _ := json.Marshal(diagnostics.UploadSafeDiagnostic(allowed))
	return safe
}
