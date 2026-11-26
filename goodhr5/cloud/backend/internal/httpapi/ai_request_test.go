// 本文件用合成 AI 上游验证服务器统一重试、完整 JSON 检查、费用合并及旧客户端不叠加重试，不访问付费模型。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// serverAIEnvelope 为测试生成带真实用量结构的合成普通或 SSE 响应。
func serverAIEnvelope(content string, stream bool) string {
	value, _ := json.Marshal(content)
	if stream {
		return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n", value)
	}
	return fmt.Sprintf(`{"choices":[{"message":{"content":%s}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`, value)
}

// TestServerRetriesModelJSONBeforeReturning 验证客户端只发一次请求，服务器内部第四次成功且坏回复不泄漏、费用逐笔记账。
func TestServerRetriesModelJSONBeforeReturning(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var calls atomic.Int32
			service, wallet := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				content := `{"score":80,"reason":"通过"}`
				if calls.Add(1) < serverAIMaxAttempts {
					content = "{broken-model-json"
				}
				kind := "application/json"
				if stream {
					kind = "text/event-stream"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {kind}}, Body: io.NopCloser(strings.NewReader(serverAIEnvelope(content, stream)))}, nil
			}))
			payload := fmt.Sprintf(`{"model":"first-model","stream":%t,"messages":[{"role":"system","content":"只返回JSON对象"}]}`, stream)
			request := httptest.NewRequest("POST", "/api/ai-compatible/v1/chat/completions", strings.NewReader(payload))
			request.Header.Set("Authorization", "Bearer embedding-user-key")
			request.Header.Set("X-GoodHR-Billing", "1")
			response := httptest.NewRecorder()
			service.CompatibleChat(response, request)
			if response.Code != 200 || calls.Load() != serverAIMaxAttempts || strings.Contains(response.Body.String(), "broken-model-json") || !strings.Contains(response.Body.String(), "score") || response.Header().Get("X-GoodHR-AI-Attempts") != "4" {
				t.Fatalf("calls=%d status=%d body=%s", calls.Load(), response.Code, response.Body.String())
			}
			records, _, _ := wallet.ListRecords("embedding@example.com", 20, 0)
			usages := 0
			var charged int64
			for _, record := range records {
				if record.Category == "ai_usage" {
					usages++
					charged -= record.ChangeUnits
				}
			}
			if usages != serverAIMaxAttempts || !strings.Contains(response.Body.String(), fmt.Sprintf(`"cost_units":%d`, charged)) {
				t.Fatalf("usages=%d charged=%d body=%s", usages, charged, response.Body.String())
			}
		})
	}
}

// TestServerRetryBudgetStopsLegacyClient 验证旧版耗尽后收到424，既不因正文model字样误停，也不叠加到十二次请求。
func TestServerRetryBudgetStopsLegacyClient(t *testing.T) {
	var upstream atomic.Int32
	service, _ := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		upstream.Add(1)
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"temporary"}`))}, nil
	}))
	incoming := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { incoming++; service.CompatibleChat(w, r) }))
	defer server.Close()
	status := 0
	for attempt := 0; attempt < 3; attempt++ {
		request, _ := http.NewRequest("POST", server.URL, strings.NewReader(`{"model":"first-model"}`))
		request.Header.Set("Authorization", "Bearer embedding-user-key")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		status = response.StatusCode
		if status != 429 && status < 500 {
			if !bytes.Contains(body, []byte("AI_RETRY_EXHAUSTED")) {
				t.Fatal(string(body))
			}
			break
		}
	}
	if incoming != 1 || upstream.Load() != serverAIMaxAttempts || status != 424 {
		t.Fatalf("incoming=%d upstream=%d status=%d", incoming, upstream.Load(), status)
	}
}

// TestServerRequestValidationAndStopping 验证通用 JSON、工具参数、文本协议、取消和账号错误的统一边界。
func TestServerRequestValidationAndStopping(t *testing.T) {
	fixtures := []struct {
		name, mode, body string
		chat, valid      bool
	}{
		{"坏外层", "", `{bad`, true, false},
		{"坏模型JSON", "json", serverAIEnvelope("{bad", false), true, false},
		{"合法对象", "json", serverAIEnvelope(`{"score":0}`, false), true, true},
		{"文本回复", "", serverAIEnvelope("成功", false), true, true},
		{"事件流", "json_lines", serverAIEnvelope("{\"type\":\"text\"}\n{\"type\":\"done\"}", true), true, true},
		{"坏工具参数", "", `{"choices":[{"message":{"tool_calls":[{"function":{"arguments":"{"}}]}}]}`, true, false},
		{"向量外层", "", `{"data":[]}`, false, true},
		{"空响应", "", `{"choices":[]}`, true, false},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			err := validateAIWire([]byte(fixture.body), fixture.mode, fixture.chat)
			if (err == nil) != fixture.valid {
				t.Fatalf("valid=%t err=%v", fixture.valid, err)
			}
		})
	}
	for _, status := range []int{400, 401, 402, 403, 404, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("denied"))}, nil
			})}
			request, _ := http.NewRequest("POST", "https://upstream.example.com/v1/chat/completions", strings.NewReader(`{}`))
			response, err := doServerAIRequest(client, request)
			if err != nil || response.StatusCode != status || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			response.Body.Close()
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		time.AfterFunc(10*time.Millisecond, cancel)
		return nil, errors.New("synthetic network failure")
	})}
	request, _ := http.NewRequestWithContext(ctx, "POST", "https://upstream.example.com/v1/chat/completions", strings.NewReader(`{}`))
	_, err := doServerAIRequest(client, request)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

// TestServerWireModeUsesOnlySystemContract 验证候选人资料中的JSON字样不会改变文本请求的协议。
func TestServerWireModeUsesOnlySystemContract(t *testing.T) {
	for payload, want := range map[string]string{
		`{"messages":[{"role":"user","content":"JSON是我的技能"}]}`:        "",
		`{"messages":[{"role":"system","content":"只返回JSON对象"}]}`:      "json",
		`{"response_format":{"type":"json_object"}}`:                  "json",
		`{"messages":[{"role":"system","content":"JSON事件流，每行一个对象"}]}`: "json_lines",
	} {
		if got := aiOutputMode([]byte(payload)); got != want {
			t.Fatalf("got=%s want=%s", got, want)
		}
	}
}

// TestServerRetryUnknownFeeRemainsUnknown 验证网络失联的费用未知不能被后续成功覆盖成已确认总金额。
func TestServerRetryUnknownFeeRemainsUnknown(t *testing.T) {
	calls := 0
	service, wallet := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("synthetic disconnect")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(serverAIEnvelope(`{"score":80}`, false)))}, nil
	}))
	request := httptest.NewRequest("POST", "/api/ai-compatible/v1/chat/completions", strings.NewReader(`{"model":"first-model","response_format":{"type":"json_object"}}`))
	request.Header.Set("Authorization", "Bearer embedding-user-key")
	request.Header.Set("X-GoodHR-Billing", "1")
	response := httptest.NewRecorder()
	service.CompatibleChat(response, request)
	var payload struct {
		Billing aiBillingReceipt `json:"goodhr_billing"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.Billing.Status != "unavailable" || payload.Billing.CostUnits != nil || calls != 2 {
		t.Fatalf("calls=%d payload=%+v body=%s", calls, payload, response.Body.String())
	}
	records, _, _ := wallet.ListRecords("embedding@example.com", 10, 0)
	usages := 0
	for _, record := range records {
		if record.Category == "ai_usage" {
			usages++
		}
	}
	if usages != 1 {
		t.Fatalf("未知费用被伪造成已扣费：%+v", records)
	}
}

// TestServerGuardStopsNextAttempt 验证任务取消或引用撤权后，不会把上一轮业务资料再次送给模型。
func TestServerGuardStopsNextAttempt(t *testing.T) {
	checks, calls := 0, 0
	blocked := errors.New("合成权限已撤回")
	ctx := withServerAIGuard(context.Background(), func() error {
		checks++
		if checks > 1 {
			return blocked
		}
		return nil
	})
	request, _ := http.NewRequestWithContext(ctx, "POST", "https://upstream.example.com/v1/chat/completions", strings.NewReader(`{}`))
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("合成网络中断") })}
	_, err := doServerAIRequest(client, request)
	if !errors.Is(err, blocked) || calls != 1 || checks != 2 {
		t.Fatalf("checks=%d calls=%d err=%v", checks, calls, err)
	}
}
