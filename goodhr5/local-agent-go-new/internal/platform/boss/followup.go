// Package boss 文件作用：实现 Boss 打招呼后的电话、微信、简历和追加消息动作。
package boss

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// RequestCandidateInfo 复用或打开 Boss 当前候选人聊天框，并按岗位要求索要信息。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.CandidateInfoRequest) error {
	return common.RequestCandidateInfoInChat(ctx, browser, cfg, candidate, request)
}
