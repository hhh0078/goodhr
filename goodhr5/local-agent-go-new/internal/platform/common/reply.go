// Package common 文件作用：提供所有招聘平台复用的未读会话扫描、读取和回复能力。
package common

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// EnsureUnreadConversationDrawer 在推荐页复用或打开未读联系人抽屉。
// 返回 false 表示入口没有未读数字，本轮无需打开联系人列表。
func EnsureUnreadConversationDrawer(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	opened, err := ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
	if err != nil || opened {
		return opened, err
	}
	unread, found, err := ReadOptional(ctx, browser, cfg, "message.entry_unread_count")
	if err != nil {
		return false, err
	}
	normalizedUnread := strings.Trim(strings.TrimSpace(unread), "+")
	if !found || normalizedUnread == "" || normalizedUnread == "0" {
		return false, nil
	}
	if err = ClickRequired(ctx, browser, cfg, "message.entry"); err != nil {
		return false, fmt.Errorf("打开%s未读联系人列表失败：%w", cfg.Name, err)
	}
	for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
		opened, err = ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
		if err != nil || opened {
			return opened, err
		}
		if attempt < candidateConversationPollAttempts {
			if err = waitConversationPoll(ctx); err != nil {
				return false, err
			}
		}
	}
	return false, fmt.Errorf("%s联系人列表没有在 6 秒内打开", cfg.Name)
}

// FindConfiguredConversationItems 使用平台配置读取联系人列表项及相对字段。
func FindConfiguredConversationItems(ctx context.Context, browser model.Browser, cfg model.Config, selectorKey string, expectedMissing bool) ([]contract.FindAllItem, error) {
	selector, err := RequiredSelector(cfg, selectorKey)
	if err != nil {
		return nil, err
	}
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: positiveOr(cfg.MaxItems, 100),
		Fields: cfg.ConversationFields, ExpectedMissing: expectedMissing,
	})
	if expectedMissing && IsElementMissing(err) {
		return nil, nil
	}
	return items, err
}

// OpenConfiguredConversationItem 通过最新联系人列表序号滚动并点击会话。
// 点击前使用联系人抽屉作为真实滚轮落点，避免旧序号和右侧边缘导致串会话。
func OpenConfiguredConversationItem(ctx context.Context, browser model.Browser, cfg model.Config, index int) error {
	selector, err := CandidateScopedSelectorWithParent(cfg, "message.contact_item", "message.contact_click_target", index)
	if err != nil {
		return err
	}
	anchor, err := RequiredSelector(cfg, "message.drawer_scroll")
	if err != nil {
		return err
	}
	requireFull := true
	_, scrollErr := browser.Scroll(ctx, contract.ScrollRequest{
		Target: &selector, WheelAnchor: &anchor, Distance: 180, MaxAttempts: 18,
		WaitMS: 180, RequireFull: &requireFull, ViewportMargin: 80,
	})
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 0,
	}); err != nil {
		if scrollErr != nil {
			return fmt.Errorf("滚动到%s联系人失败：%v；随后点击也失败：%w", cfg.Name, scrollErr, err)
		}
		return fmt.Errorf("打开%s候选人会话失败：%w", cfg.Name, err)
	}
	return nil
}

// ReadConfiguredConversationMessages 读取当前聊天框中的全部已加载消息和结构化字段。
func ReadConfiguredConversationMessages(ctx context.Context, browser model.Browser, cfg model.Config) ([]contract.FindAllItem, error) {
	selector, err := RequiredSelector(cfg, "message.item")
	if err != nil {
		return nil, err
	}
	return browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 5000, Fields: cfg.ConversationFields,
	})
}

// ScrollConversationHistory 使用聊天容器作为落点向上滚动一轮历史消息。
func ScrollConversationHistory(ctx context.Context, browser model.Browser, cfg model.Config) (contract.ScrollResult, error) {
	anchor, err := RequiredSelector(cfg, "message.history_scroll")
	if err != nil {
		return contract.ScrollResult{}, err
	}
	return browser.Scroll(ctx, contract.ScrollRequest{
		WheelAnchor: &anchor, Distance: -620, MaxAttempts: 1, WaitMS: 300,
	})
}

// SendAutoReplyText 复用统一输入和发送动作向当前已核对的聊天框发送消息。
func SendAutoReplyText(ctx context.Context, browser model.Browser, cfg model.Config, message string) error {
	return sendMessage(ctx, browser, cfg, "message.input", "message.send", message)
}

// CloseAutoReplyPanels 依次关闭附件预览、聊天框和联系人抽屉。
func CloseAutoReplyPanels(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if err := CloseOptionalPanel(
		ctx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", cfg.Name+"附件预览",
	); err != nil {
		return err
	}
	return CloseCandidatePanels(ctx, browser, cfg)
}

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
		threadID := firstNonEmpty(item.Fields["thread_id"], item.Fields["id"])
		key := firstNonEmpty(threadID, HashText(platformID+"|"+name+"|"+item.Text))
		result = append(result, model.Conversation{
			Index: item.Index, Key: key, Name: name, Gender: item.Fields["gender"],
			PlatformThreadID: threadID, PlatformCandidateID: item.Fields["candidate_id"],
			PlatformAccountID: item.Fields["account_id"], CommunicationPosition: item.Fields["position_name"],
			Summary: item.Text, Fields: item.Fields,
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
