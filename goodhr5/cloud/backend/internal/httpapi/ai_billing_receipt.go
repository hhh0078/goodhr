// Package httpapi 本文件为主动请求费用回执的客户端返回内置 AI 实际扣费结果，不改变旧客户端响应协议。
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// aiBillingReceipt 表示一笔 AI 调用的扣费确认，金额单位为 0.0001 元；未确认时金额留空。
type aiBillingReceipt struct {
	Status    string `json:"status"`
	CostUnits *int64 `json:"cost_units,omitempty"`
	Model     string `json:"model"`
}

// settleAIUsage 复用钱包扣费方法，只在真实扣费成功后生成含金额的回执。
func (s *AIWalletService) settleAIUsage(email string, model builtinAIModel, prompt, completion int) (aiBillingReceipt, error) {
	receipt := aiBillingReceipt{Status: "unavailable", Model: model.ID}
	if prompt+completion == 0 {
		return receipt, nil
	}
	if err := s.chargeAIUsage(email, model, prompt, completion); err != nil {
		receipt.Status = "failed"
		return receipt, err
	}
	cost := aiUsageCostUnits(model, prompt, completion)
	receipt.Status, receipt.CostUnits = "charged", &cost
	return receipt, nil
}

// writeAIBillingStream 在上游结束后发送扣费回执，再发送标准结束标记，避免客户端提前结束读取。
func writeAIBillingStream(w http.ResponseWriter, receipt aiBillingReceipt) {
	payload, _ := json.Marshal(struct {
		Choices []struct{}       `json:"choices"`
		Billing aiBillingReceipt `json:"goodhr_billing"`
	}{Choices: []struct{}{}, Billing: receipt})
	_, _ = fmt.Fprintf(w, "\ndata: %s\n\ndata: [DONE]\n\n", payload)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// withAIBillingJSON 在普通 JSON 响应中补充回执；无法解析时保留原响应，客户端显示金额未确认。
func withAIBillingJSON(body []byte, receipt aiBillingReceipt) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return body
	}
	fields["goodhr_billing"], _ = json.Marshal(receipt)
	result, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return result
}
