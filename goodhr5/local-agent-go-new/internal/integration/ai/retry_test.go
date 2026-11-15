// Package ai 本文件用本地 HTTP 替身验证所有 AI 入口共享四次请求预算、及时停止和逐次计费，不调用付费模型。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestAllAIEntriesShareRequestAndValidationBudget 验证网络故障、坏 JSON 和缺字段共用四次预算，第四次成功后不再请求。
func TestAllAIEntriesShareRequestAndValidationBudget(t *testing.T) {
	fixtures := []struct {
		name, success string
		run           func(context.Context, *Client, cloud.AIConfig) error
	}{
		{"基础评分", `{"score":80,"reason":"测试"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.EvaluateCandidatePreview(ctx, cfg, cloud.PositionSnapshot{}, model.Candidate{})
			return err
		}},
		{"详情评分", `{"score":80,"reason":"测试"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.EvaluateCandidate(ctx, cfg, cloud.PositionSnapshot{}, model.Candidate{}, model.CandidateDetail{})
			return err
		}},
		{"图片评分", `{"score":80,"reason":"测试"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.EvaluateCandidateVision(ctx, cfg, cloud.PositionSnapshot{}, model.Candidate{}, model.CandidateDetail{}, [][]byte{{1}})
			return err
		}},
		{"简历解析", `{"is_resume":true,"candidate_name":"合成候选人"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.ParseResume(ctx, cfg, "合成简历")
			return err
		}},
		{"简历摘要", `{"ai_summary":"合成摘要"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.SummarizeResume(ctx, cfg, cloud.LibraryCandidate{})
			return err
		}},
		{"图片转录", `{"text":"合成原文"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.ReadDetailTextVision(ctx, cfg, [][]byte{{1}})
			return err
		}},
		{"在线资料", `{"raw_text":"合成原文"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.ReadAutoReplyOnlineProfileVision(ctx, cfg, [][]byte{onlineVisionTestPNG(t)}, "")
			return err
		}},
		{"推荐分析", `{"match_score":80,"recommendation_level":"建议推进","executive_summary":"合成摘要"}`, func(ctx context.Context, c *Client, cfg cloud.AIConfig) error {
			_, err := c.GenerateAutoReplyRecommendation(ctx, cfg, cloud.AutoReplyRecommendationReport{})
			return err
		}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			var receipts []Billing
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := requests.Add(1)
				if attempt == 1 {
					http.Error(w, "temporary", 503)
					return
				}
				content := fixture.success
				if attempt == 2 {
					content = "{invalid"
				}
				if attempt == 3 {
					content = "{}"
				}
				encoded, _ := json.Marshal(content)
				w.Header().Set("X-GoodHR-Billing", "1")
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}],"goodhr_billing":{"status":"charged","cost_units":2}}`, encoded)
			}))
			defer server.Close()
			ctx := WithBillingReporter(context.Background(), func(b Billing) { receipts = append(receipts, b) })
			err := fixture.run(ctx, New(), cloud.AIConfig{BaseURL: server.URL, APIKey: "ghai_fixture", Model: "test"})
			if err != nil || requests.Load() != maxAIAttempts || len(receipts) != maxAIAttempts {
				t.Fatalf("requests=%d receipts=%+v err=%v", requests.Load(), receipts, err)
			}
			for _, b := range receipts[1:] {
				if b.Status != "charged" || b.CostUnits == nil || *b.CostUnits != 2 {
					t.Fatalf("重试丢失计费：%+v", receipts)
				}
			}
		})
	}
}

// TestAIInvalidOutputStopsAfterFourRequests 验证一直返回坏输出只请求四次，最后的原始诊断属于第四次。
func TestAIInvalidOutputStopsAfterFourRequests(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{invalid"}}]}`)
	}))
	defer server.Close()
	_, err := New().EvaluateCandidatePreview(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if err == nil || requests.Load() != maxAIAttempts || !ok || diagnostic.RequestAttempt != maxAIAttempts {
		t.Fatalf("requests=%d diagnostic=%+v err=%v", requests.Load(), diagnostic, err)
	}
}

// TestAIStopsForAccountsAndCancellation 验证账号、余额、权限错误不重试，等待重试时取消也不会再发请求。
func TestAIStopsForAccountsAndCancellation(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(status) }))
			defer server.Close()
			_, err := New().EvaluateCandidatePreview(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
			if err == nil || requests.Load() != 1 {
				t.Fatalf("requests=%d err=%v", requests.Load(), err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporary", 503)
		time.AfterFunc(20*time.Millisecond, cancel)
	}))
	defer server.Close()
	defer cancel()
	_, err := New().EvaluateCandidatePreview(ctx, cloud.AIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}, cloud.PositionSnapshot{}, model.Candidate{})
	if !errors.Is(err, context.Canceled) || requests.Load() != 1 {
		t.Fatalf("取消后仍请求：%d %v", requests.Load(), err)
	}
}

// TestToolJSONRetriesBeforeExecution 验证坏参数在公共 AI 层重试，通过校验后只向业务交付一次工具。
func TestToolJSONRetriesBeforeExecution(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arguments := `{"message":"合成消息"}`
		if requests.Add(1) < maxAIAttempts {
			arguments = "{invalid"
		}
		encoded, _ := json.Marshal(arguments)
		fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"send_message","arguments":%s}}]}}]}`, encoded)
	}))
	defer server.Close()
	result, err := New().ChatWithTools(context.Background(), testAIConfig(server.URL), ToolChatRequest{Messages: []ToolMessage{{Role: "user", Content: "合成测试"}}, Tools: testToolDefinitions})
	if err != nil || requests.Load() != maxAIAttempts || len(result.Message.ToolCalls) != 1 {
		t.Fatalf("requests=%d result=%+v err=%v", requests.Load(), result, err)
	}
}

// TestEarlyScoreDeliveredOnceAcrossRetries 验证早评分只发布一次，坏尾部重试不重复触发业务回调。
func TestEarlyScoreDeliveredOnceAcrossRetries(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"analysis":{"score":80,"reason":"合成评分"},"raw_text":"完整原文"}`
		if requests.Add(1) < maxAIAttempts {
			content = `{"analysis":{"score":80,"reason":"合成评分"},"raw_text":`
		}
		encoded, _ := json.Marshal(content)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\ndata: [DONE]\n\n", encoded)
	}))
	defer server.Close()
	callbacks := 0
	_, err := New().evaluateCandidate(context.Background(), testAIConfig(server.URL), cloud.PositionSnapshot{}, model.Candidate{}, model.CandidateDetail{}, func(Decision) { callbacks++ })
	if err != nil || requests.Load() != maxAIAttempts || callbacks != 1 {
		t.Fatalf("requests=%d callbacks=%d err=%v", requests.Load(), callbacks, err)
	}
}

// TestEarlyDecisionRemainsAuthoritativeAfterRetry 验证尾部补读的模型评分变化不会改写已用于业务动作的第一份有效评分。
func TestEarlyDecisionRemainsAuthoritativeAfterRetry(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"analysis":{"score":80,"reason":"第一份有效评分"},"raw_text":`
		if requests.Add(1) > 1 {
			content = `{"analysis":{"score":99,"reason":"重试中的不同评分"},"raw_text":"补齐的合成原文"}`
		}
		encoded, _ := json.Marshal(content)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\ndata: [DONE]\n\n", encoded)
	}))
	defer server.Close()
	position := cloud.PositionSnapshot{}
	position.CommonConfig.OutputStructuredResume = true
	evaluation, err := New().EvaluateCandidateEarly(context.Background(), testAIConfig(server.URL), position, model.Candidate{}, model.CandidateDetail{})
	if err != nil || evaluation.Decision.Score != 80 || evaluation.Final == nil {
		t.Fatalf("evaluation=%+v err=%v", evaluation, err)
	}
	final := <-evaluation.Final
	if final.Err != nil || final.Decision.Score != 80 || final.Decision.Reason != evaluation.Decision.Reason || final.Decision.Resume == nil || final.Decision.Resume.RawText != "补齐的合成原文" || requests.Load() != 2 {
		t.Fatalf("requests=%d final=%+v", requests.Load(), final)
	}
}

// TestValidationDiagnosticKeepsLatestModelResponse 验证先有 HTTP 故障后仍返回坏字段时，最终诊断指向最新模型正文而非过期错误页。
func TestValidationDiagnosticKeepsLatestModelResponse(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "old-http-error", 503)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	}))
	defer server.Close()
	_, err := New().SummarizeResume(context.Background(), testAIConfig(server.URL), cloud.LibraryCandidate{})
	diagnostic, ok := ResponseDiagnosticFromError(err)
	if !ok || diagnostic.Response != "{}" || diagnostic.RequestAttempt != maxAIAttempts || requests.Load() != maxAIAttempts {
		t.Fatalf("requests=%d diagnostic=%+v err=%v", requests.Load(), diagnostic, err)
	}
}
