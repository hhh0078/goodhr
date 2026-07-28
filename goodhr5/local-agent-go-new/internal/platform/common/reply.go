// Package common 文件作用：提供所有招聘平台复用的未读会话扫描、读取和回复能力。
package common

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ScanUnreadConversations 读取未读会话列表并整理统一字段。
func ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config, platformID string) ([]model.Conversation, error) {
	selector, err := RequiredSelector(cfg, "message.unread_item")
	if err != nil {
		return nil, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: positiveOr(cfg.MaxItems, 100), Fields: cfg.ConversationFields,
	})
	if err != nil {
		return nil, err
	}
	result := make([]model.Conversation, 0, len(items))
	for _, item := range items {
		name := firstNonEmpty(item.Fields["name"], item.Text)
		key := firstNonEmpty(item.Fields["id"], HashText(platformID+"|"+name+"|"+item.Text))
		result = append(result, model.Conversation{
			Index: item.Index, Key: key, Name: name, Summary: item.Text, Fields: item.Fields,
		})
	}
	return result, nil
}

// ReadConversation 打开指定未读会话并读取上下文。
func ReadConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (string, error) {
	selector, err := IndexedSelector(cfg, "message.unread_item", conversation.Index)
	if err != nil {
		return "", err
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector}); err != nil {
		return "", err
	}
	contextSelector, err := RequiredSelector(cfg, "message.context")
	if err != nil {
		return "", err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: contextSelector, Property: "text"})
	return result.Value, err
}

// ReplyConversation 输入非空回复并点击发送。
func ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, reply string) error {
	if strings.TrimSpace(reply) == "" {
		return fmt.Errorf("回复内容不能为空")
	}
	if err := InputRequired(ctx, browser, cfg, "message.input", reply); err != nil {
		return err
	}
	return ClickRequired(ctx, browser, cfg, "message.send")
}
