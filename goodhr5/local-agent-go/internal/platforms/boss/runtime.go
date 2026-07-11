// Package boss 提供 Boss 直聘平台的本地运行时实现。
package boss

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// Runtime 实现 Boss 平台运行时能力。
type Runtime struct{}

// NewRuntime 创建 Boss 平台运行时实例。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// OpenEntryPage 打开 Boss 入口页面。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，entryURL 为入口地址。
func (r *Runtime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	if strings.TrimSpace(entryURL) == "" {
		return fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	exec.Log("info", "入口页面打开成功："+entryURL)
	_, err := exec.Post(ctx, "/api/v1/page/open", map[string]any{"url": entryURL})
	return err
}

// PrepareEntryPage 处理 Boss 入口页可能出现的确认弹框。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置。
func (r *Runtime) PrepareEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) error {
	dialog := map[string]any{"selector": ".boss-dialog__body"}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", map[string]any{
		"element": dialog,
		"timeout": 1500,
	})
	if err != nil {
		return err
	}
	data := workerDataMap(result)
	if data["found"] != true && intFromMap(data, "count") <= 0 {
		exec.Log("info", "Boss 入口页未发现需要确认的弹框")
		return nil
	}
	exec.Log("info", "Boss 入口页发现确认弹框，准备点击确认")
	_, err = exec.Post(ctx, "/api/v1/page/click", map[string]any{
		"element": map[string]any{
			"selector":         ".confirm-btn",
			"parent_classes":   []any{[]any{".boss-dialog__body"}},
			"find_attempts":    2,
			"find_interval_ms": 300,
		},
		"timeout": 3000,
	})
	if err != nil {
		return err
	}
	return exec.Delay(ctx, "等待 Boss 弹框关闭", 0.3)
}

// IsTaskEntryPage 判断当前页面是否仍是 Boss 任务入口页面。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置。
func (r *Runtime) IsTaskEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (bool, error) {
	entry := platformEntryPage(cfg)
	if strings.TrimSpace(stringFromMap(entry, "url")) == "" {
		return false, fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	result, err := exec.Post(ctx, "/api/v1/page/list", map[string]any{})
	if err != nil {
		return false, err
	}
	pages := mapList(workerData(result, "pages"))
	if len(pages) == 0 {
		return false, nil
	}
	current := currentDefaultPage(pages)
	return pageMatchesEntry(stringFromMap(current, "url"), entry), nil
}

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
		_, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
			"index": intFromMap(found, "index"),
			"item":  item,
		})
		return err
	}
	return fmt.Errorf("岗位列表中未找到岗位：%s，请确认岗位模板名称是否和Boss直聘岗位名称一致", positionName)
}

// ListVisibleCandidates 提取当前可见 Boss 候选人。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，maxItems 为最多数量。
func (r *Runtime) ListVisibleCandidates(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]platformcore.Candidate, error) {
	startedAt := time.Now()
	exec.Log("info", fmt.Sprintf("候选人提取请求已发送：max_items=%d", maxItems))
	result, err := exec.Post(ctx, "/api/v1/boss/candidates/extract", map[string]any{
		"platform_config": cfg,
		"max_items":       maxItems,
	})
	if err != nil {
		exec.Log("warning", fmt.Sprintf("候选人提取请求失败：elapsed=%s err=%s", time.Since(startedAt).Round(time.Millisecond), err.Error()))
		return nil, err
	}
	data := workerDataMap(result)
	items := mapList(data["candidates"])
	exec.Log("info", fmt.Sprintf("候选人卡片提取返回：found=%d candidates=%d worker_find=%s worker_convert=%s total=%s",
		intFromMap(data, "found_count"),
		len(items),
		formatElapsedMS(intFromMap(data, "find_elapsed_ms")),
		formatElapsedMS(intFromMap(data, "convert_elapsed_ms")),
		formatElapsedMS(intFromMap(data, "elapsed_ms")),
	))
	convertStartedAt := time.Now()
	candidates := make([]platformcore.Candidate, 0, len(items))
	for index, item := range items {
		if index == 0 || (index+1)%20 == 0 || index+1 == len(items) {
			exec.Log("info", fmt.Sprintf("正在整理候选人：%d/%d", index+1, len(items)))
		}
		candidate := platformcore.Candidate(item)
		if id := r.CandidateFingerprint(candidate); id != "" {
			candidate["id"] = id
		}
		candidates = append(candidates, candidate)
	}
	exec.Log("info", fmt.Sprintf("候选人整理完成：有效=%d 整理耗时=%s 总耗时=%s", len(candidates), time.Since(convertStartedAt).Round(time.Millisecond), time.Since(startedAt).Round(time.Millisecond)))
	return candidates, nil
}

// ScrollCandidateList 滚动 Boss 候选人列表。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，distance 为滚动距离。
func (r *Runtime) ScrollCandidateList(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, distance int) error {
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/scroll", map[string]any{
		"platform_config": cfg,
		"distance":        distance,
	})
	return err
}

// FetchCandidateDetail 读取 Boss 候选人详情。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人，request 为详情请求。
func (r *Runtime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	name := candidateName(candidate)
	exec.Log("info", fmt.Sprintf("调用详情提取接口：name=%s mode=%s card_index=%d", name, detailModeLabel(request.Mode), intFromMap(candidate, "card_index")))
	result, err := exec.Post(ctx, "/api/v1/boss/candidates/detail", map[string]any{
		"platform_config": cfg,
		"card_index":      intFromMap(candidate, "card_index"),
		"element_ref":     stringFromMap(candidate, "element_ref"),
		"screenshot":      request.Mode == "ocr" || request.Mode == "ai",
		"force_scroll":    true,
		"dir":             filepath.Join(request.ScreenshotsDir, request.TaskID),
		"filename":        request.Filename,
	})
	if err != nil {
		return platformcore.DetailResult{}, err
	}
	data := workerDataMap(result)
	detailText := strings.TrimSpace(firstNonEmpty(stringFromMap(data, "detail_text"), stringFromMap(data, "text")))
	// 调试截图信息
	if dbg := stringFromMap(data, "_screenshot_debug"); dbg != "" {
		exec.Log("info", "详情截图调试: "+dbg)
	}
	screenshot := mapFromAny(data["screenshot"])
	if len(screenshot) > 0 {
		if scrollDebug := stringFromMap(screenshot, "_scroll_debug"); scrollDebug != "" {
			// exec.Log("info", "详情截图滚动调试: "+scrollDebug)
		}
		if partsCount := intFromMap(screenshot, "parts_count"); partsCount > 0 {
			exec.Log("info", fmt.Sprintf("详情截图分段完成：name=%s parts=%d scrollable=%v", name, partsCount, screenshot["scrollable_container"] == true))
		} else {
			exec.Log("info", fmt.Sprintf("详情截图无分段: name=%s width=%d height=%d scrollable=%v parts_count=%d", name, intFromMap(screenshot, "width"), intFromMap(screenshot, "height"), stringFromMap(screenshot, "scrollable_container") == "true", intFromMap(screenshot, "parts_count")))
		}
		screenshot = stitchDetailScreenshot(exec, request.TaskID, request.ScreenshotsDir, candidate, screenshot)
	} else {
		exec.Log("warning", "详情截图返回为空")
	}
	return platformcore.DetailResult{Text: detailText, Screenshot: screenshot, Source: request.Mode}, nil
}

// CloseCandidateDetail 关闭 Boss 候选人详情。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	return r.closeCandidateDetail(ctx, exec, cfg, candidateName(candidate))
}

// GreetCandidate 执行 Boss 打招呼。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	exec.Log("info", fmt.Sprintf("准备调用打招呼接口：name=%s", candidateName(candidate)))
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/greet", map[string]any{
		"platform_config": cfg,
		"card_index":      intFromMap(candidate, "card_index"),
		"element_ref":     stringFromMap(candidate, "element_ref"),
	})
	return err
}

// CandidateFilterText 返回 Boss 候选人筛选文本。
// candidate 为候选人。
func (r *Runtime) CandidateFilterText(candidate platformcore.Candidate) string {
	return strings.TrimSpace(firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text")))
}

// CandidateFingerprint 返回 Boss 候选人去重指纹。
// candidate 为候选人，仅使用姓名和年龄生成稳定 ID。
func (r *Runtime) CandidateFingerprint(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	name := firstNonEmpty(stringFromMap(candidate, "candidate_name"), stringFromMap(candidate, "name"), stringFromMap(fields, "name"))
	age := candidateAge(candidate)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(age) == "" {
		return ""
	}
	return "boss_" + normalizeCandidateIDPart(name) + "_" + normalizeCandidateIDPart(age)
}

// CleanCandidateDetailText 清理 Boss 详情文本中的平台附加内容。
// text 为 OCR、DOM 或 AI 提取出的详情文本。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	cutMarkers := []string{
		"牛人分析器",
	}
	for _, marker := range cutMarkers {
		if index := strings.Index(text, marker); index >= 0 {
			text = strings.TrimSpace(text[:index])
		}
	}
	return text
}

// candidateAge 读取 Boss 候选人年龄。
// candidate 为候选人卡片数据，优先读取结构字段，缺失时从文本中提取“xx岁”。
func candidateAge(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	age := firstNonEmpty(
		stringFromMap(candidate, "age"),
		stringFromMap(candidate, "candidate_age"),
		stringFromMap(fields, "age"),
		stringFromMap(fields, "candidate_age"),
	)
	if age != "" {
		return age
	}
	text := firstNonEmpty(stringFromMap(candidate, "raw_text"), stringFromMap(candidate, "filter_text"), stringFromMap(fields, "basic_info"))
	match := regexp.MustCompile(`([1-9][0-9]?)\s*岁`).FindStringSubmatch(text)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// normalizeCandidateIDPart 规范化 Boss 候选人 ID 组成部分。
// value 为姓名或年龄文本，返回去除空白后的文本。
func normalizeCandidateIDPart(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

// closeCandidateDetail 关闭 Boss 候选人详情。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidateName 为候选人名称。
func (r *Runtime) closeCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidateName string) error {
	name := strings.TrimSpace(candidateName)
	if name == "" {
		name = "候选人"
	}
	exec.Log("info", "正在关闭"+name+"详情")
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/detail/close", map[string]any{
		"platform_config": cfg,
		"key":             "Escape",
		"candidate_name":  name,
	})
	if err == nil {
		exec.Log("info", name+"详情已关闭")
	}
	return err
}
