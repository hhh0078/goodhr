// Package boss 文件作用：实现 Boss 打招呼后的电话、微信、简历和追加消息动作。
package boss

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// EnsureCandidateConversation 打开或复用 Boss 当前候选人的聊天框。
func (r *Runtime) EnsureCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.EnsureCandidateConversation(ctx, browser, cfg, candidate)
}

// RequestCandidateInfo 在已确认身份的 Boss 聊天框内按岗位要求索要信息。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate, request model.CandidateInfoRequest) error {
	return common.RequestCandidateInfo(ctx, browser, cfg, request)
}

// SendCandidateMessage 向已确认身份的 Boss 候选人发送消息。
func (r *Runtime) SendCandidateMessage(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, message string) error {
	return common.SendCandidateMessage(ctx, browser, cfg, candidate, message)
}

// CloseCandidateConversation 关闭 Boss 候选人聊天框和可能打开的侧边栏。
func (r *Runtime) CloseCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.CloseCandidateConversation(ctx, browser, cfg)
}
