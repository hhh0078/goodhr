// Package greeting 文件作用：生成悬浮窗使用的 AI 与关键词结构化判断状态。
package greeting

import (
	"strings"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// keywordMatchResult 保存关键词、排除词及其命中结果。
type keywordMatchResult struct {
	Accepted        bool
	Keywords        []string
	MatchedKeywords []string
	ExcludeKeywords []string
	MatchedExcludes []string
	Reason          string
}

// matchKeywords 对候选人卡片和可选详情文本执行不区分大小写的关键词过滤。
func matchKeywords(candidate model.Candidate, detailText string, position cloud.PositionSnapshot) keywordMatchResult {
	parts := []string{candidate.Name, candidate.Summary, detailText}
	for _, value := range candidate.Fields {
		parts = append(parts, value)
	}
	content := strings.ToLower(strings.Join(parts, "\n"))
	result := keywordMatchResult{
		Keywords:        cleanKeywords(position.Keywords),
		ExcludeKeywords: cleanKeywords(position.ExcludeKeywords),
	}
	if len(result.Keywords) == 0 && strings.TrimSpace(position.Keyword) != "" {
		result.Keywords = []string{strings.TrimSpace(position.Keyword)}
	}
	for _, keyword := range result.ExcludeKeywords {
		if strings.Contains(content, strings.ToLower(keyword)) {
			result.MatchedExcludes = append(result.MatchedExcludes, keyword)
		}
	}
	for _, keyword := range result.Keywords {
		if strings.Contains(content, strings.ToLower(keyword)) {
			result.MatchedKeywords = append(result.MatchedKeywords, keyword)
		}
	}
	switch {
	case len(result.MatchedExcludes) > 0:
		result.Reason = "命中了排除词，本次先跳过"
	case len(result.Keywords) == 0:
		result.Accepted = true
		result.Reason = "没有设置关键词，默认通过"
	case position.IsAndMode && len(result.MatchedKeywords) == len(result.Keywords):
		result.Accepted = true
		result.Reason = "需要的关键词都命中了"
	case !position.IsAndMode && len(result.MatchedKeywords) > 0:
		result.Accepted = true
		result.Reason = "已经命中关键词"
	case position.IsAndMode:
		result.Reason = "还有必需关键词没有命中"
	default:
		result.Reason = "暂时没有命中关键词"
	}
	return result
}

// cleanKeywords 清理空关键词并保留用户配置的原始顺序。
func cleanKeywords(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// reportKeywordMatch 把关键词判断结果发送给全平台共用悬浮窗。
func reportKeywordMatch(logger shared.Logger, taskID string, candidate model.Candidate, result keywordMatchResult) {
	accepted := result.Accepted
	shared.ReportAnalysis(logger, taskID, shared.AnalysisStatus{
		Kind: "keyword", Phase: "result", CandidateName: candidateDisplayName(candidate),
		Accepted: &accepted, Reason: result.Reason,
		Keywords: result.Keywords, MatchedKeywords: result.MatchedKeywords,
		ExcludeKeywords: result.ExcludeKeywords, MatchedExcludes: result.MatchedExcludes,
	})
}

// reportAILoading 把 AI 请求中的候选人和判断阶段发送给悬浮窗。
func reportAILoading(logger shared.Logger, taskID string, candidateName string, reason string) {
	shared.ReportAnalysis(logger, taskID, shared.AnalysisStatus{
		Kind: "ai", Phase: "loading", CandidateName: strings.TrimSpace(candidateName),
		Reason: strings.TrimSpace(reason),
	})
}

// reportAIResult 把已经完整返回的分数、阈值和原因发送给悬浮窗。
func reportAIResult(logger shared.Logger, taskID string, candidate model.Candidate, decision ai.Decision, threshold float64) {
	score := decision.Score
	accepted := decision.Accepted
	shared.ReportAnalysis(logger, taskID, shared.AnalysisStatus{
		Kind: "ai", Phase: "result", CandidateName: candidateDisplayName(candidate),
		Score: &score, Threshold: &threshold, Accepted: &accepted, Reason: decision.Reason,
	})
}

// reportAIError 把 AI 请求错误发送给悬浮窗，不伪造评分。
func reportAIError(logger shared.Logger, taskID string, candidate model.Candidate, err error) {
	reason := "AI 这次没有返回判断结果"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		reason = err.Error()
	}
	shared.ReportAnalysis(logger, taskID, shared.AnalysisStatus{
		Kind: "ai", Phase: "error", CandidateName: candidateDisplayName(candidate),
		Reason: reason,
	})
}

// candidateDisplayName 返回悬浮窗使用的候选人名称。
func candidateDisplayName(candidate model.Candidate) string {
	if name := strings.TrimSpace(candidate.Name); name != "" {
		return name
	}
	return "当前候选人"
}

// previewScoreThreshold 返回 AI 基础信息预判断的分数阈值。
func previewScoreThreshold(position cloud.PositionSnapshot) float64 {
	if position.AIOptions.DetailScoreThreshold > 0 {
		return position.AIOptions.DetailScoreThreshold
	}
	return 60
}

// greetScoreThreshold 返回 AI 最终打招呼判断的分数阈值。
func greetScoreThreshold(position cloud.PositionSnapshot) float64 {
	if position.AIOptions.GreetScoreThreshold > 0 {
		return position.AIOptions.GreetScoreThreshold
	}
	if position.AI.ScoreThreshold > 0 {
		return position.AI.ScoreThreshold
	}
	return 70
}
