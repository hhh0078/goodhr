// Package shared 本文件为 AI 请求绑定明确的任务、岗位、候选人与分析阶段，复用现有岗位日志链路。
package shared

import (
	"context"

	"goodhr5/local-agent-go-new/internal/integration/ai"
)

// AIBillingLogger 接收调用现场的费用回执，不能依赖当前正在展示的候选人推断归属。
type AIBillingLogger interface {
	AIBilling(taskID, positionID, candidateName, stage string, billing ai.Billing)
}

// WithAIBilling 在请求现场固定日志归属；旧日志器无需实现费用接口，仍保持兼容。
func WithAIBilling(ctx context.Context, logger Logger, taskID, positionID, candidateName, stage string) context.Context {
	if target, ok := logger.(AIBillingLogger); ok {
		return ai.WithBillingReporter(ctx, func(billing ai.Billing) {
			target.AIBilling(taskID, positionID, candidateName, stage, billing)
		})
	}
	return ctx
}
