// Package boss 文件作用：把 Boss 直聘完整自动回复能力接入公共配置驱动流程。
package boss

import (
	"context"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var _ model.AutoReplyRuntime = (*Runtime)(nil)

var bossAutoReplyAdapter = common.ConfiguredAutoReplyAdapter{
	PlatformID: "boss",
	MessageRules: common.AutoReplyMessageRules{
		CandidateTokens: []string{"item-friend", "receive", "other", "left"},
		SelfTokens:      []string{"item-myself", "send", "self", "right"},
	},
	CleanPosition: cleanBossAutoReplyPosition,
}

// cleanBossAutoReplyPosition 清理 Boss 聊天头部的沟通职位前缀和空白。
func cleanBossAutoReplyPosition(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "沟通职位：")
	return strings.Join(strings.Fields(value), " ")
}

// InitializeAutoReplyPage 清理 Boss 遗留弹层。
func (r *Runtime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.InitializeConfiguredAutoReplyPage(ctx, browser, cfg)
}

// ScanUnreadConversations 返回 Boss 真实未读会话。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	return common.ScanConfiguredAutoReplyConversations(ctx, browser, cfg, bossAutoReplyAdapter)
}

// OpenAutoReplyConversation 打开 Boss 会话并读取身份、岗位和差量历史。
func (r *Runtime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, knownMessageKeys []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	return common.OpenConfiguredAutoReplyConversation(ctx, browser, cfg, bossAutoReplyAdapter, conversation, knownMessageKeys, maxHistory)
}

// ReadAutoReplyMessages 读取 Boss 当前聊天框的差量消息。
func (r *Runtime) ReadAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	return common.ReadConfiguredAutoReplyMessages(ctx, browser, cfg, bossAutoReplyAdapter, snapshot, knownMessageKeys, maxHistory)
}

// RequestAutoReplyResume 在 Boss 当前聊天框索要简历。
func (r *Runtime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	return common.RequestConfiguredAutoReplyResume(ctx, browser, cfg, bossAutoReplyAdapter, snapshot)
}

// CollectAutoReplyResume 只下载 Boss 真实附件，不用 DOM 在线简历冒充附件解析。
func (r *Runtime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	return common.CollectConfiguredAutoReplyResume(ctx, browser, cfg, bossAutoReplyAdapter, snapshot, false)
}

// SendAutoReplyMessage 核对 Boss 候选人和岗位后发送消息。
func (r *Runtime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	return common.SendConfiguredAutoReplyMessage(ctx, browser, cfg, bossAutoReplyAdapter, snapshot, message)
}

// ReadLatestAutoReplyMessage 返回 Boss 当前聊天框最新双方消息。
func (r *Runtime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	return common.ReadLatestConfiguredAutoReplyMessage(ctx, browser, cfg, bossAutoReplyAdapter, snapshot)
}

// ReadOpenAutoReplyLatestMessage 仅在 Boss 目标聊天框仍打开时读取最新消息。
func (r *Runtime) ReadOpenAutoReplyLatestMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	return common.ReadOpenLatestConfiguredAutoReplyMessage(ctx, browser, cfg, bossAutoReplyAdapter, snapshot)
}

// CloseAutoReplyConversation 关闭 Boss 附件、聊天框和联系人列表。
func (r *Runtime) CloseAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.AutoReplyConversationSnapshot) error {
	return common.CloseConfiguredAutoReplyConversation(ctx, browser, cfg)
}

// ReadConversation 兼容平台公共接口并返回可读聊天正文。
func (r *Runtime) ReadConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (string, error) {
	snapshot, err := r.OpenAutoReplyConversation(ctx, browser, cfg, conversation, nil, 5000)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		if text := strings.TrimSpace(message.TextContent); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// ReplyConversation 兼容平台公共接口并在发送前继续核对候选人。
func (r *Runtime) ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, reply string) error {
	return r.SendAutoReplyMessage(ctx, browser, cfg, model.AutoReplyConversationSnapshot{
		Conversation: conversation, CandidateName: conversation.Name,
		CommunicationPosition: conversation.CommunicationPosition,
	}, reply)
}
