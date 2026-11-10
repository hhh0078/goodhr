// Package auto_reply 本文件在普通答复确认后执行平台原生资料索要，复用服务器会话状态和发送账本防止跨轮重复操作。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// infoContactState 是从完整服务器记录生成的防重复摘要，不向模型或日志重复提供联系方式。
type infoContactState struct {
	Received bool `json:"received"`
	Asked    bool `json:"asked"`
	Declined bool `json:"declined"`
}

var chatWechatValue = regexp.MustCompile(`(?i)(?:微信(?:号)?|wechat)\s*(?:是|为|[:：])\s*([a-z][a-z0-9_\-]{5,31})`)

// asksForWechat 识别配置话术及明确的索要问句，不把普通提及微信当成索要。
func asksForWechat(text, configured string) bool {
	return asksForInfoInSameClause(text, configured,
		[]string{"微信", "wechat"},
		[]string{"方便", "提供", "留个", "给个", "发我", "加个", "交换", "加一下", "加您", "加你", "可以加", "能否加", "加微信", "留微信", "多少"})
}

// asksForResume 仅识别明确索要简历的问句，签收或讨论已有简历不算重复索要。
func asksForResume(text, configured string) bool {
	return asksForInfoInSameClause(text, configured,
		[]string{"简历"},
		[]string{"方便发", "发一份", "发份", "给一份", "提供", "索要", "索取", "发送一下", "发给我", "发我", "投递一下"})
}

// inspectInfoContacts 在历史正文被裁剪前结合已有资料、原生卡片和持久请求状态生成三类摘要。
func inspectInfoContacts(input ReplyContext) map[string]infoContactState {
	phone := inspectPhoneContact(input)
	result := map[string]infoContactState{"phone": {Received: phone.Received, Asked: phone.Asked, Declined: phone.Declined}, "resume": {Received: input.CandidateState.HasResumeAttachment || input.PageSnapshot.ResumeCardAvailable, Asked: input.Conversation.Status == "waiting_resume"}, "wechat": {}}
	if c := input.CandidateState.Candidate; c != nil {
		value := result["wechat"]
		value.Received = strings.TrimSpace(c.Wechat) != ""
		result["wechat"] = value
	}
	for kind, progress := range input.Conversation.InfoRequests {
		value, valid := result[kind]
		if !valid {
			continue
		}
		switch progress.Status {
		case model.AutoReplyInfoReceived:
			value.Received = true
		case model.AutoReplyInfoRequested, model.AutoReplyInfoAlreadyRequested, "unknown":
			value.Asked = true
		case model.AutoReplyInfoDeclined:
			value.Declined = true
		}
		result[kind] = value
	}
	previous := map[string]bool{}
	for _, message := range input.Messages {
		var card struct {
			InfoType   string `json:"info_type"`
			InfoStatus string `json:"info_status"`
		}
		_ = json.Unmarshal(message.CardContent, &card)
		if value, valid := result[card.InfoType]; valid {
			switch card.InfoStatus {
			case model.AutoReplyInfoReceived:
				value.Received = true
			case model.AutoReplyInfoRequested, model.AutoReplyInfoAlreadyRequested:
				value.Asked = true
			case model.AutoReplyInfoDeclined:
				value.Declined = true
			}
			result[card.InfoType] = value
		}
		text := strings.TrimSpace(message.TextContent)
		if message.Direction == "self" {
			previous["resume"] = asksForResume(text, fixedReplyPhrase(input.Position.Config, "request_resume"))
			previous["wechat"] = asksForWechat(text, fixedReplyPhrase(input.Position.Config, "request_wechat"))
			for _, kind := range []string{"resume", "wechat"} {
				value := result[kind]
				value.Asked = value.Asked || previous[kind]
				result[kind] = value
			}
		} else if message.Direction == "candidate" {
			value := result["wechat"]
			value.Received = value.Received || chatWechatValue.MatchString(text)
			result["wechat"] = value
			for _, kind := range []string{"resume", "wechat"} {
				label := "简历"
				if kind == "wechat" {
					label = "微信"
				}
				if (previous[kind] || strings.Contains(text, label)) && containsAnyText(text, "不方便", "不想", "不愿", "不发", "不提供", "不给", "不加", "不交换", "暂不", "先不", "拒绝") {
					value := result[kind]
					value.Declined = true
					result[kind] = value
				}
				previous[kind] = false
			}
		}
	}
	return result
}

// validateNativeInfoProposal 校验原生动作提议，旧平台保持文本行为，原生平台禁止把索要夹进答案绕过按钮条件。
func validateNativeInfoProposal(input ReplyContext, request model.CandidateInfoRequest, messages []ReplyMessage) error {
	requested := request.RequestResume || request.RequestPhone || request.RequestWechat
	if !input.NativeInfoRequests {
		if request.RequestPhone || request.RequestWechat {
			return argumentError("当前平台未启用原生联系方式索要，请沿用已有沟通方式")
		}
		if request.RequestResume && (input.ConditionsOnly || len(messages) != 1 || messages[0].Type != "answer") {
			return argumentError("需先回答本轮问题，再按实际需要索要附件")
		}
		return nil
	}
	for _, message := range messages {
		if message.RequestsResume || asksForResume(message.Content, fixedReplyPhrase(input.Position.Config, "request_resume")) || asksForPhone(message.Content, fixedReplyPhrase(input.Position.Config, "request_phone")) || asksForWechat(message.Content, fixedReplyPhrase(input.Position.Config, "request_wechat")) {
			return argumentError("当前平台通过原生按钮索要资料。先正常回答，不要把索要混入正文；需要手机或微信时填写info_requests，简历由固定流程处理。")
		}
	}
	if !requested {
		return nil
	}
	if input.ConditionsOnly || len(messages) != 1 || messages[0].Type != "answer" {
		return argumentError("资料索要需要先送达一条普通答复，本轮不要再混入条件追问")
	}
	contacts := input.InfoContact
	if contacts == nil {
		contacts = inspectInfoContacts(input)
	}
	for kind, selected := range map[string]bool{"resume": request.RequestResume, "phone": request.RequestPhone, "wechat": request.RequestWechat} {
		if !selected {
			continue
		}
		state := contacts[kind]
		if fixedReplyPhrase(input.Position.Config, "request_"+kind) == "" || state.Received || state.Asked || state.Declined {
			return argumentError("该资料已获得、已申请、被拒绝或话术已停用，请只回答本轮问题")
		}
	}
	return nil
}

// nativeInfoRequest 生成只含一种资料的既有请求类型，动作顺序固定，不由AI直接操作页面。
func nativeInfoRequest(kind string) model.CandidateInfoRequest {
	return model.CandidateInfoRequest{RequestResume: kind == "resume", RequestPhone: kind == "phone", RequestWechat: kind == "wechat"}
}

// requestInfoAfterRecoveredResume 只有简历已整理且仍缺电话时复用最近已送达的签收，不重答历史或凭旧卡片猜联系方式。
func (f *Flow) requestInfoAfterRecoveredResume(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) error {
	if !prepared.Platform.Behavior.AutoReplyNativeInfoRequests || state.Candidate == nil || !state.Candidate.ResumeStructured || state.Candidate.Phone != "" {
		return nil
	}
	latest, opened, err := runtime.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil || !opened {
		return err
	}
	if latest.Direction != "self" || latest.MessageType != "text" || strings.TrimSpace(latest.TextContent) == "" {
		return nil
	}
	_, err = f.requestNativeInfoAfterAnswer(ctx, prepared, runtime, position, conversation, snapshot, state, conversation.LastCandidateMessageKey, latest.TextContent, model.CandidateInfoRequest{})
	return err
}

// requestNativeInfoAfterAnswer 先核实原答案已送达，再按新入站推进尚未完成的资料动作；未知结果永不补点。
func (f *Flow) requestNativeInfoAfterAnswer(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, basedOn, previousContent string, proposed model.CandidateInfoRequest) (bool, error) {
	if !prepared.Platform.Behavior.AutoReplyNativeInfoRequests {
		return false, nil
	}
	requester, ok := runtime.(model.AutoReplyInfoRequester)
	if !ok {
		return false, fmt.Errorf("当前平台的原生资料索要能力未能保留")
	}
	ready, anchor, err := f.latestMessageAllowsFollowup(ctx, runtime, prepared, snapshot, previousContent)
	if err != nil || !ready {
		return false, err
	}
	if err = f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
		return false, err
	}
	current, found, err := f.Cloud.AutoReplyAnswerConversation(ctx, credentials(prepared), conversation.ID)
	if err != nil || !found {
		return false, err
	}
	if current.LastCandidateMessageKey != basedOn {
		return false, nil
	}
	history, err := f.Cloud.AutoReplyMessages(ctx, credentials(prepared), current.ID)
	if err != nil {
		return false, err
	}
	if candidateEndedAllContact(history) {
		return false, nil
	}
	input := ReplyContext{Position: position, Conversation: current, CandidateState: state, PageSnapshot: snapshot, Messages: history}
	contacts := inspectInfoContacts(input)
	// 在线资料优先；仅执行AI已说明确有必要的附件提议或先前明确暂缓的同一待办。
	// 简历已整理但缺电话时，正式入库门槛有明确依据；不在解析附件前猜测还缺手机号。
	if state.Candidate != nil && state.Candidate.ResumeStructured && !contacts["phone"].Received && !contacts["phone"].Asked && !contacts["phone"].Declined && fixedReplyPhrase(position.Config, "request_phone") != "" {
		proposed.RequestPhone = true
	}
	if proposed.RequestResume || proposed.RequestPhone || proposed.RequestWechat {
		if err = f.Cloud.PlanAutoReplyInfoRequests(ctx, credentials(prepared), current.ID, basedOn, proposed); err != nil {
			return false, err
		}
	}
	current, found, err = f.Cloud.AutoReplyAnswerConversation(ctx, credentials(prepared), current.ID)
	if err != nil || !found {
		return false, err
	}
	handled := false
	receiptAnchor := ""
	for _, kind := range []string{"resume", "phone", "wechat"} {
		progress, planned := current.InfoRequests[kind]
		if !planned {
			continue
		}
		contact := contacts[kind]
		known := ""
		if contact.Received {
			known = model.AutoReplyInfoReceived
		} else if contact.Declined {
			known = model.AutoReplyInfoDeclined
		} else if contact.Asked && progress.Status != "unknown" {
			known = model.AutoReplyInfoAlreadyRequested
		}
		if known != "" {
			if err = f.Cloud.SetAutoReplyInfoRequestProgress(ctx, credentials(prepared), current.ID, kind, known, basedOn); err != nil {
				return handled, err
			}
			continue
		}
		if progress.Status != "planned" && progress.Status != model.AutoReplyInfoDeferred && progress.Status != model.AutoReplyInfoUnavailable || progress.LastAttemptMessageKey == basedOn || fixedReplyPhrase(position.Config, "request_"+kind) == "" {
			continue
		}
		handled = true
		latest, opened, readErr := runtime.ReadOpenAutoReplyLatestMessage(ctx, f.Browser, prepared.Platform, snapshot)
		if readErr != nil {
			return handled, readErr
		}
		latestKey := firstNonEmpty(latest.PlatformMessageID, latest.Key)
		if !opened || latest.Direction == "candidate" || latestKey == "" || latestKey != anchor && (receiptAnchor == "" || latestKey != receiptAnchor) {
			return handled, nil
		}
		anchor = latestKey
		snapshot.ExpectedSendMessageKey = anchor
		if err = f.Cloud.CompleteAutoReplyMessage(ctx, credentials(prepared), current.ID, basedOn, "sent"); err != nil {
			return handled, err
		}
		key, content := "info-request:"+kind, "平台资料索要："+kind
		status, claimed, claimErr := f.Cloud.ClaimAutoReplySend(ctx, credentials(prepared), current.ID, key, content, 0)
		if claimErr != nil {
			return handled, claimErr
		}
		if !claimed {
			next := "unknown"
			if status == "sent" {
				next = model.AutoReplyInfoAlreadyRequested
			}
			if err = f.Cloud.SetAutoReplyInfoRequestProgress(ctx, credentials(prepared), current.ID, kind, next, basedOn); err != nil {
				return handled, err
			}
			continue
		}
		started := time.Now()
		result, requestErr := requester.RequestAutoReplyCandidateInfo(cloud.WithAutoReplySendIntent(ctx, cloud.AutoReplySendIntentID(current.ID, key, 0)), f.Browser, prepared.Platform, snapshot, nativeInfoRequest(kind))
		if requestErr != nil {
			if errors.Is(requestErr, model.ErrAutoReplySupersededBeforeSend) {
				if err = f.Cloud.ReleaseUnsentAutoReply(ctx, credentials(prepared), current.ID, key, 0, content); err != nil {
					return handled, err
				}
				return handled, f.Cloud.SetAutoReplyInfoRequestProgress(ctx, credentials(prepared), current.ID, kind, model.AutoReplyInfoDeferred, basedOn)
			}
			_ = f.Cloud.FinishAutoReplySend(context.WithoutCancel(ctx), credentials(prepared), current.ID, key, 0, "unknown")
			_ = f.Cloud.SetAutoReplyInfoRequestProgress(context.WithoutCancel(ctx), credentials(prepared), current.ID, kind, "unknown", basedOn)
			f.log(prepared.Request.TaskID, "request_info_"+kind, "warning", started, fmt.Errorf("原生资料索要结果未确认，已阻止重复请求"))
			return handled, fmt.Errorf("%w：原生资料索要结果未确认", errAutoReplySendNeedsReview)
		}
		switch result.Status {
		case model.AutoReplyInfoDeferred, model.AutoReplyInfoUnavailable:
			if err = f.Cloud.ReleaseUnsentAutoReply(ctx, credentials(prepared), current.ID, key, 0, content); err != nil {
				return handled, err
			}
		case model.AutoReplyInfoRequested, model.AutoReplyInfoAlreadyRequested, model.AutoReplyInfoReceived, model.AutoReplyInfoDeclined:
			if err = f.Cloud.FinishAutoReplySend(ctx, credentials(prepared), current.ID, key, 0, "sent", result.EvidenceMessageID); err != nil {
				return handled, err
			}
		default:
			_ = f.Cloud.FinishAutoReplySend(context.WithoutCancel(ctx), credentials(prepared), current.ID, key, 0, "unknown")
			_ = f.Cloud.SetAutoReplyInfoRequestProgress(context.WithoutCancel(ctx), credentials(prepared), current.ID, kind, "unknown", basedOn)
			return handled, fmt.Errorf("%w：资料动作回执状态不完整", errAutoReplySendNeedsReview)
		}
		// 原生动作已经读到回执，直接保存该原消息，不为时间线再次读取整段聊天。
		if evidence := result.EvidenceMessage; evidence != nil {
			if !nativeInfoEvidenceMatches(kind, result) {
				return handled, fmt.Errorf("资料动作回执与来源消息不一致")
			}
			messages, _, convertErr := convertMessages([]model.ConversationMessage{*evidence})
			if convertErr != nil {
				return handled, convertErr
			}
			if _, syncErr := f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{ConversationID: current.ID, Messages: messages}); syncErr != nil {
				return handled, syncErr
			}
		}
		if err = f.Cloud.SetAutoReplyInfoRequestProgress(ctx, credentials(prepared), current.ID, kind, result.Status, basedOn); err != nil {
			return handled, err
		}
		if kind == "resume" && (result.Status == model.AutoReplyInfoRequested || result.Status == model.AutoReplyInfoAlreadyRequested) {
			if err = f.saveResumeConversationStatus(ctx, prepared, conversation, "waiting_resume"); err != nil {
				return handled, err
			}
		}
		if result.Status == model.AutoReplyInfoReceived && (kind == "phone" || kind == "wechat") && (result.Phone != "" || result.Wechat != "") {
			if err = f.Cloud.SaveAutoReplyNativeContact(ctx, credentials(prepared), current.ID, kind, result); err != nil {
				return handled, err
			}
			if kind == "phone" {
				if err = f.publishOnlineCandidateIfPhoneReady(ctx, prepared, position, current, snapshot, state); err != nil {
					return handled, err
				}
			}
		}
		f.log(prepared.Request.TaskID, "request_info_"+kind, result.Status, started, nil)
		receiptAnchor = result.EvidenceMessageID
	}
	return handled, nil
}

// nativeInfoEvidenceMatches 核对动作回执的真实消息键、卡片种类、状态和已展示值，泛申请仅接受本次明确的联系方式动作。
func nativeInfoEvidenceMatches(kind string, result model.AutoReplyInfoRequestResult) bool {
	evidence := result.EvidenceMessage
	if evidence == nil || result.EvidenceMessageID == "" || firstNonEmpty(evidence.PlatformMessageID, evidence.Key) != result.EvidenceMessageID {
		return false
	}
	var card struct {
		InfoType   string `json:"info_type"`
		InfoStatus string `json:"info_status"`
		Phone      string `json:"phone"`
		Wechat     string `json:"wechat"`
	}
	if json.Unmarshal(evidence.CardContent, &card) != nil {
		return false
	}
	status := result.Status
	if status == model.AutoReplyInfoAlreadyRequested {
		status = model.AutoReplyInfoRequested
	}
	if card.InfoStatus != status || card.InfoType != kind && !(card.InfoType == "contact" && status == model.AutoReplyInfoRequested && (kind == "phone" || kind == "wechat")) {
		return false
	}
	if status == model.AutoReplyInfoReceived {
		switch kind {
		case "phone":
			return card.Phone != "" && card.Phone == result.Phone
		case "wechat":
			return card.Wechat != "" && card.Wechat == result.Wechat
		}
	}
	return true
}

// candidateEndedAllContact 将拒绝继续招聘沟通与只拒绝某类资料区分，后者不会错误封掉其他正常问答。
func candidateEndedAllContact(messages []cloud.AutoReplyMessage) bool {
	for _, message := range messages {
		if message.Direction == "candidate" && containsAnyText(message.TextContent, "不考虑", "不感兴趣", "不用联系", "不要联系", "不找工作", "停止联系") {
			return true
		}
	}
	return false
}
