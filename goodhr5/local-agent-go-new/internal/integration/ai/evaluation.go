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
	select {
	case decision := <-early:
		return Evaluation{Decision: decision, Final: final}, nil
	case result := <-final:
		return Evaluation{Decision: result.Decision}, result.Err
	case <-ctx.Done():
		return Evaluation{}, ctx.Err()
	}
}
