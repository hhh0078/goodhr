// 本文件使用 HTTP 替身验证云端 AI 的统一重试预算、字段校验、停止边界和失败事件隔离，不访问付费模型或生产数据库。
package httpapi

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
)

// TestAITextRequestAndValidationShareBudget 验证服务故障、坏 JSON 和缺字段共用四次预算，业务只接收最终有效值。
func TestAITextRequestAndValidationShareBudget(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := calls.Add(1)
		if attempt == 1 {
			http.Error(w, "temporary", 503)
			return
		}
		content := `{"value":"通过校验"}`
		if attempt == 2 {
			content = "{invalid"
		}
		if attempt == 3 {
			content = "{}"
		}
		encoded, _ := json.Marshal(content)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, encoded)
	}))
	defer server.Close()
	service := &AIConfigService{httpClient: server.Client()}
	value := ""
	_, err := service.requestAIText(context.Background(), aiConfigRequest{BaseURL: server.URL, APIKey: "synthetic", Model: "test"}, []AIMsg{{Role: "user", Content: "合成测试"}}, func(content string) error {
		var result struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			return err
		}
		if result.Value == "" {
			return errors.New("缺少value")
		}
		value = result.Value
		return nil
	})
	if err != nil || calls.Load() != maxAIAttempts || value != "通过校验" {
		t.Fatalf("calls=%d value=%s err=%v", calls.Load(), value, err)
	}
}

// TestCloudAIStopsForAccountsAndCancellation 验证账号错误立即结束，Retry-After 等待可以被取消且不会多发请求。
func TestCloudAIStopsForAccountsAndCancellation(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			_, err := retryAI(context.Background(), "合成测试", func(int) (string, error) {
				calls++
				return "", aiHTTPFailure(&http.Response{StatusCode: status, Header: make(http.Header)}, "请求被拒绝")
			})
			if err == nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	started := time.Now()
	_, err := retryAI(ctx, "合成限流", func(int) (string, error) {
		calls++
		time.AfterFunc(20*time.Millisecond, cancel)
		return "", aiHTTPFailure(&http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"30"}}}, "限流")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || time.Since(started) > time.Second {
		t.Fatalf("calls=%d elapsed=%s err=%v", calls, time.Since(started), err)
	}
}

// TestAttachmentAIUsesUnifiedBudget 验证附件识别的坏结果统一重试，明确不是简历时不浪费后续调用。
func TestAttachmentAIUsesUnifiedBudget(t *testing.T) {
	for _, nonResume := range []bool{false, true} {
		t.Run(fmt.Sprint(nonResume), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := calls.Add(1)
				content := `{"is_resume":true,"candidate_name":"合成候选人"}`
				if attempt < maxAIAttempts {
					content = "{invalid"
				}
				if nonResume {
					content = `{"is_resume":false}`
				}
				encoded, _ := json.Marshal(content)
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, encoded)
			}))
			defer server.Close()
			service := &AttachmentParseService{ai: &AIConfigService{httpClient: server.Client(), systemConfigs: NewMemorySystemConfigStore()}}
			value, code, message, _ := service.parseAI(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "synthetic", Model: "test"}, attachmentParseRequest{Phase: "basic", Text: "合成候选人 合成简历"})
			if nonResume {
				if code != "NOT_A_RESUME" || calls.Load() != 1 {
					t.Fatalf("calls=%d code=%s message=%s", calls.Load(), code, message)
				}
			} else if code != "" || calls.Load() != maxAIAttempts || value["candidate_name"] != "合成候选人" {
				t.Fatalf("calls=%d value=%v code=%s message=%s", calls.Load(), value, code, message)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, code, _, _ = service.parseAI(ctx, AIConfig{}, attachmentParseRequest{})
			if code == "" {
				t.Fatal("取消被误报成功")
			}
		})
	}
}

// TestAssistantFailedAttemptDoesNotPublishEvents 验证失败尝试的文字与工具只用于纠错，最终成功轮只发布一次。
func TestAssistantFailedAttemptDoesNotPublishEvents(t *testing.T) {
	t.Parallel()
	calls := 0
	service := &AssistantService{modelCall: func(_ context.Context, _ CandidateLibraryScope, _ []AIMsg, emit func(assistantModelEvent) error) error {
		calls++
		if err := emit(assistantModelEvent{Type: "text", Text: "合成正文"}); err != nil {
			return err
		}
		if calls < maxAIAttempts {
			return errors.New("合成断流")
		}
		return emit(assistantModelEvent{Type: "done"})
	}}
	delivered := []assistantModelEvent{}
	err := service.callValidatedModel(context.Background(), CandidateLibraryScope{}, nil, func(event assistantModelEvent) error { delivered = append(delivered, event); return nil })
	if err != nil || calls != maxAIAttempts || len(delivered) != 2 || delivered[0].Type != "text" || delivered[1].Type != "done" {
		t.Fatalf("calls=%d delivered=%+v err=%v", calls, delivered, err)
	}
}

// TestCloudAIBillingFailureIsNotRetried 验证本地账本失败不能靠重发模型修复，防止重复付费。
func TestCloudAIBillingFailureIsNotRetried(t *testing.T) {
	calls := 0
	expected := errors.New("合成账本错误")
	_, err := retryAI(context.Background(), "合成计费", func(int) (string, error) { calls++; return "", noAIRetry(expected) })
	if calls != 1 || !errors.Is(err, expected) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
