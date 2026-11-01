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

// GreetCandidate 沿用公共招呼动作，必须确认当前卡片进入已沟通状态才返回成功。
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
	for attempt := 0; attempt < 10; attempt++ {
		scopedConfig, scopedCandidate, scopeErr := scopeCandidate(ctx, browser, cfg, candidate)
		if scopeErr != nil && !errors.Is(scopeErr, errCandidateCardMissing) {
			return fmt.Errorf("%w：确认招呼卡片身份失败：%w", model.ErrGreetingUnconfirmed, scopeErr)
		}
		confirmed := false
		var err error
		if scopeErr == nil {
			confirmed, err = common.CandidateActionExists(ctx, browser, scopedConfig, scopedCandidate, "candidate.continue")
		}
		if err != nil {
			return fmt.Errorf("%w：前程无忧招呼后的状态没读到：%w", model.ErrGreetingUnconfirmed, err)
		}
		if confirmed {
			return nil
		}
		if attempt == 9 {
			break
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
