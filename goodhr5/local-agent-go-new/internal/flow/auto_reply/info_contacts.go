// Package auto_reply 本文件把新读到的原生交换卡片和候选人明确提供的联系方式同步到服务器，避免依赖下一次索要才能保存。
package auto_reply

import (
	"context"
	"encoding/json"
	"strings"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// syncNativeChatContacts 只接受当前已核身消息窗口的完整值，删除前消息已由平台HistoryAfter边界排除。
func (f *Flow) syncNativeChatContacts(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) (cloud.AutoReplyCandidateState, error) {
	if !prepared.Platform.Behavior.AutoReplyNativeInfoRequests || state.Candidate == nil {
		return state, nil
	}
	changed := false
	for _, message := range snapshot.Messages {
		if message.Direction != "candidate" && message.Direction != "system" {
			continue
		}
		var card struct {
			InfoType   string `json:"info_type"`
			InfoStatus string `json:"info_status"`
			Phone      string `json:"phone"`
			Wechat     string `json:"wechat"`
			Summary    string `json:"summary"`
			OwnerName  string `json:"owner_name"`
		}
		_ = json.Unmarshal(message.CardContent, &card)
		key := firstNonEmpty(message.PlatformMessageID, message.Key)
		if key == "" {
			continue
		}
		if card.InfoType != "phone" && card.InfoType != "wechat" {
			if message.Direction != "candidate" || message.MessageType != "text" {
				continue
			}
			phone, _, wechat := common.ParseConfiguredResumeContacts(message.TextContent)
			if onlyPhoneNumber([]cloud.AutoReplyMessage{{Direction: "candidate", MessageType: "text", TextContent: message.TextContent}}) {
				phone = phoneDigits(mainlandChatMobile.FindString(message.TextContent))
			}
			if phone != "" {
				card.InfoType, card.Phone, card.InfoStatus = "phone", phone, model.AutoReplyInfoReceived
			} else if wechat != "" {
				card.InfoType, card.Wechat, card.InfoStatus = "wechat", wechat, model.AutoReplyInfoReceived
			} else {
				continue
			}
		}
		if card.InfoStatus != model.AutoReplyInfoReceived || card.Phone == "" && card.Wechat == "" {
			continue
		}
		if card.OwnerName != "" && strings.TrimSpace(card.OwnerName) != strings.TrimSpace(conversation.CandidateName) {
			continue
		}
		existing, incoming := state.Candidate.Phone, card.Phone
		if card.InfoType == "wechat" {
			existing, incoming = state.Candidate.Wechat, card.Wechat
		}
		if existing != "" && incoming != "" && !cloud.AutoReplyContactValuesEqual(card.InfoType, existing, incoming) {
			if err := f.Cloud.RecordAutoReplyContactConflict(ctx, credentials(prepared), conversation.ID, card.InfoType, key); err != nil {
				return state, err
			}
			continue // 旧卡片或不同来源的号码冲突只待核对，不覆盖现值、不阻断正常问答。
		}
		// 同值仍需收敛未知的交换结果；只有服务器已确认收到时才跳过重复保存。
		sameValue := cloud.AutoReplyContactValuesEqual(card.InfoType, existing, incoming)
		if sameValue && conversation.InfoRequests[card.InfoType].Status == model.AutoReplyInfoReceived {
			continue
		}
		if err := f.Cloud.SetAutoReplyInfoRequestProgress(ctx, credentials(prepared), conversation.ID, card.InfoType, model.AutoReplyInfoReceived, key); err != nil {
			return state, err
		}
		if sameValue && existing != incoming {
			changed = true // 仅格式不同不再写一次联系方式，保留原格式并收敛真实交换状态。
			continue
		}
		result := model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, EvidenceMessageID: key, Phone: card.Phone, Wechat: card.Wechat}
		if err := f.Cloud.SaveAutoReplyNativeContact(ctx, credentials(prepared), conversation.ID, card.InfoType, result); err != nil {
			return state, err
		}
		changed = true
	}
	if !changed {
		return state, nil
	}
	return f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID, PlatformThreadID: snapshot.PlatformThreadID})
}
