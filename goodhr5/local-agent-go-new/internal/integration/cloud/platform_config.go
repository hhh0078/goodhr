// Package cloud 把云端旧平台配置在 JSON 边界转换成新版统一 SelectorSpec。
package cloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// legacyLocator 表示旧平台配置中的父级和目标 CSS 数组。
type legacyLocator struct {
	ParentClasses [][]string `json:"parent_classes"`
	TargetClasses [][]string `json:"target_classes"`
}

// legacyPlatformConfig 表示当前云端仍在使用的平台配置结构。
type legacyPlatformConfig struct {
	ID   string `json:"id"`
	Auth struct {
		EntryURL string `json:"entry_url"`
	} `json:"auth"`
	Card struct {
		Item   legacyLocator     `json:"item"`
		Scroll legacyLocator     `json:"scroll"`
		Fields []json.RawMessage `json:"fields"`
	} `json:"card"`
	Actions struct {
		GreetButton    legacyLocator `json:"greetBtn"`
		ContinueButton legacyLocator `json:"continueBtn"`
		ConfirmButton  legacyLocator `json:"confirmBtn"`
	} `json:"actions"`
	Detail struct {
		OpenTarget  legacyLocator `json:"openTarget"`
		Content     legacyLocator `json:"content"`
		CloseButton legacyLocator `json:"closeBtn"`
	} `json:"detail"`
	Position struct {
		SwitchButton legacyLocator `json:"switchBtn"`
		Item         legacyLocator `json:"item"`
	} `json:"position"`
}

// decodePlatformConfig 解析新版配置，缺少新版字段时自动转换旧结构。
func decodePlatformConfig(raw json.RawMessage, platformID string) (model.Config, error) {
	normalized, err := unwrapJSONString(raw)
	if err != nil {
		return model.Config{}, fmt.Errorf("解析平台配置失败：%w", err)
	}
	var current model.Config
	if err := json.Unmarshal(normalized, &current); err != nil {
		return model.Config{}, fmt.Errorf("解析新版平台配置失败：%w", err)
	}
	if current.EntryURL != "" && len(current.Selectors) > 0 {
		return normalizePlatformConfig(current, platformID), nil
	}
	var legacy legacyPlatformConfig
	if err := json.Unmarshal(normalized, &legacy); err != nil {
		return model.Config{}, fmt.Errorf("解析旧平台配置失败：%w", err)
	}
	converted, err := convertLegacyPlatformConfig(legacy, platformID)
	if err != nil {
		return model.Config{}, err
	}
	return normalizePlatformConfig(converted, platformID), nil
}

// convertLegacyPlatformConfig 把旧的 auth/card/actions/detail/position 转成新版逻辑键。
func convertLegacyPlatformConfig(legacy legacyPlatformConfig, platformID string) (model.Config, error) {
	cfg := model.Config{
		ID:              firstNonEmpty(legacy.ID, strings.ToLower(strings.TrimSpace(platformID))),
		EntryURL:        strings.TrimSpace(legacy.Auth.EntryURL),
		Selectors:       make(map[string]contract.SelectorSpec),
		CandidateFields: make(map[string]contract.SelectorSpec),
		ScrollDistance:  620,
		MaxItems:        100,
	}
	addLegacySelector(cfg.Selectors, "candidate.item", legacy.Card.Item)
	addLegacySelector(cfg.Selectors, "candidate.list", legacy.Card.Scroll)
	addLegacySelector(cfg.Selectors, "candidate.open_target", legacy.Detail.OpenTarget)
	addLegacySelector(cfg.Selectors, "candidate.detail", legacy.Detail.Content)
	addLegacySelector(cfg.Selectors, "candidate.detail_close", legacy.Detail.CloseButton)
	addLegacySelector(cfg.Selectors, "candidate.greet_send", legacy.Actions.GreetButton)
	addLegacySelector(cfg.Selectors, "candidate.greet_continue", legacy.Actions.ContinueButton)
	addLegacySelector(cfg.Selectors, "candidate.greet_confirm", legacy.Actions.ConfirmButton)
	addLegacySelector(cfg.Selectors, "position.open", legacy.Position.SwitchButton)
	addLegacySelector(cfg.Selectors, "position.item", legacy.Position.Item)
	if err := convertLegacyFields(legacy.Card.Fields, cfg.CandidateFields); err != nil {
		return model.Config{}, err
	}
	if cfg.EntryURL == "" {
		return model.Config{}, fmt.Errorf("平台 %s 缺少入口地址", cfg.ID)
	}
	if _, ok := cfg.Selectors["candidate.item"]; !ok {
		return model.Config{}, fmt.Errorf("平台 %s 缺少候选人卡片选择器", cfg.ID)
	}
	return cfg, nil
}

// convertLegacyFields 转换旧候选人字段的动态字段名。
func convertLegacyFields(items []json.RawMessage, result map[string]contract.SelectorSpec) error {
	for _, item := range items {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return fmt.Errorf("解析候选人字段配置失败：%w", err)
		}
		for name, raw := range fields {
			var locator legacyLocator
			if err := json.Unmarshal(raw, &locator); err != nil {
				return fmt.Errorf("解析候选人字段 %s 失败：%w", name, err)
			}
			if selector, ok := legacySelector(name, locator); ok {
				result[name] = selector
			}
		}
	}
	return nil
}

// addLegacySelector 转换并添加非空旧选择器。
func addLegacySelector(target map[string]contract.SelectorSpec, key string, locator legacyLocator) {
	if selector, ok := legacySelector(key, locator); ok {
		target[key] = selector
	}
}

// legacySelector 把旧嵌套 CSS 数组转换成父级层级和目标候选数组。
func legacySelector(description string, locator legacyLocator) (contract.SelectorSpec, bool) {
	parents := make([]contract.SelectorGroup, 0, len(locator.ParentClasses))
	for _, level := range locator.ParentClasses {
		candidates := cssCandidates(level)
		if len(candidates) > 0 {
			parents = append(parents, contract.SelectorGroup{Selectors: candidates})
		}
	}
	targets := make([]contract.SelectorCandidate, 0)
	for _, group := range locator.TargetClasses {
		targets = append(targets, cssCandidates(group)...)
	}
	if len(targets) == 0 {
		return contract.SelectorSpec{}, false
	}
	return contract.SelectorSpec{
		Parents:     parents,
		Target:      contract.SelectorGroup{Selectors: targets},
		State:       "visible",
		TimeoutMS:   5000,
		Description: description,
	}, true
}

// cssCandidates 清理旧 CSS 字符串并转换成候选选择器。
func cssCandidates(values []string) []contract.SelectorCandidate {
	result := make([]contract.SelectorCandidate, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, contract.SelectorCandidate{Type: "css", Value: value})
	}
	return result
}

// unwrapJSONString 解析对象或被字符串包裹的 JSON。
func unwrapJSONString(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("配置内容为空")
	}
	if raw[0] != '"' {
		return raw, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// normalizePlatformConfig 补齐平台配置默认集合和值。
func normalizePlatformConfig(cfg model.Config, platformID string) model.Config {
	if cfg.ID == "" {
		cfg.ID = strings.ToLower(strings.TrimSpace(platformID))
	}
	if cfg.Selectors == nil {
		cfg.Selectors = make(map[string]contract.SelectorSpec)
	}
	if cfg.CandidateFields == nil {
		cfg.CandidateFields = make(map[string]contract.SelectorSpec)
	}
	if cfg.ConversationFields == nil {
		cfg.ConversationFields = make(map[string]contract.SelectorSpec)
	}
	return cfg
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
