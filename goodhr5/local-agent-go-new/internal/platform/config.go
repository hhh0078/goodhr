// Package platform 加载随本地程序发布的平台页面配置。
package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

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

// LoadConfig 返回平台目录内随程序发布的本地运行配置。
func LoadConfig(platformID string) (model.Config, error) {
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
