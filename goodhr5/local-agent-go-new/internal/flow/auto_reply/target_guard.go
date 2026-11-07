// Package auto_reply 本文件把单会话限制放在平台动作边界，覆盖打开、恢复、读取、索要和每次发送。
package auto_reply

import (
	"context"
	"errors"
	"fmt"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// targetedAutoReplyRuntime 在原有平台能力之前执行限制，不增加浏览器通道或自行猜测平台选择器。
type targetedAutoReplyRuntime struct {
	model.AutoReplyRuntime
	identity model.AutoReplyIdentityVerifier
	target   shared.AutoReplyTarget
}

// restrictAutoReplyRuntime 保留普通任务行为，限制任务必须由平台提供当前页面稳定身份校验能力。
func restrictAutoReplyRuntime(runtime model.AutoReplyRuntime, target *shared.AutoReplyTarget) (model.AutoReplyRuntime, error) {
	if err := shared.ValidateAutoReplyTarget("auto_reply", target); err != nil {
		return nil, err
	}
	if target == nil {
		return runtime, nil
	}
	identity, ok := runtime.(model.AutoReplyIdentityVerifier)
	if !ok {
		return nil, fmt.Errorf("当前平台还不能核对单会话稳定身份，本次限制任务没有启动")
	}
	return &targetedAutoReplyRuntime{AutoReplyRuntime: runtime, identity: identity, target: *target}, nil
}

// targetConversationAllowed 同时匹配完整姓名和平台会话编号，缺失编号或同名的其他会话均不放行。
func targetConversationAllowed(target *shared.AutoReplyTarget, conversation model.Conversation) bool {
	if target == nil {
		return true
	}
	return conversation.Name == target.CandidateName && conversation.PlatformThreadID == target.PlatformThreadID &&
		(conversation.Key == "" || conversation.Key == target.PlatformThreadID) &&
		(conversation.PlatformCandidateID == "" || conversation.PlatformCandidateID == target.PlatformCandidateID)
}

// checkSnapshot 拒绝错人、缺失身份和嵌套会话信息冲突，防止底层恢复会话时落到限制外。
func (r *targetedAutoReplyRuntime) checkSnapshot(snapshot model.AutoReplyConversationSnapshot) error {
	if snapshot.CandidateName != r.target.CandidateName || snapshot.PlatformThreadID != r.target.PlatformThreadID || snapshot.PlatformCandidateID != r.target.PlatformCandidateID {
		return fmt.Errorf("当前会话不属于本次指定候选人，已阻止页面动作")
	}
	conversation := snapshot.Conversation
	if conversation.Name == "" {
		conversation.Name = snapshot.CandidateName
	}
	if conversation.PlatformThreadID == "" {
		conversation.PlatformThreadID = snapshot.PlatformThreadID
	}
	if !targetConversationAllowed(&r.target, conversation) {
		return fmt.Errorf("会话恢复信息与本次指定候选人不一致，已阻止页面动作")
	}
	return nil
}

// checkPage 复用平台只读校验，在每个动作前重新证明当前页面候选人编号。
func (r *targetedAutoReplyRuntime) checkPage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if err := r.checkSnapshot(snapshot); err != nil {
		return err
	}
	return r.identity.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
}

// InitializeAutoReplyPage 把任务锁定的身份传给初始化；旧附件挡住联系人时只能收起目标自己的预览。
func (r *targetedAutoReplyRuntime) InitializeAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config, _ *model.AutoReplyConversationSnapshot) error {
	target := model.AutoReplyConversationSnapshot{CandidateName: r.target.CandidateName, PlatformCandidateID: r.target.PlatformCandidateID, PlatformThreadID: r.target.PlatformThreadID}
	return r.AutoReplyRuntime.InitializeAutoReplyPage(ctx, browser, cfg, &target)
}

// OpenAutoReplyConversation 只允许按目标会话编号打开，并在返回任何聊天资料前再次校验实际页面身份。
func (r *targetedAutoReplyRuntime) OpenAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation, known []string, limit int) (model.AutoReplyConversationSnapshot, error) {
	if !targetConversationAllowed(&r.target, conversation) {
		return model.AutoReplyConversationSnapshot{}, fmt.Errorf("待打开会话不属于本次指定候选人，已阻止打开")
	}
	conversation.PlatformCandidateID = r.target.PlatformCandidateID
	snapshot, err := r.AutoReplyRuntime.OpenAutoReplyConversation(ctx, browser, cfg, conversation, known, limit)
	if err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	if err = r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return model.AutoReplyConversationSnapshot{}, err
	}
	return snapshot, nil
}

// InspectAutoReplyContact 删后核身同样受单会话约束，必须复核实际页面编号才能继续。
func (r *targetedAutoReplyRuntime) InspectAutoReplyContact(ctx context.Context, browser model.Browser, cfg model.Config, conversation model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	inspector, ok := r.AutoReplyRuntime.(model.AutoReplyContactInspector)
	if !ok || !targetConversationAllowed(&r.target, conversation) {
		return model.AutoReplyConversationSnapshot{}, storage.ErrAutoReplyCandidateDeleted
	}
	conversation.PlatformCandidateID = r.target.PlatformCandidateID
	snapshot, err := inspector.InspectAutoReplyContact(ctx, browser, cfg, conversation)
	if err == nil {
		err = r.checkPage(ctx, browser, cfg, snapshot)
	}
	return snapshot, err
}

// ReadAutoReplyMessages 在差量读取前后核对目标，发生切人时不把错误聊天写入本机。
func (r *targetedAutoReplyRuntime) ReadAutoReplyMessages(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, known []string, limit int) ([]model.ConversationMessage, bool, error) {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return nil, false, err
	}
	messages, complete, err := r.AutoReplyRuntime.ReadAutoReplyMessages(ctx, browser, cfg, snapshot, known, limit)
	if err == nil {
		err = r.checkPage(ctx, browser, cfg, snapshot)
	}
	if err != nil {
		return nil, false, err
	}
	return messages, complete, nil
}

// RequestAutoReplyResume 把平台索要简历按钮也纳入单会话限制，避免绕过文本发送守卫。
func (r *targetedAutoReplyRuntime) RequestAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return err
	}
	return r.AutoReplyRuntime.RequestAutoReplyResume(ctx, browser, cfg, snapshot)
}

// CollectAutoReplyResume 仅采集已核实目标的简历，恢复聊天框后继续复核当前稳定身份。
func (r *targetedAutoReplyRuntime) CollectAutoReplyResume(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	resume, err := r.AutoReplyRuntime.CollectAutoReplyResume(ctx, browser, cfg, snapshot)
	if err == nil {
		err = r.checkPage(ctx, browser, cfg, snapshot)
	}
	if err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	return resume, nil
}

// SendAutoReplyMessage 每条回答、确认和简历提示都重新核对页面编号，缺失或切换身份时禁止发送。
func (r *targetedAutoReplyRuntime) SendAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return err
	}
	return r.AutoReplyRuntime.SendAutoReplyMessage(ctx, browser, cfg, snapshot, message)
}

// ReadLatestAutoReplyMessage 在发送复核和回读前确认页面身份，不允许通过底层自动恢复进入其他会话。
func (r *targetedAutoReplyRuntime) ReadLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return model.ConversationMessage{}, err
	}
	message, err := r.AutoReplyRuntime.ReadLatestAutoReplyMessage(ctx, browser, cfg, snapshot)
	if err == nil {
		err = r.checkPage(ctx, browser, cfg, snapshot)
	}
	if err != nil {
		return model.ConversationMessage{}, err
	}
	return message, nil
}

// ReadOpenAutoReplyLatestMessage 只监控已确认的目标聊天，用户切换到其他候选人时不主动恢复页面。
func (r *targetedAutoReplyRuntime) ReadOpenAutoReplyLatestMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		// 切换未读列表可能收起已读聊天；明确未打开时清除旧快照，由正常恢复入口重新打开指定目标。
		if errors.Is(err, model.ErrAutoReplyConversationNotOpen) {
			return model.ConversationMessage{}, false, nil
		}
		return model.ConversationMessage{}, false, err
	}
	return r.AutoReplyRuntime.ReadOpenAutoReplyLatestMessage(ctx, browser, cfg, snapshot)
}

// CloseAutoReplyConversation 只清理目标候选人的页面，避免异常清理影响用户另行打开的会话。
func (r *targetedAutoReplyRuntime) CloseAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	if err := r.checkPage(ctx, browser, cfg, snapshot); err != nil {
		return err
	}
	return r.AutoReplyRuntime.CloseAutoReplyConversation(ctx, browser, cfg, snapshot)
}
