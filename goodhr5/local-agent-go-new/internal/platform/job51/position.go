// Package job51 文件作用：实现前程无忧职位弹层选择和基础筛选。
package job51

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SelectPosition 记录任务岗位供招呼弹框使用，保留手动搜索结果；推荐页保留同一岗位，或在内联列表切换岗位。
func (r *Runtime) SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	manualSearch, err := common.CurrentPageUsesManualSearch(ctx, browser, cfg)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(position.Name)
	r.positionName = name
	if manualSearch {
		return nil
	}
	if name == "" {
		return fmt.Errorf("岗位名称还没填，请复制平台里的完整岗位名称后再开始")
	}
	if err := waitForGreetingPageReady(ctx, browser, cfg); err != nil {
		return err
	}
	current, found, err := common.ReadOptional(ctx, browser, cfg, "position.current")
	if err != nil {
		return fmt.Errorf("读取前程无忧当前岗位失败：%w", err)
	}
	if found && samePositionName(current, name) {
		return nil
	}
	searchPage, err := common.ProbeSelectorExists(ctx, browser, cfg, "position.open")
	if err != nil {
		return err
	}
	if searchPage {
		if err = common.ClickRequired(ctx, browser, cfg, "position.open"); err != nil {
			return fmt.Errorf("打开前程无忧岗位列表失败：%w", err)
		}
	}
	itemSelector, err := common.RequiredSelector(cfg, "position.item")
	if err != nil {
		return err
	}
	itemText, err := common.RequiredSelector(cfg, "position.item_text")
	if err != nil {
		return err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: itemSelector, MaxItems: 200, Fields: map[string]contract.SelectorSpec{"position_name": itemText},
	})
	if err != nil {
		return fmt.Errorf("读取前程无忧岗位列表失败：%w", err)
	}
	index := -1
	selectedName := ""
	for _, item := range items {
		if samePositionName(item.Fields["position_name"], name) {
			if index >= 0 {
				return fmt.Errorf("前程无忧有多个同名岗位，请先在平台里手动选好目标岗位后再开始")
			}
			index = item.Index
			selectedName = strings.TrimSpace(item.Fields["position_name"])
		}
	}
	if index < 0 {
		return fmt.Errorf("前程无忧岗位列表里没找到“%s”，请核对平台里的完整岗位名称", name)
	}
	clickTarget, err := common.CandidateScopedSelectorWithParent(cfg, "position.item", "position.click_target", index)
	if err != nil {
		return err
	}
	selected := cfg.Selectors["position.current"].MapVariants(func(spec contract.SelectorSpec) contract.SelectorSpec {
		exact := true
		spec.Target.Text = selectedName
		spec.Target.ExactText = &exact
		spec.Target.Index = nil
		return spec
	})
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: clickTarget, ViewportMargin: 24,
		Verify: &contract.ClickVerification{TargetVisible: &selected, TimeoutMS: 5000},
	}); err != nil {
		return fmt.Errorf("选择前程无忧岗位失败：%w", err)
	}
	return waitForCandidateResults(ctx, browser, cfg)
}

// samePositionName 只忽略大小写和排版空白，保留括号内容，避免把不同方向的同名岗位认成同一个。
func samePositionName(current string, target string) bool {
	current = strings.Join(strings.Fields(current), "")
	target = strings.Join(strings.Fields(target), "")
	return current != "" && target != "" && strings.EqualFold(current, target)
}

// ApplyBasicFilters 保留手动搜索条件，并兼容未配置手动搜索地址时的旧筛选方式。
func (r *Runtime) ApplyBasicFilters(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	manualSearch, err := common.CurrentPageUsesManualSearch(ctx, browser, cfg)
	if err != nil || manualSearch {
		return err
	}
	if keyword := strings.TrimSpace(position.Keyword); keyword != "" {
		searchPage, err := common.ProbeSelectorExists(ctx, browser, cfg, "search.input")
		if err != nil {
			return err
		}
		if searchPage {
			if err = common.InputRequired(ctx, browser, cfg, "search.input", keyword); err != nil {
				return fmt.Errorf("输入前程无忧人才搜索词失败：%w", err)
			}
			if err = common.ClickRequired(ctx, browser, cfg, "search.submit"); err != nil {
				return fmt.Errorf("提交前程无忧人才搜索失败：%w", err)
			}
			if err = waitForCandidateResults(ctx, browser, cfg); err != nil {
				return fmt.Errorf("等待前程无忧搜索结果失败：%w", err)
			}
		}
	}
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.FilterActions)
}
