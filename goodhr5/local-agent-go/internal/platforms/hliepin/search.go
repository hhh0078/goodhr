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
	hliepinShortcutItemSelector = ".quick-search-box li.save .info"
	hliepinPublishedJobSelector = ".quick-search-box li.job .job-name"
	hliepinExpandShortcuts      = ".quick-search-box:has(li.save) .control"
	hliepinExpandJobsSelector   = ".quick-search-box:has(li.job) .control"
	hliepinReloadWaitTimeoutMS  = 5000
	hliepinReloadPollIntervalMS = 100
)

// PreparePositionSearch 根据快捷搜索名是否为空，在“快捷搜索”和“发布职位匹配”之间严格二选一。
func (r *Runtime) PreparePositionSearch(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionSnapshot map[string]any) error {
	positionName := strings.TrimSpace(stringFromMap(positionSnapshot, "name"))
	if positionName == "" {
		return fmt.Errorf("猎聘岗位运行岗位名称为空，无法匹配快捷搜索或正在发布的职位，岗位运行已停止")
	}
	commonConfig := mapFromAny(positionSnapshot["common_config"])
	shortcutName := strings.TrimSpace(stringFromMap(commonConfig, "hliepin_shortcut_search_name"))
	if shortcutName == "" {
		r.shouldSelectGreetJob = false
		exec.Log("info", "猎聘候选人搜索：未填写快捷搜索名，改用正在发布的职位匹配，岗位运行岗位="+positionName)
		if err := r.selectPublishedPosition(ctx, exec, positionName); err != nil {
			return err
		}
		r.shouldSelectGreetJob = true
		exec.Log("info", "猎聘候选人搜索：已匹配正在发布的职位，后续开聊弹框需要选择岗位")
		return r.ensureHiddenCandidateFilters(ctx, exec, commonConfig)
	}
	r.shouldSelectGreetJob = false
	if err := r.selectShortcutSearch(ctx, exec, positionName, shortcutName); err != nil {
		return err
	}
	exec.Log("info", "猎聘候选人搜索：已匹配快捷搜索，后续开聊弹框不选择岗位")
	return r.ensureHiddenCandidateFilters(ctx, exec, commonConfig)
}

// selectShortcutSearch 展开快捷搜索列表并按配置名称进行完整匹配。
func (r *Runtime) selectShortcutSearch(ctx context.Context, exec platformcore.Executor, positionName string, shortcutName string) error {
	exec.Log("info", "猎聘候选人搜索：准备选择快捷搜索，配置名称="+shortcutName)
	if err := r.expandSearchItemsIfPresent(ctx, exec, hliepinExpandShortcuts, "快捷搜索"); err != nil {
		return err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinShortcutItemSelector},
		"visible_only": true, "max_items": 100,
	})
	if err != nil {
		return fmt.Errorf("查找猎聘快捷搜索失败，岗位运行已停止：%w", err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 {
		return fmt.Errorf("未找到猎聘快捷搜索列表，无法选择配置的快捷搜索“%s”，岗位运行已停止", shortcutName)
	}
	matchIndex, matchName := matchingShortcutItem(items, shortcutName)
	if matchIndex < 0 {
		return fmt.Errorf("猎聘快捷搜索中没有与配置名称“%s”完全一致的项目，当前快捷搜索=%s，岗位运行已停止", shortcutName, searchItemNames(items))
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"element": map[string]any{"selector": hliepinShortcutItemSelector},
		"index":   matchIndex, "timeout": 8000,
	}); err != nil {
		return fmt.Errorf("点击猎聘快捷搜索“%s”失败，岗位运行已停止：%w", matchName, err)
	}
	exec.Log("info", "猎聘候选人搜索：快捷搜索已选择="+matchName)
	r.currentPosition = positionName
	if err := exec.Delay(ctx, "等待猎聘快捷搜索结果刷新", 1.2); err != nil {
		return err
	}
	return nil
}

// selectPublishedPosition 展开正在发布的职位并按岗位运行岗位名称选择对应职位。
func (r *Runtime) selectPublishedPosition(ctx context.Context, exec platformcore.Executor, positionName string) error {
	if err := r.expandSearchItemsIfPresent(ctx, exec, hliepinExpandJobsSelector, "正在发布的职位"); err != nil {
		return err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinPublishedJobSelector},
		"visible_only": true, "max_items": 200,
	})
	if err != nil {
		return fmt.Errorf("读取猎聘正在发布的职位失败，岗位运行已停止：%w", err)
	}
	items := mapList(workerData(result, "items"))
	if len(items) == 0 {
		return fmt.Errorf("展开后未找到猎聘正在发布的职位，岗位运行岗位=%s，岗位运行已停止", positionName)
	}
	matchIndex, matchName := matchingPublishedJobItem(items, positionName)
	if matchIndex < 0 {
		return fmt.Errorf("猎聘正在发布的职位中未找到岗位运行岗位“%s”，当前职位=%s，岗位运行已停止", positionName, searchItemNames(items))
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"element": map[string]any{"selector": hliepinPublishedJobSelector},
		"index":   matchIndex, "timeout": 8000,
	}); err != nil {
		return fmt.Errorf("点击猎聘正在发布的职位“%s”失败，岗位运行已停止：%w", matchName, err)
	}
	exec.Log("info", fmt.Sprintf("猎聘候选人搜索：正在发布的职位已选择=%s，岗位运行岗位=%s，列表序号=%d", matchName, positionName, matchIndex))
	r.currentPosition = positionName
	if err := exec.Delay(ctx, "等待猎聘职位候选人结果刷新", 1.2); err != nil {
		return err
	}
	return nil
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

// ensureHiddenCandidateFilters 按岗位配置勾选猎聘候选人隐藏条件，旧岗位缺少配置时保持三项全选。
func (r *Runtime) ensureHiddenCandidateFilters(ctx context.Context, exec platformcore.Executor, commonConfig map[string]any) error {
	for _, label := range configuredHiddenCandidateFilters(commonConfig) {
		if _, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"distance": -10000, "skip_mouse_move": true}); err != nil {
			return fmt.Errorf("设置猎聘筛选“%s”前回到页面顶部失败：%w", label, err)
		}
		if _, err := exec.Post(ctx, "/api/v1/page/ensure-checked-by-text", map[string]any{
			"text": label, "required": true,
			"timeout": hliepinReloadWaitTimeoutMS, "verify_timeout": hliepinReloadWaitTimeoutMS,
			"poll_interval_ms": hliepinReloadPollIntervalMS, "viewport_margin": 20,
		}); err != nil {
			return fmt.Errorf("勾选猎聘筛选“%s”失败：%w", label, err)
		}
		exec.Log("info", "猎聘候选人搜索：已勾选"+label)
	}
	return nil
}

// configuredHiddenCandidateFilters 返回岗位启用的猎聘候选人隐藏条件，字段缺失或类型异常时按启用处理。
func configuredHiddenCandidateFilters(commonConfig map[string]any) []string {
	options := []struct {
		key   string
		label string
	}{
		{key: "hliepin_hide_viewed", label: "隐藏已查看"},
		{key: "hliepin_hide_contacted", label: "隐藏已沟通"},
		{key: "hliepin_hide_contact_obtained", label: "隐藏已获取联系方式"},
	}
	labels := make([]string, 0, len(options))
	for _, option := range options {
		enabled, exists := commonConfig[option.key].(bool)
		if exists && !enabled {
			continue
		}
		labels = append(labels, option.label)
	}
	return labels
}

// matchingShortcutItem 返回与配置快捷搜索名完整一致的页面项目。
func matchingShortcutItem(items []map[string]any, shortcutName string) (int, string) {
	target := strings.TrimSpace(shortcutName)
	for index, item := range items {
		name := strings.TrimSpace(stringFromMap(item, "text"))
		if name == target {
			return index, name
		}
	}
	return -1, ""
}

// matchingPublishedJobItem 优先完整匹配发布职位；页面名称被省略号截断时，去掉省略号并按前六个字选择第一个匹配项。
func matchingPublishedJobItem(items []map[string]any, positionName string) (int, string) {
	target := normalizePublishedJobName(positionName)
	for index, item := range items {
		name := strings.TrimSpace(stringFromMap(item, "text"))
		if normalized := normalizePublishedJobName(name); normalized != "" && normalized == target {
			return index, name
		}
	}
	targetPrefix := firstRunes(target, 6)
	if len([]rune(targetPrefix)) < 6 {
		return -1, ""
	}
	for index, item := range items {
		name := strings.TrimSpace(stringFromMap(item, "text"))
		normalized := normalizePublishedJobName(name)
		if len([]rune(normalized)) >= 6 && firstRunes(normalized, 6) == targetPrefix {
			return index, name
		}
	}
	return -1, ""
}

// normalizePublishedJobName 统一发布职位比较文本，并移除页面截断产生的末尾英文句点或中文省略号。
func normalizePublishedJobName(value string) string {
	return strings.TrimRight(strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "")), ".…")
}

// firstRunes 返回字符串前 limit 个字符，字符不足时返回原字符串。
func firstRunes(value string, limit int) string {
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// searchItemNames 汇总页面搜索项名称，用于岗位运行停止时输出可核查的错误信息。
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
