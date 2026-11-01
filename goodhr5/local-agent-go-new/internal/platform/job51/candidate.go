// Package job51 文件作用：实现前程无忧候选人列表读取、定位、滚动和翻页。
package job51

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回前程无忧当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	loading, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.loading")
	if err != nil {
		return nil, err
	}
	if loading {
		if err = waitForCandidateResults(ctx, browser, cfg); err != nil {
			return nil, err
		}
	}
	candidates, err := common.FindCandidates(ctx, identityBrowser{browser}, cfg, r.PlatformID(), options)
	if err == nil {
		for index := range candidates {
			stableID := strings.TrimSpace(candidates[index].Fields["platform_candidate_id"])
			if !numericCandidateID(stableID) {
				return nil, fmt.Errorf("前程无忧有张候选人卡片的身份编号还没读完整，这批先不操作，请等列表加载好后重试")
			}
			candidates[index].Fields["platform_candidate_id"] = stableID
			candidates[index].Fingerprint = r.PlatformID() + "_" + stableID
		}
		return candidates, nil
	}
	if !common.IsElementMissing(err) {
		return nil, err
	}
	empty, probeErr := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.empty")
	if probeErr != nil {
		return nil, probeErr
	}
	if empty {
		return []model.Candidate{}, nil
	}
	return nil, err
}

// waitForCandidateResults 只探测加载层和首张卡片，等页面就绪后由主流程读取一次完整名单；明确空态才视为空结果。
func waitForCandidateResults(ctx context.Context, browser model.Browser, cfg model.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	card, err := common.RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return err
	}
	stableSince := time.Time{}
	for {
		loading, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.loading")
		if err != nil {
			return err
		}
		if loading {
			stableSince = time.Time{}
		} else {
			empty, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.empty")
			if err != nil {
				return err
			}
			if empty {
				return nil
			}
			items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: card, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
			if err != nil && !common.IsElementMissing(err) {
				return err
			}
			if len(items) > 0 {
				if stableSince.IsZero() {
					stableSince = time.Now()
				}
				if time.Since(stableSince) >= 600*time.Millisecond {
					return nil
				}
			} else {
				stableSince = time.Time{}
			}
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("前程无忧候选人列表还没加载好，请看一下平台页面后重试：%w", ctx.Err())
		case <-timer.C:
		}
	}
}

// ScrollToCandidate 通过真实滚轮定位指定前程无忧候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	cfg, candidate, err := scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	selector, err := common.CandidateSelector(cfg, candidate)
	if err != nil {
		return err
	}
	anchor, err := common.RequiredSelector(cfg, "candidate.list")
	if err != nil {
		return err
	}
	full := true
	request := contract.ScrollRequest{Target: &selector, WheelAnchor: &anchor, Distance: 260, MaxAttempts: 30, WaitMS: 180, RequireFull: &full, ViewportMargin: 48}
	result, err := browser.Scroll(ctx, request)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		if result.After == nil || result.After.Box.Width <= 0 || result.After.Box.Height <= 0 || !result.After.Visible {
			return fmt.Errorf("前程无忧卡片位置还没确认，这次先不点击，请重新读取名单")
		}
		if result.After.Box.Y >= float64(cfg.Behavior.CandidateTopInset)-1 {
			return nil
		}
		if attempt >= 3 {
			return fmt.Errorf("前程无忧固定页头还挡着候选人，这次先不点击，请把目标卡片向下移一点再开始")
		}
		distance := -int(math.Ceil(float64(cfg.Behavior.CandidateTopInset) - result.After.Box.Y + 16))
		if _, err = browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &anchor, Distance: distance, MaxAttempts: 1, WaitMS: 180}); err != nil {
			return err
		}
		request.MaxAttempts = 1
		result, err = browser.Scroll(ctx, request)
		if err != nil {
			return err
		}
	}
}

// scrollCandidateAction 用真实滚轮把精确绑定的按钮移入上下安全区，并完整露出；宽卡片仅相交不能证明按钮可点击。
func (r *Runtime) scrollCandidateAction(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, key string) error {
	if err := r.ScrollToCandidate(ctx, browser, cfg, candidate); err != nil {
		return err
	}
	scopedConfig, scopedCandidate, err := scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	target, err := common.CandidateActionSelector(scopedConfig, key, scopedCandidate)
	if err != nil {
		return err
	}
	anchor, err := common.RequiredSelector(cfg, "candidate.list")
	if err != nil {
		return err
	}
	full := true
	request := contract.ScrollRequest{Target: &target, WheelAnchor: &anchor, Distance: 260, MaxAttempts: 30, WaitMS: 180, RequireFull: &full, ViewportMargin: 48}
	result, err := browser.Scroll(ctx, request)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		view := result.After
		if view == nil || !view.Visible || view.Box.Width <= 0 || view.Box.Height <= 0 || view.Viewport.Width <= 0 || view.Viewport.Height <= 0 ||
			view.Box.X < -1 || view.Box.X+view.Box.Width > view.Viewport.Width+1 {
			return fmt.Errorf("前程无忧操作按钮还没完整露出，这次先不点击，请调整窗口后再开始")
		}
		top := math.Max(0, float64(cfg.Behavior.CandidateTopInset))
		bottom := view.Viewport.Height - math.Max(0, float64(cfg.Behavior.CandidateBottomInset))
		if bottom-top < view.Box.Height-1 {
			return fmt.Errorf("前程无忧窗口太矮，按钮放不进安全区域，这次先不点击，请把窗口拉高后再开始")
		}
		if view.Box.Y >= top-1 && view.Box.Y+view.Box.Height <= bottom+1 {
			return nil
		}
		if attempt >= 3 {
			return fmt.Errorf("前程无忧页头或底部浮层还挡着按钮，这次先不点击，请调整目标卡片后再开始")
		}
		// 平台真实小滚轮会衰减，保留足够步长，最终仍以按钮几何位置决定是否可操作。
		distance := max(120, int(math.Ceil(view.Box.Y+view.Box.Height-bottom+64)))
		if view.Box.Y < top-1 {
			distance = -max(120, int(math.Ceil(top-view.Box.Y+64)))
		}
		if _, err := browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &anchor, Distance: distance, MaxAttempts: 1, WaitMS: 180}); err != nil {
			return err
		}
		request.MaxAttempts = 1
		result, err = browser.Scroll(ctx, request)
		if err != nil {
			return err
		}
	}
}

// AdvanceCandidateList 通过公共真实滚轮能力加载更多前程无忧候选人。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	return common.AdvanceCandidateList(ctx, identityBrowser{browser}, cfg, r.PlatformID(), before, options)
}
