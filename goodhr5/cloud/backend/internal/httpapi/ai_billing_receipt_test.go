// Package httpapi 本文件使用合成上游核对 AI 回执与钱包实际扣费、流式顺序及旧客户端兼容性，不调用付费服务。
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// billingFailureWallet 模拟只有实际消费写账失败的情况，余额与身份查询仍走原内存钱包。
type billingFailureWallet struct{ AIWalletStore }

// AdjustBalance 拒绝消费写账，以验证客户端不会收到伪造的扣费金额。
func (w billingFailureWallet) AdjustBalance(record AIWalletRecord) (int64, error) {
	return 0, errors.New("模拟扣费记录保存失败")
}

// TestCompatibleChatBillingMatchesWallet 验证普通及流式回执与余额实际变化一致，旧客户端保留原响应。
func TestCompatibleChatBillingMatchesWallet(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		stream, optIn, fail     bool
	}{
		{"json", "application/json", `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":181,"completion_tokens":4361}}`, false, true, false},
		{"stream_json_fallback", "application/json", `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":181,"completion_tokens":4361}}`, true, true, false},
		{"stream", "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":181,\"completion_tokens\":4361}}\n\ndata: [DONE]\n\n", true, true, false},
		{"legacy", "text/event-stream", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":181,\"completion_tokens\":4361}}\n\ndata: [DONE]\n\n", true, false, false},
		{"missing_usage", "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n", true, true, false},
		{"ledger_failure", "text/event-stream", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":181,\"completion_tokens\":4361}}\n\ndata: [DONE]\n\n", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, wallet := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			if tc.fail {
				service.wallet = billingFailureWallet{service.wallet}
			}
			requestBody, _ := json.Marshal(struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}{"first-model", tc.stream})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(requestBody)))
			req.Header.Set("Authorization", "Bearer embedding-user-key")
			if tc.optIn {
				req.Header.Set("X-GoodHR-Billing", "1")
			}
			response := httptest.NewRecorder()
			service.CompatibleChat(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("响应失败：%d %s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			if !tc.optIn {
				if body != tc.body || response.Header().Get("X-GoodHR-Billing") != "" {
					t.Fatal("旧客户端响应被改变")
				}
				return
			}
			if response.Header().Get("X-GoodHR-Billing") != "1" {
				t.Fatal("缺少回执协议标识")
			}
			var payload struct {
				Billing aiBillingReceipt `json:"goodhr_billing"`
			}
			if tc.contentType == "text/event-stream" {
				if strings.Count(body, "data: [DONE]") != 1 || strings.Index(body, "goodhr_billing") > strings.Index(body, "data: [DONE]") {
					t.Fatal("扣费回执必须在唯一结束标记之前返回")
				}
				for _, line := range strings.Split(body, "\n") {
					if strings.Contains(line, "goodhr_billing") {
						if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
							t.Fatal(err)
						}
					}
				}
			} else if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			balance, _ := wallet.BalanceUnits("embedding@example.com")
			if tc.fail || tc.name == "missing_usage" {
				if payload.Billing.Status == "charged" || payload.Billing.CostUnits != nil || balance != 100000 {
					t.Fatalf("未扣费却返回金额：%+v balance=%d", payload.Billing, balance)
				}
			} else if payload.Billing.Status != "charged" || payload.Billing.CostUnits == nil || *payload.Billing.CostUnits != 100000-balance || *payload.Billing.CostUnits <= 0 {
				t.Fatalf("回执与真实扣费不一致：%+v balance=%d", payload.Billing, balance)
			}
		})
	}
}
