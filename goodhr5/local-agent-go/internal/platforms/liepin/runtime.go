// Package liepin 提供猎聘企业端平台的本地运行时实现。
package liepin

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// Runtime 实现猎聘企业端平台运行时能力。
type Runtime struct {
	platformID   string
	platformName string
}

// NewRuntime 创建猎聘企业端平台运行时实例。
func NewRuntime() *Runtime { return &Runtime{platformID: "liepin", platformName: "猎聘企业端"} }

// OpenEntryPage 打开猎聘企业端入口页面。
func (r *Runtime) OpenEntryPage(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, entryURL string) error {
	if strings.TrimSpace(entryURL) == "" {
		return fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	exec.Log("info", "入口页面打开成功："+entryURL)
	_, err := exec.Post(ctx, "/api/v1/page/open", map[string]any{"url": entryURL})
	return err
}

// PrepareEntryPage 处理猎聘企业端入口页初始化动作。
func (r *Runtime) PrepareEntryPage(context.Context, platformcore.Executor, cloudapi.PlatformConfig) error {
	return nil
}

// IsTaskEntryPage 判断当前页面是否仍是猎聘企业端任务入口页。
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

// ListVisibleCandidates 提取当前可见猎聘企业端候选人。
func (r *Runtime) ListVisibleCandidates(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]platformcore.Candidate, error) {
	startedAt := time.Now()
	item := platformElement(cfg, "card", "item")
	if item == nil {
		return nil, fmt.Errorf("平台配置中无候选人卡片选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{"element": item, "visible_only": true, "fields": cardFieldRequests(cfg), "max_items": maxItems})
	if err != nil {
		return nil, err
	}
	rawItems := mapList(workerData(result, "items"))
	candidates := make([]platformcore.Candidate, 0, len(rawItems))
	for _, item := range rawItems {
		fields := mapFromAny(item["fields"])
		rawText := firstNonEmpty(stringFromMap(item, "text"), candidateRawText(fields))
		name := firstNonEmpty(stringFromMap(fields, "name"), fmt.Sprintf("候选人%d", intFromMap(item, "index")+1))
		candidates = append(candidates, platformcore.Candidate{
			"name":           name,
			"candidate_name": name,
			"status":         "scanned",
			"raw_text":       rawText,
			"filter_text":    rawText,
			"platform_id":    r.platformID,
			"card_index":     intFromMap(item, "index"),
			"element_ref":    stringFromMap(item, "ref"),
			"fields":         fields,
		})
	}
	exec.Log("info", fmt.Sprintf("候选人提取完成：count=%d elapsed=%s", len(candidates), formatElapsedMS(int(time.Since(startedAt).Milliseconds()))))
	return candidates, nil
}

// ScrollCandidateList 滚动猎聘企业端候选人列表。
func (r *Runtime) ScrollCandidateList(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, distance int) error {
	scroll := platformElement(cfg, "card", "scroll")
	if scroll != nil {
		_, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"element": scroll, "distance": distance})
		return err
	}
	_, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"distance": distance})
	return err
}

// FetchCandidateDetail 读取猎聘企业端新开详情页中的 DOM 文本。
func (r *Runtime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	if strings.ToLower(strings.TrimSpace(request.Mode)) != "dom" {
		return platformcore.DetailResult{}, fmt.Errorf("%s只支持 DOM 详情识别", r.platformName)
	}
	item := platformElement(cfg, "card", "item")
	if item == nil {
		return platformcore.DetailResult{}, fmt.Errorf("平台配置中无候选人卡片选择器")
	}
	clickTarget := platformElement(cfg, "detail", "openTarget")
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": clickTarget, "timeout": 10000}); err != nil {
		return platformcore.DetailResult{}, err
	}
	if err := exec.Delay(ctx, "等待猎聘详情页打开", 1.2); err != nil {
		return platformcore.DetailResult{}, err
	}
	content := platformElement(cfg, "detail", "content")
	payload := map[string]any{"timeout": 5000}
	if content != nil {
		payload["element"] = content
	}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", payload)
	if err != nil {
		return platformcore.DetailResult{}, err
	}
	data := workerDataMap(result)
	text := strings.TrimSpace(firstNonEmpty(stringFromMap(data, "text"), firstStringFromAny(data["texts"])))
	if text == "" {
		return platformcore.DetailResult{}, fmt.Errorf("猎聘详情页未读取到 DOM 文本")
	}
	return platformcore.DetailResult{Text: text, Source: "dom"}, nil
}

// CloseCandidateDetail 关闭猎聘企业端候选人详情页。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	closeBtn := platformElement(cfg, "detail", "closeBtn")
	if closeBtn != nil {
		_, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{"element": closeBtn, "timeout": 1500})
		if err == nil {
			return nil
		}
	}
	_, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape", "wait_ms": 200})
	return err
}

// GreetCandidate 执行猎聘企业端候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	item := platformElement(cfg, "card", "item")
	greetBtn := platformElement(cfg, "actions", "greetBtn")
	if item == nil || greetBtn == nil {
		return fmt.Errorf("平台配置中无候选人卡片或打招呼按钮选择器")
	}
	_, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": greetBtn, "timeout": 10000})
	return err
}

// CandidateFilterText 返回猎聘企业端候选人筛选文本。
func (r *Runtime) CandidateFilterText(candidate platformcore.Candidate) string {
	return strings.TrimSpace(firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text")))
}

// CandidateFingerprint 返回猎聘企业端候选人去重指纹。
func (r *Runtime) CandidateFingerprint(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	name := firstNonEmpty(stringFromMap(candidate, "candidate_name"), stringFromMap(candidate, "name"), stringFromMap(fields, "name"))
	age := candidateAge(candidate)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(age) == "" {
		return ""
	}
	return r.platformID + "_" + normalizeCandidateIDPart(name) + "_" + normalizeCandidateIDPart(age)
}

// CleanCandidateDetailText 清理猎聘企业端详情文本中的平台附加内容。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// candidateRawText 组装候选人卡片原始文本。
func candidateRawText(fields map[string]any) string {
	parts := []string{}
	for _, key := range []string{"name", "basic_info", "education", "university", "description"} {
		if text := stringFromMap(fields, key); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// candidateAge 读取猎聘企业端候选人年龄。
func candidateAge(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	age := firstNonEmpty(stringFromMap(candidate, "age"), stringFromMap(candidate, "candidate_age"), stringFromMap(fields, "age"), stringFromMap(fields, "candidate_age"))
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

// normalizeCandidateIDPart 规范化猎聘企业端候选人 ID 组成部分。
func normalizeCandidateIDPart(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}
