// Package preflight 文件作用：验证免费关键词与 OCR 任务不会被误判为必须使用 AI 或会员能力。
package preflight

import (
	"context"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// TestFreeKeywordOCRSkipsAIAndSubscription 验证只使用关键词和 OCR 时不访问 AI 或会员客户端。
func TestFreeKeywordOCRSkipsAIAndSubscription(t *testing.T) {
	checker := &Checker{}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "greeting"},
		Position: cloud.PositionSnapshot{RequiresAI: false, RequiresOCR: true},
	}
	if err := checker.checkSubscription(context.Background(), prepared); err != nil {
		t.Fatalf("免费关键词 OCR 任务不应检查会员：%v", err)
	}
	if err := checker.checkAI(context.Background(), prepared); err != nil {
		t.Fatalf("免费关键词 OCR 任务不应检查 AI：%v", err)
	}
}
