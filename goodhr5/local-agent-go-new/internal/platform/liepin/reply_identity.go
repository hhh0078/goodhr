// Package liepin 本文件通过当前可见聊天框的标准读取能力核验精确候选人编号，供单会话测试限制使用。
package liepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// VerifyAutoReplyConversationIdentity 只读核对当前姓名和按钮cid，不从历史消息或旧快照推测当前页面归属。
func (r *Runtime) VerifyAutoReplyConversationIdentity(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if strings.TrimSpace(snapshot.CandidateName) == "" || strings.TrimSpace(snapshot.PlatformCandidateID) == "" {
		return fmt.Errorf("指定候选人的姓名或平台编号不完整，已阻止页面动作")
	}
	// 缺少配置不能当成聊天关闭，只有 Worker 明确找不到姓名节点才允许流程重新打开目标。
	if _, err := common.RequiredSelector(cfg, "message.current_name"); err != nil {
		return err
	}
	name, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_name")
	if err != nil {
		return err
	}
	if !found {
		return model.ErrAutoReplyConversationNotOpen
	}
	if strings.TrimSpace(name) != snapshot.CandidateName {
		return fmt.Errorf("当前聊天框不是指定候选人，已阻止页面动作")
	}
	actualID, err := readLiepinCandidateID(ctx, browser, cfg, nil)
	if err != nil {
		return err
	}
	if actualID == "" || actualID != snapshot.PlatformCandidateID {
		return fmt.Errorf("当前聊天框的候选人编号无法核实，已阻止页面动作")
	}
	return nil
}

// openLiepinConversation 普通会话沿用原入口，带精确候选人编号时用平台属性锁定联系人，避免滚动期间列表重排点错人。
func openLiepinConversation(ctx context.Context, browser model.Browser, cfg model.Config, expected model.Conversation) error {
	if expected.PlatformCandidateID == "" {
		index, err := locateLiepinConversation(ctx, browser, cfg, expected)
		if err != nil {
			return err
		}
		return common.OpenConfiguredConversationItem(ctx, browser, cfg, index)
	}
	items, err := common.FindConfiguredConversationItems(ctx, browser, cfg, "message.contact_item", false)
	if err != nil {
		return err
	}
	conversations, err := liepinConversations(items, false)
	if err != nil {
		return err
	}
	matches, metadata := 0, ""
	for _, conversation := range conversations {
		if conversation.PlatformThreadID == expected.PlatformThreadID && conversation.Name == expected.Name {
			matches++
			metadata = conversation.Fields["thread_meta"]
		}
	}
	attribute := cfg.ConversationFields["thread_meta"].ReadAttribute
	if expected.PlatformThreadID == "" || matches != 1 || metadata == "" || attribute == "" {
		return fmt.Errorf("指定会话的稳定页面属性无法确认，已阻止打开")
	}
	card, err := common.RequiredSelector(cfg, "message.contact_item")
	if err != nil {
		return err
	}
	card = card.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
		attributes := make(map[string]string, len(variant.Target.Attributes)+1)
		for key, value := range variant.Target.Attributes {
			attributes[key] = value
		}
		attributes[attribute] = metadata
		variant.Target.Attributes = attributes
		variant.Target.Index = nil
		return variant
	})
	action, err := common.RequiredSelector(cfg, "message.contact_click_target")
	if err != nil {
		return err
	}
	action = action.WithinSelector(card)
	anchor, err := common.RequiredSelector(cfg, "message.drawer_scroll")
	if err != nil {
		return err
	}
	requireFull := true
	_, err = browser.Scroll(ctx, contract.ScrollRequest{Target: &action, WheelAnchor: &anchor, Distance: 180, MaxAttempts: 18, WaitMS: 180, RequireFull: &requireFull, ViewportMargin: 12})
	if err != nil {
		return fmt.Errorf("滚动到指定会话失败，已阻止打开：%w", err)
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: action, ViewportMargin: 0})
	return err
}
