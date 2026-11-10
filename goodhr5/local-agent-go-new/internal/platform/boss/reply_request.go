// Package boss 本文件按双方真实沟通和原生控件状态索要单类资料，未执行与点击后未知结果严格区分。
package boss

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// requestBossAutoReplyCandidateInfo 每次只处理一种资料，双向不足或按钮未开放时暂缓，不改成文字索要。
func requestBossAutoReplyCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, request model.CandidateInfoRequest) (model.AutoReplyInfoRequestResult, error) {
	kind := model.CandidateInfoRequestKind(request)
	if kind == "" {
		return model.AutoReplyInfoRequestResult{}, fmt.Errorf("一次只处理一项资料索要")
	}
	if snapshot.ResumeOfferMessageID != "" {
		if kind != "resume" {
			return model.AutoReplyInfoRequestResult{}, fmt.Errorf("附件邀约不能用于其它资料操作")
		}
		return acceptBossAutoReplyResumeOffer(ctx, browser, cfg, snapshot)
	}
	if snapshot.ExpectedSendMessageKey == "" {
		return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "本项索要还没有可核实的沟通锚点"}, nil
	}
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	before, err := readBossInfoMessages(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	if err = bindBossNativeInfoCards(before, snapshot.CandidateName); err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	if result, found := bossInfoMessageResult(bossInfoMessagesAfter(before, snapshot.HistoryAfter), kind, nil); found {
		if result.Status == model.AutoReplyInfoRequested {
			result.Status = model.AutoReplyInfoAlreadyRequested
		}
		return result, nil
	}
	if !bossMutualCommunication(before) && !bossMutualCommunication(snapshot.Messages) {
		return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "平台尚未确认双方都已沟通"}, nil
	}
	available, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.request_"+kind)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	// 头部先出现、按钮后解除disabled时，只短促复查状态；已有可用按钮直接继续。
	for attempt := 0; !available && attempt < 2; attempt++ {
		select {
		case <-ctx.Done():
			return model.AutoReplyInfoRequestResult{}, ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
		available, err = common.ProbeSelectorExists(ctx, browser, cfg, "candidate.request_"+kind)
		if err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
	}
	if !available {
		return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "平台暂未开放本项索要"}, nil
	}
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	latest, err := readBossLatestAutoReplyMessage(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	if expected := snapshot.ExpectedSendMessageKey; expected != "" && expected != bossActionMessageKey(latest) {
		if latest.Direction == "candidate" {
			return model.AutoReplyInfoRequestResult{}, model.ErrAutoReplySupersededBeforeSend
		}
		return model.AutoReplyInfoRequestResult{}, fmt.Errorf("索要前聊天已有新变化，这一步先放下")
	}
	if err = common.RequestCandidateInfo(ctx, browser, cfg, request); err != nil {
		return model.AutoReplyInfoRequestResult{}, err // 点过后的错误保留未知，不能变成可再次索要。
	}
	known := make(map[string]bool, len(before))
	for _, message := range before {
		known[bossActionMessageKey(message)] = true
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
		messages, err := readBossInfoMessages(ctx, browser, cfg)
		if err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
		if err = bindBossNativeInfoCards(messages, snapshot.CandidateName); err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
		if result, found := bossInfoMessageResult(bossInfoMessagesAfter(messages, snapshot.HistoryAfter), kind, known); found {
			return result, nil
		}
		if !time.Now().Before(deadline) {
			return model.AutoReplyInfoRequestResult{}, fmt.Errorf("还没核对到本项索要的实际回执，暂不重复索要")
		}
		select {
		case <-ctx.Done():
			return model.AutoReplyInfoRequestResult{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// readBossInfoMessages 只读当前尾部，原生卡片和正常文字共用BOSS消息解析器。
func readBossInfoMessages(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.ConversationMessage, error) {
	return readBossRecentMessages(ctx, browser, cfg, 16)
}

// bossMutualCommunication 仅把有正文的双方文字气泡当作沟通，系统提示与交换卡片不补足资格。
func bossMutualCommunication(messages []model.ConversationMessage) bool {
	candidate, self := false, false
	for _, message := range messages {
		if message.MessageType != "text" || strings.TrimSpace(message.TextContent) == "" {
			continue
		}
		candidate = candidate || message.Direction == "candidate"
		self = self || message.Direction == "self"
		if candidate && self {
			return true
		}
	}
	return false
}

// bossInfoMessageResult 只认解析器标注的真实原生卡片，按最近一张同类卡片返回状态。
func bossInfoMessageResult(messages []model.ConversationMessage, kind string, known map[string]bool) (model.AutoReplyInfoRequestResult, bool) {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		key := bossActionMessageKey(message)
		if key == "" || (known != nil && known[key]) {
			continue
		}
		var card struct {
			InfoType   string `json:"info_type"`
			InfoStatus string `json:"info_status"`
			Phone      string `json:"phone"`
			Wechat     string `json:"wechat"`
		}
		if json.Unmarshal(message.CardContent, &card) != nil {
			continue
		}
		if card.InfoType != kind {
			// 平台只显示泛联系方式提示；仅本次已执行的明确动作和新消息键能证明类型，历史提示不能猜。
			currentContactReceipt := card.InfoType == "contact" && card.InfoStatus == model.AutoReplyInfoRequested && known != nil && (kind == "phone" || kind == "wechat")
			if !currentContactReceipt {
				continue
			}
		}
		switch card.InfoStatus {
		case model.AutoReplyInfoRequested, model.AutoReplyInfoReceived, model.AutoReplyInfoDeclined:
			result := model.AutoReplyInfoRequestResult{Status: card.InfoStatus, EvidenceMessageID: key, EvidenceMessage: &message}
			if card.InfoStatus == model.AutoReplyInfoReceived {
				result.Phone, result.Wechat = card.Phone, card.Wechat
			}
			return result, true
		}
	}
	return model.AutoReplyInfoRequestResult{}, false
}

// bossInfoMessagesAfter 删除后的资料状态只接受可证明晚于删除边界的原生卡片，不复活旧交换记录。
func bossInfoMessagesAfter(messages []model.ConversationMessage, after *time.Time) []model.ConversationMessage {
	if after == nil {
		return messages
	}
	result := make([]model.ConversationMessage, 0, len(messages))
	for _, message := range messages {
		if message.SentAt != nil && message.SentAt.After(*after) {
			result = append(result, message)
		}
	}
	return result
}

// bossActionMessageKey 优先使用平台编号，其次使用BOSS解析器已核实的稳定消息键。
func bossActionMessageKey(message model.ConversationMessage) string {
	if message.PlatformMessageID != "" {
		return message.PlatformMessageID
	}
	return message.Key
}
