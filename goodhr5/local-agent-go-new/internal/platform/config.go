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
	for _, key := range []string{
		"message.entry", "message.entry_unread_count",
		"message.drawer", "message.drawer_scroll",
		"message.unread_item", "message.contact_item", "message.contact_click_target",
		"message.current_name", "message.current_position", "message.current_avatar",
		"message.item", "message.history_scroll",
		"message.input", "message.send",
	} {
		selector, exists := config.Selectors[key]
		if !exists || len(selector.Target.Selectors) == 0 {
			return fmt.Errorf("%s 自动回复缺少选择器 %s", config.Name, key)
		}
	}
	attachmentKeys := []string{
		"message.resume_attachment_entry", "message.attachment_preview",
		"message.attachment_download", "message.attachment_preview_close",
	}
	switch strings.TrimSpace(config.Behavior.AttachmentMode) {
	case "new_page_document":
		attachmentKeys = attachmentKeys[:1]
		for _, item := range []struct {
			name  string
			value string
		}{
			{name: "attachment_allowed_scheme", value: config.Behavior.AttachmentAllowedScheme},
			{name: "attachment_allowed_host", value: config.Behavior.AttachmentAllowedHost},
			{name: "attachment_allowed_path", value: config.Behavior.AttachmentAllowedPath},
		} {
			if strings.TrimSpace(item.value) == "" {
				return fmt.Errorf("%s 自动回复的新标签页附件缺少平台配置 %s", config.Name, item.name)
			}
		}
	case "", "preview_download":
	default:
		return fmt.Errorf("%s 自动回复的附件模式 %s 暂不支持", config.Name, config.Behavior.AttachmentMode)
	}
	for _, key := range attachmentKeys {
		selector, exists := config.Selectors[key]
		if !exists || len(selector.Target.Selectors) == 0 {
			return fmt.Errorf("%s 自动回复的附件简历能力尚未适配完整（缺少 %s），这次我先不启动，避免把候选人简历处理错；请更新本地程序后再试", config.Name, key)
		}
	}
	return nil
}
