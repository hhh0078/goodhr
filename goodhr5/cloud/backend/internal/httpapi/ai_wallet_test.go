// 本文件负责测试内置 AI 钱包扣费精度和 token 用量计费。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAIWalletSummaryReturnsTenYuanDefaultRecharge 验证钱包摘要返回 10 元默认充值金额。
func TestAIWalletSummaryReturnsTenYuanDefaultRecharge(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "wallet-default@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/ai-wallet", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("summary status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		DefaultRechargeCents int `json:"default_recharge_cents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.DefaultRechargeCents != 1000 {
		t.Fatalf("default recharge cents = %d, want 1000", payload.DefaultRechargeCents)
	}
}

// TestAIUsageCostUnitsUsesFourDecimalPrecision 验证 AI 扣费按 0.0001 元精度向上取整。
func TestAIUsageCostUnitsUsesFourDecimalPrecision(t *testing.T) {
	model := builtinAIModel{
		ID:                    "qwen3.5-plus",
		InputPricePer1MCents:  80,
		OutputPricePer1MCents: 480,
	}

	cost := aiUsageCostUnits(model, 181, 4361)
	if cost != 211 {
		t.Fatalf("cost units = %d, want 211", cost)
	}
	if got := aiUnitsToYuanString(cost); got != "0.0211" {
		t.Fatalf("cost yuan = %q, want 0.0211", got)
	}
}

// TestEnsureAIStreamUsage 验证流式请求会补充 token 用量返回开关。
func TestEnsureAIStreamUsage(t *testing.T) {
	body := ensureAIStreamUsage([]byte(`{"model":"qwen","stream":true}`))
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	options := mapFromAny(payload["stream_options"])
	if options["include_usage"] != true {
		t.Fatalf("include_usage = %v, want true", options["include_usage"])
	}
}

// TestAIUsageFromStreamLine 验证可以从 SSE 分片里读取 token 用量。
func TestAIUsageFromStreamLine(t *testing.T) {
	line := `data: {"choices":[],"usage":{"prompt_tokens":181,"completion_tokens":4361,"total_tokens":4542}}`
	promptTokens, completionTokens := aiUsageFromStreamLine(line)
	if promptTokens != 181 || completionTokens != 4361 {
		t.Fatalf("usage = %d/%d, want 181/4361", promptTokens, completionTokens)
	}
}
