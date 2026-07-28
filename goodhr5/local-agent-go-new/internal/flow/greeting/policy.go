// Package greeting 文件作用：集中定义候选人详情、索要信息和错误停止策略。
package greeting

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var errorNumberPattern = regexp.MustCompile(`\d+`)

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

// normalizeCandidateError 归一化错误中的数字和空白，用于识别连续同类失败。
func normalizeCandidateError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(errorNumberPattern.ReplaceAllString(strings.ToLower(err.Error()), "#")), " ")
}

// shouldStopImmediately 判断任务取消、浏览器关闭和 OCR 组件故障等不可继续错误。
func shouldStopImmediately(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || ocr.IsUnavailable(err) {
		return true
	}
	if ai.IsPositionStoppingError(err) {
		return true
	}
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) {
		return false
	}
	switch workerErr.Body.Code {
	case "BROWSER_NOT_RUNNING", "PAGE_CLOSED", "PAGE_NOT_AVAILABLE":
		return true
	default:
		return false
	}
}
