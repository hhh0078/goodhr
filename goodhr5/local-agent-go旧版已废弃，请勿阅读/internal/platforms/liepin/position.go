// Package liepin 文件作用：承载 position.go 对应的平台职责实现。
package liepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// CurrentPositionName 读取当前页面岗位名称。
func (r *Runtime) CurrentPositionName(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (string, error) {
	current := platformElement(cfg, "position", "current")
	if current == nil {
		return "", fmt.Errorf("平台配置中无当前岗位选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", map[string]any{"element": current, "timeout": 2500})
	if err != nil {
		return "", err
	}
	data := workerDataMap(result)
	name := normalizePositionName(firstNonEmpty(stringFromMap(data, "text"), firstStringFromAny(data["texts"])))
	if name == "" {
		return "", fmt.Errorf("页面当前岗位为空")
	}
	return name, nil
}

// SelectPosition 在猎聘企业端页面切换岗位。
func (r *Runtime) SelectPosition(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionName string) error {
	switchButton := platformElement(cfg, "position", "switchBtn")
	if switchButton == nil {
		return fmt.Errorf("平台配置中无岗位选择入口")
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{"element": switchButton, "timeout": 10000}); err != nil {
		return err
	}
	if err := exec.Delay(ctx, "等待岗位列表展开", 0.5); err != nil {
		return err
	}
	list := platformElement(cfg, "position", "list")
	item := positionListItemElement(list, platformElement(cfg, "position", "item"))
	itemText := platformElement(cfg, "position", "itemText")
	if item == nil || itemText == nil {
		return fmt.Errorf("平台配置中无岗位列表或岗位文字选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{"element": item, "visible_only": true, "fields": []any{map[string]any{"position_name": itemText}}})
	if err != nil {
		return err
	}
	items := mapList(workerData(result, "items"))
	target := normalizePositionName(positionName)
	for _, found := range items {
		fields := mapFromAny(found["fields"])
		name := firstNonEmpty(stringFromMap(fields, "position_name"), stringFromMap(found, "text"))
		if target == "" || !strings.Contains(normalizePositionName(name), target) {
			continue
		}
		_, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(found, "index"), "item": item})
		return err
	}
	return fmt.Errorf("岗位列表中未找到岗位：%s，请确认岗位模板名称是否和%s岗位名称一致", positionName, r.platformName)
}
