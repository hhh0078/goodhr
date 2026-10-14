// Package greeting 文件作用：验证候选人总超时会留下明确错误，慢 AI 等待受总时限约束且后台输出不被误取消。
package greeting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// candidateTimeoutLogger 保存总超时日志的状态、原始开始时间和错误。
type candidateTimeoutLogger struct {
	count     int
	status    string
	startedAt time.Time
	err       error
}

// Step 收集候选人总超时错误，忽略其它流程日志。
func (l *candidateTimeoutLogger) Step(_ string, _ string, step string, status string, startedAt time.Time, err error) {
	if step == "candidate_timeout" {
		l.count++
		l.status, l.startedAt, l.err = status, startedAt, err
	}
}

// TestCandidateTimeoutKeepsOriginalFailure 验证达到总时限时写入错误和完整耗时，同时保留浏览器故障类型。
func TestCandidateTimeoutKeepsOriginalFailure(t *testing.T) {
	ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Second), errCandidateProcessingTimeout)
	defer cancel()
	startedAt := time.Now().Add(-candidateTimeout)
	original := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE", Message: "单步操作中断"}}
	logger := &candidateTimeoutLogger{}
	flow := Flow{Logger: logger}
	err := flow.finishCandidateProcessing(ctx, "timeout-task", startedAt, original)
	if !errors.Is(err, errCandidateProcessingTimeout) || !errors.Is(err, original) ||
		logger.count != 1 || logger.status != "failed" || !logger.startedAt.Equal(startedAt) ||
		!errors.Is(logger.err, errCandidateProcessingTimeout) || !strings.Contains(err.Error(), "候选人处理超时") {
		t.Fatalf("总超时原因或耗时日志丢失：err=%v logger=%+v", err, logger)
	}
	if !shared.ShouldStopTaskImmediately(err) {
		t.Fatal("包装总超时后不应丢失浏览器故障的停止策略")
	}
}

// TestCandidateTimeoutDoesNotMislabelOtherStops 验证用户停止、父任务超时和单步超时不会误报候选人五分钟超时。
func TestCandidateTimeoutDoesNotMislabelOtherStops(t *testing.T) {
	parentExpired, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelParent()
	parentStopped, stopParent := context.WithCancel(context.Background())
	stopParent()
	for _, test := range []struct {
		name   string
		parent context.Context
		err    error
	}{
		{name: "parent_timeout", parent: parentExpired, err: context.DeadlineExceeded},
		{name: "user_stop", parent: parentStopped, err: context.Canceled},
		{name: "step_timeout", parent: context.Background(), err: &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}},
		{name: "success", parent: context.Background()},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeoutCause(test.parent, candidateTimeout, errCandidateProcessingTimeout)
			defer cancel()
			logger := &candidateTimeoutLogger{}
			flow := Flow{Logger: logger}
			err := flow.finishCandidateProcessing(ctx, "other-stop", time.Now(), test.err)
			if err != test.err || logger.count != 0 {
				t.Fatalf("其它停止原因被误报为候选人总超时：err=%v logger=%+v", err, logger)
			}
		})
	}
}

// TestSlowAIStopsAtCandidateDeadline 验证评分迟迟未返回时遵守候选人剩余预算，不再独立等待完整五分钟。
func TestSlowAIStopsAtCandidateDeadline(t *testing.T) {
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(requestCancelled)
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prepared := shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "slow-ai"},
		Position: cloud.PositionSnapshot{
			RequiresAI: true, Keywords: []string{"符合"},
			CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword", DetailMode: "dom"},
			AI:           cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture"},
		},
	}
	logger := &candidateTimeoutLogger{}
	flow := Flow{AI: ai.New(), Store: store, Logger: logger}
	runtime := &greetingSettingRuntime{}
	stats := shared.Stats{}
	// 缩短测试时限模拟仅剩少量预算，避免实际等待五分钟。
	ctx, cancel := context.WithTimeoutCause(context.Background(), 500*time.Millisecond, errCandidateProcessingTimeout)
	defer cancel()
	startedAt := time.Now()
	err = flow.processCandidate(ctx, prepared, runtime, model.Candidate{Name: "合成候选人"}, &stats)
	err = flow.finishCandidateProcessing(ctx, prepared.Request.TaskID, startedAt, err)
	if !errors.Is(err, errCandidateProcessingTimeout) || logger.count != 1 ||
		runtime.greetings != 0 || runtime.closes != 1 || stats.Failed != 1 || time.Since(startedAt) > 1500*time.Millisecond {
		t.Fatalf("慢 AI 没有及时停止或记录总超时：err=%v logger=%+v runtime=%+v stats=%+v", err, logger, runtime, stats)
	}
	select {
	case <-requestCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("候选人总超时后仍在等待 AI 请求")
	}
}

// TestCandidateCompletionKeepsBackgroundAI 验证评分返回后取消候选人上下文，完整响应仍能在后台继续读取。
func TestCandidateCompletionKeepsBackgroundAI(t *testing.T) {
	releaseFinal := make(chan struct{})
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"analysis\\\":{\\\"score\\\":0,\\\"reason\\\":\\\"不匹配\\\"},\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-releaseFinal:
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\\\"note\\\":\\\"完整输出\\\"}\"}}]}\n\ndata: [DONE]\n\n")
		case <-r.Context().Done():
			close(requestCancelled)
		}
	}))
	defer server.Close()
	defer close(releaseFinal)
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prepared := shared.PreparedTask{Position: cloud.PositionSnapshot{
		RequiresAI: true, Keywords: []string{"符合"},
		CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword", DetailMode: "dom"},
		AI:           cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture"},
	}}
	flow := Flow{AI: ai.New(), Store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stats := shared.Stats{}
	if err := flow.processCandidate(ctx, prepared, &greetingSettingRuntime{}, model.Candidate{}, &stats); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-requestCancelled:
		t.Fatal("评分返回后的后台响应被候选人结束误取消")
	case <-time.After(50 * time.Millisecond):
	}
}
