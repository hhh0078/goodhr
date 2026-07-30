// Package hliepin 文件作用：实现猎聘猎头端岗位上下文和基础筛选。
package hliepin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 保存后端岗位名称，并按配置完整匹配猎聘快捷搜索。
func (r *Runtime) SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	r.positionName = strings.TrimSpace(position.Name)
	shortcutName := strings.TrimSpace(position.HLiepinShortcutSearchName)
	if cfg.Behavior.SkipPositionSelection {
		return nil
	}
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
	if _, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Home", DelayMS: 120}); err != nil {
		return fmt.Errorf("选择猎聘快捷搜索前回到页面顶部失败：%w", err)
	}
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
	currentPage, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.current_page")
	if err != nil {
		return fmt.Errorf("读取猎聘当前候选人页码失败：%w", err)
	}
	if found {
		pageNumber, parseErr := strconv.Atoi(strings.TrimSpace(currentPage))
		if parseErr == nil && pageNumber > 0 {
			r.nextCandidatePage = pageNumber + 1
		}
	}
	return nil
}

// ApplyBasicFilters 按配置应用猎聘猎头端筛选，未配置时不改变用户手动条件。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Position) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
