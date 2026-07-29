// Package hliepin 文件作用：实现猎聘猎头端候选人详情打开、提取和关闭。
package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenCandidateDetail 打开指定猎聘猎头端候选人详情。
func (r *Runtime) OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	selector, err := common.CandidateActionSelector(cfg, "candidate.open_target", candidate)
	if err != nil {
		return err
	}
	result, err := browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 48,
		WaitForNewPage: true, NewPageTimeoutMS: 10000,
	})
	if err != nil {
		return err
	}
	if !result.NewPageOpened {
		return fmt.Errorf("猎聘候选人详情没有打开新页面")
	}
	return nil
}

// ExtractCandidateDetail 提取当前猎聘猎头端候选人详情文本。
func (r *Runtime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) (model.CandidateDetail, error) {
	return common.ExtractCandidateDetail(ctx, browser, cfg)
}

// CleanCandidateDetailText 清理猎聘猎头端候选人详情两端空白。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// BrowseCandidateDetail 在 AI 判断前使用真实滚轮把猎聘猎头端详情向下浏览。
func (r *Runtime) BrowseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.BrowseCandidateDetail(ctx, browser, cfg, 360, 8)
}

// CloseCandidateDetail 关闭当前猎聘猎头端候选人详情。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return browser.ClosePage(ctx)
}
