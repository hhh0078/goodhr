// Package ai 文件作用：验证 AI 临时故障重试、SSE 解析和致命错误分类。
package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestEvaluateCandidateRetriesAndReadsStream 验证 5xx 后会重试并解析 SSE 评分。
func TestEvaluateCandidateRetriesAndReadsStream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"score\\\":82,\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\\\"reason\\\":\\\"匹配\\\"}\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client := New()
	decision, err := client.EvaluateCandidate(
		context.Background(),
		cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test", ScoreThreshold: 70},
		cloud.PositionSnapshot{Name: "测试岗位"},
		model.Candidate{Name: "候选人"},
		model.CandidateDetail{Text: "详情"},
	)
	if err != nil {
		t.Fatalf("EvaluateCandidate() error = %v", err)
	}
	if !decision.Accepted || decision.Score != 82 || calls.Load() != 2 {
		t.Fatalf("decision = %+v, calls = %d", decision, calls.Load())
	}
}

// TestUnauthorizedIsPositionStoppingError 验证鉴权错误会停止岗位任务。
func TestUnauthorizedIsPositionStoppingError(t *testing.T) {
	err := newHTTPServiceError(http.StatusUnauthorized, "bad key", "")
	if !IsPositionStoppingError(err) {
		t.Fatal("unauthorized error should stop position")
	}
}
