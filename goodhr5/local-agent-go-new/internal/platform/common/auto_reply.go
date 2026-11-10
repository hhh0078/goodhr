// Package common 文件作用：提供配置驱动的自动回复会话打开、身份校验、消息发送和关闭能力。
package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ConfiguredAutoReplyAdapter 保存平台消息方向和岗位文本的最小差异。
type ConfiguredAutoReplyAdapter struct {
	PlatformID         string
	MessageRules       AutoReplyMessageRules
	ParseConversations func([]contract.FindAllItem, bool) ([]model.Conversation, error)
	ParseMessages      func([]contract.FindAllItem, time.Time) ([]model.ConversationMessage, error)
	ParseCandidateID   func(string) string
	CleanPosition      func(string) string
	PositionMatches    func(string, string) bool
}

// InitializeConfiguredAutoReplyPage 清理遗留附件、聊天框和联系人列表。
func InitializeConfiguredAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if err := CloseOptionalPanel(ctx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", cfg.Name+"附件预览"); err != nil {
		return err
	}
	return CloseCandidatePanels(ctx, browser, cfg)
}

// ScanConfiguredAutoReplyConversations 打开联系人列表并只返回当前真实未读会话。
func ScanConfiguredAutoReplyConversations(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter) ([]model.Conversation, error) {
	ready, unreadTotal, err := EnsureUnreadConversationDrawer(ctx, browser, cfg)
	if err != nil || !ready {
		return nil, err
	}
	items, err := FindConfiguredConversationItems(ctx, browser, cfg, "message.unread_item", true)
	if err != nil {
		return nil, err
	}
	conversations, err := configuredAdapterConversations(items, adapter, true)
	if err != nil {
		return nil, err
	}
	if unreadTotal > 0 && len(conversations) > unreadTotal {
		conversations = conversations[:unreadTotal]
	}
	return conversations, nil
}

// ConfiguredConversations 把联系人列表字段整理为统一会话结构。
func ConfiguredConversations(items []contract.FindAllItem, platformID string, unreadOnly bool) ([]model.Conversation, error) {
	result := make([]model.Conversation, 0, len(items))
	for _, item := range items {
		unreadText := strings.TrimSuffix(strings.TrimSpace(item.Fields["unread_count"]), "+")
		if unreadOnly {
			if unreadText == "" {
				continue
			}
			count, err := strconv.Atoi(unreadText)
			if err != nil {
				return nil, fmt.Errorf("联系人未读数字无法确认：%q", item.Fields["unread_count"])
			}
			if count <= 0 {
				continue
			}
		}
		name := strings.TrimSpace(firstNonEmpty(item.Fields["name"], item.Text))
		lastMessage := strings.TrimSpace(firstNonEmpty(item.Fields["last_message"], item.Fields["summary"], item.Text))
		threadID := strings.TrimSpace(firstNonEmpty(item.Fields["thread_id"], item.Fields["id"], item.Fields["thread_meta"]))
		key := threadID
		if key == "" {
			var err error
			key, err = LocalConversationKey(platformID, name, item.Fields["avatar_url"])
			if err != nil {
				return nil, err
			}
		}
		result = append(result, model.Conversation{
			Index: item.Index, Key: key, Name: name, AvatarURL: strings.TrimSpace(item.Fields["avatar_url"]),
			Gender: strings.TrimSpace(item.Fields["gender"]), PlatformThreadID: threadID,
			PlatformCandidateID:   strings.TrimSpace(item.Fields["candidate_id"]),
			PlatformAccountID:     strings.TrimSpace(item.Fields["account_id"]),
			CommunicationPosition: strings.TrimSpace(item.Fields["position_name"]),
			Summary:               lastMessage, Fields: item.Fields,
		})
	}
	return result, nil
}

// LocalConversationKey 在平台没有原生会话编号时按平台、姓名和稳定头像路径生成本地键。
// 查询参数、最后一条消息和列表序号都不能参与，避免签名刷新或新消息把同一会话拆成两条。
func LocalConversationKey(platformID string, name string, avatarURL string) (string, error) {
	normalizedName := normalizeCandidateName(name)
	if normalizedName == "" {
		return "", fmt.Errorf("%s联系人缺少候选人姓名，无法生成稳定会话键", platformID)
	}
	identity := strings.ToLower(strings.TrimSpace(platformID)) + "|" + normalizedName
	if avatarIdentity := normalizedConversationAvatarIdentity(avatarURL); avatarIdentity != "" {
		identity += "|" + avatarIdentity
	}
	return "local:" + HashText(identity), nil
}

// normalizedConversationAvatarIdentity 清除头像地址中会变化的签名参数，只保留稳定来源和路径。
func normalizedConversationAvatarIdentity(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimSpace(parsed.String())
}

// configuredAdapterConversations 优先使用平台解析器处理稳定会话编号。
func configuredAdapterConversations(items []contract.FindAllItem, adapter ConfiguredAutoReplyAdapter, unreadOnly bool) ([]model.Conversation, error) {
	if adapter.ParseConversations != nil {
		return adapter.ParseConversations(items, unreadOnly)
	}
	return ConfiguredConversations(items, adapter.PlatformID, unreadOnly)
}

// OpenConfiguredAutoReplyConversation 稳定定位会话并读取身份、岗位、差量消息和简历卡片。
func OpenConfiguredAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, conversation model.Conversation, knownMessageKeys []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	ready, _, err := EnsureUnreadConversationDrawer(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if !ready {
		return model.AutoReplyConversationSnapshot{}, fmt.Errorf("%s未读联系人列表没有打开", cfg.Name)
	}
	index, err := locateConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, conversation)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if err = OpenConfiguredConversationItem(ctx, browser, cfg, index); err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	name, position, err := waitConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, conversation.Name, conversation.CommunicationPosition)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	history, err := readConfiguredAutoReplyHistory(ctx, browser, cfg, adapter, knownMessageKeys, maxHistory)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	messages := history.Messages
	gender, err := ReadConfiguredAutoReplyGender(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	candidateID, err := readConfiguredAutoReplyCandidateID(ctx, browser, cfg, adapter, messages)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	return model.AutoReplyConversationSnapshot{
		Conversation: conversation, CandidateName: name, AvatarURL: conversation.AvatarURL,
		Gender:                firstNonEmpty(gender, conversation.Gender),
		PlatformThreadID:      conversation.PlatformThreadID,
		PlatformCandidateID:   firstNonEmpty(candidateID, conversation.PlatformCandidateID),
		PlatformAccountID:     conversation.PlatformAccountID,
		CommunicationPosition: position, Messages: messages, HistoryComplete: history.HistoryComplete,
		ResumeCardAvailable: history.ResumeCardAvailable, ResumeSourceMessageID: history.ResumeSourceMessageID,
	}, nil
}

// ReadConfiguredAutoReplyMessages 读取当前聊天框的差量消息，不重复打开联系人列表。
func ReadConfiguredAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	if err := EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot); err != nil {
		return nil, false, err
	}
	return ReadConfiguredAutoReplyHistory(ctx, browser, cfg, adapter, knownMessageKeys, maxHistory)
}

// SendConfiguredAutoReplyMessage 再次核对候选人与岗位后发送消息。
func SendConfiguredAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot, message string) error {
	if err := EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot); err != nil {
		return err
	}
	actual, found, err := ReadOptional(ctx, browser, cfg, "message.current_position")
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(snapshot.CommunicationPosition); expected != "" {
		matches := configuredPositionMatches(adapter, expected, actual)
		if !found || !matches {
			return fmt.Errorf("%s当前聊天框的沟通岗位已经变化，消息没有发送", cfg.Name)
		}
	}
	return SendAutoReplyText(ctx, browser, cfg, message)
}

// ReadLatestConfiguredAutoReplyMessage 返回当前聊天框最后一条候选人或 HR 消息。
func ReadLatestConfiguredAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if err := EnsureConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot); err != nil {
		return model.ConversationMessage{}, err
	}
	return readLatestConfiguredAutoReplyMessage(ctx, browser, cfg, adapter)
}

// ReadOpenLatestConfiguredAutoReplyMessage 仅在目标聊天框仍打开时读取最新消息。
func ReadOpenLatestConfiguredAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	matched, err := configuredAutoReplyConversationMatches(ctx, browser, cfg, adapter, snapshot)
	if err != nil || !matched {
		return model.ConversationMessage{}, false, err
	}
	latest, err := readLatestConfiguredAutoReplyMessage(ctx, browser, cfg, adapter)
	return latest, true, err
}

// readLatestConfiguredAutoReplyMessage 读取已经加载消息中的最后一条双方消息。
func readLatestConfiguredAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter) (model.ConversationMessage, error) {
	items, err := ReadConfiguredConversationMessages(ctx, browser, cfg)
	if err != nil {
		return model.ConversationMessage{}, err
	}
	messages, err := configuredAdapterMessages(items, adapter, timeNow())
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

// EnsureConfiguredAutoReplyConversation 复用当前会话或从全部联系人中恢复目标候选人。
func EnsureConfiguredAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot) error {
	matched, err := configuredAutoReplyConversationMatches(ctx, browser, cfg, adapter, snapshot)
	if err != nil {
		return err
	}
	if matched {
		return nil
	}
	if err = ensureConfiguredConversationDrawer(ctx, browser, cfg); err != nil {
		return err
	}
	expected := snapshot.Conversation
	expected.Name = firstNonEmpty(expected.Name, snapshot.CandidateName)
	expected.PlatformThreadID = firstNonEmpty(expected.PlatformThreadID, snapshot.PlatformThreadID)
	expected.Key = firstNonEmpty(expected.Key, expected.PlatformThreadID)
	index, err := locateConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, expected)
	if err != nil {
		return err
	}
	if err = OpenConfiguredConversationItem(ctx, browser, cfg, index); err != nil {
		return err
	}
	_, _, err = waitConfiguredAutoReplyConversation(ctx, browser, cfg, adapter, snapshot.CandidateName, snapshot.CommunicationPosition)
	return err
}

// configuredAutoReplyConversationMatches 同时核对当前聊天框候选人和沟通岗位。
func configuredAutoReplyConversationMatches(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, snapshot model.AutoReplyConversationSnapshot) (bool, error) {
	name, found, err := ReadOptional(ctx, browser, cfg, "message.current_name")
	if err != nil || !found || !CandidateNamesMatch(snapshot.CandidateName, name) {
		return false, err
	}
	expectedPosition := strings.TrimSpace(snapshot.CommunicationPosition)
	if expectedPosition == "" {
		return true, nil
	}
	position, found, err := ReadOptional(ctx, browser, cfg, "message.current_position")
	if err != nil || !found {
		return false, err
	}
	return configuredPositionMatches(adapter, expectedPosition, position), nil
}

// CloseConfiguredAutoReplyConversation 关闭附件、聊天框和联系人抽屉。
func CloseConfiguredAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return CloseAutoReplyPanels(ctx, browser, cfg)
}

// ReadConfiguredAutoReplyGender 根据当前聊天框内互斥的男女标记读取性别。
func ReadConfiguredAutoReplyGender(ctx context.Context, browser model.Browser, cfg model.Config) (string, error) {
	female, err := ProbeSelectorExists(ctx, browser, cfg, "message.gender_female")
	if err != nil {
		return "", err
	}
	male, err := ProbeSelectorExists(ctx, browser, cfg, "message.gender_male")
	if err != nil {
		return "", err
	}
	if female == male {
		return "", nil
	}
	if female {
		return "女", nil
	}
	return "男", nil
}

// readConfiguredAutoReplyCandidateID 优先读取当前聊天动作编号，再从简历卡片中安全后备。
func readConfiguredAutoReplyCandidateID(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, messages []model.ConversationMessage) (string, error) {
	if _, configured := cfg.Selectors["message.candidate_id_source"]; configured {
		value, found, err := ReadOptional(ctx, browser, cfg, "message.candidate_id_source")
		if err != nil {
			return "", err
		}
		if found {
			candidateID := strings.TrimSpace(value)
			if adapter.ParseCandidateID != nil {
				candidateID = strings.TrimSpace(adapter.ParseCandidateID(value))
			}
			if candidateID != "" {
				return candidateID, nil
			}
		}
	}
	for index := len(messages) - 1; index >= 0; index-- {
		var card struct {
			CandidateID string `json:"candidate_id"`
		}
		if json.Unmarshal(messages[index].CardContent, &card) == nil && strings.TrimSpace(card.CandidateID) != "" {
			return strings.TrimSpace(card.CandidateID), nil
		}
	}
	return "", nil
}

// locateConfiguredAutoReplyConversation 每次点击前重新扫描并唯一定位目标会话。
func locateConfiguredAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, expected model.Conversation) (int, error) {
	items, err := FindConfiguredConversationItems(ctx, browser, cfg, "message.contact_item", false)
	if err != nil {
		return -1, err
	}
	conversations, err := configuredAdapterConversations(items, adapter, false)
	if err != nil {
		return -1, err
	}
	matches := make([]model.Conversation, 0, 1)
	for _, item := range conversations {
		if expected.PlatformThreadID != "" && item.PlatformThreadID == expected.PlatformThreadID {
			matches = append(matches, item)
			continue
		}
		if expected.PlatformThreadID == "" && strings.HasPrefix(expected.Key, "local:") && item.Key == expected.Key {
			matches = append(matches, item)
			continue
		}
		if expected.PlatformThreadID == "" && expected.Key == "" && CandidateNamesMatch(expected.Name, item.Name) {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return -1, fmt.Errorf("%s联系人列表里匹配到%d个目标会话，我不敢猜要回复哪一个", cfg.Name, len(matches))
	}
	return matches[0].Index, nil
}

// ensureConfiguredConversationDrawer 不依赖未读数字打开联系人列表，供恢复已读会话使用。
func ensureConfiguredConversationDrawer(ctx context.Context, browser model.Browser, cfg model.Config) error {
	opened, err := ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
	if err != nil {
		return err
	}
	if !opened {
		if err = ClickRequired(ctx, browser, cfg, "message.entry"); err != nil {
			return fmt.Errorf("重新打开%s联系人列表失败：%w", cfg.Name, err)
		}
		for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
			opened, err = ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
			if err != nil {
				return err
			}
			if opened {
				break
			}
			if attempt < candidateConversationPollAttempts {
				if err = waitConversationPoll(ctx); err != nil {
					return err
				}
			}
		}
		if !opened {
			return fmt.Errorf("%s联系人列表没有重新打开", cfg.Name)
		}
	}
	return ensureConfiguredAllConversationTab(ctx, browser, cfg)
}

// ensureConfiguredAllConversationTab 在恢复已读会话前切到全部联系人，避免目标从未读列表消失。
func ensureConfiguredAllConversationTab(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if _, configured := cfg.Selectors["message.all_tab"]; !configured {
		return nil
	}
	selected, err := ProbeSelectorExists(ctx, browser, cfg, "message.all_tab_selected")
	if err != nil || selected {
		return err
	}
	if err = ClickRequired(ctx, browser, cfg, "message.all_tab"); err != nil {
		return fmt.Errorf("切换%s全部联系人失败：%w", cfg.Name, err)
	}
	for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
		selected, err = ProbeSelectorExists(ctx, browser, cfg, "message.all_tab_selected")
		if err != nil || selected {
			return err
		}
		if attempt < candidateConversationPollAttempts {
			if err = waitConversationPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("%s联系人列表没有切到全部标签", cfg.Name)
}

// waitConfiguredAutoReplyConversation 每 300 毫秒核对候选人姓名并读取沟通岗位。
func waitConfiguredAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, expectedName string, expectedPosition string) (string, string, error) {
	for attempt := 1; attempt <= candidateConversationPollAttempts; attempt++ {
		name, found, err := ReadOptional(ctx, browser, cfg, "message.current_name")
		if err != nil {
			return "", "", err
		}
		if found && CandidateNamesMatch(expectedName, name) {
			position, _, readErr := ReadOptional(ctx, browser, cfg, "message.current_position")
			position = cleanConfiguredPosition(adapter, position)
			if readErr != nil {
				return "", "", readErr
			}
			if strings.TrimSpace(expectedPosition) == "" || configuredPositionMatches(adapter, expectedPosition, position) {
				return name, position, nil
			}
		}
		if attempt < candidateConversationPollAttempts {
			if err = waitConversationPoll(ctx); err != nil {
				return "", "", err
			}
		}
	}
	return "", "", fmt.Errorf("%s聊天框候选人与待回复对象不一致，消息没有发送", cfg.Name)
}

// cleanConfiguredPosition 清理平台岗位文字，未提供平台规则时只压缩空白。
func cleanConfiguredPosition(adapter ConfiguredAutoReplyAdapter, value string) string {
	if adapter.CleanPosition != nil {
		return adapter.CleanPosition(value)
	}
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// configuredPositionMatches 比较完整岗位和页面可能省略的岗位前缀。
func configuredPositionMatches(adapter ConfiguredAutoReplyAdapter, expected string, actual string) bool {
	if adapter.PositionMatches != nil {
		return adapter.PositionMatches(expected, actual)
	}
	expected = cleanConfiguredPosition(adapter, expected)
	actual = cleanConfiguredPosition(adapter, actual)
	if expected == "" || actual == "" {
		return false
	}
	if expected == actual {
		return true
	}
	actualPrefix, actualTruncated := trimConfiguredPositionEllipsis(actual)
	expectedPrefix, expectedTruncated := trimConfiguredPositionEllipsis(expected)
	return (actualTruncated && strings.HasPrefix(expected, actualPrefix)) ||
		(expectedTruncated && strings.HasPrefix(actual, expectedPrefix))
}

// trimConfiguredPositionEllipsis 返回平台省略岗位文字的可比较前缀。
func trimConfiguredPositionEllipsis(value string) (string, bool) {
	for _, suffix := range []string{"...", "…"} {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix), true
		}
	}
	return value, false
}

// timeNow 隔离当前时间，保持消息解析调用点简洁。
func timeNow() time.Time {
	return time.Now()
}
