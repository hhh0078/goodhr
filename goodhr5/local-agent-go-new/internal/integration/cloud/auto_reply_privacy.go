// Package cloud 本文件生成不会泄露手机号、邮箱、聊天正文和附件路径的自动回复日志摘要。
package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// AutoReplyPrivacyInput 表示生成安全日志摘要时可能持有的敏感上下文。
type AutoReplyPrivacyInput struct {
	ConversationID      string
	PlatformCandidateID string
	Phone               string
	Email               string
	LatestMessage       string
	AttachmentPath      string
	MessageCount        int
	AttachmentCount     int
}

// AutoReplyPrivacySummary 表示只保留存在性、数量和会话哈希的日志安全字段。
type AutoReplyPrivacySummary struct {
	ConversationRef      string
	HasPlatformCandidate bool
	HasPhone             bool
	HasEmail             bool
	HasLatestMessage     bool
	HasAttachment        bool
	MessageCount         int
	AttachmentCount      int
}

// AutoReplySafeSummary 把敏感上下文转换为可写入普通运行日志的安全摘要。
func AutoReplySafeSummary(input AutoReplyPrivacyInput) AutoReplyPrivacySummary {
	return AutoReplyPrivacySummary{
		ConversationRef:      autoReplyReference(input.ConversationID),
		HasPlatformCandidate: strings.TrimSpace(input.PlatformCandidateID) != "",
		HasPhone:             strings.TrimSpace(input.Phone) != "",
		HasEmail:             strings.TrimSpace(input.Email) != "",
		HasLatestMessage:     strings.TrimSpace(input.LatestMessage) != "",
		HasAttachment:        strings.TrimSpace(input.AttachmentPath) != "",
		MessageCount:         max(input.MessageCount, 0),
		AttachmentCount:      max(input.AttachmentCount, 0),
	}
}

// String 返回适合统一步骤日志的短摘要，不包含任何敏感原文。
func (summary AutoReplyPrivacySummary) String() string {
	return fmt.Sprintf(
		"会话=%s 平台编号=%t 手机=%t 邮箱=%t 新消息=%t 附件=%t 消息数=%d 附件数=%d",
		summary.ConversationRef,
		summary.HasPlatformCandidate,
		summary.HasPhone,
		summary.HasEmail,
		summary.HasLatestMessage,
		summary.HasAttachment,
		summary.MessageCount,
		summary.AttachmentCount,
	)
}

// autoReplyReference 为日志生成不可逆且稳定的短会话引用。
func autoReplyReference(value string) string {
	if strings.TrimSpace(value) == "" {
		return "无"
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:12]
}
