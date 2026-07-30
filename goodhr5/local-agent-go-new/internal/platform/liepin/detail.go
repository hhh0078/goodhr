// Package liepin 文件作用：实现猎聘企业端候选人详情打开、提取和关闭。
package liepin

import (
	"context"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenCandidateDetail 打开指定猎聘企业端候选人详情。
func (r *Runtime) OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	if err := common.OpenCandidateDetail(ctx, browser, cfg, candidate); err != nil {
		return err
	}
	return common.ClickOptional(ctx, browser, cfg, "candidate.detail_guide_confirm")
}

// ExtractCandidateDetail 提取当前猎聘企业端候选人详情文本。
func (r *Runtime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) (model.CandidateDetail, error) {
	return common.ExtractCandidateDetail(ctx, browser, cfg)
}

// BrowseCandidateDetail 在 AI 判断前使用真实滚轮浏览猎聘企业端详情弹框。
func (r *Runtime) BrowseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.BrowseCandidateDetail(ctx, browser, cfg, 360, 6)
}

// CleanCandidateDetailText 清理猎聘企业端候选人详情两端空白。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// CloseCandidateDetail 关闭当前猎聘企业端候选人详情。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.CloseCandidateDetail(ctx, browser, cfg)
}
