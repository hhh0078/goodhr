// Package httpapi 文件作用：用合成上游验证每次请求证据、主备三项切换、停止边界与无凭据的诊断回执，不调用模型。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// aiAttemptRequestForTest 创建可重放的合成评分请求，模型和密钥均为测试值。
func aiAttemptRequestForTest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://primary.example.com/v1/chat/completions", strings.NewReader(`{"model":"primary-model","stream":true,"messages":[{"role":"system","content":"只返回JSON对象"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer primary-secret")
	return req
}

// aiAttemptResponseForTest 返回合成的 OpenAI 响应，避免测试依赖外部平台。
func aiAttemptResponseForTest(status int, content string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"provider-safe-id"}}, Body: io.NopCloser(strings.NewReader(serverAIEnvelope(content, false)))}
}

// TestServerFallbackSwitchesAllRequestFields 验证可重试错误之后同时切换备用地址、模型及密钥，并保留原请求合同。
func TestServerFallbackSwitchesAllRequestFields(t *testing.T) {
	for _, kind := range []string{"429", "503", "invalid-json", "error-block"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			var events []serverAIAttemptEvent
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				var body struct {
					Model    string `json:"model"`
					Stream   bool   `json:"stream"`
					Messages []any  `json:"messages"`
				}
				if json.NewDecoder(req.Body).Decode(&body) != nil || !body.Stream || len(body.Messages) != 1 {
					t.Fatal("原请求合同丢失")
				}
				if calls == 1 {
					if req.URL.Host != "primary.example.com" || req.Header.Get("Authorization") != "Bearer primary-secret" || body.Model != "primary-model" {
						t.Fatal("主用三项不一致")
					}
					switch kind {
					case "429":
						return aiAttemptResponseForTest(429, `{}`), nil
					case "503":
						return aiAttemptResponseForTest(503, `{}`), nil
					case "error-block":
						response := aiAttemptResponseForTest(200, `{}`)
						response.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"private-error-value"}}`))
						return response, nil
					default:
						return aiAttemptResponseForTest(200, `{broken`), nil
					}
				}
				if req.URL.String() != "https://fallback.example.com/v1/chat/completions" || req.Header.Get("Authorization") != "Bearer fallback-secret" || body.Model != "fallback-model" || req.ContentLength <= 0 {
					t.Fatal("备用三项或正文长度未一起切换")
				}
				return aiAttemptResponseForTest(200, `{"score":80,"reason":"ok"}`), nil
			})}
			fallback := &BuiltinAIFallbackConfig{Enabled: true, BaseURL: " https://fallback.example.com/v1 ", APIKey: " fallback-secret ", Model: " fallback-model "}
			response, err := doServerAIRequest(client, aiAttemptRequestForTest(t, context.Background()), serverAIRequestOptions{Fallback: fallback, ClientRequestID: "client-1", AnalysisGroupID: "group-1", Observe: func(e serverAIAttemptEvent) { events = append(events, e) }})
			if err != nil || response == nil || calls != 2 || len(events) != 4 {
				t.Fatalf("calls=%d events=%d err=%v", calls, len(events), err)
			}
			response.Body.Close()
			first, last := events[1], events[3]
			if first.Event != "end" || first.Provider != "primary" || !first.WillRetry || first.BackoffMS != 500 || first.Result != "failed" || last.Provider != "fallback" || last.Model != "fallback-model" || last.ServerAttempt != 2 || last.Result != "success" || last.WillRetry {
				t.Fatalf("重试证据错误：%+v %+v", first, last)
			}
			for _, e := range events {
				if e.ClientRequestID != "client-1" || e.AnalysisGroupID != "group-1" || e.MaxAttempts != 4 {
					t.Fatal("关联或次数丢失")
				}
			}
			if first.ProviderRequestID != "provider-safe-id" || first.FinishedAt == "" || first.StartedAt == "" || first.ElapsedMS < 0 {
				t.Fatal("开始结束或平台ID丢失")
			}
		})
	}
}

// TestServerFallbackKeepsLegacyAndStopping 验证未配置、关闭、缺项和非重试状态不会改变旧主用或取消行为。
func TestServerFallbackKeepsLegacyAndStopping(t *testing.T) {
	for _, fallback := range []*BuiltinAIFallbackConfig{nil, {Enabled: false, BaseURL: "https://fallback.example.com", APIKey: "backup", Model: "backup"}, {Enabled: true, BaseURL: "https://fallback.example.com", APIKey: "", Model: "backup"}, {Enabled: true, BaseURL: "https://fallback.example.com?key=secret", APIKey: "backup", Model: "backup"}} {
		t.Run("legacy", func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Host != "primary.example.com" || req.Header.Get("Authorization") != "Bearer primary-secret" {
					t.Fatal("未启用的备用被使用")
				}
				if calls == 1 {
					return aiAttemptResponseForTest(503, `{}`), nil
				}
				return aiAttemptResponseForTest(200, `{"score":80}`), nil
			})}
			resp, err := doServerAIRequest(client, aiAttemptRequestForTest(t, context.Background()), serverAIRequestOptions{Fallback: fallback})
			if err != nil || calls != 2 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			resp.Body.Close()
		})
	}
	fallback := &BuiltinAIFallbackConfig{Enabled: true, BaseURL: "https://fallback.example.com", APIKey: "backup", Model: "backup"}
	for _, status := range []int{400, 401, 402, 403, 404, 422} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return aiAttemptResponseForTest(status, `{}`), nil
			})}
			resp, err := doServerAIRequest(client, aiAttemptRequestForTest(t, context.Background()), serverAIRequestOptions{Fallback: fallback})
			if err != nil || calls != 1 || resp.StatusCode != status {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			resp.Body.Close()
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unused") })}
	_, err := doServerAIRequest(client, aiAttemptRequestForTest(t, ctx), serverAIRequestOptions{Fallback: fallback})
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("已取消仍请求")
	}
}

// TestCompatibleChatAttemptDetailsSurviveSuccess 验证最终成功的回执仍保留前次失败，拒绝上游伪造诊断且不包含密钥或模型正文。
func TestCompatibleChatAttemptDetailsSurviveSuccess(t *testing.T) {
	calls := 0
	service, _ := embeddingFixture(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			response := aiAttemptResponseForTest(200, `{private-resume-broken`)
			response.Header.Set("X-Request-Id", "upstream-secret")
			return response, nil
		}
		resp := aiAttemptResponseForTest(200, `{"score":80,"reason":"ok"}`)
		resp.Header.Set(aiAttemptDetailsHeader, `[{"forged":"fallback-secret"}]`)
		resp.Header.Set("X-Request-Id", "fallback-secret")
		resp.Header.Set("Unknown-Key", "must-not-be-in-diagnostic")
		return resp, nil
	}))
	cfg := builtinAIPolicyConfigForTest()
	cfg.Fallback = &BuiltinAIFallbackConfig{Enabled: true, BaseURL: "https://fallback.example.com", APIKey: "fallback-secret", Model: "fallback-model"}
	saveBuiltinAIConfigForTest(t, service.systemConfigs, cfg)
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	request := httptest.NewRequest(http.MethodPost, "/api/ai-compatible/v1/chat/completions", strings.NewReader(`{"model":"first-model","messages":[{"role":"system","content":"只返回JSON"}]}`))
	request.Header.Set("Authorization", "Bearer embedding-user-key")
	request.Header.Set(aiDiagnosticHeader, "1")
	request.Header.Set(aiClientRequestIDHeader, "client-1")
	request.Header.Set(aiAnalysisGroupIDHeader, "group-1")
	writer := httptest.NewRecorder()
	service.CompatibleChat(writer, request)
	var details []serverAIAttemptEvent
	if writer.Code != 200 || json.Unmarshal([]byte(writer.Header().Get(aiAttemptDetailsHeader)), &details) != nil || len(details) != 2 || calls != 2 {
		t.Fatalf("status=%d calls=%d details=%d", writer.Code, calls, len(details))
	}
	if writer.Header().Get("X-Request-ID") != "" {
		t.Fatal("普通请求标识响应头泄露密钥")
	}
	if details[0].Reason != "model_json_invalid" || details[1].Result != "success" || details[1].Provider != "fallback" || details[1].ProviderRequestID != "" || details[0].ProviderRequestID != "" {
		t.Fatal("失败历史或安全平台标识错误")
	}
	for _, secret := range []string{"embedding-user-key", "upstream-secret", "fallback-secret", "private-resume-broken", "unsafe-secret", "must-not-be-in-diagnostic"} {
		if strings.Contains(writer.Header().Get(aiAttemptDetailsHeader)+output.String(), secret) {
			t.Fatal("安全诊断泄露敏感内容")
		}
	}
	if !strings.Contains(output.String(), `"event":"start"`) || !strings.Contains(output.String(), `"event":"end"`) {
		t.Fatal("服务端未记录开始结束")
	}
}

// TestAttemptDetailsCancellationAndLegacyNegotiation 验证退避取消不虚增请求次数，未协商客户端没有新增诊断头。
func TestAttemptDetailsCancellationAndLegacyNegotiation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := httptest.NewRecorder()
	request := aiAttemptRequestForTest(t, ctx)
	request.Header.Set(aiDiagnosticHeader, "1")
	details := newAIAttemptDetailsWriter(writer, request)
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		return aiAttemptResponseForTest(503, `{}`), nil
	})}
	_, err := doServerAIRequest(client, request, serverAIRequestOptions{Observe: details.observe})
	details.stop(err)
	if !errors.Is(err, context.Canceled) || calls != 1 || len(details.attempts) != 1 || details.attempts[0].WillRetry || details.attempts[0].StopReason != "cancelled" {
		t.Fatal("取消证据不正确")
	}
	legacy := httptest.NewRecorder()
	newAIAttemptDetailsWriter(legacy, httptest.NewRequest("POST", "/", nil)).observe(serverAIAttemptEvent{Event: "end"})
	if legacy.Header().Get(aiAttemptDetailsHeader) != "" {
		t.Fatal("旧客户端协议被改变")
	}
}

// TestServerFallbackKeepsFourAttemptBudget 验证第二至第四次均使用同一完整备用，且总次数和失败分类保持原规则。
func TestServerFallbackKeepsFourAttemptBudget(t *testing.T) {
	calls := 0
	var ended []serverAIAttemptEvent
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(req.Body).Decode(&body)
			if req.URL.Host != "fallback.example.com" || req.Header.Get("Authorization") != "Bearer backup-secret" || body.Model != "backup-model" {
				t.Fatal("后续重试没有继续使用完整备用")
			}
		}
		return aiAttemptResponseForTest(200, `{broken`), nil
	})}
	fallback := &BuiltinAIFallbackConfig{Enabled: true, BaseURL: "https://fallback.example.com", APIKey: "backup-secret", Model: "backup-model"}
	_, err := doServerAIRequest(client, aiAttemptRequestForTest(t, context.Background()), serverAIRequestOptions{Fallback: fallback, Observe: func(e serverAIAttemptEvent) {
		if e.Event == "end" {
			ended = append(ended, e)
		}
	}})
	var failure *serverAIRequestError
	if !errors.As(err, &failure) || failure.Attempts != 4 || failure.Reason != "model_json_invalid" || calls != 4 || len(ended) != 4 || ended[3].WillRetry || ended[3].BackoffMS != 0 {
		t.Fatalf("calls=%d ends=%d err=%v", calls, len(ended), err)
	}
	for i, e := range ended {
		if e.ServerAttempt != i+1 || e.Reason != "model_json_invalid" {
			t.Fatal("真实请求次数或原因丢失")
		}
	}
}

// TestAIProviderRequestIDOmitsKnownCredentials 验证合法字符的回显密钥及含密钥的标识仍被省略，正常不透明请求ID保留。
func TestAIProviderRequestIDOmitsKnownCredentials(t *testing.T) {
	for _, value := range []string{"sk-primary-fixture", "sk-fallback-fixture", "req-sk-fallback-fixture-suffix"} {
		header := http.Header{}
		header.Set("X-Request-ID", value)
		if aiProviderRequestID(header, "sk-primary-fixture", "sk-fallback-fixture") != "" {
			t.Fatal("上游回显的实际凭据进入了请求标识")
		}
	}
	header := http.Header{}
	header.Set("X-Request-ID", "opaque-req-123")
	if aiProviderRequestID(header, "sk-primary-fixture", "sk-fallback-fixture") != "opaque-req-123" {
		t.Fatal("正常平台标识丢失")
	}
}
