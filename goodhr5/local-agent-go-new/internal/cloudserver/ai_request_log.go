// Package cloudserver 文件作用：把云端自动回复的逐次AI请求元数据写入既有权威账本，固定请求发起岗位，不关联变化中的候选会话。
package cloudserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/storage"
)

// AIRequestAttempt 保留请求元数据和已脱敏的服务错误说明，不保存模型正文、候选人或密钥，也不借用当前岗位和会话游标。
func (l *runLogger) AIRequestAttempt(line []byte, _ error) {
	if l == nil || l.run == nil || l.run.client == nil {
		return
	}
	var record ai.RequestLogLine
	if json.Unmarshal(line, &record) != nil || record.Action != "ai.request" || record.Step != "ai_request_attempt" || strings.TrimSpace(record.PositionID) == "" || record.Output.Attempt.ClientRequestID == "" {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	if err != nil {
		return
	}
	attempt := ai.NormalizeRequestAttempt(record.Output.Attempt)
	if (attempt.Layer != "client" && attempt.Layer != "server") || (attempt.Phase != "start" && attempt.Phase != "end" && attempt.Phase != "retry_wait_stopped") {
		return
	}
	// 相同请求阶段使用稳定账本键，事务冲突重试和重复回调不能制造第二次AI请求。
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", l.run.id, attempt.Layer, attempt.ClientRequestID, attempt.ServerAttempt, attempt.Phase)))
	id := "ai-request:" + hex.EncodeToString(digest[:])
	event := struct {
		RunID       string            `json:"run_id"`
		TaskID      string            `json:"task_id"`
		PositionID  string            `json:"position_id"`
		Title       string            `json:"title"`
		Status      string            `json:"status"`
		Description string            `json:"description"`
		OccurredAt  time.Time         `json:"occurred_at"`
		DurationMS  int64             `json:"duration_ms"`
		Attempt     ai.RequestAttempt `json:"ai_request_attempt"`
	}{l.run.id, record.TaskID, record.PositionID, "ai_request_attempt", attempt.Result, ai.RequestAttemptMessage(attempt), at.UTC(), attempt.ElapsedMS, attempt}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := l.run.client.ServerAutoReplyTransaction(ctx, l.run.cred, func(tx *storage.AutoReplyTx) error {
		old, err := tx.ServerLedger("runtime_events", id, nil)
		if err != nil || len(old) > 0 {
			return err
		}
		_, err = tx.ServerLedger("runtime_events", id, event)
		return err
	}); err != nil {
		l.run.fail("云端没能保存操作记录，请检查连接后重新启动。")
	}
}
