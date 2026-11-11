// Package zhaopin 文件作用：仅推荐页启用按唯一身份继续加载断点，搜索页仍保留原前缀与分页保护。
package zhaopin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// RestoreCandidateList 在推荐页找回换序或尚未加载的锚点，搜索页不放松原有名单顺序核对。
func (r *Runtime) RestoreCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, point model.CandidateResumePoint, cursor *model.CandidateListCursor) (bool, error) {
	if r.manualSearch {
		cfg.Behavior.CandidateResumeByIdentity = false
	}
	return r.CandidateResumeTracker.RestoreCandidateList(ctx, browser, cfg, point, cursor)
}
