// Package boss 文件作用：把 Boss 直聘完整自动回复能力接入公共配置驱动流程。
package boss

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var _ model.AutoReplyRuntime = (*Runtime)(nil)
var _ model.AutoReplyInfoRequester = (*Runtime)(nil)

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
func (r *Runtime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config, target *model.AutoReplyConversationSnapshot) error {
	r.contactRows = nil
	return initializeBossAutoReplyPage(ctx, browser, cfg, target)
}

// ScanUnreadConversations 返回全部联系人，服务器按持久水位发现真实变化，不依赖已消费的未读红点。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	if err := ensureBossConversationDrawer(ctx, browser, cfg); err != nil {
		return nil, err
	}
	return r.readBossContacts(ctx, browser, cfg)
}

// OpenAutoReplyConversation 打开 Boss 会话并读取身份、岗位和差量历史。
func (r *Runtime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, knownMessageKeys []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	snapshot, err := openBossAutoReplyContact(ctx, browser, cfg, conversation)
	if err != nil {
		return snapshot, err
	}
	history, err := readBossHistoryWindow(ctx, browser, cfg, knownMessageKeys, maxHistory, nil)
	if err != nil {
		return snapshot, err
	}
	// 历史采集可能翻页；返回资料前再次核对当前原生身份，避免期间手动切会话而串入别人的消息。
	if err = r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		return snapshot, err
	}
	if err = bindBossNativeInfoCards(history.Messages, snapshot.CandidateName); err != nil {
		return snapshot, err
	}
	snapshot.Messages, snapshot.HistoryComplete = history.Messages, history.HistoryComplete
	snapshot.ResumeCardAvailable, snapshot.ResumeSourceMessageID = history.ResumeCardAvailable, history.ResumeSourceMessageID
	return snapshot, nil
}

// ReadAutoReplyMessages 读取 Boss 当前聊天框的差量消息。
func (r *Runtime) ReadAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return nil, false, err
	}
	if snapshot.DeliveryPageURL != "" {
		messages, err := readBossRecentMessages(ctx, browser, cfg, min(16, max(1, maxHistory)))
		if err != nil {
			return nil, false, err
		}
		known := make(map[string]bool, len(knownMessageKeys))
		for _, key := range knownMessageKeys {
			known[key] = true
		}
		for i := len(messages) - 1; i >= 0; i-- {
			if known[messages[i].Key] {
				messages = messages[bossPendingResumeOfferStart(messages, i+1):]
				break
			}
		}
		if err = r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
			return nil, false, err
		}
		return messages, false, bindBossNativeInfoCards(messages, snapshot.CandidateName)
	}
	history, err := readBossHistoryWindow(ctx, browser, cfg, knownMessageKeys, maxHistory, snapshot.HistoryAfter)
	if err == nil {
		err = r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
	}
	if err != nil {
		return nil, false, err
	}
	return history.Messages, history.HistoryComplete, bindBossNativeInfoCards(history.Messages, snapshot.CandidateName)
}

// RequestAutoReplyResume 在 Boss 当前聊天框索要简历。
func (r *Runtime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return err
	}
	if snapshot.ExpectedSendMessageKey == "" {
		latest, err := readBossLatestAutoReplyMessage(ctx, browser, cfg)
		if err != nil {
			return err
		}
		snapshot.ExpectedSendMessageKey = latest.Key
	}
	result, err := requestBossAutoReplyCandidateInfo(ctx, browser, cfg, snapshot, model.CandidateInfoRequest{RequestResume: true})
	if err != nil {
		return err
	}
	if result.Status == model.AutoReplyInfoUnavailable {
		return model.ErrAutoReplyResumeRequestUnavailable
	}
	if result.Status == model.AutoReplyInfoDeferred {
		return fmt.Errorf("BOSS资料申请尚需等待双方沟通，本轮未触发索要")
	}
	return nil
}

// RequestAutoReplyCandidateInfo 只在平台已核实双向沟通与原生入口状态后执行一类资料索要。
func (r *Runtime) RequestAutoReplyCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, request model.CandidateInfoRequest) (model.AutoReplyInfoRequestResult, error) {
	return requestBossAutoReplyCandidateInfo(ctx, browser, cfg, snapshot, request)
}

// CollectAutoReplyResume 只下载 Boss 真实附件，不用 DOM 在线简历冒充附件解析。
func (r *Runtime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	return collectBossAutoReplyResume(ctx, browser, cfg, snapshot)
}

// SendAutoReplyMessage 核对 Boss 候选人和岗位后发送消息。
func (r *Runtime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	return sendBossAutoReplyMessage(ctx, browser, cfg, snapshot, message)
}

// ReadLatestAutoReplyMessage 返回 Boss 当前聊天框最新双方消息。
func (r *Runtime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return model.ConversationMessage{}, err
	}
	return readBossLatestAutoReplyMessage(ctx, browser, cfg)
}

// ReadOpenAutoReplyLatestMessage 仅在 Boss 目标聊天框仍打开时读取最新消息。
func (r *Runtime) ReadOpenAutoReplyLatestMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	matched, err := bossAutoReplyConversationMatches(ctx, browser, cfg, snapshot)
	if err != nil || !matched {
		return model.ConversationMessage{}, false, err
	}
	latest, err := readBossLatestAutoReplyMessage(ctx, browser, cfg)
	return latest, true, err
}

// CloseAutoReplyConversation 关闭 Boss 附件、聊天框和联系人列表。
func (r *Runtime) CloseAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	return closeBossAutoReplyConversation(ctx, browser, cfg, snapshot)
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
