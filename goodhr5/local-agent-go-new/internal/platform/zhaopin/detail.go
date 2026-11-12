// Package zhaopin 文件作用：实现智联候选人详情打开、提取和关闭。
package zhaopin

import (
	"context"
	"maps"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenCandidateDetail 打开指定智联候选人详情。
func (r *Runtime) OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	if r.manualSearch {
		var err error
		cfg, err = candidateDetailConfig(ctx, browser, cfg, candidate)
		if err != nil {
			return err
		}
	}
	return common.OpenCandidateDetail(ctx, browser, cfg, candidate)
}

// candidateDetailConfig 仅搜索页只读核对当前候选人所在的卡片路径，避免先按推荐页空等五秒并误记失败。
// 保留每条路径的身份约束和详情备用入口；加载中全未命中时仍使用完整配置等待，不缓存跨人选的匹配结果。
func candidateDetailConfig(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (model.Config, error) {
	card, err := common.CandidateSelector(cfg, candidate)
	if err != nil || len(card.Alternatives) == 0 {
		return cfg, err
	}
	base := cfg.Selectors["candidate.item"].Variants()
	var matched []contract.SelectorSpec
	for index, variant := range card.Variants() {
		variant.TimeoutMS = 1
		items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
			Selector: variant, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true,
		})
		if err != nil && !common.IsElementMissing(err) {
			return cfg, err
		}
		if len(items) > 0 {
			matched = append(matched, base[index])
		}
	}
	if len(matched) > 0 {
		cfg.Selectors = maps.Clone(cfg.Selectors)
		cfg.Selectors["candidate.item"] = contract.SelectorVariants(matched)
	}
	return cfg, nil
}

// ExtractCandidateDetail 提取当前智联候选人详情文本。
func (r *Runtime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) (model.CandidateDetail, error) {
	return common.ExtractCandidateDetail(ctx, browser, cfg)
}

// CleanCandidateDetailText 清理智联候选人详情两端空白。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// BrowseCandidateDetail 在 AI 判断前使用真实滚轮浏览智联详情弹框。
func (r *Runtime) BrowseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.BrowseCandidateDetail(ctx, browser, cfg, 320, 4)
}

// CloseCandidateDetail 关闭当前智联候选人详情。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.CloseCandidateDetail(ctx, browser, cfg)
}
