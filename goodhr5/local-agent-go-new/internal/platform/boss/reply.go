// Package boss 文件作用：实现 Boss 未读会话扫描、上下文读取和回复。
package boss

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ScanUnreadConversations 返回 Boss 未读会话摘要。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	return common.ScanUnreadConversations(ctx, browser, cfg, r.PlatformID())
}

// ReadConversation 打开 Boss 未读会话并读取上下文。
func (r *Runtime) ReadConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (string, error) {
	return common.ReadConversation(ctx, browser, cfg, conversation)
}

// ReplyConversation 向当前 Boss 会话发送回复。
func (r *Runtime) ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, reply string) error {
	return common.ReplyConversation(ctx, browser, cfg, conversation, reply)
}
