// Package httpapi 本文件验证推荐报告 AI 输出、公开字段和邮件内容的安全边界。
package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestParseRecommendationAIOutputValidatesAndCleans 验证推荐报告只接受约定等级，并清理重复问题和空列表项。
func TestParseRecommendationAIOutputValidatesAndCleans(t *testing.T) {
	raw := `{"match_score":86.5,"recommendation_level":"优先推荐","executive_summary":"核心条件已确认","strengths":[{"title":"经验匹配","detail":"有同类经历","evidence":["简历原文"]},{"title":"","detail":"","evidence":[]}],"risks":[],"bonus_items":[],"unconfirmed_items":[],"job_intent_summary":"意向明确","stability_summary":"经历稳定","communication_summary":"沟通顺畅","interview_focus":[],"suggested_questions":["何时到岗？","何时到岗？",""]}`
	result, err := parseRecommendationAIOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchScore != 86.5 || len(result.Strengths) != 1 || len(result.SuggestedQuestions) != 1 {
		t.Fatalf("推荐报告清理结果不正确：%+v", result)
	}
	invalid := strings.Replace(raw, "优先推荐", "强烈录用", 1)
	if _, err = parseRecommendationAIOutput(invalid); err == nil || !strings.Contains(err.Error(), "recommendation_level") {
		t.Fatalf("未知推荐等级没有被拒绝：%v", err)
	}
}

// TestRecommendationEmailEscapesCandidateData 验证候选人和岗位文字不会进入邮件 HTML 执行环境。
func TestRecommendationEmailEscapesCandidateData(t *testing.T) {
	item := CandidateRecommendation{
		PublicID: "rec_test", MatchScore: 88, RecommendationLevel: "建议推进",
		Report: CandidateRecommendationReport{
			Candidate:        RecommendationCandidateSnapshot{Name: `<script>alert("x")</script>`},
			Position:         RecommendationPositionSnapshot{Name: `<b>测试岗位</b>`},
			ExecutiveSummary: "条件已经确认",
		},
	}
	html := recommendationEmailHTML(item)
	if strings.Contains(html, "<script>") || strings.Contains(html, "<b>测试岗位</b>") || !strings.Contains(html, "查看完整推荐报告") {
		t.Fatalf("邮件转义或按钮不正确：%s", html)
	}
}

// TestPublicRecommendationHidesInternalFields 验证公开接口不会暴露团队编号、任务哈希或 AI 费用字段。
func TestPublicRecommendationHidesInternalFields(t *testing.T) {
	item := CandidateRecommendation{
		PublicID: "rec_public", TenantID: "tenant-secret", InputHash: "hash-secret",
		Model: "private-model", TokenUsage: 123, ShareEnabled: true,
		GeneratedAt: time.Now().UTC(), Report: CandidateRecommendationReport{},
	}
	encoded, err := json.Marshal(publicRecommendation(item))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"tenant-secret", "hash-secret", "private-model", "token_usage"} {
		if strings.Contains(text, secret) {
			t.Fatalf("公开报告暴露了内部字段 %q：%s", secret, text)
		}
	}
}

// TestRecommendationPublicIDIsRandomAndOpaque 验证公开编号不可猜测且连续生成不会重复。
func TestRecommendationPublicIDIsRandomAndOpaque(t *testing.T) {
	first, err := newRecommendationPublicID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newRecommendationPublicID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "rec_") || len(first) != 52 {
		t.Fatalf("公开编号格式或随机性不正确：first=%q second=%q", first, second)
	}
}
