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

const liepinHistoryStableReadAttempts = 3

// 距离交给跨平台标准真实滚轮执行，不在Go中猜测系统缩放倍率；每轮仍按实际首尾和几何位置确认进度。
const liepinHistoryWheelDistance = 1600
const liepinLatestWheelDistance = 2400

// liepinConversationHistory 保存猎聘差量消息以及当前完整页面中发现的简历卡片。
type liepinConversationHistory struct {
	Messages              []model.ConversationMessage
	HistoryComplete       bool
	ResumeCardAvailable   bool
	ResumeSourceMessageID string
}

// readLiepinConversationHistory 用真实滚轮向上加载聊天，遇到最近两条已同步消息、历史顶部或5000条上限时停止。
func readLiepinConversationHistory(ctx context.Context, browser model.Browser, cfg model.Config, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	result, err := readLiepinConversationHistoryDetail(ctx, browser, cfg, knownMessageKeys, maxHistory)
	return result.Messages, result.HistoryComplete, err
}

// readLiepinConversationHistoryDetail 加载时只读首尾稳定编号；到达已知边界、真实顶部或上限后才读取完整消息字段。
func readLiepinConversationHistoryDetail(ctx context.Context, browser model.Browser, cfg model.Config, knownMessageKeys []string, maxHistory int) (result liepinConversationHistory, err error) {
	return readLiepinHistoryWindow(ctx, browser, cfg, knownMessageKeys, maxHistory, nil)
}

// readLiepinHistoryWindow 到达已知消息或本代删除时间即停止翻页，始终回到最新端供发送前核实。
func readLiepinHistoryWindow(ctx context.Context, browser model.Browser, cfg model.Config, knownMessageKeys []string, maxHistory int, after *time.Time) (result liepinConversationHistory, err error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if maxHistory <= 0 || maxHistory > 5000 {
		maxHistory = 5000
	}
	defer func() {
		if err == nil {
			err = ctx.Err()
			if err == nil {
				err = restoreLiepinLatestMessages(ctx, browser, cfg)
			}
		}
	}()
	previousSignature, checkedSignature := "", ""
	stableReadAttempts, blockedAttempts := 0, 0
	var progress contract.ScrollResult
	var cachedItems []contract.FindAllItem
	var cachedResult liepinConversationHistory
	for round := 0; round < 120; round++ {
		if err = ctx.Err(); err != nil {
			return liepinConversationHistory{}, err
		}
		head, tail, signature, readErr := readLiepinHistoryEdges(ctx, browser, cfg)
		if readErr != nil {
			return liepinConversationHistory{}, readErr
		}
		if signature != checkedSignature {
			cachedItems, cachedResult = nil, liepinConversationHistory{}
			checkedSignature = signature
		}
		if round > 0 {
			if progress.ProgressTargetMoved == nil || progress.ProgressTargetVisible == nil {
				return liepinConversationHistory{}, fmt.Errorf("浏览器工具没有返回聊天顶部位置，请更新本地程序后重试")
			}
			if !*progress.ProgressTargetMoved && signature == previousSignature {
				if *progress.ProgressTargetVisible {
					stableReadAttempts++
					blockedAttempts = 0
				} else {
					stableReadAttempts = 0
					blockedAttempts++
				}
			} else {
				stableReadAttempts, blockedAttempts = 0, 0
			}
			if blockedAttempts >= liepinHistoryStableReadAttempts {
				return liepinConversationHistory{}, fmt.Errorf("聊天历史还没到顶部，滚轮却没有继续移动，我先不把它当成读完")
			}
		}
		atTop := stableReadAttempts >= liepinHistoryStableReadAttempts
		atLimit := tail.TotalMatches >= maxHistory
		// 差量边界可能在中间；同一首尾和数量只完整核对一次，纯滚动不重复读取全部字段。
		if atTop || atLimit || (cachedItems == nil && (after != nil || len(knownMessageKeys) > 0)) {
			if after == nil {
				cachedItems, cachedResult, err = readLiepinFullHistory(ctx, browser, cfg)
			} else {
				cachedItems, cachedResult, err = readLiepinRecontactTail(ctx, browser, cfg, *after, knownMessageKeys, maxHistory)
			}
			if err != nil {
				return liepinConversationHistory{}, err
			}
			fullTail := cachedItems[len(cachedItems)-1]
			if fullTail.TotalMatches != tail.TotalMatches || liepinHistoryItemKey(fullTail) != liepinHistoryItemKey(tail) || (!atLimit && (after == nil || cachedItems[0].Index == head.Index) && liepinHistoryItemKey(cachedItems[0]) != liepinHistoryItemKey(head)) {
				// 读取正文期间新增或替换了消息，重新确认首尾后再决定边界。
				previousSignature, checkedSignature = "", ""
				stableReadAttempts, blockedAttempts = 0, 0
				continue
			}
		}
		if cachedItems != nil {
			result = cachedResult
			if after != nil {
				filtered, reached := liepinMessagesAfterDeletion(result.Messages, *after)
				// 日期边界在当前窗口时，整段新消息的首分钟也已覆盖，可区分同文重复。
				if reached || atTop {
					result.Messages = filtered
					result.ResumeCardAvailable, result.ResumeSourceMessageID = liepinResumeCard(filtered)
					result.HistoryComplete = true
					if b := liepinKnownMessageBoundary(filtered, knownMessageKeys); b >= 0 {
						result.HistoryComplete = false
						result.Messages = filtered[liepinDuplicateRepairStart(filtered, b, 0):]
					}
					return result, nil
				}
				// 尚未看见日期边界时，只可用已同步的稳定消息来截取差量。
				if atLimit && liepinKnownMessageBoundary(result.Messages, knownMessageKeys) < 0 {
					return liepinConversationHistory{}, fmt.Errorf("新消息超过可核对的窗口，还不能确认从哪里重新开始")
				}
			}
			if boundary := liepinKnownMessageBoundary(result.Messages, knownMessageKeys); boundary >= 0 && (atTop || result.Messages[boundary].PlatformMessageID != "" || boundary >= liepinLeadingMessageGroupEnd(result.Messages)) {
				firstStable := 0
				if !atTop {
					firstStable = liepinLeadingMessageGroupEnd(result.Messages)
				}
				// 旧版本曾合并同分钟同文消息；带上可证明完整的重复组及后续锚点，让本机幂等补齐历史而不新增待答。
				start := liepinDuplicateRepairStart(result.Messages, boundary, firstStable)
				result.Messages = result.Messages[start:]
				if after != nil {
					result.Messages, _ = liepinMessagesAfterDeletion(result.Messages, *after)
					result.ResumeCardAvailable, result.ResumeSourceMessageID = liepinResumeCard(result.Messages)
				}
				result.Messages = result.Messages[max(0, len(result.Messages)-maxHistory):]
				return result, nil
			}
			if atLimit || len(result.Messages) >= maxHistory || cachedItems[0].TotalMatches > len(cachedItems) {
				if after != nil {
					return liepinConversationHistory{}, fmt.Errorf("重新建档的消息起点仍未确认，请等下一次核对")
				}
				if !atTop || cachedItems[0].Index > 0 {
					// 截尾窗口最早一分钟可能还藏着同文消息，不能猜出现次数或把它用作差量边界。
					end := liepinLeadingMessageGroupEnd(result.Messages)
					anchored := make([]model.ConversationMessage, 0, len(result.Messages))
					for index, message := range result.Messages {
						if index >= end || message.PlatformMessageID != "" {
							anchored = append(anchored, message)
						}
					}
					result.Messages = anchored
					if len(result.Messages) == 0 {
						return liepinConversationHistory{}, fmt.Errorf("聊天截尾范围内还没有可核实的时间边界，暂不猜测重复消息编号")
					}
				}
				result.Messages = result.Messages[max(0, len(result.Messages)-maxHistory):]
				return result, nil
			}
			if atTop {
				result.HistoryComplete = true
				return result, nil
			}
		}
		previousSignature = signature
		headSelector, selectorErr := common.IndexedSelector(cfg, "message.item", head.Index)
		if selectorErr != nil {
			return liepinConversationHistory{}, selectorErr
		}
		anchor, selectorErr := common.RequiredSelector(cfg, "message.history_scroll")
		if selectorErr != nil {
			return liepinConversationHistory{}, selectorErr
		}
		waitMS := 300
		if progress.ProgressTargetVisible != nil && *progress.ProgressTargetVisible {
			// 首条已经露出时留足异步追加历史的时间；最终仍需连续三次无位移且数量、首尾不变。
			waitMS = 800
		}
		progress, err = browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &anchor, ProgressTarget: &headSelector, Distance: -liepinHistoryWheelDistance, MaxAttempts: 1, WaitMS: waitMS})
		if err != nil {
			return liepinConversationHistory{}, fmt.Errorf("向上读取%s聊天历史失败：%w", cfg.Name, err)
		}
	}
	return liepinConversationHistory{}, fmt.Errorf("聊天历史较长，这轮还没确认读完，请稍后重试")
}

// readLiepinHistoryEdges 通过标准首尾窗口只读消息编号和真实数量；不让每次滚动重读全部消息内容。
func readLiepinHistoryEdges(ctx context.Context, browser model.Browser, cfg model.Config) (head, tail contract.FindAllItem, signature string, err error) {
	selector, err := common.RequiredSelector(cfg, "message.item")
	if err != nil {
		return head, tail, "", err
	}
	fields := map[string]contract.SelectorSpec{}
	if metadata, found := cfg.ConversationFields["message_meta"]; found {
		fields["message_meta"] = metadata
	}
	for attempt := 0; attempt < 3; attempt++ {
		first, readErr := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, Fields: fields})
		if readErr != nil {
			return head, tail, "", readErr
		}
		last, readErr := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, FromEnd: true, Fields: fields})
		if readErr != nil {
			return head, tail, "", readErr
		}
		if len(first) != 1 || len(last) != 1 {
			return head, tail, "", fmt.Errorf("%s当前聊天框没有读到消息", cfg.Name)
		}
		head, tail = first[0], last[0]
		if head.TotalMatches != tail.TotalMatches {
			continue
		}
		return head, tail, liepinHistoryItemKey(head) + "|" + liepinHistoryItemKey(tail) + "|" + strconv.Itoa(tail.TotalMatches), nil
	}
	return head, tail, "", fmt.Errorf("聊天历史仍在加载，暂时不能确认首尾消息")
}

// liepinHistoryItemKey 优先使用平台编号，旧配置缺编号字段时以节点文字生成只用于加载进度的稳定键。
func liepinHistoryItemKey(item contract.FindAllItem) string {
	if id, err := parseLiepinMessageID(item.Fields["message_meta"]); err == nil && id != "" {
		return id
	}
	return common.HashText(firstLiepinValue(item.Text, item.Fields["message_text"], item.Fields["resume_card"], item.Fields["common_card"]))
}

// readLiepinFullHistory 在加载完成或需要核对差量边界时读取一次完整消息字段，并识别简历卡片。
func readLiepinFullHistory(ctx context.Context, browser model.Browser, cfg model.Config) ([]contract.FindAllItem, liepinConversationHistory, error) {
	items, err := common.ReadConfiguredConversationMessages(ctx, browser, cfg)
	if err != nil {
		return nil, liepinConversationHistory{}, err
	}
	messages, err := liepinMessages(items)
	if err != nil {
		return nil, liepinConversationHistory{}, err
	}
	if len(messages) == 0 {
		return nil, liepinConversationHistory{}, fmt.Errorf("%s当前聊天框没有读到消息", cfg.Name)
	}
	available, sourceID := liepinResumeCard(messages)
	return items, liepinConversationHistory{Messages: messages, ResumeCardAvailable: available, ResumeSourceMessageID: sourceID}, nil
}

// restoreLiepinLatestMessages 通过末条节点和真实滚轮恢复最新端，已可见时不滚动，不靠页面脚本。
func restoreLiepinLatestMessages(ctx context.Context, browser model.Browser, cfg model.Config) error {
	selector, err := common.RequiredSelector(cfg, "message.item")
	if err != nil {
		return err
	}
	anchor, err := common.RequiredSelector(cfg, "message.history_scroll")
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, readErr := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, FromEnd: true, ProbeOnly: true})
		if readErr != nil {
			return readErr
		}
		if len(items) != 1 {
			return fmt.Errorf("聊天末条消息没找到，暂时不能确认已经回到最新消息")
		}
		tail, selectorErr := common.IndexedSelector(cfg, "message.item", items[0].Index)
		if selectorErr != nil {
			return selectorErr
		}
		progress, scrollErr := browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &anchor, ProgressTarget: &tail, Distance: liepinLatestWheelDistance, MaxAttempts: 100, WaitMS: 150, StopWhenProgressVisible: true})
		if scrollErr != nil {
			return fmt.Errorf("回到最新聊天消息失败：%w", scrollErr)
		}
		if progress.ProgressTargetVisible == nil || !*progress.ProgressTargetVisible {
			return fmt.Errorf("还没看到聊天末条消息，这轮先不发送")
		}
		latest, readErr := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, FromEnd: true, ProbeOnly: true})
		if readErr != nil {
			return readErr
		}
		if len(latest) == 1 && latest[0].Index == items[0].Index && latest[0].TotalMatches == items[0].TotalMatches {
			return nil
		}
	}
	return fmt.Errorf("聊天列表还在变化，暂时没有确认最新端，请稍后重试")
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
		message, baseKey, err := liepinMessage(item, now)
		if err != nil {
			return nil, fmt.Errorf("解析第%d条猎聘消息失败：%w", item.Index+1, err)
		}
		if message.PlatformMessageID == "" {
			occurrences[baseKey]++
			if occurrences[baseKey] > 1 {
				baseKey += fmt.Sprintf("|occurrence:%d", occurrences[baseKey])
			}
			message.Key = common.StableAutoReplyMessageKey(baseKey)
		}
		result = append(result, message)
	}
	return result, nil
}

// liepinLeadingMessageGroupEnd 找到首组分钟消息的结束位置；首组来源不全时，不能确定同分钟重复项的次序。
func liepinLeadingMessageGroupEnd(messages []model.ConversationMessage) int {
	if len(messages) == 0 {
		return 0
	}
	first := messages[0].SentAt
	if first == nil {
		return len(messages)
	}
	for index, message := range messages {
		if message.SentAt != nil && !message.SentAt.Equal(*first) {
			return index
		}
	}
	return len(messages)
}

// liepinDuplicateRepairStart 为已知边界前的重复组保留原始顺序锚点；未完整首分钟不参与旧记录修复。
func liepinDuplicateRepairStart(messages []model.ConversationMessage, boundary, firstStable int) int {
	start := boundary + 1
	firstOccurrences := make(map[string]int)
	for index := firstStable; index <= boundary; index++ {
		message := messages[index]
		if message.PlatformMessageID != "" {
			continue
		}
		minute := ""
		if message.SentAt != nil {
			minute = message.SentAt.UTC().Format(time.RFC3339)
		}
		key := strings.Join([]string{message.Direction, message.MessageType, message.TextContent, minute}, "|")
		if first, ok := firstOccurrences[key]; ok {
			start = min(start, first)
		} else {
			firstOccurrences[key] = index
		}
	}
	return start
}

// liepinLatestMessageGroupCovered 确认末个候选人或HR消息的分钟组已从左侧完整覆盖，尾窗不足时要求继续扩窗。
func liepinLatestMessageGroupCovered(items []contract.FindAllItem) (bool, error) {
	messages, err := liepinMessages(items)
	if err != nil {
		return false, err
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Direction != "candidate" && message.Direction != "self" {
			continue
		}
		if message.PlatformMessageID != "" || items[0].Index == 0 {
			return true, nil
		}
		if message.SentAt == nil {
			return false, nil
		}
		for _, previous := range messages[:index] {
			if previous.SentAt != nil && previous.SentAt.Before(*message.SentAt) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

// liepinMessage 解析一条猎聘消息的方向、卡片、时间和平台编号。
func liepinMessage(item contract.FindAllItem, now time.Time) (model.ConversationMessage, string, error) {
	bodyClass := strings.ToLower(strings.TrimSpace(item.Fields["body_class"]))
	direction := "system"
	switch {
	case strings.Contains(bodyClass, "message-item-receive"):
		direction = "candidate"
	case strings.Contains(bodyClass, "message-item-send"):
		direction = "self"
	case bodyClass != "":
		return model.ConversationMessage{}, "", fmt.Errorf("消息方向类名无法确认：%s", bodyClass)
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
		return model.ConversationMessage{}, "", err
	}
	candidateID := parseLiepinCandidateID(item.Fields["candidate_meta"])
	timeText := strings.TrimSpace(item.Fields["message_time"])
	sentAt := parseLiepinMessageTime(timeText, now)
	timeKey := timeText
	if sentAt != nil {
		timeKey = sentAt.UTC().Format(time.RFC3339)
	}
	baseKey := strings.Join([]string{direction, messageType, text, timeKey}, "|")
	card := json.RawMessage(`{}`)
	if messageType != "text" {
		encoded, marshalErr := json.Marshal(struct {
			Summary     string `json:"summary"`
			CandidateID string `json:"candidate_id,omitempty"`
		}{Summary: firstLiepinValue(resumeText, text), CandidateID: candidateID})
		if marshalErr != nil {
			return model.ConversationMessage{}, "", marshalErr
		}
		card = encoded
	}
	return model.ConversationMessage{
		Key: platformMessageID, PlatformMessageID: platformMessageID, Direction: direction,
		MessageType: messageType, TextContent: text, CardContent: card, SentAt: sentAt,
	}, baseKey, nil
}

// liepinMessagesAfterDeletion 只接受可核实且晚于删除时刻的消息，平台只显示分钟时同一分钟从严排除。
func liepinMessagesAfterDeletion(messages []model.ConversationMessage, after time.Time) ([]model.ConversationMessage, bool) {
	result := make([]model.ConversationMessage, 0, len(messages))
	reached := false
	for _, m := range messages {
		if m.SentAt == nil {
			continue
		}
		if !m.SentAt.After(after) {
			reached = true
			continue
		}
		result = append(result, m)
	}
	return result, reached
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

// liepinKnownMessageBoundary 从页面末尾向前匹配任意已同步消息，返回最靠后的差量边界。
func liepinKnownMessageBoundary(messages []model.ConversationMessage, knownMessageKeys []string) int {
	known := make(map[string]struct{}, len(knownMessageKeys))
	for _, key := range knownMessageKeys {
		if key = strings.TrimSpace(key); key != "" {
			known[key] = struct{}{}
		}
	}
	if len(known) == 0 {
		return -1
	}
	for index := len(messages) - 1; index >= 0; index-- {
		key := firstLiepinValue(messages[index].PlatformMessageID, messages[index].Key)
		if _, matched := known[key]; matched {
			return index
		}
	}
	return -1
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
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
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
