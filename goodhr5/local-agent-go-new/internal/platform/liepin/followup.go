// Package liepin 文件作用：实现猎聘企业端打招呼后的电话、微信、简历和追加消息动作。
package liepin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// RequestCandidateInfo 按岗位要求向猎聘企业端候选人索要信息。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate, request model.CandidateInfoRequest) error {
	return common.RequestCandidateInfo(ctx, browser, cfg, request)
}
