// Package zhaopin 文件作用：承载 position.go 对应的平台职责实现。
package zhaopin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// CurrentPositionName 读取智联招聘当前岗位名称。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置。
func (r *Runtime) CurrentPositionName(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (string, error) {
	current := platformElement(cfg, "position", "current")
	if current == nil {
		return "", fmt.Errorf("平台配置中无当前岗位选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", map[string]any{"element": current, "timeout": 3000})
	if err != nil {
		return "", err
	}
	data := workerDataMap(result)
	name := firstNonEmpty(stringFromMap(data, "text"), firstStringFromAny(data["texts"]))
	if name == "" {
		exec.Log("warning", fmt.Sprintf("页面当前岗位提取为空：found=%v count=%d text_len=%d target=%s parent=%s frame=%s", data["found"], intFromMap(data, "count"), len(stringFromMap(data, "text")), stringFromMap(data, "selector"), stringFromMap(data, "parent_selector"), stringFromMap(data, "frame_url")))
		return "", fmt.Errorf("页面当前岗位为空")
	}
	return name, nil
}

// SelectPosition 在智联招聘页面通过职位选择弹层切换岗位。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，positionName 为目标岗位。
func (r *Runtime) SelectPosition(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionName string) error {
	query := positionSearchQuery(positionName)
	if query == "" {
		return fmt.Errorf("岗位运行岗位名称为空")
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": "a[zp-stat-id=\"talent_more_jobs\"]"},
		"timeout": 10000,
	}); err != nil {
		return fmt.Errorf("打开智联职位选择弹层失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待智联职位选择弹层展开", 0.5); err != nil {
		return err
	}
	searchInput := map[string]any{"selector": ".job-side-selector__filter input"}
	if _, err := exec.Post(ctx, "/api/v1/page/type", map[string]any{
		"element": searchInput,
		"text":    query,
		"timeout": 10000,
	}); err != nil {
		return fmt.Errorf("输入智联岗位搜索关键词失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待智联岗位搜索结果刷新", 0.7); err != nil {
		return err
	}
	item := map[string]any{"selector": ".job-side-selector__item"}
	itemText := map[string]any{"selector": ".job-side-selector__title"}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      item,
		"visible_only": true,
		"max_items":    20,
		"fields":       []any{map[string]any{"position_name": itemText}},
	})
	if err != nil {
		return fmt.Errorf("读取智联岗位搜索结果失败：%w", err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 {
		return fmt.Errorf("智联岗位搜索没有结果：%s", query)
	}
	first := items[0]
	name := firstNonEmpty(stringFromMap(mapFromAny(first["fields"]), "position_name"), stringFromMap(first, "text"))
	elementRef := firstNonEmpty(stringFromMap(first, "element_ref"), stringFromMap(first, "ref"))
	if elementRef == "" {
		return fmt.Errorf("智联第一条岗位搜索结果缺少元素引用")
	}
	exec.Log("info", fmt.Sprintf("智联岗位搜索完成：query=%s count=%d first=%s", query, len(items), name))
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element_ref":  elementRef,
		"require_full": false,
		"timeout":      10000,
	}); err != nil {
		return fmt.Errorf("点击智联第一条岗位搜索结果失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待智联岗位切换完成", 0.8); err != nil {
		return err
	}
	panelResult, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": ".job-side-selector"},
		"visible_only": true,
		"max_items":    1,
	})
	if err != nil {
		return fmt.Errorf("确认智联职位选择弹层状态失败：%w", err)
	}
	if len(mapList(workerData(panelResult, "items"))) > 0 {
		return fmt.Errorf("智联职位选择弹层未关闭")
	}
	return nil
}
