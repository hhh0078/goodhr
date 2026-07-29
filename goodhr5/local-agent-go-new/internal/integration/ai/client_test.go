// Package ai 文件作用：验证 AI 临时故障重试、SSE 解析和致命错误分类。
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

// TestParseDecisionSupportsLegacyAnalysis 验证旧岗位提示词的 analysis 嵌套评分仍能正确判断。
func TestParseDecisionSupportsLegacyAnalysis(t *testing.T) {
	decision, err := parseDecision(`{"analysis":{"score":86,"reason":"经验和岗位匹配"}}`, 70)
	if err != nil {
		t.Fatalf("parseDecision() error = %v", err)
	}
	if !decision.Accepted || decision.Score != 86 || decision.Reason != "经验和岗位匹配" {
		t.Fatalf("decision = %+v", decision)
	}
}

// TestParseDecisionRejectsMissingScore 验证缺少评分字段时明确报错而不是静默按零分跳过。
func TestParseDecisionRejectsMissingScore(t *testing.T) {
	if _, err := parseDecision(`{"reason":"缺少评分"}`, 70); err == nil {
		t.Fatal("缺少 score 时应该返回错误")
	}
}

// TestUnauthorizedIsPositionStoppingError 验证鉴权错误会停止岗位任务。
func TestUnauthorizedIsPositionStoppingError(t *testing.T) {
	err := newHTTPServiceError(http.StatusUnauthorized, "bad key", "")
	if !IsPositionStoppingError(err) {
		t.Fatal("unauthorized error should stop position")
	}
}

// TestEvaluateCandidateEarlyReturnsBeforeStructuredResume 验证评分完整后立即返回，结构化简历稍后从最终通道取得。
func TestEvaluateCandidateEarlyReturnsBeforeStructuredResume(t *testing.T) {
	releaseFinal := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"analysis\\\":{\\\"score\\\":88,\\\"reason\\\":\\\"匹配\\\"},\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-releaseFinal:
		case <-r.Context().Done():
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\\\"candidate_name\\\":\\\"候选人A\\\",\\\"work_region\\\":\\\"上海\\\"}\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	evaluation, err := client.EvaluateCandidateEarly(
		ctx,
		cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test", ScoreThreshold: 70},
		cloud.PositionSnapshot{
			Name: "测试岗位",
			CommonConfig: cloud.PositionCommonConfig{
				OutputStructuredResume: true,
			},
		},
		model.Candidate{Name: "候选人A"},
		model.CandidateDetail{Text: "详情"},
	)
	if err != nil {
		t.Fatalf("EvaluateCandidateEarly() error = %v", err)
	}
	if evaluation.Decision.Score != 88 || !evaluation.Decision.Accepted || evaluation.Final == nil {
		t.Fatalf("early evaluation = %+v", evaluation)
	}
	close(releaseFinal)
	final := <-evaluation.Final
	if final.Err != nil || final.Decision.Resume == nil {
		t.Fatalf("final evaluation = %+v", final)
	}
	if final.Decision.Resume.CandidateName != "候选人A" || final.Decision.Resume.WorkRegion != "上海" {
		t.Fatalf("resume = %+v", final.Decision.Resume)
	}
}

// TestThinkingSwitchMatchesLegacyRequest 验证关闭思考时显式发送 false，开启时沿用旧版不强塞参数。
func TestThinkingSwitchMatchesLegacyRequest(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"score\":80,\"reason\":\"匹配\"}"}}]}`)
	}))
	defer server.Close()
	client := New()
	cfg := cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}
	for _, enabled := range []bool{false, true} {
		_, err := client.EvaluateCandidate(
			context.Background(),
			cfg,
			cloud.PositionSnapshot{Name: "测试岗位", EnableThinking: enabled},
			model.Candidate{Name: "候选人"},
			model.CandidateDetail{Text: "详情"},
		)
		if err != nil {
			t.Fatalf("EvaluateCandidate(%v) error = %v", enabled, err)
		}
	}
	disabledRequest := <-requests
	enabledRequest := <-requests
	if disabledRequest["enable_thinking"] != false {
		t.Fatalf("disabled request = %#v", disabledRequest["enable_thinking"])
	}
	if _, exists := enabledRequest["enable_thinking"]; exists {
		t.Fatalf("enabled request should omit legacy-compatible field: %#v", enabledRequest)
	}
}
