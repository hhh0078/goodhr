// Package boss 本文件显式接收已核实的候选人附件邀约；只点本轮同意一次，旧PDF不能充当本次接收回执。
package boss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// acceptBossAutoReplyResumeOffer 仅供带独立服务器账本意图的显式RequestInfo调用，普通读消息和送达核验不接收文件。
func acceptBossAutoReplyResumeOffer(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyInfoRequestResult, error) {
	if snapshot.ExpectedSendMessageKey == "" || snapshot.ResumeOfferMessageID == "" {
		return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "本轮附件邀约或沟通锚点尚未核实"}, nil
	}
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	items, before, err := readBossRecentWindow(ctx, browser, cfg, 32)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	if !bossContainsMessage(before, snapshot.ResumeOfferMessageID) {
		history, readErr := readBossHistoryWindow(ctx, browser, cfg, nil, 5000, snapshot.HistoryAfter)
		if readErr != nil {
			return model.AutoReplyInfoRequestResult{}, readErr
		}
		before, items = history.Messages, nil
	}
	if !bossContainsMessage(before, snapshot.ResumeOfferMessageID) {
		return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "没有找到本轮待接收的附件邀约"}, nil
	}
	if len(items) == 0 {
		items, err = common.ReadConfiguredConversationMessages(ctx, browser, cfg)
		if err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
	}
	full, err := bossMessages(items, time.Now())
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	index := -1
	for i, message := range full {
		if bossActionMessageKey(message) != snapshot.ResumeOfferMessageID {
			continue
		}
		if index >= 0 || !bossResumeOfferMessage(message) || message.SentAt == nil || snapshot.HistoryAfter != nil && !message.SentAt.After(*snapshot.HistoryAfter) {
			return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "附件邀约的发送人或时间还不能核实"}, nil
		}
		index = i
	}
	if index < 0 {
		return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "附件邀约已变化，先重新核对来源"}, nil
	}
	if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	for i := len(before) - 1; i >= 0; i-- {
		if before[i].Direction != "candidate" && before[i].Direction != "self" {
			continue
		}
		if bossActionMessageKey(before[i]) != snapshot.ExpectedSendMessageKey {
			return model.AutoReplyInfoRequestResult{}, model.ErrAutoReplySupersededBeforeSend
		}
		break
	}
	button, err := pinBossResumeOfferButton(cfg, items[index], snapshot.PlatformCandidateID)
	if err != nil {
		return model.AutoReplyInfoRequestResult{}, err
	}
	known := make(map[string]bool, len(full))
	for _, message := range full {
		known[bossActionMessageKey(message)] = true
		if message.MessageType == "resume" {
			known[bossResumeVisibleContentKey(message)] = true
		}
	}
	clicked, err := browser.Click(ctx, contract.ElementClickRequest{Selector: button})
	if err != nil {
		if bossOfferClickNotStarted(err) {
			return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoDeferred, Reason: "尚未开始点击附件同意，保留邀约稍后再核对"}, nil
		}
		return model.AutoReplyInfoRequestResult{}, fmt.Errorf("附件同意结果尚未确认，暂不补点：%w", err)
	}
	if !clicked.Clicked {
		return model.AutoReplyInfoRequestResult{}, fmt.Errorf("附件同意结果尚未确认，暂不补点")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
		// 读取当前已加载窗口，能看到PDF替换旧邀约行，不只找追加在聊天末尾的卡片。
		_, after, readErr := readBossRecentWindow(ctx, browser, cfg, 5000)
		if readErr != nil {
			return model.AutoReplyInfoRequestResult{}, readErr
		}
		if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
		after = bossInfoMessagesAfter(after, snapshot.HistoryAfter)
		if err = bindBossNativeInfoCards(after, snapshot.CandidateName); err != nil {
			return model.AutoReplyInfoRequestResult{}, err
		}
		result, found, receiptErr := bossNewResumeOfferReceipt(after, full[index], known, snapshot.HistoryAfter)
		if receiptErr != nil || found {
			if found && receiptErr == nil {
				result.EvidenceMessages = after
			}
			return result, receiptErr
		}
		if !time.Now().Before(deadline) {
			return model.AutoReplyInfoRequestResult{}, fmt.Errorf("尚未看到本次邀约的新附件卡，已阻止重复同意")
		}
		select {
		case <-ctx.Done():
			return model.AutoReplyInfoRequestResult{}, ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
	}
}

// bossContainsMessage 只按当前已核实窗口的真实稳定键定位来源，不用标题相同的其它邀约替代。
func bossContainsMessage(messages []model.ConversationMessage, key string) bool {
	for _, message := range messages {
		if bossActionMessageKey(message) == key {
			return true
		}
	}
	return false
}

// bossResumeOfferMessage 只接受解析器依据原生图标、方向及精确标题标记的候选附件邀约。
func bossResumeOfferMessage(message model.ConversationMessage) bool {
	var card bossInfoCard
	return message.Direction == "candidate" && message.MessageType == "card" && json.Unmarshal(message.CardContent, &card) == nil && card.InfoType == "resume" && card.InfoStatus == "offered"
}

// pinBossResumeOfferButton 复用消息行索引能力，绑定总行数和原文，点击期间新增或重排消息便拒绝旧位置。
func pinBossResumeOfferButton(cfg model.Config, item contract.FindAllItem, candidateID string) (contract.SelectorSpec, error) {
	row, err := pinBossMessageRow(cfg, item, item.Fields["common_card"], candidateID)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	button, err := common.RequiredSelector(cfg, "candidate.accept_resume_offer")
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	button.Parents = nil
	return button.WithinSelector(row), nil
}

// bossOfferClickNotStarted 仅采信点击动作在查找阶段且明确尚未按下鼠标的回执，普通NotFound或验证失败仍保留未知。
func bossOfferClickNotStarted(err error) bool {
	var worker *contract.WorkerError
	if !errors.As(err, &worker) || worker == nil || worker.Body.Action != "element.click" || worker.Body.Code != "ELEMENT_NOT_FOUND" || worker.Body.Step != "find" {
		return false
	}
	var details struct {
		ClickStarted *bool `json:"click_started"`
	}
	return json.Unmarshal(worker.Body.Details, &details) == nil && details.ClickStarted != nil && !*details.ClickStarted
}

// bossNewResumeOfferReceipt 只认本次接收后新出现的唯一候选PDF；旧文件、删除前资料及多个不明来源均不冒认。
func bossNewResumeOfferReceipt(messages []model.ConversationMessage, offer model.ConversationMessage, known map[string]bool, after *time.Time) (model.AutoReplyInfoRequestResult, bool, error) {
	var found *model.ConversationMessage
	for _, message := range messages {
		key := bossActionMessageKey(message)
		if key == "" || known[key] || message.Direction != "candidate" || message.MessageType != "resume" || message.SentAt == nil || offer.SentAt == nil || message.SentAt.Before(*offer.SentAt) || after != nil && !message.SentAt.After(*after) {
			continue
		}
		if known[bossResumeVisibleContentKey(message)] {
			return model.AutoReplyInfoRequestResult{}, false, fmt.Errorf("新旧附件同文同分钟，暂不能区分本次文件来源")
		}
		if found != nil {
			return model.AutoReplyInfoRequestResult{}, false, fmt.Errorf("同时出现多份新附件，暂不能核实本次接收来源")
		}
		value := message
		found = &value
	}
	if found == nil {
		return model.AutoReplyInfoRequestResult{}, false, nil
	}
	return model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, EvidenceMessageID: bossActionMessageKey(*found), EvidenceMessage: found}, true, nil
}

// bossResumeVisibleContentKey 排除同分钟同文PDF在旧行插入后发生的重复序号位移，不将旧文件的新编号冒认成新文件。
func bossResumeVisibleContentKey(message model.ConversationMessage) string {
	if message.SentAt == nil {
		return ""
	}
	return "resume-content:" + common.StableAutoReplyMessageKey(message.SentAt.UTC().Format(time.RFC3339)+"|"+message.Direction+"|"+message.MessageType+"|"+message.TextContent)
}
