// Package greeting 验证个人节奏和索要信息阈值的关键边界。
package greeting

import (
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestShouldOpenDetailHonorsZeroProbability 验证关键词模式下零概率不会打开详情。
func TestShouldOpenDetailHonorsZeroProbability(t *testing.T) {
	prepared := shared.PreparedTask{
		Position: cloud.PositionSnapshot{
			CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword"},
		},
		Preferences: cloud.UserPreferences{DetailOpenProbability: 0},
	}
	if shouldOpenDetail(prepared) {
		t.Fatal("详情打开概率为 0 时不应打开详情")
	}
}

// TestRequestScoreThresholdFallback 验证索要阈值优先级和默认值。
func TestRequestScoreThresholdFallback(t *testing.T) {
	position := cloud.PositionSnapshot{
		AIOptions: cloud.PositionAIOptions{
			GreetScoreThreshold:   75,
			RequestScoreThreshold: 82,
		},
	}
	if threshold := requestScoreThreshold(position); threshold != 82 {
		t.Fatalf("应优先使用索要阈值，实际为 %.1f", threshold)
	}
	position.AIOptions.RequestScoreThreshold = 0
	if threshold := requestScoreThreshold(position); threshold != 75 {
		t.Fatalf("索要阈值为空时应使用打招呼阈值，实际为 %.1f", threshold)
	}
}

// TestCandidateInfoRequestConfigured 验证任一索要项或追加消息都能启用后续动作。
func TestCandidateInfoRequestConfigured(t *testing.T) {
	if candidateInfoRequestConfigured(model.CandidateInfoRequest{}) {
		t.Fatal("空索要配置不应启用后续动作")
	}
	if !candidateInfoRequestConfigured(model.CandidateInfoRequest{RequestPhone: true}) {
		t.Fatal("勾选电话后应启用后续动作")
	}
}

// TestCandidateInfoAllowedRequiresStrictlyGreaterScore 验证等于阈值时不能索要信息。
func TestCandidateInfoAllowedRequiresStrictlyGreaterScore(t *testing.T) {
	if candidateInfoAllowed(true, 80, 80) {
		t.Fatal("最终分数等于阈值时不应索要信息")
	}
	if !candidateInfoAllowed(true, 80.1, 80) {
		t.Fatal("最终分数严格大于阈值时应允许索要信息")
	}
	if candidateInfoAllowed(false, 100, 80) {
		t.Fatal("没有最终 AI 分数时不应索要信息")
	}
}
