// Package liepin 文件作用：实现猎聘企业端打招呼、收藏和不合适动作。
package liepin

import (
	"context"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// GreetCandidate 向指定猎聘企业端候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.GreetRequest) error {
	alreadyContacted, err := common.CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
	if err != nil {
		return err
	}
	if alreadyContacted {
		return model.ErrCandidateAlreadyContacted
	}
	if err := common.CandidateAction(ctx, browser, cfg, candidate, "candidate.greet"); err != nil {
		return err
	}
	// 只有同一卡片变成“继续沟通”才算成功，权益不足或弹窗拦截不能只凭点击计数。
	return verifyGreetingSent(ctx, browser, cfg, candidate)
}

// verifyGreetingSent 等待当前候选人按钮确认发送状态，无法确认时停止整轮而不盲目重发。
func verifyGreetingSent(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	selector, err := common.CandidateActionSelector(cfg, "candidate.continue", candidate)
	if err != nil {
		return err
	}
	selector.TimeoutMS = 200
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		items, findErr := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, ExpectedMissing: true})
		if findErr != nil && !common.IsElementMissing(findErr) {
			return findErr
		}
		if len(items) > 0 {
			return nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return model.ErrGreetingUnconfirmed
		case <-timer.C:
		}
	}
}

// FavoriteCandidate 收藏指定猎聘企业端候选人。
func (r *Runtime) FavoriteCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.favorite")
}

// RejectCandidate 将指定猎聘企业端候选人标记为不合适。
func (r *Runtime) RejectCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.reject")
}
