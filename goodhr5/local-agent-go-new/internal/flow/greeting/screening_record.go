// Package greeting 文件作用：在实际判定现场持久保存每次两轮筛选，记录真实时间与完整原因，不依赖简历上传开关。
package greeting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	screeningsync "goodhr5/local-agent-go-new/internal/flow/screening_sync"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/screening"
	"goodhr5/local-agent-go-new/internal/storage"
)

// screeningContextKey 让本候选人的动作保存复用同一尝试编号，不将编号塞入平台或Worker模型。
type screeningContextKey struct{}

// beginScreeningAttempt 在真正开始处理候选人时先落盘；未装配账号的旧测试不创建无归属记录。
func (f *Flow) beginScreeningAttempt(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate) (string, error) {
	owner := screeningsync.OwnerKey(prepared.Session)
	if f.Store == nil || owner == "" || prepared.Position.ID == "" || prepared.Request.TaskID == "" {
		return "", nil
	}
	id, err := screening.NewID()
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(candidate.Fingerprint)
	if key == "" {
		key = "unidentified:" + id
	}
	item := screening.Snapshot{AttemptID: id, TaskID: prepared.Request.TaskID, PlatformID: prepared.Position.PlatformID, PlatformCandidateID: strings.TrimSpace(candidate.Fields["platform_candidate_id"]), CandidateKey: key, CandidateName: candidateDisplayName(candidate), OccurredAt: time.Now().UTC(), Revision: 1, First: screening.NotExecuted("尚未开始第一轮判定"), Second: screening.NotExecuted("尚未进入详情评分")}
	if err = f.Store.CreateScreeningRecord(context.WithoutCancel(ctx), storage.ScreeningRecord{OwnerID: owner, PositionID: prepared.Position.ID, Snapshot: item}); err != nil {
		return "", fmt.Errorf("筛选记录没有保存，暂不继续判定：%w", err)
	}
	return id, nil
}

// startScreeningRound 在外部AI调用前保存running状态，返回本轮时钟，不包含并发队列等待。
func (f *Flow) startScreeningRound(ctx context.Context, id string, number int, kind string) (time.Time, error) {
	started := time.Now()
	if id == "" {
		return started, nil
	}
	_, err := f.Store.UpdateScreeningRecord(context.WithoutCancel(ctx), id, func(item *screening.Snapshot) error {
		if item.Completed {
			return errors.New("本次筛选已经结束")
		}
		stamp := started.UTC()
		round := screening.Round{Kind: kind, Status: "running", Reason: "正在进行本轮判定", StartedAt: &stamp}
		if number == 1 {
			item.First = round
		} else {
			item.Second = round
		}
		return nil
	})
	return started, err
}

// finishScreeningRound 在score和reason可用时保存本轮结果；不等待后台结构化简历，也不把错误当零分。
func (f *Flow) finishScreeningRound(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate, id string, number int, kind string, started time.Time, decision *ai.Decision, threshold float64, decisionErr error) error {
	finished := time.Now()
	startStamp, endStamp := started.UTC(), finished.UTC()
	duration := max(int64(0), finished.Sub(started).Milliseconds())
	round := screening.Round{Kind: kind, Status: "failed", Reason: "本轮未返回有效判定", StartedAt: &startStamp, FinishedAt: &endStamp, DurationMS: &duration}
	if decisionErr != nil {
		round.Reason = decisionErr.Error()
		if errors.Is(decisionErr, context.Canceled) {
			round.Status = "cancelled"
		}
	} else if decision != nil {
		accepted, score, limit := decision.Accepted, decision.Score, threshold
		round.Accepted = &accepted
		round.Reason = decision.Reason
		round.Status = "rejected"
		if accepted {
			round.Status = "passed"
		}
		if kind == "ai" {
			round.Score = &score
			round.Threshold = &limit
		}
	}
	round.Reason = limitScreeningReason(round.Reason)
	if id != "" {
		_, err := f.Store.UpdateScreeningRecord(context.WithoutCancel(ctx), id, func(item *screening.Snapshot) error {
			if number == 1 {
				item.First = round
			} else {
				item.Second = round
			}
			if number == 2 || round.Status != "passed" {
				item.Completed = true
				item.TerminationReason = "两轮筛选已结束"
				if number == 1 {
					item.TerminationReason = "第一轮未通过或未完成，未进行第二轮"
					item.Second = screening.NotExecuted(item.TerminationReason)
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("保存本轮筛选结果失败：%w", err)
		}
	}
	f.logScreeningRound(prepared.Request.TaskID, candidate, number, round, started)
	return nil
}

// recordKeywordScreening 记录实际关键词判定；分数和阈值留空，且不覆盖已经执行的卡片AI第一轮。
func (f *Flow) recordKeywordScreening(ctx context.Context, prepared shared.PreparedTask, candidate model.Candidate, id string, started time.Time, result keywordMatchResult) error {
	decision := ai.Decision{Accepted: result.Accepted, Reason: result.Reason}
	return f.finishScreeningRound(ctx, prepared, candidate, id, 1, "keyword", started, &decision, 0, nil)
}

// completeScreeningAttempt 为实际未进入第二轮的尝试保存明确原因，已完成的真实评分不被动作结果覆盖。
func (f *Flow) completeScreeningAttempt(ctx context.Context, id, reason string) error {
	if id == "" || f.Store == nil {
		return nil
	}
	_, err := f.Store.UpdateScreeningRecord(context.WithoutCancel(ctx), id, func(item *screening.Snapshot) error {
		screening.CloseInterrupted(item, limitScreeningReason(reason))
		return nil
	})
	return err
}

// finalizeScreeningTask 结束未消费的并发预筛结果，达到招呼上限和用户停止都不能丢失已经实际调用的第一轮。
func (f *Flow) finalizeScreeningTask(ctx context.Context, prepared shared.PreparedTask, stats shared.Stats, runErr error) {
	if f.Store == nil || screeningsync.OwnerKey(prepared.Session) == "" || prepared.Request.TaskID == "" {
		return
	}
	reason := "本轮已结束，未进入第二轮详情评分"
	if prepared.Position.MatchLimit > 0 && stats.Succeeded >= prepared.Position.MatchLimit {
		reason = "已达到本次打招呼上限，未进入第二轮详情评分"
	}
	if shared.GracefulStopRequested(ctx) || errors.Is(runErr, context.Canceled) {
		reason = "任务已停止或取消，未进入第二轮详情评分"
	} else if runErr != nil {
		reason = "任务处理失败，未进入第二轮详情评分：" + runErr.Error()
	}
	if err := f.Store.FinishScreeningTask(context.WithoutCancel(ctx), prepared.Request.TaskID, limitScreeningReason(reason)); err != nil {
		f.log(prepared.Request.TaskID, "save_screening_result", "warning", time.Now(), err)
	}
}

// logScreeningRound 将每轮完整原因写入普通日志；状态与耗时独立于后续招呼和结构化响应。
func (f *Flow) logScreeningRound(taskID string, candidate model.Candidate, number int, round screening.Round, started time.Time) {
	if logger, ok := f.Logger.(interface {
		ScreeningRound(string, string, int, screening.Round)
	}); ok {
		logger.ScreeningRound(taskID, candidateDisplayName(candidate), number, round)
		return
	}
	step := "screening_first"
	if number == 2 {
		step = "screening_second"
	}
	label := "未完成"
	switch round.Status {
	case "passed":
		label = "通过"
	case "rejected":
		label = "不通过"
	case "cancelled":
		label = "已取消"
	}
	f.log(taskID, step, round.Status, started, fmt.Errorf("候选人“%s”%s，原因：%s", candidateDisplayName(candidate), label, round.Reason))
}

// limitScreeningReason 与跨端合同保持最多16000字符，保留模型的完整正常原因而不是旧版30字截断。
func limitScreeningReason(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 16000 {
		return string(runes[:16000])
	}
	return string(runes)
}
