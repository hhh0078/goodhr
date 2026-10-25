// Package ai 文件作用：验证流式提前判定拒绝非有限分数并完整保留正常原因，避免持久快照无法编码。
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestScreeningBufferedDecisionWinsCancellation 在取消、早评分和尾部错误同时可读时确定性保留真实零分及完整原因。
func TestScreeningBufferedDecisionWinsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	early := make(chan Decision, 1)
	final := make(chan EvaluationResult, 1)
	early <- Decision{Accepted: false, Score: 0, Reason: "真正已经可用的评分原因"}
	tailErr := errors.New("评分之后尾部中断")
	final <- EvaluationResult{Err: tailErr}
	close(final)
	evaluation, err := waitEvaluation(ctx, early, final)
	if err != nil || evaluation.Decision.Score != 0 || evaluation.Decision.Reason != "真正已经可用的评分原因" || evaluation.Final == nil {
		t.Fatalf("取消吞掉已产出评分: %+v err=%v", evaluation, err)
	}
	if result := <-evaluation.Final; !errors.Is(result.Err, tailErr) {
		t.Fatal("保留早评分时丢失尾部诊断")
	}
}

// TestScreeningCompletedTailRechecksEarly 覆盖Final分支已经取走尾部错误后，再从缓冲早评分恢复真实结果的路径。
func TestScreeningCompletedTailRechecksEarly(t *testing.T) {
	early := make(chan Decision, 1)
	early <- Decision{Accepted: true, Score: 80, Reason: "评分已完成"}
	tailErr := errors.New("尾部输出不完整")
	evaluation, err := evaluationFromFinal(early, EvaluationResult{Err: tailErr})
	if err != nil || !evaluation.Decision.Accepted || evaluation.Decision.Score != 80 || evaluation.Final == nil {
		t.Fatal("Final先就绪吞掉了早评分")
	}
	if result := <-evaluation.Final; !errors.Is(result.Err, tailErr) {
		t.Fatal("已经消费的尾部诊断未恢复")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	final := make(chan EvaluationResult, 1)
	final <- EvaluationResult{Err: context.Canceled}
	close(final)
	if _, err := waitEvaluation(ctx, make(chan Decision, 1), final); !errors.Is(err, context.Canceled) {
		t.Fatal("没有实际评分时仍应保留取消")
	}
}

// TestScreeningCancelledBranchKeepsSuccessfulJSON 确定性直接覆盖取消分支：普通JSON没有early回调，收尾成功的真实判定仍须保留。
func TestScreeningCancelledBranchKeepsSuccessfulJSON(t *testing.T) {
	final := make(chan EvaluationResult, 1)
	final <- EvaluationResult{Decision: Decision{Accepted: true, Score: 88, Reason: "完整JSON已经成功解析"}}
	close(final)
	evaluation, err := evaluationAfterCancellation(make(chan Decision, 1), final)
	if err != nil || !evaluation.Decision.Accepted || evaluation.Decision.Score != 88 || evaluation.Decision.Reason != "完整JSON已经成功解析" {
		t.Fatalf("取消覆盖了普通JSON真实结果: %+v err=%v", evaluation, err)
	}
	failed := make(chan EvaluationResult, 1)
	failed <- EvaluationResult{Err: context.Canceled}
	close(failed)
	if result, err := evaluationAfterCancellation(make(chan Decision, 1), failed); !errors.Is(err, context.Canceled) || result.Decision.Reason != "" {
		t.Fatal("真正没有完成的判定没有保留取消")
	}
}

// TestScreeningEarlyScoreFinite 验证NaN、Inf及超出数值范围的字符串不会产生可用评分。
func TestScreeningEarlyScoreFinite(t *testing.T) {
	for _, score := range []string{"NaN", "+Inf", "-Inf", "Infinity", "1e1000"} {
		if _, ok := tryExtractStreamDecision(fmt.Sprintf(`{"score":%q,"reason":"无效分数"}`, score), 60); ok {
			t.Fatalf("非有限评分被接受: %s", score)
		}
	}
	reason := strings.Repeat("完整原因", 100)
	decision, ok := tryExtractStreamDecision(fmt.Sprintf(`{"score":0,"reason":%q}`, reason), 60)
	if !ok || decision.Score != 0 || decision.Accepted || decision.Reason != reason {
		t.Fatal("提前评分丢失零分或截断完整原因")
	}
}
