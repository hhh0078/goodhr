// Package hliepin 本文件负责猎聘关键词、快捷搜索和发布职位的二选一搜索流程。
package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

const (
	hliepinKeywordInputSelector = ".search-auto-complete-box .auto-input-wrap-v3 input"
	hliepinSearchButtonSelector = ".search-auto-complete-box button.search-btn"
	hliepinShortcutItemSelector = ".quick-search-box li.save .info"
	hliepinPublishedJobSelector = ".quick-search-box li.job .job-name"
	hliepinExpandShortcuts      = ".quick-search-box:has(li.save) .control"
	hliepinExpandJobsSelector   = ".quick-search-box:has(li.job) .control"
)

// PreparePositionSearch 根据关键词是否为空，在“关键词+快捷搜索”和“发布职位匹配”之间严格二选一。
func (r *Runtime) PreparePositionSearch(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionSnapshot map[string]any) error {
	positionName := strings.TrimSpace(stringFromMap(positionSnapshot, "name"))
	if positionName == "" {
		return fmt.Errorf("猎聘任务岗位名称为空，无法匹配快捷搜索或正在发布的职位，任务已停止")
	}
	commonConfig := mapFromAny(positionSnapshot["common_config"])
	keyword := strings.TrimSpace(stringFromMap(commonConfig, "hliepin_search_keyword"))
	if keyword == "" {
		exec.Log("info", "猎聘候选人搜索：未填写搜索关键词，改用正在发布的职位匹配，任务岗位="+positionName)
		return r.selectPublishedPosition(ctx, exec, positionName)
	}
	return r.searchKeywordWithShortcut(ctx, exec, positionName, keyword)
}

// searchKeywordWithShortcut 输入关键词并选择被任务岗位名称包含的快捷搜索项。
func (r *Runtime) searchKeywordWithShortcut(ctx context.Context, exec platformcore.Executor, positionName string, keyword string) error {
	exec.Log("info", "猎聘候选人搜索：使用关键词+快捷搜索，关键词="+keyword+"，任务岗位="+positionName)
	if _, err := exec.Post(ctx, "/api/v1/page/type", map[string]any{
		// 提示文字由 Ant Design 组件绘制，不是 input.placeholder；通过顶部搜索容器定位真正可编辑的输入框。
		"element": map[string]any{"selector": hliepinKeywordInputSelector},
		"text":    keyword, "timeout": 10000,
	}); err != nil {
		return fmt.Errorf("输入猎聘搜索关键词失败：%w", err)
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": hliepinSearchButtonSelector}, "timeout": 8000,
	}); err != nil {
		return fmt.Errorf("点击猎聘搜索按钮失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待猎聘关键词搜索结果刷新", 0.6); err != nil {
		return err
	}
	if err := r.expandSearchItemsIfPresent(ctx, exec, hliepinExpandShortcuts, "快捷搜索"); err != nil {
		return err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinShortcutItemSelector},
		"visible_only": true, "max_items": 100,
	})
	if err != nil {
		return fmt.Errorf("查找猎聘快捷搜索失败，任务已停止：%w", err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 {
		return fmt.Errorf("未找到猎聘快捷搜索列表，无法按任务岗位“%s”选择快捷搜索，任务已停止", positionName)
	}
	matchIndex, matchName := matchingShortcutItem(items, positionName)
	if matchIndex < 0 {
		return fmt.Errorf("猎聘快捷搜索中没有被任务岗位“%s”包含的项目，当前快捷搜索=%s，任务已停止", positionName, searchItemNames(items))
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"element": map[string]any{"selector": hliepinShortcutItemSelector},
		"index":   matchIndex, "timeout": 8000,
	}); err != nil {
		return fmt.Errorf("点击猎聘快捷搜索“%s”失败，任务已停止：%w", matchName, err)
	}
	exec.Log("info", "猎聘候选人搜索：快捷搜索已选择="+matchName)
	r.currentPosition = positionName
	if err := exec.Delay(ctx, "等待猎聘快捷搜索结果刷新", 1.2); err != nil {
		return err
	}
	return r.ensureHiddenCandidateFilters(ctx, exec)
}

// selectPublishedPosition 展开正在发布的职位并按任务岗位名称选择对应职位。
func (r *Runtime) selectPublishedPosition(ctx context.Context, exec platformcore.Executor, positionName string) error {
	if err := r.expandSearchItemsIfPresent(ctx, exec, hliepinExpandJobsSelector, "正在发布的职位"); err != nil {
		return err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinPublishedJobSelector},
		"visible_only": true, "max_items": 200,
	})
	if err != nil {
		return fmt.Errorf("读取猎聘正在发布的职位失败，任务已停止：%w", err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 {
		return fmt.Errorf("展开后未找到猎聘正在发布的职位，任务岗位=%s，任务已停止", positionName)
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": positionName, "exact": true,
		"element":         map[string]any{"selector": hliepinPublishedJobSelector},
		"resolve_tooltip": true, "tooltip_wait_ms": 300, "timeout": 8000,
	}); err != nil {
		return fmt.Errorf("猎聘正在发布的职位中未找到任务岗位“%s”，当前职位=%s，任务已停止：%w", positionName, searchItemNames(items), err)
	}
	exec.Log("info", "猎聘候选人搜索：正在发布的职位已选择="+positionName)
	r.currentPosition = positionName
	if err := exec.Delay(ctx, "等待猎聘职位候选人结果刷新", 1.2); err != nil {
		return err
	}
	return r.ensureHiddenCandidateFilters(ctx, exec)
}

// expandSearchItemsIfPresent 在对应搜索分组显示展开入口时先点击展开；入口不存在表示列表无需展开。
func (r *Runtime) expandSearchItemsIfPresent(ctx context.Context, exec platformcore.Executor, selector string, label string) error {
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element": map[string]any{"selector": selector}, "visible_only": true, "max_items": 1,
	})
	if err != nil {
		return fmt.Errorf("检查猎聘%s展开入口失败：%w", label, err)
	}
	if len(mapList(workerData(result, "items"))) == 0 {
		exec.Log("info", "猎聘候选人搜索："+label+"未显示展开入口，无需展开")
		return nil
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{"selector": selector}, "timeout": 3000,
	}); err != nil {
		return fmt.Errorf("展开猎聘%s失败：%w", label, err)
	}
	exec.Log("info", "猎聘候选人搜索："+label+"已展开")
	return exec.Delay(ctx, "等待猎聘"+label+"展开", 0.3)
}

// ensureHiddenCandidateFilters 在完成岗位或快捷搜索选择后强制勾选三个隐藏候选人条件。
func (r *Runtime) ensureHiddenCandidateFilters(ctx context.Context, exec platformcore.Executor) error {
	for _, label := range []string{"隐藏已查看", "隐藏已沟通", "隐藏已获取联系方式"} {
		if _, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"distance": -10000, "skip_mouse_move": true}); err != nil {
			return fmt.Errorf("设置猎聘筛选“%s”前回到页面顶部失败：%w", label, err)
		}
		if _, err := exec.Post(ctx, "/api/v1/page/ensure-checked-by-text", map[string]any{
			"text": label, "required": true, "timeout": 3500, "viewport_margin": 20,
		}); err != nil {
			return fmt.Errorf("勾选猎聘筛选“%s”失败：%w", label, err)
		}
		exec.Log("info", "猎聘候选人搜索：已勾选"+label)
	}
	return nil
}

// matchingShortcutItem 返回被岗位名称包含的最长快捷搜索项，避免较短的同类项被优先误选。
func matchingShortcutItem(items []map[string]any, positionName string) (int, string) {
	target := normalizeSearchMatchText(positionName)
	matchIndex := -1
	matchName := ""
	matchLength := 0
	for index, item := range items {
		name := strings.TrimSpace(stringFromMap(item, "text"))
		normalized := normalizeSearchMatchText(name)
		if normalized == "" || !strings.Contains(target, normalized) || len([]rune(normalized)) <= matchLength {
			continue
		}
		matchIndex = index
		matchName = name
		matchLength = len([]rune(normalized))
	}
	return matchIndex, matchName
}

// normalizeSearchMatchText 统一搜索项比较时的大小写并移除空白。
func normalizeSearchMatchText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

// searchItemNames 汇总页面搜索项名称，用于任务停止时输出可核查的错误信息。
func searchItemNames(items []map[string]any) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name := strings.TrimSpace(stringFromMap(item, "text")); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "无"
	}
	return strings.Join(names, "、")
}
