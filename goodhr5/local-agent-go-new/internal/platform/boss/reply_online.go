// Package boss 本文件接入当前会话的只读在线基础资料，入口和字段只取本平台已核实配置。
package boss

import (
	"context"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

var _ model.AutoReplyOnlineProfileReader = (*Runtime)(nil)

// ReadAutoReplyOnlineProfile 先核对当前候选人，读取真实在线资料并关闭详情，不改变附件拥有状态。
func (r *Runtime) ReadAutoReplyOnlineProfile(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyOnlineProfile, error) {
	verify := func(ctx context.Context) error {
		return r.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot)
	}
	restore := func(ctx context.Context) error { return ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot) }
	return common.ReadConfiguredAutoReplyOnlineProfile(ctx, browser, cfg, snapshot, verify, restore, r.CleanCandidateDetailText)
}
