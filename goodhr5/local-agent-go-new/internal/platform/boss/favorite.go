// Package boss 文件作用：在候选人详情中完成收藏并验证状态，已收藏时保持原状态。
package boss

import (
	"context"
	"fmt"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FavoriteCandidate 只点击当前详情中唯一的未收藏按钮，等待已收藏状态，不重复取消收藏。
func (r *Runtime) FavoriteCandidate(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	saved, err := common.ProbeUniqueSelectorExists(ctx, browser, cfg, "candidate.favorite_saved")
	if err != nil || saved {
		return err
	}
	button, err := common.RequiredSelector(cfg, "candidate.favorite")
	if err != nil {
		return err
	}
	savedSelector, err := common.RequiredSelector(cfg, "candidate.favorite_saved")
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: button, ViewportMargin: 24,
		Verify: &contract.ClickVerification{TargetVisible: &savedSelector, TimeoutMS: 5000},
	})
	if err != nil {
		return fmt.Errorf("Boss候选人收藏尚未确认：%w", err)
	}
	return nil
}
