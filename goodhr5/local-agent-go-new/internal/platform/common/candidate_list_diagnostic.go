// Package common 文件作用：记录一次候选人列表操作已观察到的阶段、计数和耗时，并在失败时附带有限的安全快照。
package common

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type listDiagnosticKey struct{}

// CandidateListTrace 只在本次同步列表调用内使用；不保存候选人、DOM、请求内容或页面地址。
type CandidateListTrace struct {
	started, stageStarted, deadline time.Time
	parent                          context.Context
	readRole                        string
	Facts                           model.CandidateListDiagnostic
}

type candidateListError struct {
	cause    error
	snapshot model.CandidateListDiagnostic
}

// Error 保留原始用户提示，额外证据只存入结构化日志。
func (e *candidateListError) Error() string { return e.cause.Error() }

// Unwrap 让停止任务、取消及 Worker 错误策略继续识别底层错误。
func (e *candidateListError) Unwrap() error { return e.cause }

// CandidateListDiagnostic 返回失败时拍下的安全平台操作摘要。
func (e *candidateListError) CandidateListDiagnostic() model.CandidateListDiagnostic {
	return e.snapshot
}

// StartCandidateListTrace 给指定平台本次列表操作建立独立记录，操作结束即可释放。
func StartCandidateListTrace(ctx context.Context, platform, operation string) (context.Context, *CandidateListTrace) {
	trace := &CandidateListTrace{started: time.Now(), parent: ctx, Facts: model.CandidateListDiagnostic{Platform: platform, Operation: operation}}
	return context.WithValue(ctx, listDiagnosticKey{}, trace), trace
}

// CandidateTrace 返回本次读取链路的记录器；未开启记录的调用保持原行为。
func CandidateTrace(ctx context.Context) *CandidateListTrace {
	t, _ := ctx.Value(listDiagnosticKey{}).(*CandidateListTrace)
	return t
}

// Begin 标记正在执行的中文步骤及配置键，保留开始时间以便失败时核对。
func (t *CandidateListTrace) Begin(stage, selector string) {
	if t != nil {
		t.Facts.Stage, t.Facts.SelectorKey, t.stageStarted = stage, selector, time.Now()
	}
}

// finishEvent 仅保留最近八个步骤，即使长时间滚动也不会产生无界记录。
func (t *CandidateListTrace) finishEvent(status string, now time.Time) {
	if len(t.Facts.Timeline) == 8 {
		copy(t.Facts.Timeline, t.Facts.Timeline[1:])
		t.Facts.Timeline = t.Facts.Timeline[:7]
	}
	t.Facts.Timeline = append(t.Facts.Timeline, fmt.Sprintf("+%d ms %s · %s（%d ms）", now.Sub(t.started).Milliseconds(), t.Facts.Stage, status, now.Sub(t.stageStarted).Milliseconds()))
}

// Done 标记上一步成功；诊断区分“已确认没有匹配”和“探测本身失败”。
func (t *CandidateListTrace) Done() {
	if t != nil && t.Facts.Stage != "" {
		t.Facts.LastSuccessStage = t.Facts.Stage
		t.finishEvent("完成", time.Now())
	}
}

// observed 保存已观测的数值或真假，包括零和假。
func observed[T int | int64 | bool](v T) *T { return &v }

// Budget 标记该流程自己规定的时限及实际有效的截止时间，传零可以结束子阶段的计时。
func (t *CandidateListTrace) Budget(ctx context.Context, limit time.Duration) {
	if t == nil {
		return
	}
	t.deadline = time.Time{}
	t.Facts.ScopeBudgetMS = nil
	if limit > 0 {
		t.Facts.ScopeBudgetMS = observed(limit.Milliseconds())
		t.deadline, _ = ctx.Deadline()
	}
}

// RecordMatches 只采用本次同一选择器返回的、与所有卡片下标一致的正数总量；空数组无法证明总数是零。
func (t *CandidateListTrace) RecordMatches(items []contract.FindAllItem) {
	if t == nil || len(items) == 0 {
		return
	}
	total := items[0].TotalMatches
	if total <= 0 {
		return
	}
	for _, item := range items {
		if item.TotalMatches != total || item.Index < 0 || item.Index >= total {
			return
		}
	}
	switch t.Facts.WindowPhase {
	case "before_scroll":
		t.Facts.BeforeTotalMatches = observed(total)
	case "after_scroll":
		t.Facts.AfterTotalMatches = observed(total)
	default:
		if t.Facts.Operation == "find_candidates" {
			t.Facts.TotalMatches = observed(total)
		}
	}
}

// startScrollRound 清空上一轮的采样；滚轮没有执行或没有回执时不沿用旧的定位结果。
func (t *CandidateListTrace) startScrollRound(round, bottom, blocked int) {
	if t == nil {
		return
	}
	t.Facts.ScrollRound, t.Facts.PollRound = observed(round), nil
	t.Facts.BottomRounds, t.Facts.BlockedRounds = observed(bottom), observed(blocked)
	t.Facts.TailMoved, t.Facts.TailVisible = nil, nil
	t.Facts.BeforeTotalMatches, t.Facts.AfterTotalMatches = nil, nil
	t.Facts.HeadChanged, t.Facts.NewCards = nil, nil
	t.Facts.BatchStartIndex, t.Facts.MaxItems, t.Facts.FieldCount, t.Facts.ReadConcurrency = nil, nil, nil, nil
	t.Facts.CardsRead, t.Facts.MissingNames, t.Facts.ReadAttempt = nil, nil, nil
	t.Facts.WindowPhase = ""
}

// readStage 记录本次实际送给浏览器的窗口与读取并发，丢弃上一批的卡片统计。
func (t *CandidateListTrace) readStage(request contract.ElementFindAllRequest, attempt int) {
	if t == nil {
		return
	}
	stage := t.readRole
	if stage == "" {
		stage = "读取候选人卡片"
	}
	if attempt > 1 {
		stage += "（缺姓名重读）"
	}
	t.Begin(stage, "candidate.item")
	t.Facts.BatchStartIndex = observed(request.StartIndex)
	t.Facts.MaxItems = observed(request.MaxItems)
	t.Facts.ReadConcurrency = observed(request.ReadConcurrency)
	t.Facts.FieldCount = observed(len(request.Fields))
	t.Facts.ReadAttempt = observed(attempt)
	t.Facts.CardsRead, t.Facts.MissingNames = nil, nil
}

// Wrap 在操作真正结束时生成快照，保留错误链与触发失败时的上下文。
func (t *CandidateListTrace) Wrap(err error) error {
	if err == nil || t == nil {
		return err
	}
	now := time.Now()
	if parentErr := t.parent.Err(); parentErr != nil {
		t.Facts.ParentStatus = parentErr.Error()
	}
	t.Facts.ElapsedMS = now.Sub(t.started).Milliseconds()
	if t.Facts.Stage != "" {
		t.Facts.StageElapsedMS = now.Sub(t.stageStarted).Milliseconds()
		t.finishEvent("失败", now)
	}
	if !t.deadline.IsZero() {
		t.Facts.ScopeRemainingMS = observed(max(int64(0), t.deadline.Sub(now).Milliseconds()))
	}
	return &candidateListError{cause: err, snapshot: t.Facts}
}
