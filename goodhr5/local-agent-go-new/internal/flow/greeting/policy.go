// Package greeting 文件作用：集中定义候选人详情、索要信息和错误停止策略。
package greeting

import (
	"strings"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// needsCandidateDetail 判断当前岗位和平台是否需要打开候选人详情。
func needsCandidateDetail(prepared shared.PreparedTask) bool {
	return prepared.Platform.Behavior.NeedsDetail ||
		prepared.Position.RequiresAI ||
		prepared.Position.RequiresOCR ||
		strings.TrimSpace(prepared.Position.CommonConfig.DetailMode) != ""
}

// shouldOpenDetail 按关键词模式的个人概率判断本次是否打开详情。
func shouldOpenDetail(prepared shared.PreparedTask) bool {
	if strings.ToLower(strings.TrimSpace(prepared.Position.CommonConfig.ModeDefault)) != "keyword" {
		return true
	}
	probability := prepared.Preferences.DetailOpenProbability
	if probability >= 100 {
		return true
	}
	if probability <= 0 {
		return false
	}
	return randomIntRange(0, 99) < probability
}

// candidateInfoRequestConfigured 判断岗位是否勾选了任一索要动作或追加消息。
func candidateInfoRequestConfigured(request model.CandidateInfoRequest) bool {
	return request.RequestPhone || request.RequestWechat || request.RequestResume || strings.TrimSpace(request.Message) != ""
}

// requestScoreThreshold 返回索要信息使用的严格大于阈值。
func requestScoreThreshold(position cloud.PositionSnapshot) float64 {
	if position.AIOptions.RequestScoreThreshold > 0 {
		return position.AIOptions.RequestScoreThreshold
	}
	if position.AIOptions.GreetScoreThreshold > 0 {
		return position.AIOptions.GreetScoreThreshold
	}
	return 70
}

// candidateInfoAllowed 判断最终 AI 分数是否存在且严格大于索要阈值。
func candidateInfoAllowed(hasScore bool, score float64, threshold float64) bool {
	return hasScore && score > threshold
}
