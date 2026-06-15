// Package boss 测试 Boss 平台运行时的候选人解析规则。
package boss

import (
	"testing"

	"goodhr5/local-agent-go/internal/platformcore"
)

// TestCandidateFingerprintUsesOnlyNameAndAge 验证 Boss 候选人 ID 只由姓名和年龄决定。
func TestCandidateFingerprintUsesOnlyNameAndAge(t *testing.T) {
	runtime := NewRuntime()
	first := platformcore.Candidate{
		"candidate_name": "范召",
		"raw_text":       "范召 29岁 本科 5年 带货主播",
		"fields":         map[string]any{"name": "范召", "basic_info": "29岁 本科 5年 带货主播"},
	}
	second := platformcore.Candidate{
		"candidate_name": "范召",
		"raw_text":       "范召 29岁 大专 8年 直播运营",
		"fields":         map[string]any{"name": "范召", "basic_info": "29岁 大专 8年 直播运营"},
	}
	if runtime.CandidateFingerprint(first) != "boss_范召_29" {
		t.Fatalf("候选人 ID 应只包含姓名年龄：%s", runtime.CandidateFingerprint(first))
	}
	if runtime.CandidateFingerprint(first) != runtime.CandidateFingerprint(second) {
		t.Fatalf("同名同年龄应生成相同 ID：first=%s second=%s", runtime.CandidateFingerprint(first), runtime.CandidateFingerprint(second))
	}
}

// TestCandidateFingerprintRequiresAge 验证缺少年龄时不生成 Boss 候选人 ID。
func TestCandidateFingerprintRequiresAge(t *testing.T) {
	runtime := NewRuntime()
	candidate := platformcore.Candidate{"candidate_name": "范召", "raw_text": "范召 本科 5年"}
	if id := runtime.CandidateFingerprint(candidate); id != "" {
		t.Fatalf("缺少年龄时不应生成 ID：%s", id)
	}
}

// TestCleanCandidateDetailText 验证 Boss 平台附加分析内容不会进入候选人详情。
func TestCleanCandidateDetailText(t *testing.T) {
	runtime := NewRuntime()
	raw := "解婷 25岁 大专 工作经历 主播\n牛人分析器\nVIP专享 同类牛人\n平台隐私声明"
	cleaned := runtime.CleanCandidateDetailText(raw)
	if cleaned != "解婷 25岁 大专 工作经历 主播" {
		t.Fatalf("cleaned = %q", cleaned)
	}
}
