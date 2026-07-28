// Package boss 文件作用：验证 Boss 候选人详情中的平台附加内容不会进入判断文本。
package boss

import "testing"

// TestCleanCandidateDetailText 验证牛人分析器之后的内容会被移除。
func TestCleanCandidateDetailText(t *testing.T) {
	raw := "解婷 25岁 大专 工作经历 主播\n牛人分析器\nVIP专享 同类牛人\n平台隐私声明"
	cleaned := NewRuntime().CleanCandidateDetailText(raw)
	if cleaned != "解婷 25岁 大专 工作经历 主播" {
		t.Fatalf("Boss 详情清理结果不正确：%q", cleaned)
	}
}
