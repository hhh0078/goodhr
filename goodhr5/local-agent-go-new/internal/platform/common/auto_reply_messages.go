// Package common 文件作用：统一解析招聘平台聊天消息、差量历史和最新消息，平台只提供方向类名规则。
package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const autoReplyHistoryStableAttempts = 3

// AutoReplyMessageRules 定义平台消息方向类名的最小差异。
type AutoReplyMessageRules struct {
	CandidateTokens []string
	SelfTokens      []string
}

// configuredAutoReplyHistory 保存差量消息以及当前页面完整消息中发现的简历卡片。
type configuredAutoReplyHistory struct {
	Messages              []model.ConversationMessage
	HistoryComplete       bool
	ResumeCardAvailable   bool
	ResumeSourceMessageID string
}

// ReadConfiguredAutoReplyHistory 使用真实滚轮读取到云端已知消息、历史顶部或数量上限。
func ReadConfiguredAutoReplyHistory(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	result, err := readConfiguredAutoReplyHistory(ctx, browser, cfg, adapter, knownMessageKeys, maxHistory)
	return result.Messages, result.HistoryComplete, err
}

// readConfiguredAutoReplyHistory 同时保留完整已加载消息中的简历卡片，差量边界只裁剪待同步消息。
func readConfiguredAutoReplyHistory(ctx context.Context, browser model.Browser, cfg model.Config, adapter ConfiguredAutoReplyAdapter, knownMessageKeys []string, maxHistory int) (configuredAutoReplyHistory, error) {
	if maxHistory <= 0 || maxHistory > 5000 {
		maxHistory = 5000
	}
	previousSignature := ""
	stableAttempts := 0
	resumeAvailable := false
	resumeSourceID := ""
	for {
		items, err := ReadConfiguredConversationMessages(ctx, browser, cfg)
		if err != nil {
			return configuredAutoReplyHistory{}, err
		}
		messages, err := configuredAdapterMessages(items, adapter, time.Now())
		if err != nil {
			return configuredAutoReplyHistory{}, err
		}
		if len(messages) == 0 {
			return configuredAutoReplyHistory{}, fmt.Errorf("%s当前聊天框没有读到消息", cfg.Name)
		}
		if available, sourceID := ConfiguredAutoReplyResumeCard(messages); available {
			resumeAvailable = true
			resumeSourceID = sourceID
		}
		if boundary := configuredKnownMessageBoundary(messages, knownMessageKeys); boundary >= 0 {
			return configuredAutoReplyHistory{
				Messages: messages[boundary+1:], ResumeCardAvailable: resumeAvailable,
				ResumeSourceMessageID: resumeSourceID,
			}, nil
		}
		if len(messages) >= maxHistory {
			return configuredAutoReplyHistory{
				Messages: messages[len(messages)-maxHistory:], ResumeCardAvailable: resumeAvailable,
				ResumeSourceMessageID: resumeSourceID,
			}, nil
		}
		signature := firstNonEmpty(messages[0].PlatformMessageID, messages[0].Key) + "|" + strconv.Itoa(len(messages))
		if signature == previousSignature {
			stableAttempts++
			if stableAttempts >= autoReplyHistoryStableAttempts {
				return configuredAutoReplyHistory{
					Messages: messages, HistoryComplete: true, ResumeCardAvailable: resumeAvailable,
					ResumeSourceMessageID: resumeSourceID,
				}, nil
			}
		} else {
			previousSignature = signature
			stableAttempts = 0
		}
		if _, err = ScrollConversationHistory(ctx, browser, cfg); err != nil {
			return configuredAutoReplyHistory{}, fmt.Errorf("向上读取%s聊天历史失败：%w", cfg.Name, err)
		}
	}
}

// configuredAdapterMessages 优先使用平台解析器补充稳定消息编号和候选人编号。
func configuredAdapterMessages(items []contract.FindAllItem, adapter ConfiguredAutoReplyAdapter, now time.Time) ([]model.ConversationMessage, error) {
	if adapter.ParseMessages != nil {
		return adapter.ParseMessages(items, now)
	}
	return ParseConfiguredAutoReplyMessages(items, adapter.MessageRules, now)
}

// ParseConfiguredAutoReplyMessages 把配置读取的消息字段转换为统一方向、类型和稳定编号。
func ParseConfiguredAutoReplyMessages(items []contract.FindAllItem, rules AutoReplyMessageRules, now time.Time) ([]model.ConversationMessage, error) {
	result := make([]model.ConversationMessage, 0, len(items))
	for _, item := range items {
		message, baseKey, err := parseConfiguredAutoReplyMessage(item, rules, now)
		if err != nil {
			return nil, fmt.Errorf("解析第%d条聊天消息失败：%w", item.Index+1, err)
		}
		if message.PlatformMessageID == "" {
			message.Key = StableAutoReplyMessageKey(baseKey)
		}
		result = append(result, message)
	}
	return result, nil
}

// parseConfiguredAutoReplyMessage 解析单条消息的方向、卡片、时间和稳定指纹。
func parseConfiguredAutoReplyMessage(item contract.FindAllItem, rules AutoReplyMessageRules, now time.Time) (model.ConversationMessage, string, error) {
	direction := configuredMessageDirection(item.Fields, rules)
	text := strings.TrimSpace(firstNonEmpty(item.Fields["message_text"], item.Fields["text"], item.Text))
	messageType := strings.ToLower(strings.TrimSpace(item.Fields["message_type"]))
	resumeText := strings.TrimSpace(item.Fields["resume_card"])
	cardText := strings.TrimSpace(firstNonEmpty(item.Fields["common_card"], item.Fields["attachment_card"]))
	if messageType == "" {
		switch {
		case resumeText != "":
			messageType = "resume"
		case cardText != "":
			messageType = "card"
		case direction == "system":
			messageType = "system"
		default:
			messageType = "text"
		}
	}
	timeText := strings.TrimSpace(item.Fields["message_time"])
	sentAt := parseConfiguredMessageTime(timeText, now)
	timeKey := timeText
	if sentAt != nil {
		timeKey = sentAt.UTC().Format(time.RFC3339)
	}
	baseKey := strings.Join([]string{direction, messageType, text, timeKey}, "|")
	platformMessageID := strings.TrimSpace(firstNonEmpty(item.Fields["message_id"], item.Fields["id"]))
	card := json.RawMessage(`{}`)
	if messageType != "text" {
		encoded, err := json.Marshal(struct {
			Summary     string `json:"summary"`
			CandidateID string `json:"candidate_id,omitempty"`
		}{Summary: firstNonEmpty(resumeText, cardText, text), CandidateID: strings.TrimSpace(item.Fields["candidate_id"])})
		if err != nil {
			return model.ConversationMessage{}, "", err
		}
		card = encoded
	}
	return model.ConversationMessage{
		Key: platformMessageID, PlatformMessageID: platformMessageID, Direction: direction,
		MessageType: messageType, TextContent: text, CardContent: card, SentAt: sentAt,
	}, baseKey, nil
}

// StableAutoReplyMessageKey 为没有平台消息编号的消息生成上下文稳定指纹。
// 只使用消息自身标准内容，加载更早历史或收到更新消息都不会改动旧编号；平台无法区分的同文同时间消息按同一条幂等处理。
func StableAutoReplyMessageKey(baseKey string) string {
	return HashText(baseKey)
}

// configuredMessageDirection 根据显式方向字段、相对标记和平台类名判断消息方向。
func configuredMessageDirection(fields map[string]string, rules AutoReplyMessageRules) string {
	explicit := strings.ToLower(strings.TrimSpace(firstNonEmpty(fields["direction"], fields["sender_role"])))
	switch explicit {
	case "candidate", "receive", "received", "other", "left":
		return "candidate"
	case "self", "send", "sent", "mine", "right", "hr":
		return "self"
	case "system":
		return "system"
	}
	if strings.TrimSpace(fields["candidate_marker"]) != "" {
		return "candidate"
	}
	if strings.TrimSpace(fields["self_marker"]) != "" {
		return "self"
	}
	className := strings.ToLower(strings.TrimSpace(firstNonEmpty(fields["body_class"], fields["item_class"])))
	if containsConfiguredToken(className, rules.CandidateTokens) {
		return "candidate"
	}
	if containsConfiguredToken(className, rules.SelfTokens) {
		return "self"
	}
	return "system"
}

// containsConfiguredToken 判断页面类名是否包含任一非空方向标记。
func containsConfiguredToken(value string, tokens []string) bool {
	for _, token := range tokens {
		if token = strings.ToLower(strings.TrimSpace(token)); token != "" && strings.Contains(value, token) {
			return true
		}
	}
	return false
}

// ConfiguredAutoReplyResumeCard 返回最后一张候选人简历卡片和来源消息编号。
func ConfiguredAutoReplyResumeCard(messages []model.ConversationMessage) (bool, string) {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Direction == "candidate" && message.MessageType == "resume" {
			return true, firstNonEmpty(message.PlatformMessageID, message.Key)
		}
	}
	return false, ""
}

// configuredKnownMessageBoundary 从页面末尾寻找最靠后的云端已知消息边界。
func configuredKnownMessageBoundary(messages []model.ConversationMessage, knownMessageKeys []string) int {
	known := make(map[string]struct{}, len(knownMessageKeys))
	for _, key := range knownMessageKeys {
		if key = strings.TrimSpace(key); key != "" {
			known[key] = struct{}{}
		}
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if _, ok := known[firstNonEmpty(messages[index].PlatformMessageID, messages[index].Key)]; ok {
			return index
		}
	}
	return -1
}

// parseConfiguredMessageTime 解析常见的当天、昨天和月日聊天时间。
func parseConfiguredMessageTime(value string, now time.Time) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.Local
	}
	now = now.In(location)
	var parsed time.Time
	switch {
	case strings.HasPrefix(value, "昨天 "):
		clock, parseErr := time.ParseInLocation("15:04", strings.TrimPrefix(value, "昨天 "), location)
		if parseErr != nil {
			return nil
		}
		day := now.AddDate(0, 0, -1)
		parsed = time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	case strings.Contains(value, "月"):
		clock, parseErr := time.ParseInLocation("1月2日 15:04", value, location)
		if parseErr != nil {
			return nil
		}
		parsed = time.Date(now.Year(), clock.Month(), clock.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
		if parsed.After(now.Add(24 * time.Hour)) {
			parsed = parsed.AddDate(-1, 0, 0)
		}
	case len(value) == 5 && strings.Contains(value, ":"):
		clock, parseErr := time.ParseInLocation("15:04", value, location)
		if parseErr != nil {
			return nil
		}
		parsed = time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	default:
		return nil
	}
	utc := parsed.UTC()
	return &utc
}
