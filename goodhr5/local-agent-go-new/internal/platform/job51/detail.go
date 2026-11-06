// Package job51 文件作用：实现前程无忧候选人详情打开、提取和关闭。
package job51

import (
	"context"
	"errors"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenCandidateDetail 打开指定前程无忧候选人详情。
func (r *Runtime) OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	if err := r.scrollCandidateAction(ctx, browser, cfg, candidate, "candidate.open_target"); err != nil {
		return err
	}
	cfg, candidate, err := scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	closing := &closeMouseBrowser{Browser: browser, closeDescription: cfg.Selectors["candidate.detail_close"].Description}
	err = common.OpenCandidateDetail(ctx, closing, cfg, candidate)
	if closing.moveErr != nil {
		return errors.Join(err, model.ErrCandidateDetailCleanupFailed, closing.moveErr)
	}
	return err
}

// ExtractCandidateDetail 提取当前前程无忧候选人详情文本。
func (r *Runtime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) (model.CandidateDetail, error) {
	return common.ExtractCandidateDetail(ctx, browser, cfg)
}

// CleanCandidateDetailText 清理前程无忧候选人详情两端空白。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// BrowseCandidateDetail 在 AI 判断前使用真实滚轮浏览前程无忧详情弹框。
func (r *Runtime) BrowseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.BrowseCandidateDetail(ctx, browser, cfg, 320, 4)
}

// CloseCandidateDetail 沿用公共关闭流程；仅确认同一详情仍可见时额外再关闭一次，避免平台短暂不响应。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	visible, probeErr := common.ProbeUniqueSelectorExists(ctx, browser, cfg, "candidate.detail_dialog")
	if probeErr != nil {
		return probeErr
	}
	if !visible {
		return nil
	}
	closing := &closeMouseBrowser{Browser: browser}
	firstErr := common.CloseCandidateDetail(ctx, closing, cfg)
	if closing.moveErr != nil {
		return errors.Join(firstErr, closing.moveErr)
	}
	if firstErr == nil || ctx.Err() != nil || contract.IsSessionUnavailable(firstErr) {
		return firstErr
	}
	visible, probeErr = common.ProbeUniqueSelectorExists(ctx, browser, cfg, "candidate.detail_dialog")
	if probeErr != nil {
		return errors.Join(firstErr, probeErr)
	}
	if !visible {
		return nil
	}
	if retryErr := common.CloseCandidateDetail(ctx, closing, cfg); retryErr != nil || closing.moveErr != nil {
		return errors.Join(firstErr, retryErr, closing.moveErr)
	}
	return nil
}
