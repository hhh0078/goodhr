// Package hliepin 文件作用：在猎聘猎头候选人详情中选择默认分组、保存收藏并确认结果。
package hliepin

import (
	"context"
	"fmt"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FavoriteCandidate 收藏当前详情到默认未分组，已收藏直接跳过，每次点击均要求唯一匹配并验证。
func (r *Runtime) FavoriteCandidate(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	saved, err := common.ProbeUniqueSelectorExists(ctx, browser, cfg, "candidate.favorite_saved")
	if err != nil || saved {
		return err
	}
	button, err := common.RequiredSelector(cfg, "candidate.favorite")
	if err != nil {
		return err
	}
	panel, err := common.RequiredSelector(cfg, "candidate.favorite_panel")
	if err != nil {
		return err
	}
	group, err := common.RequiredSelector(cfg, "candidate.favorite_group")
	if err != nil {
		return err
	}
	selectedGroup, err := common.RequiredSelector(cfg, "candidate.favorite_group_selected")
	if err != nil {
		return err
	}
	save, err := common.RequiredSelector(cfg, "candidate.favorite_save")
	if err != nil {
		return err
	}
	savedSelector, err := common.RequiredSelector(cfg, "candidate.favorite_saved")
	if err != nil {
		return err
	}
	anchor, err := common.RequiredSelector(cfg, "candidate.detail")
	if err != nil {
		return err
	}
	// 详情评分前已向下浏览，收藏按钮可能在屏幕上方；用真实滚轮回到按钮，再重新查找点击。
	// 置顶收藏入口会贴在窗口上边缘，只需有安全落点，不能要求它离开固定栏。
	requireFull := false
	if _, err = browser.Scroll(ctx, contract.ScrollRequest{
		Target: &button, WheelAnchor: &anchor, Distance: 520,
		MaxAttempts: 40, WaitMS: 120, RequireFull: &requireFull,
	}); err != nil {
		return fmt.Errorf("滚动到猎聘详情收藏按钮失败：%w", err)
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: button,
		Verify: &contract.ClickVerification{TargetVisible: &panel, TimeoutMS: 5000},
	}); err != nil {
		return fmt.Errorf("打开猎聘收藏分组失败：%w", err)
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: group, ViewportMargin: 24,
		Verify: &contract.ClickVerification{TargetVisible: &selectedGroup, TimeoutMS: 5000},
	}); err != nil {
		return fmt.Errorf("选择猎聘默认收藏分组失败：%w", err)
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: save, ViewportMargin: 24,
		Verify: &contract.ClickVerification{TargetVisible: &savedSelector, TargetHidden: &panel, TimeoutMS: 5000},
	}); err != nil {
		return fmt.Errorf("猎聘候选人收藏尚未确认：%w", err)
	}
	return nil
}
