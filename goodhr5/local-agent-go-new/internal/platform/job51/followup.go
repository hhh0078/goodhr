// Package job51 文件作用：实现前程无忧打招呼后的电话、微信、简历和追加消息动作。
package job51

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// EnsureCandidateConversation 从当前唯一稳定卡片重新打开聊天；页面未公开会话身份编号，不能仅凭同姓或脱敏名复用旧会话。
func (r *Runtime) EnsureCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	if err := common.CloseCandidatePanels(ctx, browser, cfg); err != nil {
		return fmt.Errorf("前程无忧旧聊天框还没收好，消息先不发送：%w", err)
	}
	if err := r.scrollCandidateAction(ctx, browser, cfg, candidate, "candidate.continue"); err != nil {
		return err
	}
	cfg, candidate, err := scopeCandidate(ctx, browser, cfg, candidate)
	if err != nil {
		return err
	}
	return common.EnsureCandidateConversationFromCard(ctx, browser, cfg, candidate)
}

// RequestCandidateInfo 在已确认身份的前程无忧聊天框内按岗位要求索要信息。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.CandidateInfoRequest) error {
	if !request.RequestPhone && !request.RequestWechat && !request.RequestResume {
		return nil
	}
	name, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.chat_name")
	if err != nil {
		return err
	}
	if !found || !common.CandidateNamesMatch(candidate.Name, name) {
		return fmt.Errorf("前程无忧聊天对象还没确认，资料先不索要，请重新打开当前候选人聊天")
	}
	if request.RequestPhone {
		message := strings.TrimSpace(cfg.Behavior.RequestPhoneMessage)
		if message == "" {
			return fmt.Errorf("前程无忧电话索要文案还没配置，这次先不发送，也不会点击付费取号")
		}
		if err = common.SendCandidateMessage(ctx, browser, cfg, candidate, message); err != nil {
			return err
		}
	}
	if request.RequestWechat {
		if err = common.RequestCandidateInfo(ctx, browser, cfg, model.CandidateInfoRequest{RequestWechat: true}); err != nil {
			return err
		}
	}
	if request.RequestResume {
		message := strings.TrimSpace(cfg.Behavior.RequestResumeMessage)
		if message == "" {
			return fmt.Errorf("前程无忧简历索要文案还没配置，这次先不发送，请更新后重试")
		}
		if err = common.SendCandidateMessage(ctx, browser, cfg, candidate, message); err != nil {
			return err
		}
	}
	return nil
}

// SendCandidateMessage 向已确认身份的前程无忧候选人发送消息。
func (r *Runtime) SendCandidateMessage(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, message string) error {
	return common.SendCandidateMessage(ctx, browser, cfg, candidate, message)
}

// CloseCandidateConversation 关闭前程无忧候选人聊天框；卡片继续沟通路径不会打开联系人列表。
func (r *Runtime) CloseCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.CloseCandidatePanels(ctx, browser, cfg)
}
