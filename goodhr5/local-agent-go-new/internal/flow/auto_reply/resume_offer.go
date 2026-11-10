// Package auto_reply 本文件把候选人主动发送附件的原生邀约作为一次性平台动作处理，真实新文件同步后才进入原有下载待办。
package auto_reply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// visibleResumeOffer 从本轮纯读消息派生原生邀请，不依赖只有Open才能返回的snapshot字段或旧待回复状态。
func visibleResumeOffer(snapshot model.AutoReplyConversationSnapshot) string {
	for i := len(snapshot.Messages) - 1; i >= 0; i-- {
		m := snapshot.Messages[i]
		if snapshot.HistoryAfter != nil && (m.SentAt == nil || !m.SentAt.After(*snapshot.HistoryAfter)) {
			continue
		}
		if cloud.IsAutoReplyResumeOfferMessage(cloud.AutoReplyMessage{Direction: m.Direction, MessageType: m.MessageType, CardContent: m.CardContent}) {
			return firstNonEmpty(m.PlatformMessageID, m.Key)
		}
	}
	return ""
}

// withoutResumeOffers 不把平台接收按钮交给AI当成候选人问题，真实文字和真正附件消息保持原顺序。
func withoutResumeOffers(messages []cloud.AutoReplyMessage) []cloud.AutoReplyMessage {
	out := make([]cloud.AutoReplyMessage, 0, len(messages))
	for _, message := range messages {
		if !cloud.IsAutoReplyResumeOfferMessage(message) {
			out = append(out, message)
		}
	}
	return out
}

// acceptResumeOffer 在当前已核实latest锚点下只同意一次，未知结果不补点，既有普通简历索要状态不阻挡主动邀约。
func (f *Flow) acceptResumeOffer(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot *model.AutoReplyConversationSnapshot) error {
	offer := visibleResumeOffer(*snapshot)
	snapshot.ResumeOfferMessageID = offer
	if offer == "" {
		return nil
	}
	if conversation.Status == "manual" || conversation.Status == "closed" || conversation.Status == "paused" {
		return fmt.Errorf("会话已转人工或停止，本轮没有接收简历")
	}
	key := cloud.AutoReplyResumeOfferSendKey(offer)
	status, _, err := f.Cloud.AutoReplySendReceipt(ctx, credentials(prepared), conversation.ID, key, 0)
	if err != nil {
		return err
	}
	if settledResumeOfferStatus(status) {
		return f.skipSettledResumeOffer(ctx, prepared, *conversation, snapshot, offer)
	}
	requester, supported := runtime.(model.AutoReplyInfoRequester)
	if !supported || !prepared.Platform.Behavior.AutoReplyNativeInfoRequests {
		return fmt.Errorf("原生简历邀请暂时无法核实，本轮没有点击或发送回复")
	}
	if err := f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
		return err
	}
	latest, opened, err := runtime.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, prepared.Platform, *snapshot)
	if err != nil || !opened {
		return errors.Join(err, fmt.Errorf("接收简历前原会话没有核实，暂不点击"))
	}
	anchor := firstNonEmpty(latest.PlatformMessageID, latest.Key)
	if anchor == "" {
		return fmt.Errorf("接收简历前没有可核实的新消息锚点")
	}
	history, err := f.Cloud.AutoReplyMessages(ctx, credentials(prepared), conversation.ID)
	if err != nil {
		return err
	}
	if candidateEndedAllContact(history) {
		// 停止联系的原文仍由普通流程处理，本轮只跳过平台同意操作。
		return nil
	}
	known := make(map[string]bool, len(history))
	for _, m := range history {
		known[firstNonEmpty(m.PlatformMessageID, m.Fingerprint)] = true
	}
	status, claimed, err := f.Cloud.ClaimAutoReplySend(ctx, credentials(prepared), conversation.ID, key, cloud.AutoReplyResumeOfferContent, 0)
	if err != nil {
		return err
	}
	if !claimed {
		if !settledResumeOfferStatus(status) {
			return fmt.Errorf("简历邀请的操作记录暂时无法核实")
		}
		// 同意回执与原文件待办已原子提交；旧邀请仍可见时不能把后续新附件待办倒退成旧文件。
		return f.skipSettledResumeOffer(ctx, prepared, *conversation, snapshot, offer)
	}
	unknown := func(cause error) error {
		if err := f.Cloud.FinishAutoReplySend(context.WithoutCancel(ctx), credentials(prepared), conversation.ID, key, 0, "unknown"); err != nil {
			return err
		}
		// 仅首次未知记录一次提示；它只暂停这个同意动作，不能挡住普通问题或使列表每圈重试。
		f.log(prepared.Request.TaskID, "resume_offer", "warning", time.Now(), fmt.Errorf("接收简历申请的结果待核实，我先不重复点击；其它问题继续处理：%v", cause))
		return f.skipSettledResumeOffer(ctx, prepared, *conversation, snapshot, offer)
	}
	started := time.Now()
	snapshot.ExpectedSendMessageKey = anchor
	result, err := requester.RequestAutoReplyCandidateInfo(cloud.WithAutoReplySendIntent(ctx, cloud.AutoReplySendIntentID(conversation.ID, key, 0)), f.Browser, prepared.Platform, *snapshot, model.CandidateInfoRequest{RequestResume: true})
	if err != nil {
		if errors.Is(err, model.ErrAutoReplySupersededBeforeSend) || errors.Is(err, cloud.ErrAutoReplyGoalChanged) {
			if releaseErr := f.Cloud.ReleaseUnsentAutoReply(ctx, credentials(prepared), conversation.ID, key, 0, cloud.AutoReplyResumeOfferContent); releaseErr != nil {
				return releaseErr
			}
			return err
		}
		return unknown(err)
	}
	if result.Status == model.AutoReplyInfoDeferred || result.Status == model.AutoReplyInfoUnavailable {
		return f.Cloud.ReleaseUnsentAutoReply(ctx, credentials(prepared), conversation.ID, key, 0, cloud.AutoReplyResumeOfferContent)
	}
	if !resumeOfferPDFEvidence(result, offer, known) {
		return unknown(fmt.Errorf("没有读到原邀请之外的新附件消息"))
	}
	window := result.EvidenceMessages
	if len(window) == 0 {
		// 兼容只回传单条证据的设备：同一已开会话内补读顺序窗口，不能把新文件直接追加到旧HR消息之后。
		window, _, err = runtime.ReadAutoReplyMessages(ctx, f.Browser, prepared.Platform, *snapshot, nil, 32)
		if err != nil {
			return unknown(err)
		}
	}
	if !resumeOfferWindowMatches(window, result, anchor, offer, known) {
		return unknown(fmt.Errorf("真实附件与原会话顺序窗口没有对上"))
	}
	messages, _, err := convertMessages(window)
	if err != nil {
		return unknown(err)
	}
	if _, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: messages}); err != nil {
		return unknown(err)
	}
	saved, err := f.Cloud.CommitAutoReplyResumeOffer(ctx, credentials(prepared), conversation.ID, offer, result.EvidenceMessageID)
	if err != nil {
		return unknown(err)
	}
	*conversation = saved
	snapshot.Messages, snapshot.ResumeCardAvailable, snapshot.ResumeSourceMessageID, snapshot.ResumeOfferMessageID = window, true, result.EvidenceMessageID, ""
	f.log(prepared.Request.TaskID, "resume_offer_received", "received", started, nil)
	return nil
}

// settledResumeOfferStatus 表示此邀请已经尝试过；未知与执行中只暂停该动作，不允许重新点击。
func settledResumeOfferStatus(status string) bool {
	return status == "sent" || status == "unknown" || status == "sending"
}

// skipSettledResumeOffer 只完成已落账本的原生控制卡前缀，普通问题之前的处理范围绝不越过。
func (f *Flow) skipSettledResumeOffer(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, snapshot *model.AutoReplyConversationSnapshot, offer string) error {
	snapshot.ResumeOfferMessageID = ""
	pending, err := f.Cloud.PendingAutoReplyMessages(ctx, credentials(prepared), conversation.ID)
	if err != nil {
		return err
	}
	last := ""
	for _, message := range pending {
		if !cloud.IsAutoReplyResumeOfferMessage(message) {
			break
		}
		key := firstNonEmpty(message.PlatformMessageID, message.Fingerprint)
		status, _, err := f.Cloud.AutoReplySendReceipt(ctx, credentials(prepared), conversation.ID, cloud.AutoReplyResumeOfferSendKey(key), 0)
		if err != nil {
			return err
		}
		if !settledResumeOfferStatus(status) {
			break
		}
		last = key
		if key == offer {
			break
		}
	}
	if last != "" {
		return f.Cloud.CompleteAutoReplyMessage(ctx, credentials(prepared), conversation.ID, last, "completed")
	}
	return nil
}

// resumeOfferReplyContext 仅把已尝试的原生邀请保留为真实尾部锚点，待答正文仍使用普通问题，所有原消息原样保留。
func (f *Flow) resumeOfferReplyContext(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, basedOn string, messages []cloud.AutoReplyMessage) (string, []cloud.AutoReplyMessage, error) {
	anchor := ""
	for index := len(snapshot.Messages) - 1; index >= 0; index-- {
		message := snapshot.Messages[index]
		if message.Direction == "system" {
			continue
		}
		if cloud.IsAutoReplyResumeOfferMessage(cloud.AutoReplyMessage{Direction: message.Direction, MessageType: message.MessageType, CardContent: message.CardContent}) {
			anchor = firstNonEmpty(message.PlatformMessageID, message.Key)
		}
		break
	}
	if anchor != "" && anchor == conversation.LastCandidateMessageKey && basedOn != "" && anchor != basedOn {
		status, _, err := f.Cloud.AutoReplySendReceipt(ctx, credentials(prepared), conversation.ID, cloud.AutoReplyResumeOfferSendKey(anchor), 0)
		if err != nil {
			return "", nil, err
		}
		before := false
		for _, message := range messages {
			key := firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID)
			if key == basedOn && message.Direction == "candidate" && !cloud.IsAutoReplyResumeOfferMessage(message) {
				before = true
			}
			if key == anchor {
				if before && settledResumeOfferStatus(status) && cloud.IsAutoReplyResumeOfferMessage(message) {
					basedOn = anchor
				} else {
					anchor = ""
				}
				break
			}
		}
	} else {
		anchor = ""
	}
	kept := make([]cloud.AutoReplyMessage, 0, len(messages))
	for _, message := range messages {
		if !cloud.IsAutoReplyResumeOfferMessage(message) || anchor != "" && firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID) == anchor {
			kept = append(kept, message)
		}
	}
	return basedOn, kept, nil
}

// resumeOfferPDFEvidence 只信本次同意后新出现的原生附件卡；旧PDF、邀请编号和普通文字都不能冒充下载来源。
func resumeOfferPDFEvidence(result model.AutoReplyInfoRequestResult, offer string, known map[string]bool) bool {
	m := result.EvidenceMessage
	if result.Status != model.AutoReplyInfoReceived || m == nil || result.EvidenceMessageID == "" || result.EvidenceMessageID == offer || known[result.EvidenceMessageID] || firstNonEmpty(m.PlatformMessageID, m.Key) != result.EvidenceMessageID || m.Direction != "candidate" || m.MessageType != "resume" {
		return false
	}
	var card struct {
		InfoType   string `json:"info_type"`
		InfoStatus string `json:"info_status"`
	}
	return json.Unmarshal(m.CardContent, &card) == nil && card.InfoType == "resume" && card.InfoStatus == "received"
}

// resumeOfferWindowMatches 核对新文件确在原DOM顺序中，已有HR锚点存在时不能省掉它而误把旧位置文件追加到末尾。
func resumeOfferWindowMatches(window []model.ConversationMessage, result model.AutoReplyInfoRequestResult, anchor, offer string, known map[string]bool) bool {
	if len(window) == 0 || len(window) > cloud.AutoReplyMaxHistoryMessages || result.EvidenceMessage == nil {
		return false
	}
	found, anchored := false, anchor == offer
	seen := map[string]bool{}
	for _, m := range window {
		key := firstNonEmpty(m.PlatformMessageID, m.Key)
		if key == "" || seen[key] {
			return false
		}
		seen[key] = true
		anchored = anchored || key == anchor
		if key == result.EvidenceMessageID {
			original := result.EvidenceMessage
			found = m.Direction == original.Direction && m.MessageType == original.MessageType && m.TextContent == original.TextContent && bytes.Equal(m.CardContent, original.CardContent) && sameResumeOfferTime(m.SentAt, original.SentAt)
		}
	}
	return found && anchored
}

// sameResumeOfferTime 要求附件证据和顺序窗口保留同一次读取的真实平台时间，缺一侧时间不能视为一致。
func sameResumeOfferTime(left, right *time.Time) bool {
	return left != nil && right != nil && left.Equal(*right)
}

// enqueueObservedResume 登记本轮确实读到且尚未整理的新附件来源，包括被平台插在已知HR消息之前的文件。
func (f *Flow) enqueueObservedResume(ctx context.Context, prepared shared.PreparedTask, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) error {
	if state.Candidate == nil || !snapshot.ResumeCardAvailable || snapshot.ResumeSourceMessageID == "" || state.Candidate.ResumeStructured && state.Candidate.ResumeSourceMessageID == snapshot.ResumeSourceMessageID {
		return nil
	}
	for _, m := range snapshot.Messages {
		if m.Direction == "candidate" && m.MessageType == "resume" && firstNonEmpty(m.PlatformMessageID, m.Key) == snapshot.ResumeSourceMessageID {
			saved, err := f.Cloud.EnqueueAutoReplyResumeRetry(ctx, credentials(prepared), conversation.ID, snapshot.ResumeSourceMessageID)
			if err == nil {
				*conversation = saved
			}
			return err
		}
	}
	return nil
}
