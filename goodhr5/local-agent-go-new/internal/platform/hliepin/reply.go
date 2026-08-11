// Package hliepin 文件作用：把猎聘猎头端完整自动回复能力接入公共配置驱动流程。
package hliepin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var _ model.AutoReplyRuntime = (*Runtime)(nil)

var hliepinAutoReplyMessageRules = common.AutoReplyMessageRules{
	CandidateTokens: []string{"message-item-receive"},
	SelfTokens:      []string{"message-item-send"},
}

var hliepinAutoReplyAdapter = common.ConfiguredAutoReplyAdapter{
	PlatformID:         "hliepin",
	MessageRules:       hliepinAutoReplyMessageRules,
	ParseConversations: parseHLiepinAutoReplyConversations,
	ParseMessages:      parseHLiepinAutoReplyMessages,
	ParseCandidateID:   parseHLiepinAutoReplyCandidateID,
	CleanPosition:      cleanHLiepinAutoReplyPosition,
}

// InitializeAutoReplyPage 清理猎聘猎头端遗留弹层。
func (r *Runtime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.InitializeConfiguredAutoReplyPage(ctx, browser, cfg)
}

// ScanUnreadConversations 返回猎聘猎头端真实带未读数字的会话。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	return common.ScanConfiguredAutoReplyConversations(ctx, browser, cfg, hliepinAutoReplyAdapter)
}

// OpenAutoReplyConversation 打开猎聘猎头端会话并读取身份、岗位和差量历史。
func (r *Runtime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, knownMessageKeys []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	return common.OpenConfiguredAutoReplyConversation(ctx, browser, cfg, hliepinAutoReplyAdapter, conversation, knownMessageKeys, maxHistory)
}

// ReadAutoReplyMessages 读取猎聘猎头端当前聊天框的差量消息。
func (r *Runtime) ReadAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	return common.ReadConfiguredAutoReplyMessages(ctx, browser, cfg, hliepinAutoReplyAdapter, snapshot, knownMessageKeys, maxHistory)
}

// RequestAutoReplyResume 在猎聘猎头端当前聊天框索要简历。
func (r *Runtime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	return common.RequestConfiguredAutoReplyResume(ctx, browser, cfg, hliepinAutoReplyAdapter, snapshot)
}

// CollectAutoReplyResume 下载猎聘猎头端真实附件，不用整页正文冒充在线简历。
func (r *Runtime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	return common.CollectConfiguredAutoReplyResume(ctx, browser, cfg, hliepinAutoReplyAdapter, snapshot, false)
}

// SendAutoReplyMessage 核对猎聘猎头端候选人和岗位后发送消息。
func (r *Runtime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	return common.SendConfiguredAutoReplyMessage(ctx, browser, cfg, hliepinAutoReplyAdapter, snapshot, message)
}

// ReadLatestAutoReplyMessage 返回猎聘猎头端当前聊天框的最新双方消息。
func (r *Runtime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	return common.ReadLatestConfiguredAutoReplyMessage(ctx, browser, cfg, hliepinAutoReplyAdapter, snapshot)
}

// ReadOpenAutoReplyLatestMessage 仅在猎聘猎头端目标聊天框仍打开时读取最新消息。
func (r *Runtime) ReadOpenAutoReplyLatestMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	return common.ReadOpenLatestConfiguredAutoReplyMessage(ctx, browser, cfg, hliepinAutoReplyAdapter, snapshot)
}

// CloseAutoReplyConversation 关闭猎聘猎头端附件、聊天框和联系人列表。
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

// cleanHLiepinAutoReplyPosition 清理猎聘聊天头部的沟通职位前缀和空白。
func cleanHLiepinAutoReplyPosition(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "沟通职位：")
	return strings.Join(strings.Fields(value), " ")
}

// parseHLiepinAutoReplyConversations 从猎聘埋点 JSON 中提取稳定 to_imid，再交给公共字段整理。
func parseHLiepinAutoReplyConversations(items []contract.FindAllItem, unreadOnly bool) ([]model.Conversation, error) {
	prepared := make([]contract.FindAllItem, 0, len(items))
	for _, item := range items {
		fields := make(map[string]string, len(item.Fields)+1)
		for key, value := range item.Fields {
			fields[key] = value
		}
		encoded := strings.TrimSpace(fields["thread_meta"])
		if encoded != "" {
			decoded, err := url.QueryUnescape(encoded)
			if err != nil {
				return nil, fmt.Errorf("解析猎聘猎头端联系人会话编号失败：%w", err)
			}
			var meta struct {
				ThreadID string `json:"to_imid"`
			}
			if err = json.Unmarshal([]byte(decoded), &meta); err != nil {
				return nil, fmt.Errorf("解析猎聘猎头端联系人会话编号失败：%w", err)
			}
			fields["thread_id"] = strings.TrimSpace(meta.ThreadID)
			delete(fields, "thread_meta")
		}
		item.Fields = fields
		prepared = append(prepared, item)
	}
	return common.ConfiguredConversations(prepared, "hliepin", unreadOnly)
}

// parseHLiepinAutoReplyMessages 解码猎聘消息属性中的稳定 message_id 和候选人 cid。
func parseHLiepinAutoReplyMessages(items []contract.FindAllItem, now time.Time) ([]model.ConversationMessage, error) {
	prepared := make([]contract.FindAllItem, 0, len(items))
	for _, item := range items {
		fields := make(map[string]string, len(item.Fields)+2)
		for key, value := range item.Fields {
			fields[key] = value
		}
		messageID, err := parseHLiepinAutoReplyMessageID(fields["message_meta"])
		if err != nil {
			return nil, fmt.Errorf("解析猎聘猎头端消息编号失败：%w", err)
		}
		fields["message_id"] = messageID
		fields["candidate_id"] = parseHLiepinAutoReplyCandidateID(fields["candidate_meta"])
		item.Fields = fields
		prepared = append(prepared, item)
	}
	return common.ParseConfiguredAutoReplyMessages(prepared, hliepinAutoReplyMessageRules, now)
}

// parseHLiepinAutoReplyMessageID 解码 data-tlg-ext 中的平台消息编号。
func parseHLiepinAutoReplyMessageID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return "", err
	}
	var meta struct {
		MessageID string `json:"message_id"`
	}
	if err = json.Unmarshal([]byte(decoded), &meta); err != nil {
		return "", err
	}
	return strings.TrimSpace(meta.MessageID), nil
}

// parseHLiepinAutoReplyCandidateID 从 data-tlg-scm 查询串中读取候选人 cid。
func parseHLiepinAutoReplyCandidateID(value string) string {
	values, err := url.ParseQuery(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values.Get("cid"))
}
