// Package boss 文件作用：实现 Boss 候选人详情打开、提取和关闭。
package boss

import (
	"context"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenCandidateDetail 打开指定 Boss 候选人详情。
func (r *Runtime) OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.OpenCandidateDetail(ctx, browser, cfg, candidate)
}

// ExtractCandidateDetail 提取当前 Boss 候选人详情文本。
func (r *Runtime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) (model.CandidateDetail, error) {
	return common.ExtractCandidateDetail(ctx, browser, cfg)
}

// CleanCandidateDetailText 清除 Boss 详情中的牛人分析器等平台附加内容。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.Index(text, "牛人分析器"); index >= 0 {
		text = strings.TrimSpace(text[:index])
	}
	return text
}

// CloseCandidateDetail 关闭当前 Boss 候选人详情。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.CloseCandidateDetail(ctx, browser, cfg)
}
