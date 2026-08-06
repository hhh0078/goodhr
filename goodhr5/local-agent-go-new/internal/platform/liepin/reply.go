// Package liepin 文件作用：实现猎聘企业端自动回复的未读扫描、会话校验、消息发送和关闭动作。
package liepin

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const liepinConversationPollAttempts = 20
const liepinConversationPollInterval = 300 * time.Millisecond

// InitializeAutoReplyPage 清理遗留弹层，让每轮扫描都从悬浮入口的新消息数字开始。
func (r *Runtime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if err := common.CloseOptionalPanel(
		ctx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", cfg.Name+"附件预览",
	); err != nil {
		return err
	}
	return common.CloseCandidatePanels(ctx, browser, cfg)
}

// ScanUnreadConversations 返回猎聘企业端当前联系人抽屉中的未读会话。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	ready, unreadCount, err := ensureLiepinUnreadConversationDrawer(ctx, browser, cfg)
	if err != nil || !ready {
		return nil, err
	}
	items, err := common.FindConfiguredConversationItems(ctx, browser, cfg, "message.unread_item", true)
	if err != nil {
		return nil, err
	}
	conversations, err := liepinConversations(items, true)
	if err != nil {
		return nil, err
	}
	if unreadCount > 0 && len(conversations) > unreadCount {
		conversations = conversations[:unreadCount]
	}
	return conversations, nil
}

// ensureLiepinUnreadConversationDrawer 打开联系人抽屉并确认切到未读标签，保证会话始终从列表首项依次处理。
func ensureLiepinUnreadConversationDrawer(ctx context.Context, browser model.Browser, cfg model.Config) (bool, int, error) {
	ready, unreadCount, err := common.EnsureUnreadConversationDrawer(ctx, browser, cfg)
	if err != nil || !ready {
		return ready, unreadCount, err
	}
	selected, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.unread_tab_selected")
	if err != nil || selected {
		return ready, unreadCount, err
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err = common.ClickRequired(ctx, browser, cfg, "message.unread_tab"); err != nil {
			continue
		}
		selected, err = common.ProbeSelectorExists(ctx, browser, cfg, "message.unread_tab_selected")
		if err != nil {
			return false, 0, err
		}
		if selected {
			return true, unreadCount, nil
		}
	}
	if err != nil {
		return false, 0, fmt.Errorf("切换%s联系人未读列表失败：%w", cfg.Name, err)
	}
	return false, 0, fmt.Errorf("%s联系人列表没有切到未读标签", cfg.Name)
}

// OpenAutoReplyConversation 重新按稳定会话编号定位联系人，并读取身份、岗位、聊天和简历卡片。
func (r *Runtime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, knownLastMessageKey string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	ready, _, err := ensureLiepinUnreadConversationDrawer(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if !ready {
		return model.AutoReplyConversationSnapshot{}, fmt.Errorf("%s未读联系人列表没有打开", cfg.Name)
	}
	index, err := locateLiepinConversation(ctx, browser, cfg, conversation)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if err = common.OpenConfiguredConversationItem(ctx, browser, cfg, index); err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	name, position, err := waitLiepinConversation(ctx, browser, cfg, conversation.Name)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	messages, historyComplete, err := readLiepinConversationHistory(
		ctx, browser, cfg, knownLastMessageKey, maxHistory,
	)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	gender, err := readLiepinGender(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	candidateID, err := readLiepinCandidateID(ctx, browser, cfg, messages)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	resumeAvailable, resumeSourceKey := liepinResumeCard(messages)
	return model.AutoReplyConversationSnapshot{
		Conversation: conversation, CandidateName: name, Gender: gender,
		PlatformThreadID:    firstLiepinValue(conversation.PlatformThreadID, conversation.Key),
		PlatformCandidateID: candidateID, PlatformAccountID: conversation.PlatformAccountID,
		CommunicationPosition: position, Messages: messages, HistoryComplete: historyComplete,
		ResumeCardAvailable: resumeAvailable, ResumeSourceMessageID: resumeSourceKey,
	}, nil
}

// SendAutoReplyMessage 核对候选人和沟通岗位后发送一条自动回复。
func (r *Runtime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName); err != nil {
		return err
	}
	position, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_position")
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(snapshot.CommunicationPosition); expected != "" && (!found || !liepinPositionMatches(expected, position)) {
		return fmt.Errorf("%s当前聊天框的沟通岗位已经变化，消息没有发送", cfg.Name)
	}
	return common.SendAutoReplyText(ctx, browser, cfg, message)
}

// ReadLatestAutoReplyMessage 返回当前聊天框最后一条候选人或 HR 消息。
func (r *Runtime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if _, _, err := waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName); err != nil {
		return model.ConversationMessage{}, err
	}
	items, err := common.ReadConfiguredConversationMessages(ctx, browser, cfg)
	if err != nil {
		return model.ConversationMessage{}, err
	}
	messages, err := liepinMessages(items)
	if err != nil {
		return model.ConversationMessage{}, err
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Direction == "candidate" || messages[index].Direction == "self" {
			return messages[index], nil
		}
	}
	return model.ConversationMessage{}, fmt.Errorf("%s当前聊天框没有读到候选人或 HR 消息", cfg.Name)
}

// CloseAutoReplyConversation 关闭附件预览、候选人聊天框和联系人抽屉。
func (r *Runtime) CloseAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.AutoReplyConversationSnapshot) error {
	return common.CloseAutoReplyPanels(ctx, browser, cfg)
}

// ReadConversation 兼容平台公共接口，安全打开会话后返回可读聊天正文。
func (r *Runtime) ReadConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (string, error) {
	snapshot, err := r.OpenAutoReplyConversation(ctx, browser, cfg, conversation, "", 5000)
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

// ReplyConversation 兼容平台公共接口，并在发送前继续核对当前候选人。
func (r *Runtime) ReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, reply string) error {
	snapshot := model.AutoReplyConversationSnapshot{
		Conversation: conversation, CandidateName: conversation.Name,
		CommunicationPosition: conversation.CommunicationPosition,
	}
	return r.SendAutoReplyMessage(ctx, browser, cfg, snapshot, reply)
}

// locateLiepinConversation 每次点击前重新扫描全部联系人，并按会话编号唯一定位最新序号。
func locateLiepinConversation(ctx context.Context, browser model.Browser, cfg model.Config, expected model.Conversation) (int, error) {
	items, err := common.FindConfiguredConversationItems(ctx, browser, cfg, "message.contact_item", false)
	if err != nil {
		return -1, err
	}
	conversations, err := liepinConversations(items, false)
	if err != nil {
		return -1, err
	}
	matches := make([]model.Conversation, 0, 1)
	expectedThreadID := firstLiepinValue(expected.PlatformThreadID, expected.Key)
	for _, item := range conversations {
		if expectedThreadID != "" && item.PlatformThreadID == expectedThreadID {
			matches = append(matches, item)
			continue
		}
		if expectedThreadID == "" && common.CandidateNamesMatch(expected.Name, item.Name) && strings.TrimSpace(expected.Summary) == strings.TrimSpace(item.Summary) {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return -1, fmt.Errorf("%s联系人列表里匹配到%d个目标会话，我不敢猜要回复哪一个", cfg.Name, len(matches))
	}
	return matches[0].Index, nil
}

// liepinConversations 把猎聘联系人字段转换为统一会话结构。
func liepinConversations(items []contract.FindAllItem, unreadOnly bool) ([]model.Conversation, error) {
	result := make([]model.Conversation, 0, len(items))
	for _, item := range items {
		meta, err := parseLiepinContactMeta(item.Fields["thread_meta"])
		if err != nil {
			return nil, fmt.Errorf("解析猎聘联系人会话编号失败：%w", err)
		}
		if unreadOnly {
			unreadCount, countErr := parseLiepinUnreadCount(item.Fields["unread_count"])
			if countErr != nil {
				return nil, fmt.Errorf("解析猎聘联系人未读数字失败：%w", countErr)
			}
			if unreadCount <= 0 {
				continue
			}
		}
		name := strings.TrimSpace(item.Fields["name"])
		lastMessage := strings.TrimSpace(item.Fields["last_message"])
		threadID := strings.TrimSpace(meta.ThreadID)
		key := threadID
		if key == "" {
			key = common.HashText("liepin|" + name + "|" + lastMessage)
		}
		result = append(result, model.Conversation{
			Index: item.Index, Key: key, Name: name, PlatformThreadID: threadID,
			Summary: lastMessage, Fields: item.Fields,
		})
	}
	return result, nil
}

type liepinContactMeta struct {
	ThreadID string `json:"to_imid"`
}

// parseLiepinUnreadCount 只接受联系人头像上真实可见的正整数未读数字。
func parseLiepinUnreadCount(value string) (int, error) {
	normalized := strings.TrimSuffix(strings.TrimSpace(value), "+")
	if normalized == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(normalized)
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("未读数字无法确认：%q", value)
	}
	return count, nil
}

// parseLiepinContactMeta 解码联系人 data-tlg-ext 中的稳定会话编号。
func parseLiepinContactMeta(value string) (liepinContactMeta, error) {
	decoded, err := url.QueryUnescape(strings.TrimSpace(value))
	if err != nil {
		return liepinContactMeta{}, err
	}
	if strings.TrimSpace(decoded) == "" {
		return liepinContactMeta{}, nil
	}
	var result liepinContactMeta
	if err = decodeLiepinJSON(decoded, &result); err != nil {
		return liepinContactMeta{}, err
	}
	return result, nil
}

// waitLiepinConversation 每 300 毫秒确认聊天框姓名，并读取页面显示的完整沟通岗位。
func waitLiepinConversation(ctx context.Context, browser model.Browser, cfg model.Config, expectedName string) (string, string, error) {
	for attempt := 1; attempt <= liepinConversationPollAttempts; attempt++ {
		name, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_name")
		if err != nil {
			return "", "", err
		}
		if found && common.CandidateNamesMatch(expectedName, name) {
			position, _, readErr := common.ReadOptional(ctx, browser, cfg, "message.current_position")
			return name, cleanLiepinPosition(position), readErr
		}
		if attempt < liepinConversationPollAttempts {
			if err = waitLiepinReplyPoll(ctx); err != nil {
				return "", "", err
			}
		}
	}
	return "", "", fmt.Errorf("%s聊天框候选人与待回复对象不一致，消息没有发送", cfg.Name)
}

// waitLiepinReplyPoll 等待猎聘聊天组件完成一次状态刷新。
func waitLiepinReplyPoll(ctx context.Context) error {
	timer := time.NewTimer(liepinConversationPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// cleanLiepinPosition 清理猎聘沟通岗位标签前缀和空白。
func cleanLiepinPosition(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "沟通职位：")
	return strings.Join(strings.Fields(value), " ")
}

// liepinPositionMatches 比较完整岗位和页面可能省略的岗位前缀。
func liepinPositionMatches(expected string, actual string) bool {
	expected = cleanLiepinPosition(expected)
	actual = cleanLiepinPosition(actual)
	return expected != "" && actual != "" && (expected == actual || strings.HasPrefix(expected, strings.TrimSuffix(actual, "...")))
}

// firstLiepinValue 返回第一段非空猎聘页面字段。
func firstLiepinValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
