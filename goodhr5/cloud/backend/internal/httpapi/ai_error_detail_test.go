// 本文件用合成服务验证503实际错误、逐次详情、最终新旧协议及隐私边界，不调用真实模型。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAIErrorDetailHTTPFailureBudget 验证真实四次失败均保留不同的上游原因，旧版424与新版422都不会叠加重试。
func TestAIErrorDetailHTTPFailureBudget(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprintf("modern=%t", modern), func(t *testing.T) {
			calls := 0
			service, _ := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				body := fmt.Sprintf(`{"error":{"message":"failure %d: no available channel for model, upstream-secret","code":"CHANNEL_UNAVAILABLE","type":"capacity"},"messages":"PRIVATE_REQUEST"}`, calls)
				return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))
			request := httptest.NewRequest(http.MethodPost, "/api/ai-compatible/v1/chat/completions", strings.NewReader(`{"model":"first-model"}`))
			request.Header.Set("Authorization", "Bearer embedding-user-key")
			if modern {
				request.Header.Set(aiDiagnosticHeader, "1")
				request.Header.Set("X-GoodHR-AI-Error-Details", "1")
			}
			response := httptest.NewRecorder()
			service.CompatibleChat(response, request)
			wantStatus := 424
			if modern {
				wantStatus = 422
			}
			var body struct {
				Error, Code string
				Count       int `json:"attempt_count"`
				Status      int `json:"upstream_status"`
				Attempts    []serverAIAttemptEvent
			}
			if response.Code != wantStatus || calls != 4 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Count != 4 || body.Status != 503 || len(body.Attempts) != 4 || body.Code != "AI_RETRY_EXHAUSTED" {
				t.Fatalf("预算或协议不符 status=%d calls=%d", response.Code, calls)
			}
			if !strings.Contains(body.Error, "failure 4") || !strings.Contains(body.Error, "503") || strings.Contains(response.Body.String(), "upstream-secret") || strings.Contains(response.Body.String(), "PRIVATE_REQUEST") {
				t.Fatal("实际末次原因丢失或泄露私有内容")
			}
			preview := []rune(response.Body.String())
			if len(preview) > 500 {
				preview = preview[:500]
			}
			if !strings.HasPrefix(response.Body.String(), `{"error":`) || !strings.Contains(string(preview), "failure 4") || !strings.Contains(string(preview), "503") {
				t.Fatal("旧版前500字摘要没有首先显示实际终止原因")
			}
			for i, attempt := range body.Attempts {
				if attempt.ServerAttempt != i+1 || attempt.ErrorCode != "CHANNEL_UNAVAILABLE" || !strings.Contains(attempt.ErrorDetail, fmt.Sprintf("failure %d", i+1)) || attempt.ResponseBytes == 0 || attempt.WillRetry != (i < 3) {
					t.Fatalf("第%d次详情不完整", i+1)
				}
			}
			if modern && response.Header().Get(aiAttemptDetailsHeader) == "" || !modern && response.Header().Get(aiAttemptDetailsHeader) != "" {
				t.Fatal("诊断协商边界错误")
			}
		})
	}
}

// TestAIErrorDetailSourcesAndRedaction 验证标准错误、HTML、纯文本与SSE只保留错误，不保留成功正文或请求回显。
func TestAIErrorDetailSourcesAndRedaction(t *testing.T) {
	fixtures := []struct {
		name, kind, raw, reason, want, forbidden string
		status                                   int
	}{
		{"json", "application/json", `{"error":{"message":"channel unavailable","code":"NO_CHANNEL"},"choices":[{"message":{"content":"PRIVATE_RESUME"}}]}`, "http_status", "channel unavailable", "PRIVATE_RESUME", 503},
		{"html", "text/html", `<html><title>Application failed to respond</title><body>PRIVATE_RESUME<script>PRIVATE_SCRIPT</script></body></html>`, "http_status", "Application failed to respond", "PRIVATE_", 503},
		{"plain", "text/plain", "Service Unavailable\nPRIVATE_RESUME", "http_status", "Service Unavailable", "PRIVATE_RESUME", 503},
		{"sse", "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"PRIVATE_RESUME\"}}]}\n\ndata: {\"error\":{\"message\":\"capacity exhausted\",\"code\":\"BUSY\"}}\n", "upstream_error", "capacity exhausted", "PRIVATE_RESUME", 200},
		{"model-invalid", "application/json", serverAIEnvelope("PRIVATE_RESUME is not json", false), "model_json_invalid", "JSON", "PRIVATE_RESUME", 200},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			event := serverAIAttemptEvent{}
			response := &http.Response{StatusCode: fixture.status, Header: http.Header{"Content-Type": {fixture.kind}}}
			fillAIErrorDetail(&event, response, []byte(fixture.raw), nil, fixture.reason, "json", nil)
			if !strings.Contains(event.ErrorDetail, fixture.want) || strings.Contains(event.ErrorDetail, fixture.forbidden) || event.ResponseBytes != int64(len(fixture.raw)) {
				t.Fatalf("错误来源未被正确提取：%+v", event)
			}
		})
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"{\"name\":\"PRIVATE_NAME\",\"phone\":\"13912345678\",\"content\":\"PRIVATE_CHAT\"}"}]}]}`)
	text := "Error: PRIVATE_NAME PRIVATE_CHAT 13912345678 key=primary-secret https://user:pwd@example.com/path?token=PRIVATE_QUERY\nAuthorization: Basic PRIVATE_BASIC\nCookie: session=PRIVATE_COOKIE"
	safe := redactAIErrorText(text, payload, "primary-secret")
	for _, forbidden := range []string{"PRIVATE_", "13912345678", "primary-secret", "user:pwd"} {
		if strings.Contains(safe, forbidden) {
			t.Fatalf("错误脱敏遗漏 %s", forbidden)
		}
	}
	for _, content := range []string{"PRIVATE_NAME 13912345678 PRIVATE_RESUME", "PRIVATE_NAME contact@example.com PRIVATE_RESUME"} {
		payload, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"content": content}}})
		if strings.Contains(redactAIErrorText("Error: "+content, payload), "PRIVATE_") {
			t.Fatal("联系方式先脱敏破坏了完整请求回显匹配")
		}
	}
}

// TestAIErrorDetailHeaderBudget 保证长中文、引号与HTML字符不挤掉四次记录；最终正文仍保留更详细的安全内容。
func TestAIErrorDetailHeaderBudget(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set(aiDiagnosticHeader, "1")
	writer := newAIAttemptDetailsWriter(response, request)
	for i := 1; i <= 4; i++ {
		detail, _ := clipAIErrorText(strings.Repeat("错误<>\"详细原因", 500), 4096)
		writer.observe(serverAIAttemptEvent{Layer: "server", Event: "end", ServerAttempt: i, MaxAttempts: 4, Provider: "primary", Model: strings.Repeat("<", 200), ClientRequestID: strings.Repeat("c", 128), AnalysisGroupID: strings.Repeat("g", 128), ProviderRequestID: strings.Repeat("p", 128), ErrorDetail: detail, HTTPStatus: 503})
	}
	header := response.Header().Get(aiAttemptDetailsHeader)
	var events []serverAIAttemptEvent
	if header == "" || len(header) > 8<<10 || json.Unmarshal([]byte(header), &events) != nil || len(events) != 4 {
		t.Fatal("长错误使逐次响应头丢失")
	}
	for i, event := range events {
		if event.ServerAttempt != i+1 || !event.ErrorDetailTruncated || len(event.ErrorDetail) > 1024 || !utf8.ValidString(event.ErrorDetail) || len(writer.attempts[i].ErrorDetail) < 4000 {
			t.Fatal("响应头裁剪未标记或污染完整错误记录")
		}
	}
	final := aiFinalFailureBody(&serverAIRequestError{Attempts: 4, Reason: "http_status"}, writer.attempts)
	if len(final["attempts"].([]serverAIAttemptEvent)) != 4 || final["attempt_count"] != 4 {
		t.Fatal("最终详情未保留所有尝试")
	}
}

// TestAIErrorDetailTerminalReason 验证结算停止和用户取消显示真实终止原因，不被前一次503错误冒充。
func TestAIErrorDetailTerminalReason(t *testing.T) {
	attempts := []serverAIAttemptEvent{{HTTPStatus: 503, ErrorDetail: "Service Unavailable"}}
	for _, item := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "取消"},
		{context.DeadlineExceeded, "时间已到"},
		{&serverAIRequestError{Attempts: 1, Reason: "billing_failed"}, "结算失败"},
	} {
		body := aiFinalFailureBody(item.err, attempts)
		if !strings.Contains(body["error"].(string), item.want) {
			t.Fatal("终止原因被上次失败覆盖")
		}
	}
}
