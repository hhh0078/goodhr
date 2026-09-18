// Package liepin 文件作用：实现猎聘企业端打招呼后的电话、微信、简历和追加消息动作。
package liepin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// EnsureCandidateConversation 打开或复用猎聘企业端当前候选人的聊天框。
func (r *Runtime) EnsureCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	if err := closeGreetingNotice(ctx, browser, cfg); err != nil {
		return err
	}
	return common.EnsureCandidateConversation(ctx, browser, cfg, candidate)
}

// RequestCandidateInfo 在已确认身份的猎聘企业端聊天框内按岗位要求索要信息。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate, request model.CandidateInfoRequest) error {
	return common.RequestCandidateInfo(ctx, browser, cfg, request)
}

// SendCandidateMessage 向已确认身份的猎聘企业端候选人发送消息。
func (r *Runtime) SendCandidateMessage(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, message string) error {
	return common.SendCandidateMessage(ctx, browser, cfg, candidate, message)
}

// CloseCandidateConversation 关闭猎聘企业端候选人聊天框和可能打开的侧边栏。
func (r *Runtime) CloseCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	if err := closeGreetingNotice(ctx, browser, cfg); err != nil {
		return err
	}
	return common.CloseCandidateConversation(ctx, browser, cfg)
}

// closeGreetingNotice 等待可能延迟出现的发送后推广提示并只关闭它；收尾失败不能抹掉已发送的招呼。
func closeGreetingNotice(ctx context.Context, browser model.Browser, cfg model.Config) error {
	opened, err := common.SelectorExists(ctx, browser, cfg, "candidate.greet_result_panel")
	if err != nil || !opened {
		return err
	}
	return common.CloseOptionalPanel(ctx, browser, cfg, "candidate.greet_result_panel", "candidate.greet_confirm", "猎聘企业端发送后提示")
}
