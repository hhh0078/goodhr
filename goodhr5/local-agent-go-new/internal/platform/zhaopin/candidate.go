// Package zhaopin 文件作用：实现智联候选人列表读取、定位、滚动和翻页。
package zhaopin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回智联当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	return common.FindCandidates(ctx, browser, cfg, r.PlatformID(), options)
}

// ScrollToCandidate 通过真实滚轮定位指定智联候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// AdvanceCandidateList 在搜索页使用真实滚轮定位并点击下一页，推荐页继续沿用原无限滚动能力。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	paging, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.pagination")
	if err != nil {
		return false, err
	}
	if paging {
		nextPage, err := common.RequiredSelector(cfg, "candidate.next_page")
		if err != nil {
			return false, err
		}
		return common.AdvanceCandidatePageTarget(ctx, browser, cfg, r.PlatformID(), nextPage, 0, before, options)
	}
	return common.AdvanceCandidateList(ctx, browser, cfg, r.PlatformID(), before, options)
}
