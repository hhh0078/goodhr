// Package zhaopin 文件作用：实现智联候选人列表读取、定位、滚动和翻页。
package zhaopin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回智联当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Candidate, error) {
	return common.FindCandidates(ctx, browser, cfg, r.PlatformID())
}

// ScrollToCandidate 通过真实滚轮定位指定智联候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// AdvanceCandidateList 通过公共真实滚轮能力加载更多智联候选人。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate) (bool, error) {
	return common.AdvanceCandidateList(ctx, browser, cfg, r.PlatformID(), before)
}
