// Package common 文件作用：提供所有招聘平台复用的岗位查找、选择和复核能力。
package common

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var positionSearchNotePattern = regexp.MustCompile(`（[^（）]*）|\([^()]*\)`)

// SelectPosition 先核对当前岗位，再按配置搜索、选择并复核目标岗位。
func SelectPosition(ctx context.Context, browser model.Browser, cfg model.Config, position model.Position) error {
	if cfg.Behavior.SkipPositionSelection {
		return nil
	}
	positionName := strings.TrimSpace(position.Name)
	if positionName == "" {
		return fmt.Errorf("岗位名称为空，无法选择平台岗位")
	}
	if !cfg.Behavior.DirectPositionSelection {
		if current, found, err := ReadOptional(ctx, browser, cfg, "position.current"); err != nil {
			return fmt.Errorf("读取当前岗位失败：%w", err)
		} else if found && positionNamesMatch(current, positionName) {
			return nil
		}
	}
	if err := ClickOptional(ctx, browser, cfg, "position.open"); err != nil {
		return fmt.Errorf("打开岗位列表失败：%w", err)
	}
	if _, ok := cfg.Selectors["position.input"]; ok {
		if err := InputRequired(ctx, browser, cfg, "position.input", PositionSearchQuery(positionName)); err != nil {
			return fmt.Errorf("输入岗位搜索词失败：%w", err)
		}
	}
	selector, ok := cfg.Selectors["position.item"]
	if !ok {
		return fmt.Errorf("平台 %s 缺少选择器 position.item", cfg.ID)
	}
	fields := make(map[string]contract.SelectorSpec)
	if itemText, exists := cfg.Selectors["position.item_text"]; exists {
		fields["position_name"] = itemText
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 200, Fields: fields,
	})
	if err != nil {
		return fmt.Errorf("读取岗位列表失败：%w", err)
	}
	matchIndex := -1
	if cfg.Behavior.SelectFirstPositionResult && len(items) > 0 {
		matchIndex = items[0].Index
	} else {
		matchIndex = matchingPositionIndex(items, positionName)
	}
	if matchIndex < 0 {
		return fmt.Errorf("岗位列表中没有找到“%s”", positionName)
	}
	clickSelector, err := IndexedSelector(cfg, "position.item", matchIndex)
	if err != nil {
		return err
	}
	if _, hasClickTarget := cfg.Selectors["position.click_target"]; hasClickTarget {
		clickSelector, err = CandidateScopedSelectorWithParent(cfg, "position.item", "position.click_target", matchIndex)
		if err != nil {
			return err
		}
	}
	var verify *contract.ClickVerification
	if panel, exists := cfg.Selectors["position.panel"]; exists {
		verify = &contract.ClickVerification{TargetHidden: &panel, TimeoutMS: 2000}
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: clickSelector, ViewportMargin: 24, Verify: verify,
	}); err != nil {
		return fmt.Errorf("点击目标岗位失败：%w", err)
	}
	if cfg.Behavior.DirectPositionSelection {
		return nil
	}
	current, found, err := ReadOptional(ctx, browser, cfg, "position.current")
	if err != nil {
		return fmt.Errorf("复核当前岗位失败：%w", err)
	}
	if found && !positionNamesMatch(current, positionName) {
		return fmt.Errorf("岗位切换后页面显示“%s”，目标是“%s”", current, positionName)
	}
	return nil
}

// PositionSearchQuery 去掉岗位名称后缀和括号备注，生成页面搜索词。
func PositionSearchQuery(value string) string {
	original := strings.TrimSpace(value)
	query := original
	if index := strings.Index(query, " _ "); index >= 0 {
		query = query[:index]
	}
	query = strings.TrimSpace(positionSearchNotePattern.ReplaceAllString(query, ""))
	if query == "" {
		return original
	}
	return query
}

// matchingPositionIndex 返回最接近目标岗位名称的列表序号。
func matchingPositionIndex(items []contract.FindAllItem, positionName string) int {
	target := normalizePositionName(positionName)
	if target == "" {
		return -1
	}
	containsIndex := -1
	containsLength := 0
	for _, item := range items {
		name := firstNonEmpty(item.Fields["position_name"], item.Text)
		normalized := normalizePositionName(name)
		if normalized == target {
			return item.Index
		}
		if normalized == "" || (!strings.Contains(normalized, target) && !strings.Contains(target, normalized)) {
			continue
		}
		if length := len([]rune(normalized)); length > containsLength {
			containsIndex = item.Index
			containsLength = length
		}
	}
	return containsIndex
}

// positionNamesMatch 判断页面岗位和任务岗位是否为同一个岗位。
func positionNamesMatch(current string, target string) bool {
	currentName := normalizePositionName(current)
	targetName := normalizePositionName(target)
	return currentName != "" && targetName != "" &&
		(currentName == targetName || strings.Contains(currentName, targetName) || strings.Contains(targetName, currentName))
}

// normalizePositionName 清理岗位名称空白、配置后缀和括号备注。
func normalizePositionName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(PositionSearchQuery(value)), ""))
}
