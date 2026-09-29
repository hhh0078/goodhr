// Package greeting 文件作用：验证平台确认的推荐卡片不会调用 AI、打开详情或发送招呼，正常跳过会清空连续错误。
package greeting

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// platformSkipLogger 记录正常跳过的用户日志，验证日志不会标记成错误。
type platformSkipLogger struct {
	status string
	reason string
	err    error
}

// Step 接收流程步骤日志，不额外执行页面动作。
func (l *platformSkipLogger) Step(string, string, string, string, time.Time, error) {}

// CandidateActivity 保存推荐卡片的正常跳过原因与错误状态。
func (l *platformSkipLogger) CandidateActivity(_ string, _ string, action string, status string, err error) {
	l.status, l.reason, l.err = status, action, err
}

// ClearCandidate 满足候选人上下文清理接口。
func (l *platformSkipLogger) ClearCandidate(string) {}

// TestPlatformSkipClearsConsecutiveErrors 验证已确认的无入口推荐卡片不会凑成第三次错误，并允许继续处理后面的普通卡片。
func TestPlatformSkipClearsConsecutiveErrors(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := &platformSkipLogger{}
	flow := Flow{Store: store, Logger: logger}
	policy := shared.ConsecutiveErrorPolicy{}
	detailErr := model.ErrCandidateDetailUnavailable
	for index := 0; index < 2; index++ {
		if err := policy.Record(detailErr); err != nil {
			t.Fatal(err)
		}
	}
	candidate := model.Candidate{Name: "尚先生", SkipReason: "推荐卡片没有打招呼入口，已跳过"}
	stats := shared.Stats{}
	// 空 Runtime 和 Browser 一旦被调用就会失败，证明正常跳过在所有页面动作之前发生。
	err = flow.processCandidate(context.Background(), shared.PreparedTask{}, nil, candidate, &stats)
	if err != nil || stats.Processed != 1 || stats.Skipped != 1 || stats.Failed != 0 {
		t.Fatalf("推荐卡片应正常跳过：统计=%+v 错误=%v", stats, err)
	}
	if logger.status != "skipped" || logger.reason != candidate.SkipReason || logger.err != nil {
		t.Fatalf("跳过原因必须记录到正常日志：%+v", logger)
	}
	if err := policy.Record(err); err != nil {
		t.Fatal(err)
	}
	if err := policy.Record(detailErr); err != nil {
		t.Fatalf("后面的普通候选人不能继承推荐卡片之前的错误计数：%v", err)
	}
	// 普通候选人的真实连续错误仍然必须在第三次停止。
	if err := policy.Record(detailErr); err != nil {
		t.Fatal(err)
	}
	if err := policy.Record(detailErr); !errors.Is(err, detailErr) {
		t.Fatalf("真实错误连续三次仍应停止：%v", err)
	}
	prepared := shared.PreparedTask{Position: cloud.PositionSnapshot{CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword"}}}
	runtime := &imageDetailRuntime{}
	if err := flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{Name: "王先生"}, &stats); err != nil || runtime.greetings != 1 {
		t.Fatalf("推荐卡片后的普通候选人必须继续正常处理：招呼=%d 错误=%v", runtime.greetings, err)
	}
}

// TestPlatformSkipBypassesAIPreview 验证 AI 岗位中的无入口推荐卡片按原顺序返回，且不调用 AI。
func TestPlatformSkipBypassesAIPreview(t *testing.T) {
	flow := Flow{}
	prepared := shared.PreparedTask{Position: cloud.PositionSnapshot{RequiresAI: true, CommonConfig: cloud.PositionCommonConfig{ModeDefault: "ai"}}}
	candidate := model.Candidate{Index: 9, SkipReason: "推荐卡片没有打招呼入口，已跳过"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ordered := flow.candidatePreviews(ctx, prepared, []model.Candidate{candidate})
	select {
	case item, ok := <-ordered:
		if !ok || item.Candidate.Index != 9 || item.Candidate.SkipReason != candidate.SkipReason || item.Decision != nil || item.Err != nil {
			t.Fatalf("推荐卡片预判断应直接通过且保留跳过原因：%+v", item)
		}
	case <-ctx.Done():
		t.Fatal("推荐卡片不应等待 AI")
	}
}
