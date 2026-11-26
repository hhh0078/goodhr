// 本文件用合成主备上游验证非流式请求、旧客户端 SSE/JSON 兼容和计费，不调用付费模型。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuiltinNonstreamPreservesClientContract 验证真实主备均关闭流，图片和思考参数保留，旧客户端继续获得合法结果与账单。
func TestBuiltinNonstreamPreservesClientContract(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			calls := 0
			service, _ := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var p map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Fatal(err)
				}
				if string(p["stream"]) != "false" || p["stream_options"] != nil || string(p["enable_thinking"]) != "false" || !strings.Contains(string(p["messages"]), "data:image/png;base64,AAAA") {
					t.Fatal("upstream mode or message changed")
				}
				if calls == 1 {
					return aiAttemptResponseForTest(503, `{}`), nil
				}
				if r.URL.Host != "backup.example.com" || r.Header.Get("Authorization") != "Bearer backup-secret" || string(p["model"]) != `"backup-model"` {
					t.Fatal("fallback configuration lost")
				}
				return aiAttemptResponseForTest(200, `{"score":88,"reason":"ok","resume":{"education":"保留完整结果"}}`), nil
			}))
			cfg := builtinAIPolicyConfigForTest()
			cfg.Fallback = &BuiltinAIFallbackConfig{Enabled: true, BaseURL: "https://backup.example.com/v1", APIKey: "backup-secret", Model: "backup-model"}
			saveBuiltinAIConfigForTest(t, service.systemConfigs, cfg)
			body := `{"model":"first-model","stream":` + map[bool]string{false: "false", true: "true"}[stream] + `,"stream_options":{"include_usage":true},"enable_thinking":false,"messages":[{"role":"system","content":"只返回JSON"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`
			req := httptest.NewRequest("POST", "/api/ai-compatible/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer embedding-user-key")
			req.Header.Set(aiDiagnosticHeader, "1")
			req.Header.Set("X-GoodHR-Billing", "1")
			w := httptest.NewRecorder()
			service.CompatibleChat(w, req)
			if calls != 2 || w.Code != 200 || !strings.Contains(w.Body.String(), "保留完整结果") || !strings.Contains(w.Body.String(), "goodhr_billing") {
				t.Fatalf("calls=%d code=%d body=%s", calls, w.Code, w.Body.String())
			}
			var events []serverAIAttemptEvent
			if err := json.Unmarshal([]byte(w.Header().Get(aiAttemptDetailsHeader)), &events); err != nil || len(events) != 2 {
				t.Fatalf("events=%+v err=%v", events, err)
			}
			for _, e := range events {
				if e.UpstreamStream == nil || *e.UpstreamStream {
					t.Fatal("mode evidence missing")
				}
			}
			if stream {
				if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") || strings.Count(w.Body.String(), "data: [DONE]") != 1 {
					t.Fatal("SSE contract lost")
				}
				r, err := readAIWire(w.Body.Bytes())
				if err != nil || !strings.Contains(r.Content, "score") || r.Prompt != 10 {
					t.Fatalf("result=%+v err=%v", r, err)
				}
			} else if !json.Valid(w.Body.Bytes()) {
				t.Fatal("JSON contract lost")
			}
		})
	}
}

// TestCompletedJSONStreamToolCalls 验证工具调用索引、模型字段及用量保留，并去掉伪造的代理私有字段。
func TestCompletedJSONStreamToolCalls(t *testing.T) {
	raw := []byte(`{"id":"response-1","model":"m","object":"chat.completion","goodhr_billing":{"status":"forged"},"GOODHR_upstream_failure":{},"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	body, err := completedAIJSONStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	r, err := readAIWire(body)
	if err != nil || len(r.Arguments) != 1 || r.Arguments[0] != `{"id":1}` || r.Prompt != 4 || r.Completion != 2 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if strings.Contains(strings.ToLower(string(body)), "goodhr_") || !strings.Contains(string(body), `"index":0`) {
		t.Fatal("private fields or tool indexes invalid")
	}
	for _, bad := range []string{`{broken`, `{"choices":[{}]}`} {
		if _, err := completedAIJSONStream([]byte(bad)); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
}

// TestNonstreamRewriteInvalidInput 验证非法或空请求不崩溃，普通非聊天重试路径保持原有行为。
func TestNonstreamRewriteInvalidInput(t *testing.T) {
	for _, raw := range []string{`null`, `bad`, `[]`} {
		if string(disableAIUpstreamStream([]byte(raw))) != raw {
			t.Fatal("invalid input modified")
		}
	}
}
