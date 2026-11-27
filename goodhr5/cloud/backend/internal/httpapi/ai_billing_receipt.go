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
func (s *AIWalletService) settleAIUsage(email string, model builtinAIModel, prompt, completion int, funding ...aiUsageFunding) (aiBillingReceipt, error) {
	receipt := aiBillingReceipt{Status: "unavailable", Model: model.ID}
	if prompt+completion == 0 {
		return receipt, nil
	}
	if err := s.chargeAIUsage(email, model, prompt, completion, funding...); err != nil {
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

// settleFailedAIResponse 保留无效模型回复已经发生的真实费用；没有可靠 usage 时不伪造金额。
func (s *AIWalletService) settleFailedAIResponse(email string, model builtinAIModel, response *http.Response, raw []byte, funding ...aiUsageFunding) error {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	result, _ := readAIWire(raw)
	if result.Prompt < 0 || result.Completion < 0 {
		return assistantFatalError{"AI用量不正确，已停止重试"}
	}
	if _, err := s.settleAIUsage(email, model, result.Prompt, result.Completion, funding...); err != nil {
		return assistantFatalError{"AI扣费记录未保存，已停止重试，请先核对账本"}
	}
	return nil
}

// mergeAIRetryBilling 合并已确认的重试费用；有未知费用时不能声称总额已经确认。
func mergeAIRetryBilling(receipt aiBillingReceipt, prior int64, unknown bool) aiBillingReceipt {
	if receipt.Status == "failed" {
		return receipt
	}
	if unknown || receipt.Status != "charged" || receipt.CostUnits == nil {
		receipt.Status = "unavailable"
		receipt.CostUnits = nil
		return receipt
	}
	total, err := checkedAIWalletAdd(*receipt.CostUnits, prior)
	if err != nil {
		receipt.Status = "unavailable"
		receipt.CostUnits = nil
		return receipt
	}
	receipt.CostUnits = &total
	return receipt
}
