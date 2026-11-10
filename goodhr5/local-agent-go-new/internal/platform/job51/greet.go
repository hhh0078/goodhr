// Package job51 文件作用：实现前程无忧打招呼、收藏和不合适动作。
package job51

import (
	"context"
	"errors"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// GreetCandidate 只点击一次目标卡片，出现招呼职位弹框才选岗，同一稳定编号变为已沟通才返回成功。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.GreetRequest) error {
	if _, configured := cfg.Selectors["candidate.continue"]; !configured {
		return fmt.Errorf("%w：前程无忧招呼完成状态还没配置，这次先不发送，请更新后再试", model.ErrGreetingUnconfirmed)
	}
	blocked, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_blocker")
	if err != nil {
		return err
	}
	if blocked {
		return fmt.Errorf("%w：平台聊天点数提示挡住按钮，请先关闭提示并重新开始，我不会自动重发", model.ErrGreetingUnconfirmed)
	}
	jobDialog, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_job_dialog")
	if err != nil {
		return fmt.Errorf("%w：检查招呼职位弹框失败：%w", model.ErrGreetingUnconfirmed, err)
	}
	if jobDialog {
		return fmt.Errorf("%w：页面上还有未处理的招呼职位弹框，请先关闭后再试", model.ErrGreetingUnconfirmed)
	}
	scopedConfig, scopedCandidate, err := scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return fmt.Errorf("%w：候选人身份还没确认，先不发送：%w", model.ErrGreetingUnconfirmed, err)
	}
	alreadyContacted, err := common.CandidateActionExists(ctx, browser, scopedConfig, scopedCandidate, "candidate.continue")
	if err != nil {
		return err
	}
	if alreadyContacted {
		return model.ErrCandidateAlreadyContacted
	}
	if err := r.scrollCandidateAction(ctx, browser, cfg, candidate, "candidate.greet"); err != nil {
		return fmt.Errorf("%w：候选人位置还没确认，先不发送：%w", model.ErrGreetingUnconfirmed, err)
	}
	scopedConfig, scopedCandidate, err = scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	if err := common.GreetCandidate(ctx, browser, scopedConfig, scopedCandidate, request); err != nil {
		if errors.Is(err, model.ErrCandidateAlreadyContacted) {
			return err
		}
		return fmt.Errorf("%w：前程无忧招呼操作没有确认完成：%w", model.ErrGreetingUnconfirmed, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	confirmedJob := false
	for time.Now().Before(deadline) {
		jobDialog, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_job_dialog")
		if err != nil {
			return fmt.Errorf("%w：检查招呼职位弹框失败：%w", model.ErrGreetingUnconfirmed, err)
		}
		if jobDialog && !confirmedJob {
			if err := selectGreetingJob(ctx, browser, cfg, r.positionName); err != nil {
				return fmt.Errorf("%w：%w", model.ErrGreetingUnconfirmed, err)
			}
			confirmedJob = true
			deadline = time.Now().Add(10 * time.Second)
			continue
		}
		scopedConfig, scopedCandidate, scopeErr := scopeCandidate(ctx, browser, cfg, candidate)
		if scopeErr != nil && !errors.Is(scopeErr, errCandidateCardMissing) {
			return fmt.Errorf("%w：确认招呼卡片身份失败：%w", model.ErrGreetingUnconfirmed, scopeErr)
		}
		confirmed := false
		if scopeErr == nil {
			confirmed, err = common.CandidateActionExists(ctx, browser, scopedConfig, scopedCandidate, "candidate.continue")
		}
		if err != nil {
			return fmt.Errorf("%w：前程无忧招呼后的状态没读到：%w", model.ErrGreetingUnconfirmed, err)
		}
		if confirmed && !jobDialog {
			jobDialog, err = common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_job_dialog")
			if err != nil {
				return fmt.Errorf("%w：复核招呼职位弹框失败：%w", model.ErrGreetingUnconfirmed, err)
			}
			if !jobDialog {
				return nil
			}
			continue
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w：招呼后确认中断：%w", model.ErrGreetingUnconfirmed, ctx.Err())
		case <-timer.C:
		}
	}
	return fmt.Errorf("%w：前程无忧卡片还没显示已沟通，请先看平台提示再继续", model.ErrGreetingUnconfirmed)
}

// FavoriteCandidate 当前前程无忧暂不支持自动收藏，直接跳过。
func (r *Runtime) FavoriteCandidate(context.Context, model.Browser, model.Config, model.Candidate) error {
	return nil
}

// RejectCandidate 将指定前程无忧候选人标记为不合适。
func (r *Runtime) RejectCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	cfg, candidate, err := scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.reject")
}
