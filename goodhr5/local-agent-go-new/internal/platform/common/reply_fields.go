// Package common 本文件按联系人和聊天消息的读取范围选择字段，兼容现有混合 conversation_fields 配置。
package common

import (
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// configuredContactFields 只读取联系人解析所需字段，避免在联系人卡片内查找消息正文和附件。
func configuredContactFields(cfg model.Config) map[string]contract.SelectorSpec {
	return configuredConversationFields(cfg, "name", "avatar_url", "last_message", "summary", "unread_count", "thread_id", "thread_meta", "id", "candidate_id", "account_id", "gender", "position_name")
}

// configuredMessageFields 只读取消息解析所需字段，保留所有方向、编号和卡片后备字段，不在消息节点中补读联系人姓名。
func configuredMessageFields(cfg model.Config) map[string]contract.SelectorSpec {
	return configuredConversationFields(cfg, "message_text", "text", "message_type", "message_time", "message_id", "message_meta", "id", "direction", "sender_role", "candidate_marker", "self_marker", "body_class", "item_class", "resume_card", "common_card", "attachment_card", "candidate_id", "candidate_meta")
}

// configuredConversationFields 从旧配置复制当前节点真正消费的字段，不修改共享平台配置或丢失选择器属性。
func configuredConversationFields(cfg model.Config, keys ...string) map[string]contract.SelectorSpec {
	fields := make(map[string]contract.SelectorSpec, len(keys))
	for _, key := range keys {
		if selector, found := cfg.ConversationFields[key]; found {
			fields[key] = selector
		}
	}
	return fields
}
