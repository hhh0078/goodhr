// Package lifecycle 文件作用：把每次AI请求与上游重试的安全事件独立写入固定岗位，成功不能抹去前次失败。
package lifecycle

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/storage"
)

// AIRequestAttempt 保存同一条归一化事件，原始错误仍用既有诊断字段展开，不依据当前任务猜测归属。
func (l *TaskLogger) AIRequestAttempt(line []byte, cause error) {
	if l == nil || l.store == nil {
		return
	}
	var event ai.RequestLogLine
	if json.Unmarshal(line, &event) != nil || event.Action != "ai.request" || event.Step != "ai_request_attempt" || event.Output.Attempt.ClientRequestID == "" {
		return
	}
	if strings.TrimSpace(event.TaskID) == "" && strings.TrimSpace(event.PositionID) == "" {
		return
	}
	var output map[string]json.RawMessage
	if json.Unmarshal(l.taskLogOutput(event.Status, event.DurationMS, cause), &output) != nil {
		return
	}
	metadata, _ := json.Marshal(event.Output.Attempt)
	output["ai_request_attempt"] = metadata
	encoded, _ := json.Marshal(output)
	saved, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{TaskID: event.TaskID, PositionID: event.PositionID, Flow: "ai", Step: "ai_request_attempt", Status: event.Status, Category: event.Category, IsError: event.IsError, Message: event.Message, ErrorMessage: event.ErrorMessage, CreatedAt: event.Timestamp, DurationMS: event.DurationMS, Input: json.RawMessage(`{}`), Output: encoded})
	if err != nil {
		log.Print("AI 请求诊断未写入岗位日志")
		return
	}
	l.recordAIResponseDiagnostic(cause, encoded, saved.ID)
}
