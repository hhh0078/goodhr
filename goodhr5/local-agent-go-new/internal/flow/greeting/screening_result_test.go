// Package greeting 文件作用：验证教程真实终态的后台同步不阻塞、可重试、并发去重且不上传候选人资料。
package greeting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// screeningTestLogger 线程安全地记录分析和后台同步失败，不接触真实候选人资料。
type screeningTestLogger struct {
	mu       sync.Mutex
	analyses []shared.AnalysisStatus
	warnings chan error
}

var _ shared.AnalysisLogger = (*screeningTestLogger)(nil)

// Step 将后台同步失败通知测试，其他流程日志保留原调用语义。
func (l *screeningTestLogger) Step(_ string, _ string, step string, status string, _ time.Time, err error) {
	if l.warnings != nil && step == "sync_screening_result" && status == "warning" {
		select {
		case l.warnings <- err:
		default:
		}
	}
}

// ReportAnalysis 保存实际流程发布的分析阶段，验证关键词通过后仍需等待 AI 判断。
func (l *screeningTestLogger) ReportAnalysis(_ string, status shared.AnalysisStatus) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.analyses = append(l.analyses, status)
}

// AnalysisStatus 返回最新分析的副本，让测试日志器完整实现已有结构化状态接口。
func (l *screeningTestLogger) AnalysisStatus(_ string) *shared.AnalysisStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.analyses) == 0 {
		return nil
	}
	status := l.analyses[len(l.analyses)-1]
	return &status
}

// ResetAnalysis 在任务切换时清空内存分析，与真实日志器保持同一能力合同。
func (l *screeningTestLogger) ResetAnalysis(_ string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.analyses = nil
}

// snapshots 返回分析副本，避免测试读取与后台日志竞争。
func (l *screeningTestLogger) snapshots() []shared.AnalysisStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]shared.AnalysisStatus(nil), l.analyses...)
}

// waitScreeningSync 有界等待后台事实，避免通过固定长睡眠猜测网络调度。
func waitScreeningSync(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("后台教程事实未在测试时限内完成")
}

// TestScreeningResultReportsOnceAndRetriesFailure 验证结果同步失败不标成功，下一个真实结果会重试且成功后不重复请求。
func TestScreeningResultReportsOnceAndRetriesFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/user-flow" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("结果同步没有复用已有流程接口：%s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Step       string         `json:"step"`
			Status     string         `json:"status"`
			PositionID string         `json:"position_id"`
			Metadata   map[string]any `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Step != "first_screening_result" || body.Status != "completed" || body.PositionID != "position-1" {
			t.Errorf("完成事件不正确：%+v", body)
		}
		if len(body.Metadata) != 3 || body.Metadata["kind"] != "keyword" || body.Metadata["phase"] != "result" || body.Metadata["terminal"] != true {
			t.Errorf("结果证据缺失或包含额外候选人信息：%+v", body.Metadata)
		}
		if calls.Load() == 1 {
			http.Error(w, `{"error":"暂时断线"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	logger := &screeningTestLogger{warnings: make(chan error, 1)}
	flow := &Flow{Cloud: cloud.New(server.URL), Logger: logger}
	prepared := shared.PreparedTask{Request: shared.StartRequest{TaskID: "task-1", Token: "test-token"}, Position: cloud.PositionSnapshot{ID: "position-1"}}
	stats := shared.Stats{Processed: 100}
	accepted := false
	result := shared.AnalysisStatus{Kind: "keyword", Phase: "result", Terminal: true, Accepted: &accepted, CandidateName: "不能上传的姓名", Reason: "不能上传的筛选说明"}
	flow.recordScreeningResult(context.Background(), prepared, &stats, result)
	select {
	case <-logger.warnings:
	case <-time.After(2 * time.Second):
		t.Fatal("同步失败没有留下告警")
	}
	if stats.ScreeningResultSync.Reported() || calls.Load() != 1 {
		t.Fatalf("同步失败伪造成功：%+v，调用次数 %d", stats, calls.Load())
	}
	waitScreeningSync(t, func() bool {
		flow.recordScreeningResult(context.Background(), prepared, &stats, result)
		return stats.ScreeningResultSync.Reported()
	})
	flow.recordScreeningResult(context.Background(), prepared, &stats, result)
	if calls.Load() != 2 || stats.Processed != 100 {
		t.Fatalf("重试或去重错误，或修改了招聘统计：%+v，调用次数 %d", stats, calls.Load())
	}
}

// TestScreeningResultIgnoresLoadingAndAcceptedPreview 验证加载、失败、仍需详情的预分析以及空统计均不记录完成。
func TestScreeningResultIgnoresLoadingAndAcceptedPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("未结束的分析不应请求云端")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	flow := &Flow{Cloud: cloud.New(server.URL)}
	stats := shared.Stats{Processed: 10}
	for _, result := range []shared.AnalysisStatus{
		{Kind: "ai", Phase: "loading", Terminal: true},
		{Kind: "ai", Phase: "error", Terminal: true},
		{Kind: "ai", Phase: "result", Stage: "preview", Terminal: false},
		{Kind: "keyword", Phase: "result", Stage: "preview", Terminal: false},
		{Kind: "ocr", Phase: "result", Terminal: true},
	} {
		flow.recordScreeningResult(context.Background(), shared.PreparedTask{}, &stats, result)
	}
	flow.recordScreeningResult(context.Background(), shared.PreparedTask{}, nil, shared.AnalysisStatus{Kind: "ai", Phase: "result", Terminal: true})
	if stats.ScreeningResultSync != nil {
		t.Fatal("无真实终态结果却标记了教程完成")
	}
}

// TestScreeningResultAcceptsRejectedAIPreview 验证 AI 基础筛选真实拒绝候选人时无需等待详情或成功打招呼。
func TestScreeningResultAcceptsRejectedAIPreview(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	flow := &Flow{Cloud: cloud.New(server.URL)}
	stats := shared.Stats{}
	prepared := shared.PreparedTask{Request: shared.StartRequest{Token: "test-token"}, Position: cloud.PositionSnapshot{ID: "position-1"}}
	result := reportAIResult(nil, "task-1", model.Candidate{}, ai.Decision{Accepted: false, Reason: "基础要求不符"}, 60, "preview", true)
	flow.recordScreeningResult(context.Background(), prepared, &stats, result)
	waitScreeningSync(t, stats.ScreeningResultSync.Reported)
	if calls.Load() != 1 || stats.Succeeded != 0 {
		t.Fatalf("真实拒绝结果没有独立完成教程：%+v，调用次数 %d", stats, calls.Load())
	}
}

// TestScreeningResultDoesNotBlockAndDeduplicatesBusy 验证慢请求不阻塞主流程，在途及成功请求均不叠加。
func TestScreeningResultDoesNotBlockAndDeduplicatesBusy(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	flow := &Flow{Cloud: cloud.New(server.URL)}
	stats := shared.Stats{Processed: 100}
	prepared := shared.PreparedTask{Position: cloud.PositionSnapshot{ID: "position-1"}}
	result := shared.AnalysisStatus{Kind: "keyword", Phase: "result", Terminal: true}
	returned := make(chan struct{})
	go func() {
		flow.recordScreeningResult(context.Background(), prepared, &stats, result)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("云端未放行时主流程仍然等待同步")
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("后台没有发起同步")
	}
	for index := 0; index < 20; index++ {
		flow.recordScreeningResult(context.Background(), prepared, &stats, result)
	}
	if stats.ScreeningResultSync.Reported() || calls.Load() != 1 {
		t.Fatalf("在途请求重复或提前成功：调用 %d", calls.Load())
	}
	releaseOnce.Do(func() { close(release) })
	waitScreeningSync(t, stats.ScreeningResultSync.Reported)
	for index := 0; index < 20; index++ {
		flow.recordScreeningResult(context.Background(), prepared, &stats, result)
	}
	if calls.Load() != 1 {
		t.Fatalf("成功后再次请求：调用 %d", calls.Load())
	}
	encoded, err := json.Marshal(stats)
	if err != nil || string(encoded) != `{"processed":100,"succeeded":0,"failed":0,"skipped":0}` {
		t.Fatalf("后台状态改动统计协议：%s %v", encoded, err)
	}
}

// TestScreeningResultSurvivesCandidateCancellation 验证候选人上下文已取消仍能用独立预算保存终态事实。
func TestScreeningResultSurvivesCandidateCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	flow := &Flow{Cloud: cloud.New(server.URL)}
	stats := shared.Stats{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flow.recordScreeningResult(ctx, shared.PreparedTask{Position: cloud.PositionSnapshot{ID: "position-1"}}, &stats, shared.AnalysisStatus{Kind: "ai", Phase: "result", Terminal: true})
	waitScreeningSync(t, stats.ScreeningResultSync.Reported)
	if calls.Load() != 1 {
		t.Fatalf("上下文取消导致真实事实未保存：调用 %d", calls.Load())
	}
}
