// Package liepin 文件作用：实现猎聘企业端自动回复的全部联系人扫描、会话校验、消息发送和关闭动作。
package liepin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const liepinConversationPollAttempts = 20
const liepinConversationPollInterval = 300 * time.Millisecond

// InitializeAutoReplyPage 收起已核实的周报或附件遮层，复用全部联系人抽屉及已打开的聊天。
func (r *Runtime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config, target *model.AutoReplyConversationSnapshot) error {
	r.contactRows = nil
	page, err := liepinAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if _, configured := cfg.Selectors["message.weekly_report"]; configured {
		if err = common.ClosePanelEscapeFirst(ctx, browser, cfg, "message.weekly_report", "message.weekly_report_close", page, nil); err != nil {
			return err
		}
	}
	var verify func(context.Context) error
	if target != nil {
		verify = func(ctx context.Context) error {
			return r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, *target)
		}
	}
	return common.ClosePanelEscapeFirst(ctx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", page, verify)
}

// liepinAutoReplyPageURL 核对当前唯一活动页确实属于本平台会话入口，再把完整地址交给后续动作逐次复核。
func liepinAutoReplyPageURL(ctx context.Context, browser model.Browser, cfg model.Config) (string, error) {
	page, err := common.UniqueCurrentPageURL(ctx, browser)
	if err != nil {
		return "", err
	}
	if !common.PageURLMatches(page, cfg.MessagesURL) {
		return "", fmt.Errorf("当前页面与招聘会话入口不一致，请先回到招聘会话页面")
	}
	return page, nil
}

// ScanUnreadConversations 提供全部联系人行及变化线索，待核对会话由任务按招聘账号的持久基线筛选。
func (r *Runtime) ScanUnreadConversations(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	if err := ensureLiepinConversationDrawer(ctx, browser, cfg); err != nil {
		return nil, err
	}
	return r.readLiepinContacts(ctx, browser, cfg)
}

// OpenAutoReplyConversation 重新按稳定会话编号定位联系人，并读取身份、岗位、聊天和简历卡片。
func (r *Runtime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, knownMessageKeys []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	snapshot, err := r.openAutoReplyContact(ctx, browser, cfg, conversation)
	if err != nil {
		return snapshot, err
	}
	history, err := readLiepinConversationHistoryDetail(ctx, browser, cfg, knownMessageKeys, maxHistory)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	snapshot.Gender, err = readLiepinGender(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	snapshot.PlatformCandidateID, err = readLiepinCandidateID(ctx, browser, cfg, history.Messages)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	snapshot.AvatarURL = conversation.AvatarURL
	snapshot.Messages, snapshot.HistoryComplete = history.Messages, history.HistoryComplete
	snapshot.ResumeCardAvailable, snapshot.ResumeSourceMessageID = history.ResumeCardAvailable, history.ResumeSourceMessageID
	return snapshot, nil
}

// openAutoReplyContact 先按平台会话编号定位，再核对当前页的人名和岗位，供普通采集和删后核身共用。
func (r *Runtime) openAutoReplyContact(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	if err := ensureLiepinConversationDrawer(ctx, browser, cfg); err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if err := openLiepinConversation(ctx, browser, cfg, conversation); err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	name, position, err := waitLiepinConversation(ctx, browser, cfg, conversation.Name)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if conversation.PlatformCandidateID != "" {
		if err = r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, model.AutoReplyConversationSnapshot{CandidateName: conversation.Name, PlatformCandidateID: conversation.PlatformCandidateID}); err != nil {
			return model.AutoReplyConversationSnapshot{}, err
		}
	}
	return model.AutoReplyConversationSnapshot{Conversation: conversation, CandidateName: name, CommunicationPosition: position, PlatformThreadID: conversation.PlatformThreadID, PlatformAccountID: conversation.PlatformAccountID}, nil
}

// InspectAutoReplyContact 只从当前会话头确认候选人编号，不采集历史消息、旧简历卡片或个人资料。
func (r *Runtime) InspectAutoReplyContact(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	snapshot, err := r.openAutoReplyContact(ctx, browser, cfg, conversation)
	if err != nil {
		return snapshot, err
	}
	snapshot.PlatformCandidateID, err = readLiepinCandidateID(ctx, browser, cfg, nil)
	if err == nil && snapshot.PlatformCandidateID == "" {
		err = fmt.Errorf("没能从当前聊天核对候选人编号，稍后再试")
	}
	return snapshot, err
}

// ReadAutoReplyMessages 从当前已经打开的聊天框读取云端游标之后的新消息，不重复打开联系人列表。
func (r *Runtime) ReadAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, knownMessageKeys []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	if snapshot.DeliveryPageURL != "" {
		if err := r.verifyOpenDeliveryPage(ctx, browser, cfg, snapshot); err != nil {
			return nil, false, err
		}
		items, err := common.ReadConfiguredConversationMessageTail(ctx, browser, cfg, min(maxHistory, 16))
		if err != nil {
			return nil, false, err
		}
		rows, err := liepinMessages(items)
		if err != nil {
			return nil, false, err
		}
		if len(items) > 0 && items[0].Index > 0 {
			// 截断的首个分钟不能证明本地同文序号，保留平台原生编号的行，其余留待正常历史采集。
			boundary := liepinLeadingMessageGroupEnd(rows)
			valid := make([]model.ConversationMessage, 0, len(rows))
			for i, row := range rows {
				if i >= boundary || row.PlatformMessageID != "" {
					valid = append(valid, row)
				}
			}
			rows = valid
		}
		return rows, false, r.verifyOpenDeliveryPage(ctx, browser, cfg, snapshot)
	}
	if err := ensureLiepinAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return nil, false, err
	}
	result, err := readLiepinHistoryWindow(ctx, browser, cfg, knownMessageKeys, maxHistory, snapshot.HistoryAfter)
	return result.Messages, result.HistoryComplete, err
}

// SendAutoReplyMessage 核对候选人和沟通岗位后发送一条自动回复。
func (r *Runtime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	if err := ensureLiepinAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return err
	}
	position, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_position")
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(snapshot.CommunicationPosition); expected != "" && (!found || !liepinPositionMatches(expected, position)) {
		return fmt.Errorf("%s当前聊天框的沟通岗位已经变化，消息没有发送", cfg.Name)
	}
	if !cfg.Behavior.AutoReplySendEnterFirst {
		return common.SendAutoReplyText(ctx, browser, cfg, message)
	}
	pageURL, err := liepinAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if snapshot.DeliveryPageURL != "" && pageURL != snapshot.DeliveryPageURL {
		return fmt.Errorf("准备发送期间招聘页面已变化")
	}
	// 按键与按钮兜底均重新核身并确认末条；读取失败不自动重新打开其他会话。
	inspect := func() (model.ConversationMessage, error) {
		active, pageErr := common.UniqueCurrentPageURL(ctx, browser)
		if pageErr != nil || active != pageURL {
			return model.ConversationMessage{}, fmt.Errorf("消息发送期间页面已变化，暂不继续")
		}
		if snapshot.PlatformCandidateID != "" {
			if err := r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
				return model.ConversationMessage{}, err
			}
		}
		matched, err := liepinAutoReplyConversationMatches(ctx, browser, cfg, snapshot)
		if err != nil || !matched {
			return model.ConversationMessage{}, fmt.Errorf("消息发送期间聊天身份或岗位无法确认，暂不继续")
		}
		return readLiepinLatestAutoReplyMessage(ctx, browser, cfg)
	}
	return common.SendAutoReplyEnterFirst(ctx, browser, cfg, pageURL, message, snapshot.ExpectedSendMessageKey, inspect)
}

// ReadLatestAutoReplyMessage 返回当前聊天框最后一条候选人或 HR 消息。
func (r *Runtime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if err := ensureLiepinAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return model.ConversationMessage{}, err
	}
	return readLiepinLatestAutoReplyMessage(ctx, browser, cfg)
}

// ReadOpenAutoReplyLatestMessage 只在目标聊天框仍然打开时读取最新消息，不为轮询重新打开旧会话。
func (r *Runtime) ReadOpenAutoReplyLatestMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	matched, err := liepinAutoReplyConversationMatches(ctx, browser, cfg, snapshot)
	if err != nil {
		return model.ConversationMessage{}, false, err
	}
	if !matched {
		return model.ConversationMessage{}, false, nil
	}
	if snapshot.DeliveryPageURL != "" {
		if err := r.verifyOpenDeliveryPage(ctx, browser, cfg, snapshot); err != nil {
			return model.ConversationMessage{}, false, err
		}
	}
	latest, err := readLiepinLatestAutoReplyMessage(ctx, browser, cfg)
	if err == nil && snapshot.DeliveryPageURL != "" {
		err = r.verifyOpenDeliveryPage(ctx, browser, cfg, snapshot)
	}
	return latest, true, err
}

// verifyOpenDeliveryPage 只核对原标签与当前聊天的实时编号，发送后绝不因为换页或换人而重新打开会话。
func (r *Runtime) verifyOpenDeliveryPage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	page, err := common.UniqueCurrentPageURL(ctx, browser)
	if err != nil {
		return err
	}
	if page != snapshot.DeliveryPageURL {
		return fmt.Errorf("发送后招聘页面变化，无法核对送达")
	}
	return r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
}

// readLiepinLatestAutoReplyMessage 从尾部逐步扩窗找最后一条候选人或HR消息，保留同一解析器和读取期间的新消息复核。
func readLiepinLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config) (model.ConversationMessage, error) {
	selector, err := common.RequiredSelector(cfg, "message.item")
	if err != nil {
		return model.ConversationMessage{}, err
	}
	fields := map[string]contract.SelectorSpec{}
	if metadata, found := cfg.ConversationFields["message_meta"]; found {
		fields["message_meta"] = metadata
	}
	for attempt := 0; attempt < 3; attempt++ {
		for size := 1; ; size = min(5000, size*2) {
			if err = ctx.Err(); err != nil {
				return model.ConversationMessage{}, err
			}
			items, readErr := common.ReadConfiguredConversationMessageTail(ctx, browser, cfg, size)
			if readErr != nil {
				return model.ConversationMessage{}, readErr
			}
			messages, parseErr := liepinMessages(items)
			if parseErr != nil {
				return model.ConversationMessage{}, parseErr
			}
			covered, coverageErr := liepinLatestMessageGroupCovered(items)
			if coverageErr != nil {
				return model.ConversationMessage{}, coverageErr
			}
			var latest model.ConversationMessage
			for index := len(messages) - 1; index >= 0; index-- {
				if messages[index].Direction == "candidate" || messages[index].Direction == "self" {
					latest = messages[index]
					break
				}
			}
			if latest.Direction != "" && covered {
				// 正文字段读取期间可能又来消息；轻读真正末条，变化时重读而不返回旧决定依据。
				tail, readErr := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, FromEnd: true, Fields: fields})
				if readErr != nil {
					return model.ConversationMessage{}, readErr
				}
				last := items[len(items)-1]
				if len(tail) == 1 && tail[0].TotalMatches == last.TotalMatches && tail[0].Text == last.Text && liepinHistoryItemKey(tail[0]) == liepinHistoryItemKey(last) {
					return latest, nil
				}
				break
			}
			if len(items) < size || (len(items) > 0 && items[0].Index == 0) || size == 5000 {
				if latest.Direction != "" && !covered {
					return model.ConversationMessage{}, fmt.Errorf("聊天末条消息的重复编号还没确认完整，这轮先不发送")
				}
				return model.ConversationMessage{}, fmt.Errorf("%s当前聊天框没有读到候选人或 HR 消息", cfg.Name)
			}
		}
	}
	return model.ConversationMessage{}, fmt.Errorf("聊天末条消息还在变化，这轮先不发送，稍后再核对")
}

// ensureLiepinAutoReplyConversation 在在线简历关闭聊天弹框后，通过稳定会话编号重新打开原候选人。
func ensureLiepinAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	matched, err := liepinAutoReplyConversationMatches(ctx, browser, cfg, snapshot)
	if err != nil {
		return err
	}
	if matched {
		return nil
	}
	if err = ensureLiepinConversationDrawer(ctx, browser, cfg); err != nil {
		return err
	}
	expected := snapshot.Conversation
	expected.Name = firstLiepinValue(expected.Name, snapshot.CandidateName)
	expected.PlatformThreadID = firstLiepinValue(expected.PlatformThreadID, snapshot.PlatformThreadID)
	expected.Key = firstLiepinValue(expected.Key, expected.PlatformThreadID)
	if err = openLiepinConversation(ctx, browser, cfg, expected); err != nil {
		return err
	}
	_, _, err = waitLiepinConversation(ctx, browser, cfg, snapshot.CandidateName, snapshot.CommunicationPosition)
	if err == nil && expected.PlatformCandidateID != "" {
		err = (&Runtime{}).VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
	}
	return err
}

// liepinAutoReplyConversationMatches 同时核对猎聘企业端当前聊天框的候选人和沟通岗位。
func liepinAutoReplyConversationMatches(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (bool, error) {
	name, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_name")
	if err != nil || !found || !common.CandidateNamesMatch(snapshot.CandidateName, name) {
		return false, err
	}
	expectedPosition := strings.TrimSpace(snapshot.CommunicationPosition)
	if expectedPosition == "" {
		return false, fmt.Errorf("%s待回复会话缺少沟通岗位，无法安全核对候选人", cfg.Name)
	}
	position, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_position")
	if err != nil || !found || strings.TrimSpace(position) == "" {
		return false, err
	}
	return liepinPositionMatches(expectedPosition, position), nil
}

// ensureLiepinConversationDrawer 不依赖未读数字打开全部联系人，供已经读过的当前会话恢复使用。
func ensureLiepinConversationDrawer(ctx context.Context, browser model.Browser, cfg model.Config) error {
	opened, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
	if err != nil {
		return err
	}
	if !opened {
		if err = common.ClickRequired(ctx, browser, cfg, "message.entry"); err != nil {
			return fmt.Errorf("重新打开%s联系人列表失败：%w", cfg.Name, err)
		}
		for attempt := 1; attempt <= liepinConversationPollAttempts; attempt++ {
			opened, err = common.ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
			if err != nil {
				return err
			}
			if opened {
				break
			}
			if attempt < liepinConversationPollAttempts {
				if err = waitLiepinReplyPoll(ctx); err != nil {
					return err
				}
			}
		}
		if !opened {
			return fmt.Errorf("%s联系人列表没有重新打开", cfg.Name)
		}
	}
	selected, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.all_tab_selected")
	if err != nil || selected {
		return err
	}
	if err = common.ClickRequired(ctx, browser, cfg, "message.all_tab"); err != nil {
		return fmt.Errorf("切换%s全部联系人失败：%w", cfg.Name, err)
	}
	for attempt := 1; attempt <= liepinConversationPollAttempts; attempt++ {
		selected, err = common.ProbeSelectorExists(ctx, browser, cfg, "message.all_tab_selected")
		if err != nil || selected {
			return err
		}
		if attempt < liepinConversationPollAttempts {
			if err = waitLiepinReplyPoll(ctx); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("%s联系人列表没有切到全部标签", cfg.Name)
}

// CloseAutoReplyConversation 关闭附件预览、候选人聊天框和联系人抽屉。
func (r *Runtime) CloseAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	page, err := liepinAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	verify := func(ctx context.Context) error {
		return r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
	}
	for _, panel := range []struct{ key, close string }{
		{"message.attachment_preview", "message.attachment_preview_close"},
		{"candidate.chat_modal", "candidate.chat_close"},
	} {
		if err = common.ClosePanelEscapeFirst(ctx, browser, cfg, panel.key, panel.close, page, verify); err != nil {
			return err
		}
	}
	return common.ClosePanelEscapeFirst(ctx, browser, cfg, "candidate.contact_drawer", "candidate.contact_drawer_close", page, func(ctx context.Context) error {
		// 已关闭原聊天后，如果用户打开了新聊天，Esc 会先落在新窗口。
		opened, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.chat_modal")
		if err == nil && opened {
			return fmt.Errorf("联系人列表上又出现了聊天框，暂不关闭")
		}
		return err
	})
}

// ReadConversation 兼容平台公共接口，安全打开会话后返回可读聊天正文。
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
	conversations, err := liepinConversations(items)
	if err != nil {
		return -1, err
	}
	matches := make([]model.Conversation, 0, 1)
	expectedThreadID := strings.TrimSpace(expected.PlatformThreadID)
	for _, item := range conversations {
		if expectedThreadID != "" && item.PlatformThreadID == expectedThreadID {
			matches = append(matches, item)
			continue
		}
		if expectedThreadID == "" && strings.HasPrefix(expected.Key, "local:") && item.Key == expected.Key {
			matches = append(matches, item)
			continue
		}
		if expectedThreadID == "" && expected.Key == "" && common.CandidateNamesMatch(expected.Name, item.Name) {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return -1, fmt.Errorf("%s联系人列表里匹配到%d个目标会话，我不敢猜要回复哪一个", cfg.Name, len(matches))
	}
	return matches[0].Index, nil
}

// liepinContactActivityAt 只解析联系人时间节点的时刻；猎聘当天显示时分，昨天及更早只显示日期，只有日期时留空。
func liepinContactActivityAt(raw string, now time.Time) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "今天 ") {
		raw = strings.TrimPrefix(raw, "今天 ")
	}
	if clock := parseLiepinMessageTime(raw, now); clock != nil {
		// 跨午夜时，未更新的昨日时分可能出现在今天的未来，不把它当成本轮的新活动。
		if clock.After(now) {
			return ""
		}
		return clock.Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006/1/2 15:04", "2006年1月2日 15:04"} {
		if clock, err := time.ParseInLocation(layout, raw, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil && !clock.After(now) {
			return clock.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// liepinConversations 把猎聘联系人字段转换为统一会话结构。
func liepinConversations(items []contract.FindAllItem) ([]model.Conversation, error) {
	now := time.Now()
	result := make([]model.Conversation, 0, len(items))
	for _, item := range items {
		meta, err := parseLiepinContactMeta(item.Fields["thread_meta"])
		if err != nil {
			return nil, fmt.Errorf("解析猎聘联系人会话编号失败：%w", err)
		}
		name := strings.TrimSpace(item.Fields["name"])
		lastMessage := strings.TrimSpace(item.Fields["last_message"])
		threadID := strings.TrimSpace(meta.ThreadID)
		key := threadID
		if key == "" {
			key, err = common.LocalConversationKey("liepin", name, item.Fields["avatar_url"])
			if err != nil {
				return nil, err
			}
		}
		if item.Fields == nil {
			item.Fields = make(map[string]string)
		}
		if _, known := item.Fields["contact_activity_at"]; !known {
			item.Fields["contact_activity_at"] = liepinContactActivityAt(item.Fields["contact_time"], now)
		}
		// 列表正文包含可见时间。持久基线只需摘要，不保存整行联系人文本。
		item.Fields["contact_list_signature"] = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{
			name, lastMessage, item.Text, strings.TrimSpace(item.Fields["contact_time"]),
		}, "\x00"))))
		result = append(result, model.Conversation{
			Index: item.Index, Key: key, Name: name, AvatarURL: strings.TrimSpace(item.Fields["avatar_url"]), PlatformThreadID: threadID,
			Summary: lastMessage, Fields: item.Fields,
		})
	}
	return result, nil
}

type liepinContactMeta struct {
	ThreadID string `json:"to_imid"`
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

// waitLiepinConversation 每 300 毫秒确认聊天框姓名和可选岗位，并读取页面显示的完整沟通岗位。
func waitLiepinConversation(ctx context.Context, browser model.Browser, cfg model.Config, expectedName string, expectedPositions ...string) (string, string, error) {
	expectedPosition := ""
	if len(expectedPositions) > 0 {
		expectedPosition = strings.TrimSpace(expectedPositions[0])
	}
	for attempt := 1; attempt <= liepinConversationPollAttempts; attempt++ {
		name, found, err := common.ReadOptional(ctx, browser, cfg, "message.current_name")
		if err != nil {
			return "", "", err
		}
		if found && common.CandidateNamesMatch(expectedName, name) {
			position, positionFound, readErr := common.ReadOptional(ctx, browser, cfg, "message.current_position")
			position = cleanLiepinPosition(position)
			if readErr != nil {
				return "", "", readErr
			}
			if positionFound && position != "" && (expectedPosition == "" || liepinPositionMatches(expectedPosition, position)) {
				return name, position, nil
			}
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
	if expected == "" || actual == "" {
		return false
	}
	if expected == actual {
		return true
	}
	prefix := ""
	switch {
	case strings.HasSuffix(actual, "..."):
		prefix = strings.TrimSpace(strings.TrimSuffix(actual, "..."))
	case strings.HasSuffix(actual, "…"):
		prefix = strings.TrimSpace(strings.TrimSuffix(actual, "…"))
	}
	return prefix != "" && strings.HasPrefix(expected, prefix)
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
