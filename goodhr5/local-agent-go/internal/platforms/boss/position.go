// Package boss 文件作用：承载 position.go 对应的平台职责实现。
package boss

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// CurrentPositionName 读取 Boss 当前岗位名称。
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

// SelectPosition 在 Boss 页面切换岗位。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，positionName 为目标岗位。
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
	if searchInput := platformElement(cfg, "position", "searchInput"); searchInput != nil {
		handled, searchErr := r.selectPositionBySearch(ctx, exec, searchInput, item, itemText, positionName)
		if handled {
			return searchErr
		}
		if searchErr != nil {
			exec.Log("warning", "岗位搜索框不可用，回退为列表滚动查找："+searchErr.Error())
		}
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      item,
		"visible_only": true,
		"fields":       []any{map[string]any{"position_name": itemText}},
	})
	if err != nil {
		return err
	}
	items := mapList(workerData(result, "items"))
	exec.Log("info", fmt.Sprintf("岗位列表共查找到 %d 个岗位项", len(items)))
	target := normalizePositionName(positionName)
	for _, found := range items {
		fields := mapFromAny(found["fields"])
		name := firstNonEmpty(stringFromMap(fields, "position_name"), stringFromMap(found, "text"))
		exec.Log("info", fmt.Sprintf("岗位列表项：index=%d name=%s", intFromMap(found, "index"), name))
		if target == "" || !strings.Contains(normalizePositionName(name), target) {
			continue
		}
		exec.Log("info", "找到匹配岗位，准备点击："+name)
		elementRef := stringFromMap(found, "element_ref")
		if elementRef == "" {
			elementRef = stringFromMap(found, "ref")
		}
		if elementRef == "" {
			exec.Log("warning", "岗位列表项缺少元素引用，回退为按序号点击："+name)
			_, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
				"index": intFromMap(found, "index"),
				"item":  item,
			})
			return err
		}
		exec.Log("info", fmt.Sprintf("准备滚动到匹配岗位：index=%d name=%s", intFromMap(found, "index"), name))
		scrollResult, err := exec.Post(ctx, "/api/v1/page/ensure-visible", map[string]any{
			"element_ref":     elementRef,
			"wheel_target":    list,
			"distance":        120,
			"wait_ms":         260,
			"max_attempts":    10,
			"viewport_margin": 24,
			"require_full":    true,
			"vertical_only":   true,
		})
		if err != nil {
			return err
		}
		scrollData := workerDataMap(scrollResult)
		finalView := mapFromAny(scrollData["final_view"])
		exec.Log("info", fmt.Sprintf("匹配岗位滚动完成：attempts=%d visible=%v full=%v", len(mapList(scrollData["attempts"])), finalView["in_viewport"], finalView["fully_visible"]))
		exec.Log("info", "匹配岗位已滚动到可点击区域，准备点击："+name)
		_, err = exec.Post(ctx, "/api/v1/page/click", map[string]any{
			"element_ref":     elementRef,
			"delay_before":    0.15,
			"viewport_margin": 40,
			"require_full":    true,
			"timeout":         10000,
		})
		return err
	}
	return fmt.Errorf("岗位列表中未找到岗位：%s，请确认岗位模板名称是否和Boss直聘岗位名称一致", positionName)
}

// selectPositionBySearch 通过 Boss 岗位搜索框切换到目标岗位。
// ctx 为运行上下文，exec 为执行器，searchInput 为搜索框定位，item 为岗位项定位，itemText 为岗位名称定位，positionName 为目标岗位名。
func (r *Runtime) selectPositionBySearch(ctx context.Context, exec platformcore.Executor, searchInput map[string]any, item map[string]any, itemText map[string]any, positionName string) (bool, error) {
	query := positionSearchQuery(positionName)
	if query == "" {
		return false, nil
	}
	exec.Log("info", "准备通过岗位搜索框查找："+query)
	if _, err := exec.Post(ctx, "/api/v1/page/type", map[string]any{
		"element": searchInput,
		"text":    query,
		"timeout": 10000,
	}); err != nil {
		return false, fmt.Errorf("输入岗位搜索关键词失败：%w", err)
	}
	target := normalizePositionName(positionName)
	var found map[string]any
	var name string
	for attempt := 1; attempt <= 4; attempt++ {
		if err := exec.Delay(ctx, "等待岗位搜索结果刷新", 0.4); err != nil {
			return true, err
		}
		result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
			"element":      item,
			"visible_only": true,
			"fields":       []any{map[string]any{"position_name": itemText}},
		})
		if err != nil {
			return true, err
		}
		items := mapList(workerData(result, "items"))
		exec.Log("info", fmt.Sprintf("岗位搜索结果检查：第%d次，共%d项", attempt, len(items)))
		for _, candidate := range items {
			fields := mapFromAny(candidate["fields"])
			candidateName := firstNonEmpty(stringFromMap(fields, "position_name"), stringFromMap(candidate, "text"))
			if target == "" || !strings.Contains(normalizePositionName(candidateName), target) {
				continue
			}
			found = candidate
			name = candidateName
			break
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		_, clearErr := exec.Post(ctx, "/api/v1/page/type", map[string]any{
			"element": searchInput,
			"text":    "",
			"timeout": 10000,
		})
		if clearErr == nil {
			_ = exec.Delay(ctx, "清空岗位搜索关键词", 0.3)
		}
		return false, fmt.Errorf("岗位搜索没有匹配到：%s，已回退到完整岗位列表查找", positionName)
	}
	exec.Log("info", "岗位搜索已匹配，准备点击："+name)
	elementRef := stringFromMap(found, "element_ref")
	if elementRef == "" {
		elementRef = stringFromMap(found, "ref")
	}
	if elementRef == "" {
		exec.Log("warning", "岗位搜索结果缺少元素引用，回退为按序号点击："+name)
		_, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
			"index": intFromMap(found, "index"),
			"item":  item,
		})
		return true, err
	}
	_, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element_ref":     elementRef,
		"delay_before":    0.15,
		"viewport_margin": 24,
		"require_full":    true,
		"timeout":         10000,
	})
	return true, err
}
