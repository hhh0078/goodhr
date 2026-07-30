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
	if err = verifyMessageConversation(ctx, browser, cfg, conversation); err != nil {
		return "", err
	}
	contextSelector, err := RequiredSelector(cfg, "message.context")
	if err != nil {
		return "", err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: contextSelector, Property: "text"})
	return result.Value, err
}

// ReplyConversation 核对当前会话身份后复用公共消息发送能力。
func ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, reply string) error {
	if strings.TrimSpace(reply) == "" {
		return fmt.Errorf("回复内容不能为空")
	}
	if err := verifyMessageConversation(ctx, browser, cfg, conversation); err != nil {
		return err
	}
	return sendMessage(ctx, browser, cfg, "message.input", "message.send", reply)
}

// verifyMessageConversation 在配置了会话姓名时确认当前打开的会话没有串到其他候选人。
func verifyMessageConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) error {
	if _, configured := cfg.Selectors["message.current_name"]; !configured {
		return nil
	}
	expected := strings.TrimSpace(conversation.Name)
	if expected == "" {
		return fmt.Errorf("%s未读会话缺少候选人姓名，回复没有发送", cfg.Name)
	}
	for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
		actual, found, err := ReadOptional(ctx, browser, cfg, "message.current_name")
		if err != nil {
			return err
		}
		if found && CandidateNamesMatch(expected, actual) {
			return nil
		}
		if attempt < candidateConversationPollAttempts {
			if err = waitConversationPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("%s当前会话候选人与待回复对象不一致，回复没有发送", cfg.Name)
}
