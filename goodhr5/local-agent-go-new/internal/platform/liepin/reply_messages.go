// Package liepin 文件作用：解析猎聘聊天消息、稳定编号、真实滚轮历史和候选人页面身份。
package liepin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// readLiepinConversationHistory 用真实滚轮向上加载聊天，遇到已同步游标、历史顶部或5000条上限时停止。
func readLiepinConversationHistory(ctx context.Context, browser model.Browser, cfg model.Config, knownLastMessageKey string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	if maxHistory <= 0 || maxHistory > 5000 {
		maxHistory = 5000
	}
	previousSignature := ""
	for {
		items, err := common.ReadConfiguredConversationMessages(ctx, browser, cfg)
		if err != nil {
			return nil, false, err
		}
		messages, err := liepinMessages(items)
		if err != nil {
			return nil, false, err
		}
		if len(messages) == 0 {
			return nil, false, fmt.Errorf("%s当前聊天框没有读到消息", cfg.Name)
		}
		if knownLastMessageKey != "" && liepinMessageKeyExists(messages, knownLastMessageKey) {
			return messages, false, nil
		}
		if len(messages) >= maxHistory {
			return messages[len(messages)-maxHistory:], false, nil
		}
		signature := firstLiepinValue(messages[0].PlatformMessageID, messages[0].Key) + "|" + strconv.Itoa(len(messages))
		if signature == previousSignature {
			return messages, true, nil
		}
		previousSignature = signature
		result, err := common.ScrollConversationHistory(ctx, browser, cfg)
		if err != nil {
			return nil, false, fmt.Errorf("向上读取%s聊天历史失败：%w", cfg.Name, err)
		}
		if !result.Scrolled {
			return messages, true, nil
		}
	}
}

// liepinMessages 把页面消息列表转换为统一方向、类型和稳定指纹。
func liepinMessages(items []contract.FindAllItem) ([]model.ConversationMessage, error) {
	return liepinMessagesAt(items, time.Now())
}

// liepinMessagesAt 使用同一个基准时间解析整批消息，避免跨零点时同批指纹前后不一致。
func liepinMessagesAt(items []contract.FindAllItem, now time.Time) ([]model.ConversationMessage, error) {
	result := make([]model.ConversationMessage, 0, len(items))
	occurrences := make(map[string]int)
	for _, item := range items {
		message, err := liepinMessage(item, occurrences, now)
		if err != nil {
			return nil, fmt.Errorf("解析第%d条猎聘消息失败：%w", item.Index+1, err)
		}
		result = append(result, message)
	}
	return result, nil
}

// liepinMessage 解析一条猎聘消息的方向、卡片、时间和平台编号。
func liepinMessage(item contract.FindAllItem, occurrences map[string]int, now time.Time) (model.ConversationMessage, error) {
	bodyClass := strings.ToLower(strings.TrimSpace(item.Fields["body_class"]))
	direction := "system"
	switch {
	case strings.Contains(bodyClass, "message-item-receive"):
		direction = "candidate"
	case strings.Contains(bodyClass, "message-item-send"):
		direction = "self"
	case bodyClass != "":
		return model.ConversationMessage{}, fmt.Errorf("消息方向类名无法确认：%s", bodyClass)
	}
	text := strings.TrimSpace(item.Fields["message_text"])
	if text == "" {
		text = strings.TrimSpace(item.Text)
	}
	messageType := "text"
	resumeText := strings.TrimSpace(item.Fields["resume_card"])
	if resumeText != "" {
		messageType = "resume"
	} else if strings.TrimSpace(item.Fields["common_card"]) != "" {
		messageType = "card"
	} else if direction == "system" {
		messageType = "system"
	}
	platformMessageID, err := parseLiepinMessageID(item.Fields["message_meta"])
	if err != nil {
		return model.ConversationMessage{}, err
	}
	candidateID := parseLiepinCandidateID(item.Fields["candidate_meta"])
	timeText := strings.TrimSpace(item.Fields["message_time"])
	sentAt := parseLiepinMessageTime(timeText, now)
	timeKey := timeText
	if sentAt != nil {
		timeKey = sentAt.UTC().Format(time.RFC3339)
	}
	baseKey := strings.Join([]string{direction, messageType, text, timeKey}, "|")
	occurrence := occurrences[baseKey]
	occurrences[baseKey] = occurrence + 1
	key := platformMessageID
	if key == "" {
		key = common.HashText(baseKey + "|" + strconv.Itoa(occurrence))
	}
	card := json.RawMessage(`{}`)
	if messageType != "text" {
		encoded, marshalErr := json.Marshal(struct {
			Summary     string `json:"summary"`
			CandidateID string `json:"candidate_id,omitempty"`
		}{Summary: firstLiepinValue(resumeText, text), CandidateID: candidateID})
		if marshalErr != nil {
			return model.ConversationMessage{}, marshalErr
		}
		card = encoded
	}
	return model.ConversationMessage{
		Key: key, PlatformMessageID: platformMessageID, Direction: direction,
		MessageType: messageType, TextContent: text, CardContent: card, SentAt: sentAt,
	}, nil
}

// liepinResumeCard 返回会话里最后一张候选人附件简历卡片和来源消息编号。
func liepinResumeCard(messages []model.ConversationMessage) (bool, string) {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Direction == "candidate" && message.MessageType == "resume" {
			return true, firstLiepinValue(message.PlatformMessageID, message.Key)
		}
	}
	return false, ""
}

// liepinMessageKeyExists 判断已加载聊天中是否包含云端差量游标。
func liepinMessageKeyExists(messages []model.ConversationMessage, expected string) bool {
	expected = strings.TrimSpace(expected)
	for _, message := range messages {
		if expected == firstLiepinValue(message.PlatformMessageID, message.Key) {
			return true
		}
	}
	return false
}

// readLiepinGender 只在当前聊天框内识别男女图标，同时出现或都未出现时返回空值。
func readLiepinGender(ctx context.Context, browser model.Browser, cfg model.Config) (string, error) {
	female, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.gender_female")
	if err != nil {
		return "", err
	}
	male, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.gender_male")
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

// readLiepinCandidateID 优先读取聊天动作上的 cid，取不到时使用简历卡片里的 cid。
func readLiepinCandidateID(ctx context.Context, browser model.Browser, cfg model.Config, messages []model.ConversationMessage) (string, error) {
	selector, err := common.RequiredSelector(cfg, "message.candidate_id_source")
	if err != nil {
		return "", err
	}
	result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: selector, Attribute: "data-tlg-scm"})
	if err == nil {
		if candidateID := parseLiepinCandidateID(result.Value); candidateID != "" {
			return candidateID, nil
		}
	} else if !common.IsElementMissing(err) {
		return "", err
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

// parseLiepinMessageID 解码 data-tlg-ext 中可用的平台消息编号。
func parseLiepinMessageID(value string) (string, error) {
	decoded, err := url.QueryUnescape(strings.TrimSpace(value))
	if err != nil || decoded == "" {
		return "", err
	}
	var meta struct {
		MessageID string `json:"message_id"`
	}
	if err = decodeLiepinJSON(decoded, &meta); err != nil {
		return "", err
	}
	return strings.TrimSpace(meta.MessageID), nil
}

// parseLiepinCandidateID 从 data-tlg-scm 查询串读取 cid。
func parseLiepinCandidateID(value string) string {
	values, err := url.ParseQuery(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values.Get("cid"))
}

// parseLiepinMessageTime 把猎聘相对时间转换为本地时区的绝对时间，无法确认时返回空值。
func parseLiepinMessageTime(value string, now time.Time) *time.Time {
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
		yesterday := now.AddDate(0, 0, -1)
		parsed = time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
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

// decodeLiepinJSON 使用强类型目标解析猎聘属性中的 JSON。
func decodeLiepinJSON(value string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
