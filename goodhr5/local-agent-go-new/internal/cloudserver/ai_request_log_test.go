// Package cloudserver 文件作用：通过真实runLogger、云状态HTTP协议与恢复查询验证逐次AI记录，不用本机TaskLogger代替云端流程。
package cloudserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestRunLoggerAIRequestLedger 验证真实失败后成功的四条请求事件进入权威HTTP账本，固定岗位且无会话串联，重复回调不增加事件。
func TestRunLoggerAIRequestLedger(t *testing.T) {
	run, fixture, conv, _, _, _ := serverGoalFixture(t)
	logger := &runLogger{run: run}
	logger.BindAutoReplyConversation(conv.ID, "unrelated-current-position")
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "temporary")
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"score\":80,\"reason\":\"合成原因\"}"}}]}`)
	}))
	defer provider.Close()
	const originPosition = "10000000-0000-4000-8000-000000000001"
	ctx := shared.WithAIBilling(context.Background(), logger, "origin-task", originPosition, "PRIVATE_CANDIDATE", "AI预分析")
	_, err := ai.New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: provider.URL, APIKey: "PRIVATE_API_KEY", Model: "synthetic-model"}, cloud.PositionSnapshot{}, model.Candidate{Summary: "PRIVATE_RESUME"})
	if err != nil || calls.Load() != 2 {
		t.Fatalf("真实请求次数改变：calls=%d err=%v", calls.Load(), err)
	}
	var saved []storage.AutoReplyStateChange
	fixture.mu.Lock()
	for key, value := range fixture.data {
		if strings.HasPrefix(key, "runtime_events|ai-request:") {
			saved = append(saved, value)
		}
	}
	fixture.mu.Unlock()
	if len(saved) != 4 {
		t.Fatalf("真实云端请求开始/结束缺失：%d", len(saved))
	}
	for _, entry := range saved {
		var event struct {
			TaskID, PositionID, ConversationID string
			Attempt                            ai.RequestAttempt `json:"ai_request_attempt"`
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry.Payload, &fields); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(fields["task_id"], &event.TaskID)
		_ = json.Unmarshal(fields["position_id"], &event.PositionID)
		_ = json.Unmarshal(fields["conversation_id"], &event.ConversationID)
		_ = json.Unmarshal(fields["ai_request_attempt"], &event.Attempt)
		if event.TaskID != "origin-task" || event.PositionID != originPosition || event.ConversationID != "" || strings.Contains(string(entry.Payload), "PRIVATE_") || strings.Contains(string(entry.Payload), "unrelated-current-position") {
			t.Fatal("日志来源串岗、关联候选人或包含私密正文")
		}
		line := ai.RequestLogLine{Timestamp: event.Attempt.StartedAt, TaskID: event.TaskID, PositionID: event.PositionID, Action: "ai.request", Step: "ai_request_attempt"}
		if event.Attempt.Phase != "start" {
			line.Timestamp = event.Attempt.FinishedAt
		}
		line.Output.Attempt = event.Attempt
		raw, _ := json.Marshal(line)
		logger.AIRequestAttempt(raw, errors.New("PRIVATE_ERROR"))
	}
	fixture.mu.Lock()
	count := 0
	for key := range fixture.data {
		if strings.HasPrefix(key, "runtime_events|ai-request:") {
			count++
		}
	}
	fixture.mu.Unlock()
	if count != 4 {
		t.Fatal("重复回调生成了重复请求")
	}
	if _, exists := run.nextPayload(nil, false, new(int64))["failures"]; exists {
		t.Fatal("请求元数据改变了原故障补领")
	}
	// 导出的是本次真实回调产生的脱敏协议，供云端真实HTTP+PG+分享接收用例继续核对。
	if directory := os.Getenv("GOODHR_AI_LOG_EVIDENCE"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.MarshalIndent(saved, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "cloudserver-ai-runtime-events.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
