// Package ai 本文件读取单次 AI 调用的实际扣费回执，通过请求上下文通知日志，避免并发任务相互串账。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// Billing 表示本次调用的扣费状态，CostUnits 单位为 0.0001 元，未知金额必须留空。
type Billing struct {
	Status    string `json:"status"`
	CostUnits *int64 `json:"cost_units,omitempty"`
	Model     string `json:"model"`
}

type billingReporterKey struct{}

// WithBillingReporter 为当前调用链绑定独立回执接收方法；后台完整响应继续使用原上下文的归属。
func WithBillingReporter(ctx context.Context, report func(Billing)) context.Context {
	return context.WithValue(ctx, billingReporterKey{}, report)
}

// billingObserver 仅保存当前 HTTP 尝试的回执，既不保存密钥，也不保存候选人原始资料。
type billingObserver struct {
	diagnostic *aiAttempt
	billing    Billing
	report     func(Billing)
	enabled    bool
}

// newBillingObserver 在发送请求前确定缺少回执时的提示，内置旧版本不能误写成自定义接口或免费。
func newBillingObserver(ctx context.Context, apiKey string) *billingObserver {
	status := "external"
	if strings.HasPrefix(strings.TrimSpace(apiKey), "ghai_") {
		status = "unavailable"
	}
	report, _ := ctx.Value(billingReporterKey{}).(func(Billing))
	return &billingObserver{billing: Billing{Status: status}, report: report}
}

// observe 只接受已确认支持 GoodHR 回执的响应，忽略模型内容中的同名字段及不合法金额。
func (b *billingObserver) observe(data []byte) {
	if b != nil && b.diagnostic != nil && b.diagnostic.trustedResponse && bytes.Contains(data, []byte(`"goodhr_upstream_failure"`)) {
		var signal struct {
			Failure json.RawMessage `json:"goodhr_upstream_failure"`
		}
		if json.Unmarshal(data, &signal) == nil {
			b.diagnostic.proxySignal(signal.Failure)
		}
	}
	if b == nil || !b.enabled || !bytes.Contains(data, []byte(`"goodhr_billing"`)) {
		return
	}
	var response struct {
		Billing *Billing `json:"goodhr_billing"`
	}
	if json.Unmarshal(data, &response) != nil || response.Billing == nil {
		return
	}
	receipt := *response.Billing
	switch receipt.Status {
	case "charged":
		if receipt.CostUnits == nil || *receipt.CostUnits < 0 {
			return
		}
	case "unavailable", "failed":
		receipt.CostUnits = nil
	default:
		return
	}
	b.billing = receipt
}

// finish 在每次 HTTP 尝试结束时仅上报一次，失败、重试和后台尾部响应都不会漏掉已有扣费。
func (b *billingObserver) finish() {
	if b.report != nil {
		b.report(b.billing)
	}
}

// streamObservation 提取与当前 HTTP 尝试绑定的观测器，兼容独立解析和旧调用者。
func streamObservation(observers []*billingObserver) *aiAttempt {
	if len(observers) == 0 || observers[0] == nil {
		return nil
	}
	return observers[0].diagnostic
}
