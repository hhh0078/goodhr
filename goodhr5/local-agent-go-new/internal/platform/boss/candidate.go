// Package boss 文件作用：实现 Boss 候选人列表读取、定位、滚动和翻页。
package boss

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回 Boss 当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	candidates, err := common.FindCandidates(ctx, browser, cfg, r.PlatformID(), options)
	if err != nil {
		return nil, err
	}
	for index := range candidates {
		candidate := &candidates[index]
		if strings.TrimSpace(candidate.Fields["recommendation_marker"]) == "" {
			continue
		}
		// 保留列表序号和同名出现次数；只读检查确认无招呼入口，不点击推荐卡片。
		selector, err := common.CandidateActionSelector(cfg, "candidate.greet", *candidate)
		if err != nil {
			return nil, err
		}
		selector.State = "attached"
		selector.TimeoutMS = 350
		buttons, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
			Selector: selector, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true, Diagnostic: true,
		})
		if err != nil && !common.IsElementMissing(err) {
			return nil, fmt.Errorf("检查推荐卡片打招呼入口失败：%w", err)
		}
		if len(buttons) == 0 {
			candidate.SkipReason = "推荐卡片没有打招呼入口，已跳过"
		}
	}
	return candidates, nil
}

// ScrollToCandidate 通过真实滚轮定位指定 Boss 候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// AdvanceCandidateList 通过公共真实滚轮能力加载更多 Boss 候选人。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	return common.AdvanceCandidateList(ctx, browser, cfg, r.PlatformID(), before, options)
}
