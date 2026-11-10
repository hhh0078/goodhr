// Package auto_reply 本文件在首次来信答复前准备在线基础资料，复用服务器缓存；图片仅服务器AI本轮识别，不作为附件或本机业务数据。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// OnlineProfileRecognizer 仅由服务器AI处理器实现图片转录，设备不调用模型、不启动本机OCR。
type OnlineProfileRecognizer interface {
	RecognizeOnlineProfile(context.Context, ReplyContext, [][]byte) (model.AutoReplyOnlineProfile, error)
}

// RecognizeOnlineProfile 使用本轮动态简历提示词与既有计费客户端一次提取在线截图，失败时保留未回复来信。
func (r *AIResponder) RecognizeOnlineProfile(ctx context.Context, input ReplyContext, images [][]byte) (model.AutoReplyOnlineProfile, error) {
	if r == nil || r.AI == nil {
		return model.AutoReplyOnlineProfile{}, fmt.Errorf("在线图片需要服务器AI识别，当前AI尚未准备好")
	}
	billingCtx := shared.WithAIBilling(ctx, r.Logger, input.TaskID, input.Position.Position.ID, input.PageSnapshot.CandidateName, "AI 在线简历识别")
	return r.AI.ReadAutoReplyOnlineProfileVision(billingCtx, input.AIConfig, images, input.Position.Config.Prompts.Resume)
}

// prepareOnlineProfile 只在当前候选人没有服务器资料缓存时读取，优先在线基础资料而不是一上来索要附件。
func (f *Flow) prepareOnlineProfile(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) (cloud.AutoReplyCandidateState, error) {
	if state.Candidate != nil && state.Candidate.OnlineProfile != nil && state.Candidate.OnlineProfile.SourceKey != "" {
		cache := state.Candidate.OnlineProfile
		if !position.Position.OutputStructuredResume || cache.CandidateID != "" {
			return state, nil
		}
		// 岗位后来开启入库时重用同代服务器缓存，避免再次打开平台和再次调用图片AI。
		if _, err := f.Cloud.SaveAutoReplyOnlineProfile(ctx, credentials(prepared), cloud.AutoReplyOnlineProfileInput{PositionID: position.Position.ID, ConversationID: conversation.ID, PlatformID: prepared.Platform.ID, PlatformCandidateID: snapshot.PlatformCandidateID, PlatformAccountID: snapshot.PlatformAccountID, Profile: cache.Profile}); err != nil {
			return state, onlineProfileStageError(err, "保存在线资料", "服务器未接受在线资料保存")
		}
		return f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID, PlatformThreadID: snapshot.PlatformThreadID})
	}
	reader, ok := runtime.(model.AutoReplyOnlineProfileReader)
	if !ok {
		return state, nil
	}
	started := time.Now()
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	profile, err := reader.ReadAutoReplyOnlineProfile(readCtx, f.Browser, prepared.Platform, snapshot)
	if errors.Is(err, model.ErrAutoReplyOnlineProfileUnavailable) {
		f.log(prepared.Request.TaskID, "online_profile", "unavailable", started, nil)
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("读取在线基础资料失败，尚未索要附件：%w", err)
	}
	expectedMode := model.DetailRecognitionMode(prepared.Platform.ID)
	if profile.Mode != expectedMode {
		return state, fmt.Errorf("平台在线资料识别方式未按固定规则准备，本轮没有继续")
	}
	if profile.Mode == "ai" {
		vision, ok := f.Responder.(OnlineProfileRecognizer)
		if !ok {
			return state, fmt.Errorf("在线图片识别的服务器AI尚未准备好")
		}
		if len(profile.Screenshots) == 0 {
			return state, fmt.Errorf("在线AI资料缺少实际可见截图，尚未索要附件")
		}
		parsed, readErr := vision.RecognizeOnlineProfile(ctx, ReplyContext{TaskID: prepared.Request.TaskID, Credentials: credentials(prepared), AIConfig: prepared.Position.AI, Position: position, Conversation: conversation, PageSnapshot: snapshot}, profile.Screenshots)
		if readErr != nil {
			return state, onlineProfileStageError(fmt.Errorf("服务器在线图片识别失败：%w", readErr), "AI识别", "服务器在线图片识别未完成")
		}
		if strings.TrimSpace(parsed.CandidateName) == "" || strings.TrimSpace(parsed.CandidateName) != strings.TrimSpace(snapshot.CandidateName) {
			return state, onlineProfileStageError(fmt.Errorf("在线简历AI姓名与当前已核实聊天不一致，资料尚未保存"), "姓名核验", "识别姓名与当前聊天不一致")
		}
		parsed.SourceKey, parsed.Mode = profile.SourceKey, profile.Mode
		profile = parsed
	}
	// 图片仅为当次识别材料，绝不下发给匹配接口或写入权威候选人缓存。
	profile.Screenshots = nil
	if _, err = f.Cloud.SaveAutoReplyOnlineProfile(ctx, credentials(prepared), cloud.AutoReplyOnlineProfileInput{PositionID: position.Position.ID, ConversationID: conversation.ID, PlatformID: prepared.Platform.ID, PlatformCandidateID: snapshot.PlatformCandidateID, PlatformAccountID: snapshot.PlatformAccountID, Profile: profile}); err != nil {
		return state, onlineProfileStageError(fmt.Errorf("保存在线基础资料失败，尚未开始候选人答复：%w", err), "保存在线资料", "服务器未接受在线资料保存")
	}
	state, err = f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID, PlatformThreadID: snapshot.PlatformThreadID})
	f.log(prepared.Request.TaskID, "online_profile", "success", started, err)
	return state, err
}

// onlineProfileStageError 仅附固定阶段、原因及已知HTTP状态，保留原错误链供排障，不把AI原文、地址或凭据写入安全审计。
func onlineProfileStageError(cause error, phase, reason string) error {
	detail := failurediag.Detail{Source: "在线资料", Operation: "准备在线基础资料", Phase: phase, Reason: reason, Impact: "资料尚未确认，本轮候选人答复尚未发送"}
	var api *cloud.APIError
	if errors.As(cause, &api) && api.StatusCode >= 400 && api.StatusCode <= 599 {
		detail.HTTPStatus = api.StatusCode
	}
	return failurediag.Wrap(cause, detail)
}

// publishOnlineCandidateIfPhoneReady 在在线资料已建立且取得真实手机号后续写同一服务器简历，不强制再等附件，也不把缺手机号临时资料冒充正式资料。
func (f *Flow) publishOnlineCandidateIfPhoneReady(ctx context.Context, prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) error {
	if state.Candidate == nil || state.Candidate.OnlineProfile == nil || !position.Position.OutputStructuredResume {
		return nil
	}
	current, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID, PlatformThreadID: snapshot.PlatformThreadID})
	if err != nil {
		return err
	}
	candidate := current.Candidate
	if candidate == nil || completeOnlinePhone(candidate.Phone) == "" || candidate.OnlineProfile == nil || candidate.OnlineProfile.CandidateID == "" || candidate.OnlineProfile.ProfileKey == "" {
		return nil
	}
	input := cloud.AutoReplyCandidateInput{StructuredCandidate: storedStructuredCandidate(candidate), LocalCandidateID: candidate.ID, PositionID: position.Position.ID, PlatformID: prepared.Platform.ID, PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID, Gender: candidate.Gender, BirthYMPrecision: candidate.BirthYMPrecision, OnlineProfileCandidateID: candidate.OnlineProfile.CandidateID, OnlineProfileKey: candidate.OnlineProfile.ProfileKey}
	f.queueStructuredCandidateSync(ctx, prepared, position, candidate.ID, input)
	return nil
}

// hasExplicitRequiredMismatch 只认可当前HR必要条件及已校验的真实反证，缺字段或AI自建目标不会触发不符话术。
func hasExplicitRequiredMismatch(position cloud.AutoReplyPositionSnapshot, items []cloud.CandidateConfirmationItem) bool {
	active, _ := configuredConfirmations(position, items)
	for _, item := range active {
		if item.ItemType == "required" && item.Status == "unmatched" && item.SourceRef != "" && item.EvidenceText != "" && !personalAttributeCondition(item.Content) {
			return true
		}
	}
	return false
}

// personalAttributeCondition 识别应由HR审慎确认的个人属性条件，不能仅凭这些属性自动发出淘汰消息。
func personalAttributeCondition(content string) bool {
	return containsAnyText(strings.ToLower(content), "年龄", "周岁", "岁以下", "岁以上", "性别", "男性", "女性", "男士", "女士", "婚姻", "已婚", "未婚", "生育", "怀孕", "民族", "宗教", "残疾", "户籍", "age", "gender", "pregnan", "marital", "religion", "ethnic", "disabil")
}

// hasPersonalAttributeMismatch 把有证据的个人属性反证交给HR，不让AI把它独立作为自动拒绝的依据。
func hasPersonalAttributeMismatch(position cloud.AutoReplyPositionSnapshot, items []cloud.CandidateConfirmationItem) bool {
	active, _ := configuredConfirmations(position, items)
	for _, item := range active {
		if item.ItemType == "required" && item.Status == "unmatched" && item.SourceRef != "" && item.EvidenceText != "" && personalAttributeCondition(item.Content) {
			return true
		}
	}
	return false
}

// completeOnlinePhone 只接受完整展示的数字及常见格式字符，掩码不能被剥成短号码后触发在线转正式或推荐。
func completeOnlinePhone(value string) string {
	value = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "＋", "+").Replace(strings.TrimSpace(value))
	normalized := normalizeAutoReplyPhone(value)
	if normalized == "" || normalized != value {
		return ""
	}
	return normalized
}
