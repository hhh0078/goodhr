// Package ai 文件作用：让候选人评分字段完整后提前返回，并在后台继续接收完整 AI 结果。
package ai

import (
	"context"
	"sync"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// EvaluationResult 表示 AI 完整流式响应结束后的最终结果。
type EvaluationResult struct {
	Decision Decision
	Err      error
}

// Evaluation 表示主流程可立即使用的评分和可选后台完整结果。
type Evaluation struct {
	Decision Decision
	Final    <-chan EvaluationResult
}

// EvaluateCandidatePreviewEarly 在列表卡片的分数与原因完整可用时返回，完整流继续后台读取。
func (c *Client) EvaluateCandidatePreviewEarly(ctx context.Context, cfg cloud.AIConfig, position cloud.PositionSnapshot, candidate model.Candidate) (Evaluation, error) {
	return startEvaluation(ctx, func(onEarly func(Decision)) (Decision, error) {
		return c.evaluateCandidatePreview(ctx, cfg, position, candidate, onEarly)
	})
}

// EvaluateCandidateEarly 在文本评分字段完整后提前返回，完整响应继续在后台读取。
func (c *Client) EvaluateCandidateEarly(
	ctx context.Context,
	cfg cloud.AIConfig,
	position cloud.PositionSnapshot,
	candidate model.Candidate,
	detail model.CandidateDetail,
) (Evaluation, error) {
	return startEvaluation(ctx, func(onEarly func(Decision)) (Decision, error) {
		return c.evaluateCandidate(ctx, cfg, position, candidate, detail, onEarly)
	})
}

// EvaluateCandidateVisionEarly 在图片评分字段完整后提前返回，完整结构化简历继续在后台读取。
func (c *Client) EvaluateCandidateVisionEarly(
	ctx context.Context,
	cfg cloud.AIConfig,
	position cloud.PositionSnapshot,
	candidate model.Candidate,
	detail model.CandidateDetail,
	images [][]byte,
) (Evaluation, error) {
	return startEvaluation(ctx, func(onEarly func(Decision)) (Decision, error) {
		return c.evaluateCandidateVision(ctx, cfg, position, candidate, detail, images, onEarly)
	})
}

// startEvaluation 并发读取 AI 完整结果，并优先返回第一次完整解析出的评分。
func startEvaluation(
	ctx context.Context,
	evaluate func(func(Decision)) (Decision, error),
) (Evaluation, error) {
	early := make(chan Decision, 1)
	final := make(chan EvaluationResult, 1)
	var earlyOnce sync.Once
	go func() {
		decision, err := evaluate(func(decision Decision) {
			earlyOnce.Do(func() {
				early <- decision
			})
		})
		final <- EvaluationResult{Decision: decision, Err: err}
		close(final)
	}()
	return waitEvaluation(ctx, early, final)
}

// waitEvaluation 优先保留已完整解析的真实评分，停止和尾部错误都不能随机覆盖已经可用的结果。
func waitEvaluation(ctx context.Context, early <-chan Decision, final <-chan EvaluationResult) (Evaluation, error) {
	select {
	case decision := <-early:
		return Evaluation{Decision: decision, Final: final}, nil
	default:
	}
	select {
	case decision := <-early:
		return Evaluation{Decision: decision, Final: final}, nil
	case result := <-final:
		return evaluationFromFinal(early, result)
	case <-ctx.Done():
		return evaluationAfterCancellation(early, final)
	}
}

// evaluationAfterCancellation 等待请求收尾，仍保留已收到并成功解析的普通JSON结果；没有真实判定时保留实际取消错误。
func evaluationAfterCancellation(early <-chan Decision, final <-chan EvaluationResult) (Evaluation, error) {
	select {
	case decision := <-early:
		return Evaluation{Decision: decision, Final: final}, nil
	default:
	}
	// 请求使用相同上下文，收尾会返回取消错误或已经成功解析的完整结果，不能伪造空评分。
	result := <-final
	return evaluationFromFinal(early, result)
}

// evaluationFromFinal 在收尾先到时再次检查早评分，并保留完整尾部结果供后台诊断，避免丢掉已消费的Final。
func evaluationFromFinal(early <-chan Decision, result EvaluationResult) (Evaluation, error) {
	select {
	case decision := <-early:
		replay := make(chan EvaluationResult, 1)
		replay <- result
		close(replay)
		return Evaluation{Decision: decision, Final: replay}, nil
	default:
		return Evaluation{Decision: result.Decision}, result.Err
	}
}
