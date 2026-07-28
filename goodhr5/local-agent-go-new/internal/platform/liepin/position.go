// Package liepin 文件作用：实现猎聘企业端岗位选择和基础筛选。
package liepin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 在猎聘企业端选择任务岗位。
func (r *Runtime) SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	return common.SelectPosition(ctx, browser, cfg, position)
}

// ApplyBasicFilters 按平台配置顺序应用猎聘企业端基础筛选。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Position) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
