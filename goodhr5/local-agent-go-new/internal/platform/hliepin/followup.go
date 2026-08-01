// Package hliepin 文件作用：把猎聘猎头端候选人对话框、索要资料和发消息接入公共能力。
package hliepin

import (
	"context"
	"errors"
	"fmt"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// EnsureCandidateConversation 打开或复用猎聘猎头端当前候选人的聊天框。
func (r *Runtime) EnsureCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.EnsureCandidateConversation(ctx, browser, cfg, candidate)
}

// RequestCandidateInfo 在已确认身份的猎聘猎头端聊天框内按岗位要求索要信息，并清理平台意外打开的简历页。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate, request model.CandidateInfoRequest) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("索要候选人资料前读取猎聘标签页失败：%w", err)
	}
	returnURL, err := currentPageURL(pages)
	if err != nil {
		return fmt.Errorf("索要候选人资料前无法确定猎聘列表页：%w", err)
	}
	requestErr := common.RequestCandidateInfo(ctx, browser, cfg, request)
	cleanupErr := closeUnexpectedResumeDetail(ctx, browser, returnURL)
	return errors.Join(requestErr, cleanupErr)
}

// SendCandidateMessage 向已确认身份的猎聘猎头端候选人发送消息。
func (r *Runtime) SendCandidateMessage(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, message string) error {
	return common.SendCandidateMessage(ctx, browser, cfg, candidate, message)
}

// CloseCandidateConversation 关闭猎聘猎头端候选人聊天框和可能打开的侧边栏。
func (r *Runtime) CloseCandidateConversation(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.CloseCandidateConversation(ctx, browser, cfg)
}
