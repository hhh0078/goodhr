// Package common 提供所有招聘平台复用的配置驱动页面基础能力，不决定具体平台流程。
package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	panelCloseCheckAttempts = 10
	panelCloseCheckInterval = 180 * time.Millisecond
)

// OpenPage 打开平台配置中的页面地址。
func OpenPage(ctx context.Context, browser model.Browser, platformID string, rawURL string, label string) error {
	_, err := openPage(ctx, browser, platformID, rawURL, label)
	return err
}

// OpenVerifiedPage 打开平台页面并确认最终地址没有跳转到登录页等其他页面。
func OpenVerifiedPage(ctx context.Context, browser model.Browser, platformID string, rawURL string, label string) error {
	page, err := openPage(ctx, browser, platformID, rawURL, label)
	if err != nil {
		return err
	}
	if !PageURLMatches(page.URL, rawURL) {
		return fmt.Errorf("平台 %s 没有停在%s，可能需要重新登录：%s", platformID, label, page.URL)
	}
	return nil
}

// openPage 校验地址并调用 Worker 打开或复用页面。
func openPage(ctx context.Context, browser model.Browser, platformID string, rawURL string, label string) (contract.PageInfo, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return contract.PageInfo{}, fmt.Errorf("平台 %s 没有配置%s地址", platformID, label)
	}
	page, err := browser.OpenPage(ctx, contract.PageOpenRequest{
		URL: rawURL, WaitUntil: "domcontentloaded", TimeoutMS: 30000,
	})
	return page, err
}

// PageURLMatches 判断最终页面地址是否包含配置页面地址。
func PageURLMatches(pageURL string, targetURL string) bool {
	page, pageErr := url.Parse(strings.TrimSpace(pageURL))
	target, targetErr := url.Parse(strings.TrimSpace(targetURL))
	if pageErr != nil || targetErr != nil || page.Scheme == "" || target.Scheme == "" {
		return false
	}
	pagePath := strings.TrimRight(page.EscapedPath(), "/")
	targetPath := strings.TrimRight(target.EscapedPath(), "/")
	pathMatches := pagePath == targetPath ||
		(targetPath != "" && strings.HasPrefix(pagePath, targetPath+"/"))
	if !strings.EqualFold(page.Scheme, target.Scheme) ||
		!strings.EqualFold(page.Host, target.Host) ||
		!pathMatches {
		return false
	}
	return target.RawQuery == "" || strings.Contains(page.RawQuery, target.RawQuery)
}

// ClickRequired 使用 Worker 完整点击能力点击必需选择器。
func ClickRequired(ctx context.Context, browser model.Browser, cfg model.Config, key string) error {
	selector, err := RequiredSelector(cfg, key)
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	return err
}

// ClickOptional 点击可选选择器，未配置或页面不存在时安全跳过。
func ClickOptional(ctx context.Context, browser model.Browser, cfg model.Config, key string) error {
	selector, ok := cfg.Selectors[key]
	if !ok || len(selector.Target.Selectors) == 0 {
		return nil
	}
	exists, err := SelectorExists(ctx, browser, cfg, key)
	if err != nil || !exists {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	if IsElementMissing(err) {
		return nil
	}
	return err
}

// ClickOptionalQuick 只短暂探测并点击即时可选弹层，避免弹层本就不存在时白等完整超时。
func ClickOptionalQuick(ctx context.Context, browser model.Browser, cfg model.Config, key string) error {
	selector, ok := cfg.Selectors[key]
	if !ok || len(selector.Target.Selectors) == 0 {
		return nil
	}
	exists, err := ProbeSelectorExists(ctx, browser, cfg, key)
	if err != nil || !exists {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
	if IsElementMissing(err) {
		return nil
	}
	return err
}

// InputRequired 使用 Worker 完整输入能力写入必需输入框。
func InputRequired(ctx context.Context, browser model.Browser, cfg model.Config, key string, value string) error {
	selector, err := RequiredSelector(cfg, key)
	if err != nil {
		return err
	}
	clear := true
	verify := true
	_, err = browser.Input(ctx, contract.ElementInputRequest{
		Selector: selector, Text: value, Clear: &clear, Verify: &verify,
	})
	return err
}

// InputOptional 向可选输入框写入非空内容。
func InputOptional(ctx context.Context, browser model.Browser, cfg model.Config, key string, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, ok := cfg.Selectors[key]; !ok {
		return nil
	}
	return InputRequired(ctx, browser, cfg, key, value)
}

// ApplyConfiguredActions 按配置顺序执行平台筛选动作。
func ApplyConfiguredActions(ctx context.Context, browser model.Browser, cfg model.Config, actions []model.ConfiguredAction) error {
	for _, action := range actions {
		var err error
		switch strings.ToLower(strings.TrimSpace(action.Type)) {
		case "click":
			if action.Optional {
				err = ClickOptional(ctx, browser, cfg, action.SelectorKey)
			} else {
				err = ClickRequired(ctx, browser, cfg, action.SelectorKey)
			}
		case "input":
			if action.Optional && strings.TrimSpace(action.Value) == "" {
				continue
			}
			err = InputRequired(ctx, browser, cfg, action.SelectorKey, action.Value)
		default:
			err = fmt.Errorf("平台 %s 的配置动作 %s 类型不支持：%s", cfg.ID, action.Name, action.Type)
		}
		if err != nil {
			return fmt.Errorf("%s失败：%w", firstNonEmpty(action.Name, action.SelectorKey), err)
		}
	}
	return nil
}

// FindCandidates 读取当前候选人列表并整理成统一结构。
func FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, platformID string) ([]model.Candidate, error) {
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return nil, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: positiveOr(cfg.MaxItems, 100), Fields: cfg.CandidateFields,
	})
	if err != nil {
		return nil, err
	}
	result := make([]model.Candidate, 0, len(items))
	identityOccurrences := make(map[string]int)
	for _, item := range items {
		name := firstNonEmpty(item.Fields["name"], item.Text)
		fingerprint := CandidateFingerprint(platformID, name, item.Fields, item.Text)
		identityTexts := CandidateIdentityTexts(name, item.Fields, item.Text)
		identityKey := strings.Join(identityTexts, "\x00")
		identityOccurrence := identityOccurrences[identityKey]
		identityOccurrences[identityKey] = identityOccurrence + 1
		result = append(result, model.Candidate{
			Index: item.Index, Fingerprint: fingerprint, Name: name,
			Summary: item.Text, Fields: item.Fields,
			IdentityTexts: identityTexts, IdentityOccurrence: identityOccurrence,
		})
	}
	return result, nil
}

// CandidateIdentityTexts 返回页面重新定位候选人时使用的姓名和年龄文本。
func CandidateIdentityTexts(name string, fields map[string]string, summary string) []string {
	result := make([]string, 0, 2)
	if name = strings.TrimSpace(name); name != "" {
		result = append(result, name)
	}
	age := firstNonEmpty(fields["age"], fields["candidate_age"], extractAge(summary))
	if age = strings.TrimSpace(age); age != "" {
		result = append(result, age)
	}
	return result
}

// CandidateFingerprint 按旧版规则使用平台、姓名和年龄生成稳定去重编号。
func CandidateFingerprint(platformID string, name string, fields map[string]string, summary string) string {
	age := firstNonEmpty(fields["age"], fields["candidate_age"], extractAge(summary))
	normalizedName := strings.Join(strings.Fields(strings.TrimSpace(name)), "")
	normalizedAge := strings.Join(strings.Fields(strings.TrimSpace(age)), "")
	if normalizedName == "" || normalizedAge == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(platformID)) + "_" + normalizedName + "_" + normalizedAge
}

// ReadOptional 读取可选选择器文本，未配置或元素不存在时返回 found=false。
func ReadOptional(ctx context.Context, browser model.Browser, cfg model.Config, key string) (value string, found bool, err error) {
	selector, ok := cfg.Selectors[key]
	if !ok || len(selector.Target.Selectors) == 0 {
		return "", false, nil
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: selector, Property: "text"})
	if IsElementMissing(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(result.Value), true, nil
}

// SelectorExists 判断可选选择器当前是否存在且符合配置状态。
func SelectorExists(ctx context.Context, browser model.Browser, cfg model.Config, key string) (bool, error) {
	return selectorExists(ctx, browser, cfg, key, 0)
}

// ProbeSelectorExists 使用短超时探测可选元素，避免“本来就可能不存在”的页面状态白等数秒。
func ProbeSelectorExists(ctx context.Context, browser model.Browser, cfg model.Config, key string) (bool, error) {
	return selectorExists(ctx, browser, cfg, key, 600)
}

// selectorExists 使用指定超时判断选择器是否存在，timeoutMS 为零时保留平台配置。
func selectorExists(ctx context.Context, browser model.Browser, cfg model.Config, key string, timeoutMS int) (bool, error) {
	selector, ok := cfg.Selectors[key]
	if !ok || len(selector.Target.Selectors) == 0 {
		return false, nil
	}
	if timeoutMS > 0 && (selector.TimeoutMS <= 0 || selector.TimeoutMS > timeoutMS) {
		selector.TimeoutMS = timeoutMS
	}
	_, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 1, ExpectedMissing: true,
	})
	if IsElementMissing(err) {
		return false, nil
	}
	return err == nil, err
}

// CloseOptionalPanel 点击关闭可选弹层并确认弹层消失，点击无效时再用 Escape 收尾。
func CloseOptionalPanel(ctx context.Context, browser model.Browser, cfg model.Config, panelKey string, closeKey string, label string) error {
	opened, err := ProbeSelectorExists(ctx, browser, cfg, panelKey)
	if err != nil || !opened {
		return err
	}
	panel, err := RequiredSelector(cfg, panelKey)
	if err != nil {
		return err
	}
	closeSelector, err := RequiredSelector(cfg, closeKey)
	if err != nil {
		return err
	}
	panel.TimeoutMS = 200
	_, clickErr := browser.Click(ctx, contract.ElementClickRequest{
		Selector: closeSelector,
		Verify: &contract.ClickVerification{
			TargetHidden: &panel,
			TimeoutMS:    1800,
		},
	})
	if clickErr == nil {
		return nil
	}
	if _, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120}); err != nil {
		return fmt.Errorf("%s没关好，点击关闭失败：%v；按 Escape 也失败：%w", label, clickErr, err)
	}
	hidden, err := waitSelectorHidden(ctx, browser, cfg, panelKey)
	if err != nil {
		return err
	}
	if !hidden {
		return fmt.Errorf("%s仍然没有关闭：%w", label, clickErr)
	}
	return nil
}

// waitSelectorHidden 短暂轮询确认弹层已经隐藏，避免把关闭动画误判成关闭失败。
func waitSelectorHidden(ctx context.Context, browser model.Browser, cfg model.Config, key string) (bool, error) {
	for attempt := 1; attempt <= panelCloseCheckAttempts; attempt++ {
		opened, err := ProbeSelectorExists(ctx, browser, cfg, key)
		if err != nil {
			return false, err
		}
		if !opened {
			return true, nil
		}
		if attempt == panelCloseCheckAttempts {
			break
		}
		timer := time.NewTimer(panelCloseCheckInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
	return false, nil
}

// CloseCandidateChat 可靠关闭各平台共用的候选人聊天框。
func CloseCandidateChat(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return CloseOptionalPanel(
		ctx,
		browser,
		cfg,
		"candidate.chat_modal",
		"candidate.chat_close",
		cfg.Name+"候选人聊天框",
	)
}

// CloseCandidatePanels 按配置依次关闭候选人聊天框和联系人抽屉。
// 未配置联系人抽屉的平台只执行聊天框清理。
func CloseCandidatePanels(ctx context.Context, browser model.Browser, cfg model.Config) error {
	steps := []struct {
		panel string
		close string
		label string
	}{
		{panel: "candidate.chat_modal", close: "candidate.chat_close", label: cfg.Name + "候选人聊天框"},
		{panel: "candidate.contact_drawer", close: "candidate.contact_drawer_close", label: cfg.Name + "联系人列表"},
	}
	var closeErrors []error
	for _, step := range steps {
		if err := CloseOptionalPanel(ctx, browser, cfg, step.panel, step.close, step.label); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

// ScrollToCandidate 使用真实鼠标滚轮把指定候选人滚动到可操作区域。
func ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	selector, err := CandidateSelector(cfg, candidate)
	if err != nil {
		return err
	}
	var wheelAnchor *contract.SelectorSpec
	if anchor, ok := cfg.Selectors["candidate.list"]; ok {
		wheelAnchor = &anchor
	}
	requireFull := false
	_, err = browser.Scroll(ctx, contract.ScrollRequest{
		Target: &selector, WheelAnchor: wheelAnchor, Distance: 180, MaxAttempts: 18,
		WaitMS: 180, RequireFull: &requireFull, ViewportMargin: 48,
	})
	return err
}

// GreetCandidate 滚动到候选人后，按平台配置完成平台首次开聊动作。
func GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, _ model.GreetRequest) error {
	selector, err := CandidateActionSelector(cfg, "candidate.greet", candidate)
	if err != nil {
		return err
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, ViewportMargin: 48}); err != nil {
		return err
	}
	if err = ClickOptional(ctx, browser, cfg, "candidate.greet_continue"); err != nil {
		return err
	}
	if err = ClickOptionalQuick(ctx, browser, cfg, "candidate.greet_confirm"); err != nil {
		return err
	}
	if _, hasDialog := cfg.Selectors["candidate.greet_dialog"]; hasDialog {
		dialogOpened, existsErr := SelectorExists(ctx, browser, cfg, "candidate.greet_dialog")
		if existsErr != nil {
			return existsErr
		}
		if !dialogOpened {
			return nil
		}
		return ClickRequired(ctx, browser, cfg, "candidate.greet_send")
	}
	if _, hasSend := cfg.Selectors["candidate.greet_send"]; hasSend {
		return ClickRequired(ctx, browser, cfg, "candidate.greet_send")
	}
	return nil
}

// CandidateAction 点击指定候选人卡片内的平台动作。
func CandidateAction(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, actionKey string) error {
	selector, err := CandidateActionSelector(cfg, actionKey, candidate)
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, ViewportMargin: 48})
	return err
}

// RequiredSelector 返回平台必需选择器。
func RequiredSelector(cfg model.Config, key string) (contract.SelectorSpec, error) {
	selector, ok := cfg.Selectors[key]
	if !ok || len(selector.Target.Selectors) == 0 {
		return contract.SelectorSpec{}, fmt.Errorf("平台 %s 缺少选择器 %s", cfg.ID, key)
	}
	return selector, nil
}

// IndexedSelector 复制选择器并设置从 0 开始的列表序号。
func IndexedSelector(cfg model.Config, key string, index int) (contract.SelectorSpec, error) {
	selector, err := RequiredSelector(cfg, key)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	indexValue := max(index, 0)
	selector.Target.Index = &indexValue
	return selector, nil
}

// CandidateSelector 使用候选人稳定文本重新定位卡片，缺少身份文本时才回退到原序号。
func CandidateSelector(cfg model.Config, candidate model.Candidate) (contract.SelectorSpec, error) {
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	identityTexts := make([]string, 0, len(candidate.IdentityTexts))
	for _, text := range candidate.IdentityTexts {
		if text = strings.TrimSpace(text); text != "" {
			identityTexts = append(identityTexts, text)
		}
	}
	if len(identityTexts) == 0 {
		index := max(candidate.Index, 0)
		selector.Target.Index = &index
		return selector, nil
	}
	selector.Target.Texts = append(selector.Target.Texts, identityTexts...)
	occurrence := max(candidate.IdentityOccurrence, 0)
	selector.Target.Index = &occurrence
	selector.Description = firstNonEmpty(candidate.Name, selector.Description)
	return selector, nil
}

// CandidateActionSelector 把稳定定位后的候选人卡片作为父级，再查找卡片内动作。
func CandidateActionSelector(cfg model.Config, actionKey string, candidate model.Candidate) (contract.SelectorSpec, error) {
	card, err := CandidateSelector(cfg, candidate)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	action, ok := cfg.Selectors[actionKey]
	if !ok {
		return contract.SelectorSpec{}, fmt.Errorf("平台 %s 缺少选择器 %s", cfg.ID, actionKey)
	}
	parents := make([]contract.SelectorGroup, 0, len(card.Parents)+1+len(action.Parents))
	parents = append(parents, card.Parents...)
	parents = append(parents, card.Target)
	parents = append(parents, action.Parents...)
	action.Parents = parents
	action.Frames = append(card.Frames, action.Frames...)
	action.Description = firstNonEmpty(action.Description, actionKey)
	return action, nil
}

// CandidateScopedSelector 把候选人卡片作为父级后定位卡片内动作。
func CandidateScopedSelector(cfg model.Config, actionKey string, index int) (contract.SelectorSpec, error) {
	return CandidateScopedSelectorWithParent(cfg, "candidate.item", actionKey, index)
}

// CandidateScopedSelectorWithParent 把指定列表项作为父级后定位项目内动作。
func CandidateScopedSelectorWithParent(cfg model.Config, parentKey string, actionKey string, index int) (contract.SelectorSpec, error) {
	card, err := RequiredSelector(cfg, parentKey)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	action, ok := cfg.Selectors[actionKey]
	if !ok {
		return contract.SelectorSpec{}, fmt.Errorf("平台 %s 缺少选择器 %s", cfg.ID, actionKey)
	}
	indexValue := max(index, 0)
	card.Target.Index = &indexValue
	parents := make([]contract.SelectorGroup, 0, len(card.Parents)+1+len(action.Parents))
	parents = append(parents, card.Parents...)
	parents = append(parents, card.Target)
	parents = append(parents, action.Parents...)
	action.Parents = parents
	action.Frames = append(card.Frames, action.Frames...)
	action.Description = actionKey
	return action, nil
}

// IsElementMissing 判断 Worker 错误是否只是可选元素不存在。
func IsElementMissing(err error) bool {
	if err == nil {
		return false
	}
	var workerErr *contract.WorkerError
	return errors.As(err, &workerErr) && workerErr.Body.Code == "ELEMENT_NOT_FOUND"
}

// HashText 返回用于本地去重的短哈希。
func HashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// extractAge 从候选人卡片文本提取年龄。
func extractAge(value string) string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r < '0' || r > '9'
	})
	for _, field := range fields {
		if len(field) == 2 {
			return field
		}
	}
	return ""
}

// positiveOr 返回正整数配置或默认值。
func positiveOr(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
