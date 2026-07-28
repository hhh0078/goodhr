// Package boss 文件作用：实现 Boss 候选人列表读取、定位、滚动和翻页。
package boss

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回 Boss 当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Candidate, error) {
	return common.FindCandidates(ctx, browser, cfg, r.PlatformID())
}

// ScrollToCandidate 通过真实滚轮定位指定 Boss 候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// NextCandidatePage 尝试进入 Boss 候选人下一页。
func (r *Runtime) NextCandidatePage(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	return common.NextCandidatePage(ctx, browser, cfg)
}

// ScrollCandidates 通过真实滚轮加载更多 Boss 候选人。
func (r *Runtime) ScrollCandidates(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.ScrollCandidates(ctx, browser, cfg)
}
