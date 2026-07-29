// Package platform 文件作用：加载平台目录中的配置模板，并为云端旧配置补齐缺失的新版能力。
package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

//go:embed boss/config.json
var bossConfigJSON []byte

//go:embed zhaopin/config.json
var zhaopinConfigJSON []byte

//go:embed liepin/config.json
var liepinConfigJSON []byte

//go:embed hliepin/config.json
var hliepinConfigJSON []byte

// DefaultConfig 返回平台目录内随程序发布的配置模板。
func DefaultConfig(platformID string) (model.Config, error) {
	content, exists := map[string][]byte{
		"boss": bossConfigJSON, "zhaopin": zhaopinConfigJSON,
		"liepin": liepinConfigJSON, "hliepin": hliepinConfigJSON,
	}[strings.ToLower(strings.TrimSpace(platformID))]
	if !exists {
		return model.Config{}, fmt.Errorf("暂不支持平台 %s", platformID)
	}
	var config model.Config
	if err := json.Unmarshal(content, &config); err != nil {
		return model.Config{}, fmt.Errorf("解析平台内置配置失败：%w", err)
	}
	return config, nil
}

// FillMissingConfig 以云端配置为主，只用内置模板补齐旧结构没有的字段。
func FillMissingConfig(config model.Config, defaults model.Config) model.Config {
	if config.ID == "" {
		config.ID = defaults.ID
	}
	if config.Name == "" {
		config.Name = defaults.Name
	}
	if config.LoginURL == "" {
		config.LoginURL = defaults.LoginURL
	}
	if config.EntryURL == "" {
		config.EntryURL = defaults.EntryURL
	}
	if config.MessagesURL == "" {
		config.MessagesURL = defaults.MessagesURL
	}
	config.Selectors = fillMissingSelectors(config.Selectors, defaults.Selectors)
	config.CandidateFields = fillMissingSelectors(config.CandidateFields, defaults.CandidateFields)
	config.ConversationFields = fillMissingSelectors(config.ConversationFields, defaults.ConversationFields)
	if len(config.LoginInitActions) == 0 {
		config.LoginInitActions = defaults.LoginInitActions
	}
	if len(config.GreetingInitActions) == 0 {
		config.GreetingInitActions = defaults.GreetingInitActions
	}
	if len(config.MessageInitActions) == 0 {
		config.MessageInitActions = defaults.MessageInitActions
	}
	if len(config.FilterActions) == 0 {
		config.FilterActions = defaults.FilterActions
	}
	if config.ScrollDistance <= 0 {
		config.ScrollDistance = defaults.ScrollDistance
	}
	if config.MaxItems <= 0 {
		config.MaxItems = defaults.MaxItems
	}
	return config
}

// ValidateTaskConfig 检查当前任务所需的平台页面和选择器是否完整。
func ValidateTaskConfig(config model.Config, taskType string) error {
	if !strings.EqualFold(strings.TrimSpace(taskType), "auto_reply") {
		return nil
	}
	if strings.TrimSpace(config.MessagesURL) == "" {
		return fmt.Errorf("%s 自动回复消息页还没有配置", config.Name)
	}
	for _, key := range []string{"message.unread_item", "message.context", "message.input", "message.send"} {
		selector, exists := config.Selectors[key]
		if !exists || len(selector.Target.Selectors) == 0 {
			return fmt.Errorf("%s 自动回复缺少选择器 %s", config.Name, key)
		}
	}
	return nil
}

// fillMissingSelectors 合并选择器映射，已经由云端提供的键保持不变。
func fillMissingSelectors(current map[string]contract.SelectorSpec, defaults map[string]contract.SelectorSpec) map[string]contract.SelectorSpec {
	if current == nil {
		current = make(map[string]contract.SelectorSpec, len(defaults))
	}
	for key, value := range defaults {
		if _, exists := current[key]; !exists {
			current[key] = value
		}
	}
	return current
}
