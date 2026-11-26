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
	"time"

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
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "模型容量暂不足" + strings.Repeat("服务暂忙", 275), "code": "ServiceUnavailable"}})
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
		if event.Attempt.Result == "failed" && (!strings.HasPrefix(event.Attempt.ErrorDetail, "模型容量暂不足") || len(event.Attempt.ErrorDetail) <= 3000 || event.Attempt.ErrorDetailTruncated || event.Attempt.ErrorCode != "ServiceUnavailable" || event.Attempt.ResponseBytes == 0 || !strings.Contains(string(fields["description"]), "模型容量暂不足") || !strings.Contains(string(fields["description"]), "详见错误详情") || len(fields["description"]) > 2000) {
			t.Fatal("真实runLogger丢失逐次错误说明、错误码或已收字节")
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
	if run.errorMessage != "" {
		t.Fatal("后续重试已成功，历史失败却污染了最终运行结果")
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

// TestRunLoggerAIErrorUsesSanitizedSummary 验证真实AI错误可显示明确说明，任意技术错误仍走原保护，步骤失败不抢先终止任务。
func TestRunLoggerAIErrorUsesSanitizedSummary(t *testing.T) {
	run, fixture, _, _, _, _ := serverGoalFixture(t)
	logger := &runLogger{run: run}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":{"message":"模型容量不足 PRIVATE_API_KEY","code":"ServiceUnavailable"}}`)
	}))
	defer provider.Close()
	ctx := shared.WithAIBilling(context.Background(), logger, "origin-task", "10000000-0000-4000-8000-000000000001", "PRIVATE_CANDIDATE", "AI分析")
	_, err := ai.New().ParseResume(ctx, cloud.AIConfig{BaseURL: provider.URL, APIKey: "PRIVATE_API_KEY", Model: "test-model"}, "PRIVATE_RESUME")
	if err == nil || !strings.Contains(ai.SafeRequestErrorSummary(err), "模型容量不足 [已隐藏]") {
		t.Fatal("真实请求没有保留已脱敏的小结")
	}
	logger.Step("origin-task", "auto_reply", "ai", "failed", time.Now(), err)
	logger.Step("origin-task", "auto_reply", "private-step", "failed", time.Now(), errors.New("PRIVATE_BROWSER_TOKEN"))
	found := false
	for key, change := range fixture.data {
		if !strings.HasPrefix(key, "runtime_events|") {
			continue
		}
		var value struct{ Title, Description string }
		_ = json.Unmarshal(change.Payload, &value)
		if value.Title == "ai" {
			found = strings.Contains(value.Description, "模型容量不足 [已隐藏]")
		}
		if strings.Contains(string(change.Payload), "PRIVATE_") {
			t.Fatal("运行步骤透出了原始凭据或技术错误")
		}
	}
	if !found || run.errorMessage != "" {
		t.Fatal("安全AI说明被固定文案覆盖，或步骤错误抢先污染最终结果")
	}
}
