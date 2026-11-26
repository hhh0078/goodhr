// Package cloudruntime 文件作用：向云端已有运行入口公开安全AI请求元数据及统一摘要，不暴露请求正文或内部AI客户端。
package cloudruntime

import "goodhr5/local-agent-go-new/internal/integration/ai"

// AIRequestAttempt 复用本地与服务器协商的同一元数据结构，避免投影时另造字段合同。
type AIRequestAttempt = ai.RequestAttempt

// AIRequestAttemptMessage 返回已有中文请求摘要，失败只包含已脱敏的服务错误说明，不包含模型输入。
func AIRequestAttemptMessage(event AIRequestAttempt) string { return ai.RequestAttemptMessage(event) }

// NormalizeAIRequestAttempt 复用逐次日志的错误字段边界，账本投影不能透传未清理的附加正文。
func NormalizeAIRequestAttempt(event AIRequestAttempt) AIRequestAttempt {
	return ai.NormalizeRequestAttempt(event)
}
