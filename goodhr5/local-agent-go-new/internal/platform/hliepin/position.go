// Package hliepin 文件作用：实现猎聘猎头端岗位上下文和基础筛选。
package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 保存岗位和开聊模式，并按配置完整匹配猎聘快捷搜索。
func (r *Runtime) SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	r.positionName = strings.TrimSpace(position.Name)
	shortcutName := strings.TrimSpace(position.HLiepinShortcutSearchName)
	r.selectJobWhenGreeting = shortcutName == ""
	if shortcutName == "" {
		return nil
	}
	selector, err := common.RequiredSelector(cfg, "position.shortcut_item")
	if err != nil {
		return err
	}
	exact := true
	selector.Target.Text = shortcutName
	selector.Target.Texts = nil
	selector.Target.ExactText = &exact
	selector.Target.Index = nil
	selector.Description = "猎聘快捷搜索“" + shortcutName + "”"
	candidates, ok := cfg.Selectors["candidate.item"]
	if !ok {
		return fmt.Errorf("平台 %s 缺少选择器 candidate.item", cfg.ID)
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector,
		Verify: &contract.ClickVerification{
			TargetVisible: &candidates,
			TimeoutMS:     8000,
		},
		ViewportMargin: 24,
	}); err != nil {
		return fmt.Errorf("选择猎聘快捷搜索“%s”失败：%w", shortcutName, err)
	}
	return nil
}

// ApplyBasicFilters 按配置应用猎聘猎头端筛选，未配置时不改变用户手动条件。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Position) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
