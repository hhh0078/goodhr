// Package boss 文件作用：实现 Boss 候选人列表读取、定位、滚动和翻页。
package boss

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回 Boss 当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	candidates, err := common.FindCandidates(ctx, browser, cfg, r.PlatformID(), options)
	if err != nil {
		return nil, err
	}
	for index := range candidates {
		candidate := &candidates[index]
		// 外层标题也属于卡片文字；保留原序号，只标记跳过，不探测按钮或执行页面动作。
		for _, text := range []string{candidate.Summary, candidate.Fields["card_text"]} {
			if strings.Contains(strings.Join(strings.Fields(text), ""), "热搜牛人") {
				candidate.SkipReason = "卡片含“热搜牛人”，已直接跳过"
				break
			}
		}
		if candidate.SkipReason == "" && strings.TrimSpace(candidate.Fields["card_text"]) == "" {
			return nil, fmt.Errorf("候选人“%s”的完整卡片文字尚未读到，暂不操作", candidate.Name)
		}
	}
	return candidates, nil
}

// ScrollToCandidate 通过真实滚轮定位指定 Boss 候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// AdvanceCandidateList 通过公共真实滚轮能力加载更多 Boss 候选人。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	return common.AdvanceCandidateList(ctx, browser, cfg, r.PlatformID(), before, options)
}
