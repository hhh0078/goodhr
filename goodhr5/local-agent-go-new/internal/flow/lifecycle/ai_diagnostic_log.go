// Package lifecycle 文件作用：将 AI 失败回复完整写入日志，并通过已有落库记录避免上层包装重复保存原文。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// taskLogOutput 保存步骤状态及 AI 失败的完整回复，原文只进入按需展开的诊断字段。
func (l *TaskLogger) taskLogOutput(status string, durationMS int64, err error) json.RawMessage {
	var response *ai.ResponseDiagnostic
	reference := ""
	if value, ok := ai.ResponseDiagnosticFromError(err); ok {
		var responseErr *ai.ResponseError
		errors.As(err, &responseErr)
		diagnosticID := responseErr.DiagnosticID()
		l.aiDiagnosticMu.Lock()
		var savedLogID int64
		for _, recorded := range l.aiDiagnosticErrors {
			if recorded.DiagnosticID == diagnosticID {
				savedLogID = recorded.LogID
				break
			}
		}
		l.aiDiagnosticMu.Unlock()
		seen := false
		if l.store != nil && savedLogID != 0 {
			seen, _ = l.store.TaskLogRetainedAfterAppend(context.Background(), savedLogID)
		}
		if seen {
			reference = "完整 AI 回复已记录在本任务前面的错误明细中"
		} else {
			response = &value
		}
	}
	output, _ := json.Marshal(struct {
		Status               string                 `json:"status"`
		DurationMS           int64                  `json:"duration_ms"`
		AIResponseDiagnostic *ai.ResponseDiagnostic `json:"ai_response_diagnostic,omitempty"`
		AIResponseReference  string                 `json:"ai_response_reference,omitempty"`
	}{status, durationMS, response, reference})
	return json.RawMessage(strings.TrimSpace(string(diagnostics.Redact(output))))
}

// recordAIResponseDiagnostic 只在完整原文成功落库后记住位置，旧记录被淘汰时允许再次完整记录。
func (l *TaskLogger) recordAIResponseDiagnostic(err error, output json.RawMessage, logID int64) {
	var diagnostic struct {
		AIResponse json.RawMessage `json:"ai_response_diagnostic"`
	}
	var responseErr *ai.ResponseError
	if json.Unmarshal(output, &diagnostic) != nil || len(diagnostic.AIResponse) == 0 || !errors.As(err, &responseErr) || responseErr == nil {
		return
	}
	diagnosticID := responseErr.DiagnosticID()
	l.aiDiagnosticMu.Lock()
	defer l.aiDiagnosticMu.Unlock()
	for index, recorded := range l.aiDiagnosticErrors {
		if recorded.DiagnosticID == diagnosticID {
			l.aiDiagnosticErrors = append(l.aiDiagnosticErrors[:index], l.aiDiagnosticErrors[index+1:]...)
			break
		}
	}
	l.aiDiagnosticErrors = append(l.aiDiagnosticErrors, aiDiagnosticLog{DiagnosticID: diagnosticID, LogID: logID})
	if len(l.aiDiagnosticErrors) > 256 {
		l.aiDiagnosticErrors = l.aiDiagnosticErrors[len(l.aiDiagnosticErrors)-256:]
	}
}
