// Package preflight 文件作用：验证启动前检查会按任务需要使用云端业务能力和本地平台配置。
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

// TestLoadPlatformUsesBundledConfig 验证平台配置无需云端客户端也能从本地加载。
func TestLoadPlatformUsesBundledConfig(t *testing.T) {
	checker := &Checker{}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "greeting"},
		Position: cloud.PositionSnapshot{PlatformID: "zhaopin"},
	}
	if err := checker.loadPlatform(context.Background(), prepared); err != nil {
		t.Fatalf("读取智联本地平台配置失败：%v", err)
	}
	selector := prepared.Platform.Selectors["position.open"]
	if len(selector.Target.Selectors) == 0 ||
		selector.Target.Selectors[0].Value != "a[zp-stat-id='talent_more_jobs']" {
		t.Fatalf("启动前检查没有使用智联本地选择器：%+v", selector)
	}
}
