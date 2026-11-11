// Package job51 文件作用：实现前程无忧候选人列表读取、定位、滚动和翻页。
package job51

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回前程无忧当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) (candidates []model.Candidate, err error) {
	ctx, trace := common.StartCandidateListTrace(ctx, r.PlatformID(), "find_candidates")
	defer func() { err = trace.Wrap(err) }()
	loading, err := probeCandidateState(ctx, browser, cfg, "candidate.loading")
	if err != nil {
		return nil, err
	}
	if loading {
		if err = waitForCandidateResults(ctx, browser, cfg); err != nil {
			return nil, err
		}
		trace.Budget(ctx, 0)
	}
	candidates, err = common.FindCandidates(ctx, identityBrowser{browser}, cfg, r.PlatformID(), options)
	if err == nil {
		trace.Begin("确认本批候选人身份", "candidate.item")
		for index := range candidates {
			stableID := strings.TrimSpace(candidates[index].Fields["platform_candidate_id"])
			if !numericCandidateID(stableID) {
				return nil, fmt.Errorf("前程无忧有张候选人卡片的身份编号还没读完整，这批先不操作，请等列表加载好后重试")
			}
			candidates[index].Fields["platform_candidate_id"] = stableID
			candidates[index].Fingerprint = r.PlatformID() + "_" + stableID
		}
		if err := r.rememberResumePoints(ctx, browser, cfg, candidates); err != nil {
			return nil, err
		}
		return candidates, nil
	}
	if !common.IsElementMissing(err) {
		return nil, err
	}
	empty, probeErr := probeCandidateState(ctx, browser, cfg, "candidate.empty")
	if probeErr != nil {
		return nil, probeErr
	}
	if empty {
		return []model.Candidate{}, nil
	}
	trace.Begin("未找到候选人卡片且页面未显示空结果", "candidate.item")
	return nil, err
}

// probeCandidateState 保存本次加载或空态检查的结果；未检查成功时保持未知。
func probeCandidateState(ctx context.Context, browser model.Browser, cfg model.Config, key string) (bool, error) {
	trace := common.CandidateTrace(ctx)
	if trace != nil {
		if key == "candidate.loading" {
			trace.Begin("检查列表加载提示", key)
			trace.Facts.LoadingVisible = nil
		} else {
			trace.Begin("检查列表空态", key)
			trace.Facts.EmptyVisible = nil
		}
	}
	visible, err := common.ProbeSelectorExists(ctx, browser, cfg, key)
	if trace != nil && err == nil {
		if key == "candidate.loading" {
			trace.Facts.LoadingVisible = &visible
		} else {
			trace.Facts.EmptyVisible = &visible
		}
		trace.Done()
	}
	return visible, err
}

// waitForCandidateResults 只探测加载层和首张卡片，等页面就绪后由主流程读取一次完整名单；明确空态才视为空结果。
func waitForCandidateResults(ctx context.Context, browser model.Browser, cfg model.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	trace := common.CandidateTrace(ctx)
	trace.Budget(ctx, 20*time.Second)
	trace.Begin("准备检查第一张卡片", "candidate.item")
	card, err := common.RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return err
	}
	stableSince := time.Time{}
	for round := 1; ; round++ {
		if trace != nil {
			trace.Facts.PollRound = &round
		}
		loading, err := probeCandidateState(ctx, browser, cfg, "candidate.loading")
		if err != nil {
			return err
		}
		if loading {
			stableSince = time.Time{}
		} else {
			empty, err := probeCandidateState(ctx, browser, cfg, "candidate.empty")
			if err != nil {
				return err
			}
			if empty {
				return nil
			}
			trace.Begin("探测第一张可读卡片", "candidate.item")
			items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: card, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
			if err != nil && !common.IsElementMissing(err) {
				return err
			}
			if trace != nil {
				n := len(items)
				trace.Facts.FirstCardCount = &n
			}
			trace.Done()
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
		trace.Begin("等待列表稳定", "candidate.item")
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("前程无忧候选人列表还没加载好，请看一下平台页面后重试：%w", ctx.Err())
		case <-timer.C:
			trace.Done()
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
	result, err := scrollCandidateTarget(ctx, browser, request)
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
	result, err := scrollCandidateTarget(ctx, browser, request)
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

// scrollCandidateTarget 在长距离定位耗尽轮数且只差48像素以内时，为同一精确目标补一段有限滚轮；断线、无进展或距离未知不重试。
// Windows 与两种Mac共用Worker几何回执，仍由最终可见区域和平台遮挡检查决定是否允许操作。
func scrollCandidateTarget(ctx context.Context, browser model.Browser, request contract.ScrollRequest) (contract.ScrollResult, error) {
	result, err := browser.Scroll(ctx, request)
	var workerErr *contract.WorkerError
	if ctx.Err() != nil || request.Target == nil || request.MaxAttempts != 30 || !errors.As(err, &workerErr) || workerErr == nil || workerErr.Body.Code != "SCROLL_FAILED" || workerErr.Body.Step != "scroll" {
		return result, err
	}
	var evidence struct {
		Attempts  int      `json:"attempts"`
		Remaining *float64 `json:"remaining_distance"`
	}
	if json.Unmarshal(workerErr.Body.Details, &evidence) != nil || evidence.Attempts != request.MaxAttempts || evidence.Remaining == nil || *evidence.Remaining < 0 || *evidence.Remaining > 48 {
		return result, err
	}
	// 只补一次，不递归；沿用绑定公开编号的选择器，元素换序或消失仍交由Worker正常报错。
	request.MaxAttempts = 20
	return browser.Scroll(ctx, request)
}

// AdvanceCandidateList 通过公共真实滚轮能力加载更多前程无忧候选人。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	ctx, trace := common.StartCandidateListTrace(ctx, r.PlatformID(), "advance_candidate_list")
	trace.Begin("准备滚动加载名单", "candidate.list")
	n := len(before)
	trace.Facts.BeforeCount = &n
	changed, err := common.AdvanceCandidateList(ctx, identityBrowser{browser}, cfg, r.PlatformID(), before, options)
	return changed, trace.Wrap(err)
}
