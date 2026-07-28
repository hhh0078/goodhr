// Package boss 文件作用：实现 Boss 打招呼、收藏和不合适动作。
package boss

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// GreetCandidate 向指定 Boss 候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.GreetRequest) error {
	return common.GreetCandidate(ctx, browser, cfg, candidate, request)
}

// FavoriteCandidate 收藏指定 Boss 候选人。
func (r *Runtime) FavoriteCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.favorite")
}

// RejectCandidate 将指定 Boss 候选人标记为不合适。
func (r *Runtime) RejectCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.CandidateAction(ctx, browser, cfg, candidate, "candidate.reject")
}
