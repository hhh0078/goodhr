// Package boss 本文件读取原生消息编号和可信时间，按服务器水位增量采集并核实删除后的新沟通。
package boss

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossConversationHistory 保留差量正文、完整性与候选人附件的真实来源编号。
type bossConversationHistory struct {
	Messages              []model.ConversationMessage
	HistoryComplete       bool
	ResumeCardAvailable   bool
	ResumeSourceMessageID string
}

// bossMessages 按可见时间组、方向、正文和组内重复次序生成内部键，不虚构平台原生消息编号。
func bossMessages(items []contract.FindAllItem, now time.Time) ([]model.ConversationMessage, error) {
	messages, err := common.ParseConfiguredAutoReplyMessages(items, bossAutoReplyAdapter.MessageRules, now)
	if err != nil {
		return nil, err
	}
	occurrences := make(map[string]int)
	var previousTime *time.Time
	for i := range messages {
		message := &messages[i]
		raw := strings.TrimSpace(items[i].Fields["message_time"])
		if raw != "" {
			previousTime = parseBossMessageTime(raw, now)
			if previousTime == nil {
				return nil, fmt.Errorf("BOSS消息时间尚未核实，暂不采集")
			}
		}
		message.SentAt = nil
		if previousTime != nil {
			value := *previousTime
			message.SentAt = &value
		}
		if err := annotateBossInfoCard(message, items[i]); err != nil {
			return nil, err
		}
		// BOSS当前页面没有消息id，内部指纹仅在完整时间组已经覆盖时可用于服务器水位。
		message.PlatformMessageID = ""
		minute := "unknown"
		if message.SentAt != nil {
			minute = message.SentAt.UTC().Format(time.RFC3339)
		}
		base := strings.Join([]string{"boss-visible-v1", minute, message.Direction, message.MessageType, message.TextContent}, "|")
		occurrences[base]++
		message.Key = common.StableAutoReplyMessageKey(fmt.Sprintf("%s|occurrence:%d", base, occurrences[base]))
	}
	return messages, nil
}

// bossInfoCard 只保存当前可见卡片已证明的交换状态，掩码联系方式不作为已获得的完整值。
type bossInfoCard struct {
	Summary          string `json:"summary"`
	InfoType         string `json:"info_type,omitempty"`
	InfoStatus       string `json:"info_status,omitempty"`
	Phone            string `json:"phone,omitempty"`
	Wechat           string `json:"wechat,omitempty"`
	OwnerName        string `json:"owner_name,omitempty"`
	StandaloneResume bool   `json:"standalone_resume,omitempty"`
}

var bossNativePhoneCardPattern = regexp.MustCompile(`^(.+?)的手机号：[[:space:]]*(1[3-9][0-9]{9})[[:space:]]*复制手机号$`)

// annotateBossInfoCard 将实测的系统索要回执与附件卡片标为资料事件，泛联系方式回执保留旧消息类型及指纹。
func annotateBossInfoCard(message *model.ConversationMessage, item contract.FindAllItem) error {
	card := bossInfoCard{Summary: message.TextContent}
	if message.MessageType == "resume" && message.Direction == "candidate" {
		card.InfoType, card.InfoStatus = "resume", "received"
		resume := strings.TrimSpace(item.Fields["resume_card"])
		card.Summary = resume
		card.StandaloneResume = strings.TrimSpace(item.Fields["message_text"]) == "" || strings.TrimSpace(item.Fields["message_text"]) == resume
	}
	if message.Direction == "candidate" && bossInfoClassToken(item.Fields["message_meta"], "message-dialog-icon-resume") && strings.Join(strings.Fields(item.Fields["common_card"]), "") == "对方想发送附件简历给您，您是否同意拒绝同意" {
		card.InfoType, card.InfoStatus = "resume", "offered"
	}
	if message.Direction == "system" {
		kind, status := bossNativeSystemInfo(message.TextContent)
		nativeSystem := bossInfoClassToken(item.Fields["body_class"], "item-system")
		if kind == "resume" || (kind == "contact" || kind == "wechat") && nativeSystem {
			card.InfoType, card.InfoStatus = kind, status
			if kind == "resume" {
				message.MessageType = "card" // 保留旧简历回执的类型与消息键；泛联系方式保留原系统类型。
			}
		}
		// 已实测的手机交换结果属于system方向；图标、结构和完整标题均具备时，先保留未绑定的原生归属。
		if nativeSystem && bossInfoClassToken(item.Fields["message_meta"], "message-dialog-icon-contact") {
			if match := bossNativePhoneCardPattern.FindStringSubmatch(strings.TrimSpace(item.Fields["common_card"])); len(match) == 3 {
				card.InfoType, card.InfoStatus = "contact", model.AutoReplyInfoReceived
				card.OwnerName, card.Phone = strings.TrimSpace(match[1]), match[2]
			}
		}
	}
	if card.InfoType != "" || message.MessageType != "text" {
		raw, err := json.Marshal(card)
		if err != nil {
			return err
		}
		message.CardContent = raw
	}
	return nil
}

// bossInfoClassToken 精确核对原生DOM类名，不能由普通正文或相似类名伪造资料事件。
func bossInfoClassToken(className, token string) bool {
	for _, value := range strings.Fields(className) {
		if value == token {
			return true
		}
	}
	return false
}

// bindBossNativeInfoCards 在当前会话已经核身后核对卡片所属姓名，不把HR或其他人的联系方式收入候选档案。
func bindBossNativeInfoCards(messages []model.ConversationMessage, candidateName string) error {
	candidateName = strings.TrimSpace(candidateName)
	for index := range messages {
		message := &messages[index]
		var card bossInfoCard
		if json.Unmarshal(message.CardContent, &card) != nil || card.InfoType != "contact" && card.InfoType != "phone" || card.InfoStatus != model.AutoReplyInfoReceived {
			continue
		}
		if message.Direction == "system" && candidateName != "" && card.OwnerName == candidateName && card.Phone != "" {
			card.InfoType = "phone"
		} else {
			card.InfoType, card.InfoStatus, card.Phone, card.Wechat = "", "", "", ""
		}
		raw, err := json.Marshal(card)
		if err != nil {
			return err
		}
		message.CardContent = raw
	}
	return nil
}

// bossNativeSystemInfo 只映射真实DOM已核实的系统文案；泛联系方式种类必须由本次动作上下文判定。
func bossNativeSystemInfo(text string) (string, string) {
	switch strings.TrimSpace(text) {
	case "简历请求已发送":
		return "resume", model.AutoReplyInfoRequested
	case "请求交换联系方式已发送":
		return "contact", model.AutoReplyInfoRequested
	case "请求交换微信已发送":
		return "wechat", model.AutoReplyInfoRequested
	}
	return "", ""
}

// parseBossMessageTime 解析可见时间或消息节点的原生时间戳，未知时间不推测为现在。
func parseBossMessageTime(raw string, now time.Time) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil && (len(raw) == 10 || len(raw) == 13) {
		parsed := time.Unix(value, 0).UTC()
		if len(raw) == 13 {
			parsed = time.UnixMilli(value).UTC()
		}
		return &parsed
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = now.Location()
	}
	localNow := now.In(location)
	for _, format := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006/01/02 15:04:05", "2006/01/02 15:04"} {
		if parsed, err := time.ParseInLocation(format, raw, location); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	day := localNow
	for _, prefix := range []struct {
		text string
		days int
	}{{"今天 ", 0}, {"昨天 ", -1}, {"前天 ", -2}} {
		if strings.HasPrefix(raw, prefix.text) {
			raw, day = strings.TrimSpace(strings.TrimPrefix(raw, prefix.text)), localNow.AddDate(0, 0, prefix.days)
			break
		}
	}
	for _, format := range []string{"15:04:05", "15:04"} {
		if clock, err := time.ParseInLocation(format, raw, location); err == nil {
			parsed := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, location).UTC()
			return &parsed
		}
	}
	for _, format := range []string{"1月2日 15:04", "01-02 15:04"} {
		if clock, err := time.ParseInLocation(format, raw, location); err == nil {
			parsed := time.Date(localNow.Year(), clock.Month(), clock.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
			if parsed.After(localNow.Add(24 * time.Hour)) {
				parsed = parsed.AddDate(-1, 0, 0)
			}
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

// readBossHistoryWindow 首次读取完整已加载窗口，之后只在边界改变时重读正文，遇到已知消息即停止翻页。
func readBossHistoryWindow(ctx context.Context, browser model.Browser, cfg model.Config, known []string, limit int, after *time.Time) (bossConversationHistory, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	anchor, err := common.RequiredSelector(cfg, "message.history_scroll")
	if err != nil {
		return bossConversationHistory{}, err
	}
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[id] = true
	}
	var items []contract.FindAllItem
	var messages []model.ConversationMessage
	needRead, stable := true, 0
	for attempt := 0; attempt < 120; attempt++ {
		if err := ctx.Err(); err != nil {
			return bossConversationHistory{}, err
		}
		if needRead {
			items, err = common.ReadConfiguredConversationMessageTail(ctx, browser, cfg, limit)
			if err != nil {
				return bossConversationHistory{}, err
			}
			messages, err = bossMessages(items, time.Now())
			if err != nil {
				return bossConversationHistory{}, err
			}
			if len(messages) == 0 {
				return bossConversationHistory{}, fmt.Errorf("BOSS当前聊天没有读到可核实的消息")
			}
			result, stop, err := bossHistoryBoundary(messages, knownSet, limit, after, bossStableGroupStart(items, messages, false), false)
			if err != nil || stop {
				return result, err
			}
		}
		head := items[0]
		progressTarget, err := common.IndexedSelector(cfg, "message.item", head.Index)
		if err != nil {
			return bossConversationHistory{}, err
		}
		progress, err := browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &anchor, ProgressTarget: &progressTarget, Distance: -1600, MaxAttempts: 1, WaitMS: 180})
		if err != nil {
			return bossConversationHistory{}, err
		}
		edges, err := readBossHistoryEdges(ctx, browser, cfg)
		if err != nil {
			return bossConversationHistory{}, err
		}
		if len(edges) != 2 || len(items) == 0 {
			return bossConversationHistory{}, fmt.Errorf("BOSS聊天历史边界尚未核实")
		}
		unchanged := sameBossMessageEdge(items[0], edges[0]) && sameBossMessageEdge(items[len(items)-1], edges[1])
		if progress.ProgressTargetVisible == nil || progress.ProgressTargetMoved == nil {
			return bossConversationHistory{}, fmt.Errorf("浏览器未返回BOSS历史滚动位置，暂不判断已到顶部")
		}
		if unchanged && !*progress.ProgressTargetMoved && *progress.ProgressTargetVisible {
			stable++
			if stable >= 2 {
				result, _, err := bossHistoryBoundary(messages, knownSet, limit, after, 0, true)
				result.HistoryComplete = true
				return result, err
			}
		} else {
			stable = 0
			if unchanged && !*progress.ProgressTargetMoved && !*progress.ProgressTargetVisible {
				return bossConversationHistory{}, fmt.Errorf("BOSS历史滚轮未到达消息区域，稍后再试")
			}
		}
		needRead = !unchanged
	}
	return bossConversationHistory{}, fmt.Errorf("BOSS历史仍未核实完整，稍后继续")
}

// bossHistoryBoundary 只发布完整时间组中的内部键，删除边界前与未知时间的资料不得重新入库。
func bossHistoryBoundary(messages []model.ConversationMessage, known map[string]bool, limit int, after *time.Time, stableStart int, atTop bool) (bossConversationHistory, bool, error) {
	result := bossConversationHistory{}
	start, stop := min(stableStart, len(messages)), false
	if after != nil {
		start = len(messages)
		for i := len(messages) - 1; i >= 0; i-- {
			m := messages[i]
			if m.SentAt == nil {
				if !atTop {
					return result, false, nil
				}
				if m.Direction == "system" {
					continue
				}
				return result, false, fmt.Errorf("BOSS新沟通有消息时间未核实，暂不重新建档")
			}
			if !m.SentAt.After(*after) {
				result.HistoryComplete, stop = true, true
				break
			}
			start = i
		}
		if !stop && !atTop {
			start = max(start, stableStart)
		}
	}
	filtered := make([]model.ConversationMessage, 0, len(messages)-start)
	for _, m := range messages[start:] {
		if after == nil || (m.SentAt != nil && m.SentAt.After(*after)) {
			filtered = append(filtered, m)
		}
	}
	if available, source := common.ConfiguredAutoReplyResumeCard(filtered); available {
		result.ResumeCardAvailable, result.ResumeSourceMessageID = true, source
	}
	for i := len(filtered) - 1; i >= 0; i-- {
		if known[filtered[i].Key] {
			// 保留真实观察到的锚点，服务器可按已同步键核实同分钟新消息的先后，不会再次答复旧消息。
			filtered, stop = filtered[bossPendingResumeOfferStart(filtered, i):], true
			break
		}
	}
	if len(messages) >= limit && !stop {
		if after != nil {
			return result, false, fmt.Errorf("BOSS新沟通的时间起点尚未核实，暂不重新建档")
		}
		if len(filtered) == 0 {
			return result, false, fmt.Errorf("BOSS截尾消息尚未覆盖完整时间组，暂不猜测重复编号")
		}
		stop = true
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	result.Messages = filtered
	return result, stop, nil
}

// bossPendingResumeOfferStart 保留已知回复之前仍待显式接收的原生邀约，服务器按旧键幂等同步而不会再次答复。
func bossPendingResumeOfferStart(messages []model.ConversationMessage, boundary int) int {
	for index := 0; index < boundary; index++ {
		if bossResumeOfferMessage(messages[index]) {
			return index
		}
	}
	return boundary
}

// bossStableGroupStart 排除可能被截断的首时间组；确实读到历史顶部后才接受首组重复序号。
func bossStableGroupStart(items []contract.FindAllItem, messages []model.ConversationMessage, atTop bool) int {
	if atTop {
		return 0
	}
	if len(items) == 0 || len(messages) == 0 {
		return len(messages)
	}
	var first *time.Time
	for i, m := range messages {
		if m.SentAt == nil {
			continue
		}
		if first == nil {
			first = m.SentAt
			if i > 0 && strings.TrimSpace(items[i].Fields["message_time"]) != "" {
				return i
			}
			continue
		}
		if !m.SentAt.Equal(*first) {
			return i
		}
	}
	return len(messages)
}

// bossMessageIdentityFields 轻读只取消息编号与时间，避免历史稳定验证重复读取整段正文。
func bossMessageIdentityFields(cfg model.Config) map[string]contract.SelectorSpec {
	fields := map[string]contract.SelectorSpec{}
	for _, key := range []string{"message_time", "body_class"} {
		if value, ok := cfg.ConversationFields[key]; ok {
			fields[key] = value
		}
	}
	return fields
}

// readBossHistoryEdges 读取当前首尾节点和原生编号，用于核对实际新增或重排。
func readBossHistoryEdges(ctx context.Context, browser model.Browser, cfg model.Config) ([]contract.FindAllItem, error) {
	selector, err := common.RequiredSelector(cfg, "message.item")
	if err != nil {
		return nil, err
	}
	head, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, Fields: bossMessageIdentityFields(cfg)})
	if err != nil || len(head) != 1 {
		return nil, err
	}
	tail, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, FromEnd: true, Fields: bossMessageIdentityFields(cfg)})
	if err != nil || len(tail) != 1 {
		return nil, err
	}
	return []contract.FindAllItem{head[0], tail[0]}, nil
}

// sameBossMessageEdge 同时比较编号、位置、总数和正文，读取期间新增消息不能沿用旧尾部结论。
func sameBossMessageEdge(a, b contract.FindAllItem) bool {
	return a.Index == b.Index && a.TotalMatches == b.TotalMatches && a.Text == b.Text && a.Fields["message_time"] == b.Fields["message_time"]
}

// waitBossReplyPoll 仅用于状态尚未就绪的短轮询，已观察成功时不再固定等待。
func waitBossReplyPoll(ctx context.Context) error {
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
