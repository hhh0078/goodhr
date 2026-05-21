// 本文件负责封装平台运行时行为，让任务主流程通过平台方法完成候选人可视区控制。
package httpapi

import (
	"fmt"
	"strings"
)

type localViewportResp struct {
	Ok         bool   `json:"ok"`
	InViewport bool   `json:"in_viewport"`
	Matched    string `json:"matched"`
}

type localElementItem struct {
	Ref   string `json:"ref"`
	Index int    `json:"index"`
}

type localFindElementsResp struct {
	Ok    bool               `json:"ok"`
	Items []localElementItem `json:"items"`
	Count int                `json:"count"`
}

type localExtractFieldsResp struct {
	Ok     bool           `json:"ok"`
	Fields map[string]any `json:"fields"`
}

type platformViewportExecutor interface {
	post(path string, body any, result any) error
	log(level, message string)
}

// OpenEntryPage 由平台运行时逻辑打开任务入口页面。
func (cfg PlatformConfig) OpenEntryPage(exec platformViewportExecutor, cookies []map[string]any) error {
	switch strings.TrimSpace(cfg.ID) {
	case "boss":
		return cfg.openDefaultEntryPage(exec, cookies, "Boss推荐页")
	default:
		return cfg.openDefaultEntryPage(exec, cookies, "平台入口页")
	}
}

// ListVisibleCandidates 由平台运行时逻辑提取当前可见候选人摘要。
func (cfg PlatformConfig) ListVisibleCandidates(exec platformViewportExecutor) ([]map[string]any, error) {
	switch strings.TrimSpace(cfg.ID) {
	case "boss":
		return cfg.listVisibleCandidatesWithElements(exec, "Boss候选人卡片")
	default:
		return cfg.listVisibleCandidatesWithElements(exec, "候选人卡片")
	}
}

// ScrollCandidateList 由平台运行时逻辑滚动到候选人列表下一屏。
func (cfg PlatformConfig) ScrollCandidateList(exec platformViewportExecutor, prefs UserPreferences) error {
	switch strings.TrimSpace(cfg.ID) {
	case "boss":
		return cfg.scrollCandidateListWithElement(exec, prefs, "Boss候选人列表")
	default:
		return cfg.scrollCandidateListWithElement(exec, prefs, "候选人列表")
	}
}

// GreetCandidate 由平台运行时逻辑执行打招呼动作。
func (cfg PlatformConfig) GreetCandidate(exec platformViewportExecutor, prefs UserPreferences, candidate map[string]any) error {
	switch strings.TrimSpace(cfg.ID) {
	case "boss":
		return cfg.greetCandidateWithActions(exec, prefs, candidate, "Boss候选人")
	default:
		return cfg.greetCandidateWithActions(exec, prefs, candidate, "候选人")
	}
}

// EnsureCandidateVisible 由平台运行时逻辑确保候选人卡片位于可视区域。
func (cfg PlatformConfig) EnsureCandidateVisible(exec platformViewportExecutor, elementRef string) error {
	switch strings.TrimSpace(cfg.ID) {
	case "boss":
		return cfg.ensureBossCandidateVisible(exec, elementRef)
	default:
		return cfg.ensureDefaultCandidateVisible(exec, elementRef)
	}
}

// ensureBossCandidateVisible 确保 Boss 候选人卡片进入可视区域后再读取字段。
func (cfg PlatformConfig) ensureBossCandidateVisible(exec platformViewportExecutor, elementRef string) error {
	return ensureCandidateVisibleWithViewport(exec, elementRef, "Boss候选人卡片")
}

// ensureDefaultCandidateVisible 使用默认逻辑确保候选人卡片进入可视区域。
func (cfg PlatformConfig) ensureDefaultCandidateVisible(exec platformViewportExecutor, elementRef string) error {
	return ensureCandidateVisibleWithViewport(exec, elementRef, "候选人卡片")
}

// ensureCandidateVisibleWithViewport 通过本地原子接口判断并滚动元素到视口内。
func ensureCandidateVisibleWithViewport(exec platformViewportExecutor, elementRef string, label string) error {
	if strings.TrimSpace(elementRef) == "" {
		return nil
	}
	var viewportResp localViewportResp
	if err := exec.post("/api/v1/page/in-viewport", map[string]any{
		"element_ref": elementRef,
	}, &viewportResp); err != nil {
		return err
	}
	if viewportResp.InViewport {
		exec.log("info", fmt.Sprintf("%s已在当前视口内：%s", label, elementRef))
		return nil
	}
	exec.log("info", fmt.Sprintf("%s不在当前视口内，准备滚动到视口：%s", label, elementRef))
	if err := exec.post("/api/v1/page/scroll-into-view", map[string]any{
		"element_ref": elementRef,
	}, &viewportResp); err != nil {
		return err
	}
	exec.log("info", fmt.Sprintf("%s已滚动到视口内：%s", label, viewportResp.Matched))
	return nil
}

// openDefaultEntryPage 打开平台默认入口页。
func (cfg PlatformConfig) openDefaultEntryPage(exec platformViewportExecutor, cookies []map[string]any, label string) error {
	if len(cfg.Pages) == 0 || strings.TrimSpace(cfg.Pages[0].URL) == "" {
		return fmt.Errorf("平台配置中没有合法页面")
	}
	url := cfg.Pages[0].URL
	exec.log("info", fmt.Sprintf("正在打开%s: %s", label, url))
	body := map[string]any{
		"url": url,
	}
	if len(cookies) > 0 {
		exec.log("info", fmt.Sprintf("打开%s前补充注入 %d 条 cookie", label, len(cookies)))
		body["cookies"] = cookies
	}
	return exec.post("/api/v1/page/open", body, nil)
}

// listVisibleCandidatesWithElements 使用通用元素协议提取当前可见候选人摘要。
func (cfg PlatformConfig) listVisibleCandidatesWithElements(exec platformViewportExecutor, label string) ([]map[string]any, error) {
	fieldRequests := cfg.Card.ExtractFieldRequests()
	cardElement := cfg.Card.CardElement()
	if len(fieldRequests) == 0 {
		return nil, fmt.Errorf("平台配置中无候选人字段选择器")
	}
	if cardElement == nil {
		return nil, fmt.Errorf("平台配置中无候选人卡片定位配置")
	}

	var findResp localFindElementsResp
	if err := exec.post("/api/v1/page/find-elements", map[string]any{
		"element":      cardElement,
		"visible_only": true,
	}, &findResp); err != nil {
		return nil, err
	}
	if findResp.Items == nil {
		findResp.Items = []localElementItem{}
	}
	exec.log("info", fmt.Sprintf("查找到 %d 个当前可见%s", len(findResp.Items), label))

	candidates := make([]map[string]any, 0, len(findResp.Items))
	for _, item := range findResp.Items {
		if err := cfg.EnsureCandidateVisible(exec, item.Ref); err != nil {
			return nil, err
		}
		var extractResp localExtractFieldsResp
		if err := exec.post("/api/v1/page/extract-fields", map[string]any{
			"element_ref": item.Ref,
			"fields":      fieldRequests,
		}, &extractResp); err != nil {
			return nil, err
		}
		if extractResp.Fields == nil {
			extractResp.Fields = map[string]any{}
		}
		extractResp.Fields["_index"] = item.Index
		extractResp.Fields["element_ref"] = item.Ref
		candidates = append(candidates, extractResp.Fields)
	}
	return candidates, nil
}

// scrollCandidateListWithElement 使用平台列表定位配置滚动下一屏。
func (cfg PlatformConfig) scrollCandidateListWithElement(exec platformViewportExecutor, prefs UserPreferences, label string) error {
	exec.log("info", fmt.Sprintf("正在滚动到下一屏%s", label))
	body := map[string]any{
		"scroll_delay_min": prefs.ScrollDelayMin,
		"scroll_delay_max": prefs.ScrollDelayMax,
		"max_scrolls":      1,
	}
	if element := cfg.Card.ScrollElement(); element != nil {
		body["element"] = element
	}
	return exec.post("/api/v1/page/scroll", body, nil)
}

// greetCandidateWithActions 使用平台动作配置执行打招呼及后续确认按钮点击。
func (cfg PlatformConfig) greetCandidateWithActions(exec platformViewportExecutor, prefs UserPreferences, candidate map[string]any, label string) error {
	exec.log("info", fmt.Sprintf("正在执行%s打招呼动作", label))
	if err := clickRequiredAction(exec, cfg.Actions.GreetBtn.AsPayload(), greetDelayBefore(prefs), "打招呼按钮"); err != nil {
		return err
	}
	_ = clickOptionalAction(exec, cfg.Actions.ContinueBtn.AsPayload(), 0.6, "继续沟通按钮")
	_ = clickOptionalAction(exec, cfg.Actions.ConfirmBtn.AsPayload(), 0.6, "确认按钮")
	return nil
}

// clickRequiredAction 点击必须成功的动作按钮。
func clickRequiredAction(exec platformViewportExecutor, element map[string]any, delayBefore float64, label string) error {
	if element == nil {
		return fmt.Errorf("无%s选择器", label)
	}
	body := map[string]any{
		"timeout":      10000,
		"delay_before": delayBefore,
		"element":      element,
	}
	return exec.post("/api/v1/page/click", body, nil)
}

// clickOptionalAction 尝试点击可选动作按钮，失败时只记日志不终止主流程。
func clickOptionalAction(exec platformViewportExecutor, element map[string]any, delayBefore float64, label string) error {
	if element == nil {
		return nil
	}
	body := map[string]any{
		"timeout":      2000,
		"delay_before": delayBefore,
		"element":      element,
	}
	if err := exec.post("/api/v1/page/click", body, nil); err != nil {
		exec.log("info", fmt.Sprintf("%s未命中，已跳过：%v", label, err))
		return err
	}
	exec.log("info", fmt.Sprintf("%s点击成功", label))
	return nil
}
