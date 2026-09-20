// Package zhaopin 文件作用：实现智联职位弹层选择和基础筛选。
package zhaopin

import (
	"context"
	"fmt"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 在智联职位选择弹层选择任务岗位。
func (r *Runtime) SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	before, err := common.FindCandidates(ctx, browser, cfg, cfg.ID, model.CandidateListOptions{})
	if err != nil && !common.IsElementMissing(err) {
		return fmt.Errorf("读取切岗前的候选人列表失败：%w", err)
	}
	cfg.Behavior.DirectPositionSelection = true
	cfg.Behavior.SelectFirstPositionResult = true
	if err := common.SelectPosition(ctx, browser, cfg, position); err != nil {
		return err
	}
	// 智联先关闭岗位弹层、后异步刷新推荐卡片，不能把旧岗位名单交给主流程。
	_, err = common.WaitForCandidateListChange(ctx, browser, cfg, cfg.ID, before)
	if err != nil {
		return fmt.Errorf("等待岗位推荐列表刷新失败：%w", err)
	}
	return common.WaitForStableCandidateList(ctx, browser, cfg, cfg.ID)
}

// ApplyBasicFilters 按平台配置顺序应用智联基础筛选。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Position) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
