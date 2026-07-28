// Package hliepin 文件作用：实现猎聘猎头端岗位上下文和基础筛选。
package hliepin

import (
	"context"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 保存岗位和开聊模式；候选人搜索仍沿用用户当前手动结果。
func (r *Runtime) SelectPosition(_ context.Context, _ model.Browser, _ model.Config, position model.Position) error {
	r.positionName = strings.TrimSpace(position.Name)
	r.selectJobWhenGreeting = strings.TrimSpace(position.HLiepinShortcutSearchName) == ""
	return nil
}

// ApplyBasicFilters 按配置应用猎聘猎头端筛选，未配置时不改变用户手动条件。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Position) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
