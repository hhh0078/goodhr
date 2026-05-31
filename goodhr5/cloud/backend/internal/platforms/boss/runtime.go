// Package boss 负责 Boss 平台运行时实现。
package boss

import (
	"fmt"
	"strings"

	"goodhr5/cloud/backend/internal/platformcore"
)

type localViewportResp struct {
	Ok         bool   `json:"ok"`
	InViewport bool   `json:"in_viewport"`
	Matched    string `json:"matched"`
}

type localElementItem struct {
	Ref    string         `json:"ref"`
	Index  int            `json:"index"`
	Fields map[string]any `json:"fields"`
}

type localFindElementsResp struct {
	Ok    bool               `json:"ok"`
	Items []localElementItem `json:"items"`
	Count int                `json:"count"`
}

type localExtractTextResp struct {
	Ok          bool     `json:"ok"`
	Text        string   `json:"text"`
	Texts       []string `json:"texts"`
	Matched     string   `json:"matched"`
	MatchedList []string `json:"matched_list"`
	Mode        string   `json:"mode"`
}

// Runtime 实现 Boss 平台运行时能力。
type Runtime struct{}

// NewRuntime 创建 Boss 平台运行时实例。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// OpenEntryPage 打开 Boss 入口页面。
func (r *Runtime) OpenEntryPage(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, cookies []map[string]any) error {
	url := authEntryURL(cfg.EntryPages)
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("平台配置中没有合法 auth.pages 入口页面")
	}
	exec.Log("info", fmt.Sprintf("正在打开Boss推荐页: %s", url))
	body := map[string]any{"url": url}
	if len(cookies) > 0 {
		exec.Log("info", fmt.Sprintf("打开Boss推荐页前补充注入 %d 条 cookie", len(cookies)))
		body["cookies"] = cookies
	}
	return exec.Post("/api/v1/page/open", body, nil)
}

// ListVisibleCandidates 提取当前可见 Boss 候选人摘要。
func (r *Runtime) ListVisibleCandidates(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig) ([]platformcore.Candidate, error) {
	if len(cfg.Card.FieldRequests) == 0 {
		return nil, fmt.Errorf("平台配置中无候选人字段选择器")
	}
	if cfg.Card.CardElement == nil {
		return nil, fmt.Errorf("平台配置中无候选人卡片定位配置")
	}
	var findResp localFindElementsResp
	if err := exec.Post("/api/v1/page/find-elements", map[string]any{
		"element":      cfg.Card.CardElement,
		"visible_only": true,
		"fields":       cfg.Card.FieldRequests,
	}, &findResp); err != nil {
		return nil, err
	}
	if findResp.Items == nil {
		findResp.Items = []localElementItem{}
	}
	exec.Log("info", fmt.Sprintf("查找到 %d 个当前可见Boss候选人卡片", len(findResp.Items)))
	candidates := make([]platformcore.Candidate, 0, len(findResp.Items))
	for _, item := range findResp.Items {
		fields := item.Fields
		if fields == nil {
			fields = map[string]any{}
		}
		exec.Log("info", fmt.Sprintf("Boss候选人卡片字段：index=%d ref=%s %s", item.Index, shortRef(item.Ref), summarizeCandidateFields(fields, cfg.Card.FieldRequests)))
		fields["_index"] = item.Index
		fields["element_ref"] = item.Ref
		candidates = append(candidates, r.MapFieldsToCandidate(cfg.PlatformID, fields))
	}
	return candidates, nil
}

// ScrollCandidateList 滚动到下一屏 Boss 候选人列表。
func (r *Runtime) ScrollCandidateList(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, prefs platformcore.RuntimePreferences) error {
	exec.Log("info", "正在滚动到下一屏Boss候选人列表")
	body := map[string]any{
		"max_scrolls": 1,
	}
	if cfg.Card.ScrollElement != nil {
		body["element"] = cfg.Card.ScrollElement
	}
	return exec.Post("/api/v1/page/scroll", body, nil)
}

// GreetCandidate 执行 Boss 候选人打招呼动作。
func (r *Runtime) GreetCandidate(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, prefs platformcore.RuntimePreferences, candidate platformcore.Candidate) error {
	exec.Log("info", fmt.Sprintf("正在执行Boss候选人打招呼动作 candidate=%s element_ref=%s", candidate.DisplayName(), shortRef(candidate.Runtime.ElementRef)))
	if err := clickActionWithinCandidate(exec, candidate, cfg.Actions.GreetBtn, prefs.GreetBeforeDelayMin, prefs.GreetBeforeDelayMax, "打招呼按钮"); err != nil {
		return err
	}
	if strings.TrimSpace(prefs.GreetMessage) == "" {
		exec.Log("info", "Boss岗位模板未配置打招呼语，跳过继续沟通/确认按钮")
		return nil
	}
	_ = clickOptionalAction(exec, cfg.Actions.ContinueBtn, 0.6, "继续沟通按钮")
	_ = clickOptionalAction(exec, cfg.Actions.ConfirmBtn, 0.6, "确认按钮")
	return nil
}

// OpenCandidateDetail 打开 Boss 候选人详情。
func (r *Runtime) OpenCandidateDetail(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, prefs platformcore.RuntimePreferences, candidate platformcore.Candidate) error {
	exec.Log("info", "正在打开Boss候选人详情")
	return clickActionWithinCandidate(exec, candidate, cfg.Detail.OpenTarget, prefs.DetailOpenDelayMin, prefs.DetailOpenDelayMax, "详情打开按钮")
}

// CloseCandidateDetail 关闭 Boss 候选人详情。
func (r *Runtime) CloseCandidateDetail(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, prefs platformcore.RuntimePreferences) error {
	exec.Log("info", "正在关闭Boss候选人详情（发送ESC）")
	if err := exec.Delay("关闭详情前", prefs.DetailCloseDelayMin, prefs.DetailCloseDelayMax); err != nil {
		return err
	}
	return exec.Post("/api/v1/page/press-key", map[string]any{
		"key": "Escape",
	}, nil)
}

// FetchCandidateDetailText 读取 Boss 候选人详情文本。
func (r *Runtime) FetchCandidateDetailText(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, prefs platformcore.RuntimePreferences, candidate platformcore.Candidate, detailMode string) (string, error) {
	if !cfg.Behavior.NeedsDetailPage {
		exec.Log("info", "Boss候选人详情无需详情页，跳过详情提取")
		return "", nil
	}
	if err := r.OpenCandidateDetail(exec, cfg, prefs, candidate); err != nil {
		return "", err
	}
	defer func() {
		if err := r.CloseCandidateDetail(exec, cfg, prefs); err != nil {
			exec.Log("warn", fmt.Sprintf("关闭Boss候选人详情失败：%v", err))
		}
	}()
	text, err := r.DetailContentText(exec, cfg, prefs, detailMode)
	if err != nil {
		return "", err
	}
	cleanedText := trimSimilarCandidateTail(text)
	if len(cleanedText) != len(strings.TrimSpace(text)) {
		exec.Log("info", "Boss候选人详情文本已截断：命中固定尾部文案=其他相似经历的牛人")
	}
	text = cleanedText
	exec.Log("info", fmt.Sprintf("Boss候选人详情文本提取完成，长度=%d", len(text)))
	return text, nil
}

// CandidateFilterText 返回 Boss 候选人筛选文本。
func (r *Runtime) CandidateFilterText(candidate platformcore.Candidate) string {
	return strings.TrimSpace(firstNonEmpty(candidate.FilterText, candidate.RawText))
}

// CandidateFingerprint 返回 Boss 候选人去重指纹。
func (r *Runtime) CandidateFingerprint(candidate platformcore.Candidate) string {
	parts := []string{
		"name=" + normalizeFingerprintText(candidate.Name),
		"edu=" + normalizeFingerprintText(candidate.EducationLevel),
		"university=" + normalizeFingerprintText(firstEducationSchool(candidate)),
		"desc=" + normalizeFingerprintText(candidate.PersonalDescription),
	}
	fingerprint := strings.Join(parts, "|")
	if strings.Trim(fingerprint, "| =") != "" {
		return fingerprint
	}
	return "raw=" + normalizeFingerprintText(candidate.RawText)
}

// MapFieldsToCandidate 将 Boss 原始字段映射为统一候选人模型。
func (r *Runtime) MapFieldsToCandidate(platformID string, fields map[string]any) platformcore.Candidate {
	return MapFieldsToCandidate(platformID, fields)
}

// DetailContentText 读取详情定位配置提取出的整段文本。
func (r *Runtime) DetailContentText(exec platformcore.RuntimeExecutor, cfg platformcore.RuntimeConfig, prefs platformcore.RuntimePreferences, detailMode string) (string, error) {
	if cfg.Detail.Content == nil {
		return "", fmt.Errorf("平台配置中无详情文本定位配置")
	}
	mode := strings.TrimSpace(detailMode)
	if mode == "" {
		mode = "dom"
	}
	var resp localExtractTextResp
	payload := buildDetailExtractPayload(cfg.Detail.Content, mode, 0)
	if err := exec.Post("/api/v1/page/extract-text", payload, &resp); err != nil {
		return "", err
	}
	if len(resp.Texts) > 0 {
		parts := make([]string, 0, len(resp.Texts))
		for _, item := range resp.Texts {
			item = strings.TrimSpace(item)
			if item != "" {
				parts = append(parts, item)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n"), nil
		}
	}
	return strings.TrimSpace(resp.Text), nil
}

// trimSimilarCandidateTail 截断 Boss 详情中混入的相似候选人推荐内容。
// text 为详情原文；未命中固定尾部文案时原样去空白返回。
func trimSimilarCandidateTail(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	const marker = "其他相似经历的牛人"
	index := strings.Index(trimmed, marker)
	if index < 0 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:index])
}

// buildDetailExtractPayload 构建详情文本提取请求。
// 始终使用 elements 数组请求本地程序，返回数组文本。
// OCR 模式下会将 target_classes 分组拆成多个元素，分别截图识别。
func buildDetailExtractPayload(content map[string]any, mode string, delayBefore float64) map[string]any {
	payload := map[string]any{
		"mode":         mode,
		"delay_before": delayBefore,
	}
	targetGroups, ok := content["target_classes"].([][]string)
	if !ok || len(targetGroups) == 0 || mode != "ocr" {
		payload["elements"] = []map[string]any{content}
		return payload
	}
	elements := make([]map[string]any, 0, len(targetGroups))
	for _, group := range targetGroups {
		item := map[string]any{
			"target_classes": [][]string{group},
		}
		if parentGroups, ok := content["parent_classes"]; ok {
			item["parent_classes"] = parentGroups
		}
		if findAttempts, ok := content["find_attempts"]; ok {
			item["find_attempts"] = findAttempts
		}
		if findInterval, ok := content["find_interval_ms"]; ok {
			item["find_interval_ms"] = findInterval
		}
		elements = append(elements, item)
	}
	payload["elements"] = elements
	return payload
}

// ensureCandidateVisible 确保候选人卡片进入可视区域。
func (r *Runtime) ensureCandidateVisible(exec platformcore.RuntimeExecutor, elementRef string, label string) error {
	if strings.TrimSpace(elementRef) == "" {
		return nil
	}
	var viewportResp localViewportResp
	if err := exec.Post("/api/v1/page/in-viewport", map[string]any{"element_ref": elementRef}, &viewportResp); err != nil {
		return err
	}
	if viewportResp.InViewport {
		exec.Log("info", fmt.Sprintf("%s已在当前视口内：%s", label, elementRef))
		return nil
	}
	exec.Log("info", fmt.Sprintf("%s不在当前视口内，准备滚动到视口：%s", label, elementRef))
	if err := exec.Post("/api/v1/page/scroll-into-view", map[string]any{"element_ref": elementRef}, &viewportResp); err != nil {
		return err
	}
	exec.Log("info", fmt.Sprintf("%s已滚动到视口内：%s", label, viewportResp.Matched))
	return nil
}

// authEntryURL 解析平台入口 URL，优先 entry 标记页面。
func authEntryURL(pages []platformcore.RuntimePage) string {
	for _, page := range pages {
		if page.Entry && strings.TrimSpace(page.URL) != "" {
			return page.URL
		}
	}
	for _, page := range pages {
		if strings.TrimSpace(page.URL) != "" {
			return page.URL
		}
	}
	return ""
}

// summarizeCandidateFields 生成候选人卡片字段日志摘要。
// fields 为本地程序提取出的字段，fieldRequests 为平台配置中的字段顺序。
func summarizeCandidateFields(fields map[string]any, fieldRequests []map[string]any) string {
	keys := orderedFieldKeys(fieldRequests)
	seen := make(map[string]struct{}, len(keys))
	parts := make([]string, 0, len(fields))
	for _, key := range keys {
		seen[key] = struct{}{}
		parts = append(parts, fmt.Sprintf("%s=%s", key, previewFieldValue(fields[key], 80)))
	}
	for key, value := range fields {
		if _, ok := seen[key]; ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, previewFieldValue(value, 80)))
	}
	if len(parts) == 0 {
		return "字段=空"
	}
	return strings.Join(parts, "；")
}

// orderedFieldKeys 按平台配置顺序返回字段名。
// fieldRequests 为形如 [{"name": {...}}] 的字段配置数组。
func orderedFieldKeys(fieldRequests []map[string]any) []string {
	keys := make([]string, 0, len(fieldRequests))
	for _, item := range fieldRequests {
		for key := range item {
			trimmed := strings.TrimSpace(key)
			if trimmed != "" {
				keys = append(keys, trimmed)
			}
		}
	}
	return keys
}

// previewFieldValue 将字段值转成短日志文本。
// value 为任意字段值，maxRunes 控制最大展示长度。
func previewFieldValue(value any, maxRunes int) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "空"
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return text
}

// normalizeFingerprintText 规范化指纹字段文本。
// value 为候选人稳定字段，返回去掉多余空白后的文本。
func normalizeFingerprintText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// firstEducationSchool 返回候选人首个教育/公司字段。
// candidate 为候选人对象，返回可用于去重的稳定学校或公司信息。
func firstEducationSchool(candidate platformcore.Candidate) string {
	if len(candidate.BasicProfile.Educations) == 0 {
		return ""
	}
	return strings.TrimSpace(candidate.BasicProfile.Educations[0].SchoolName)
}

// shortRef 压缩本地元素引用，避免任务日志太长。
// ref 为 Local Agent 返回的元素引用。
func shortRef(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "空"
	}
	runes := []rune(trimmed)
	if len(runes) <= 12 {
		return trimmed
	}
	return string(runes[:12]) + "..."
}

// clickRequiredAction 点击必须成功的动作按钮。
func clickRequiredAction(exec platformcore.RuntimeExecutor, element map[string]any, delayBefore float64, label string) error {
	if element == nil {
		return fmt.Errorf("无%s选择器", label)
	}
	return exec.Post("/api/v1/page/click", map[string]any{
		"timeout":      10000,
		"delay_before": delayBefore,
		"element":      element,
	}, nil)
}

// clickOptionalAction 尝试点击可选动作按钮，失败时只记录日志。
func clickOptionalAction(exec platformcore.RuntimeExecutor, element map[string]any, delayBefore float64, label string) error {
	if element == nil {
		return nil
	}
	if err := exec.Post("/api/v1/page/click", map[string]any{
		"timeout":      2000,
		"delay_before": delayBefore,
		"element":      element,
	}, nil); err != nil {
		exec.Log("info", fmt.Sprintf("%s未命中，已跳过：%v", label, err))
		return err
	}
	exec.Log("info", fmt.Sprintf("%s点击成功", label))
	return nil
}

// clickActionWithinCandidate 在候选人卡片内点击动作元素。
func clickActionWithinCandidate(exec platformcore.RuntimeExecutor, candidate platformcore.Candidate, element map[string]any, delayMin float64, delayMax float64, label string) error {
	if element == nil {
		return fmt.Errorf("无%s选择器", label)
	}
	elementRef := strings.TrimSpace(candidate.Runtime.ElementRef)
	if elementRef == "" {
		return fmt.Errorf("%s缺少 element_ref", label)
	}
	if err := exec.Delay(label+"前", delayMin, delayMax); err != nil {
		return err
	}
	return exec.Post("/api/v1/page/click", map[string]any{
		"timeout":     10000,
		"element_ref": elementRef,
		"element":     element,
	}, nil)
}
