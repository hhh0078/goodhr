// Package zhaopin 文件作用：实现智联职位弹层选择和基础筛选。
package zhaopin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 在智联职位选择弹层选择任务岗位。
func (r *Runtime) SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	cfg.Behavior.DirectPositionSelection = true
	cfg.Behavior.SelectFirstPositionResult = true
	return common.SelectPosition(ctx, browser, cfg, position)
}

// ApplyBasicFilters 按平台配置顺序应用智联基础筛选。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Position) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
