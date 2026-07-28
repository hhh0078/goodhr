// Package hliepin 文件作用：实现猎聘猎头端候选人列表读取、定位、滚动和翻页。
package hliepin

import (
	"context"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回猎聘猎头端当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Candidate, error) {
	candidates, err := common.FindCandidates(ctx, browser, cfg, r.PlatformID())
	if err != nil {
		return nil, err
	}
	result := make([]model.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !strings.Contains(candidate.Summary, "立即沟通") && !strings.Contains(candidate.Summary, "继续沟通") {
			continue
		}
		if candidate.Fields == nil {
			candidate.Fields = make(map[string]string)
		}
		if strings.Contains(candidate.Summary, "立即沟通") {
			candidate.Fields["greet_state"] = "immediate"
		} else {
			candidate.Fields["greet_state"] = "continue"
		}
		stableID := strings.TrimSpace(candidate.Fields["platform_candidate_id"])
		if stableID != "" {
			candidate.Fingerprint = r.PlatformID() + "_" + stableID
		} else {
			candidate.Fingerprint = common.HashText(r.PlatformID() + "|" + stableCandidateText(candidate.Summary))
		}
		result = append(result, candidate)
	}
	return result, nil
}

// ScrollToCandidate 通过真实滚轮定位指定猎聘猎头端候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// NextCandidatePage 尝试进入猎聘猎头端候选人下一页。
func (r *Runtime) NextCandidatePage(ctx context.Context, browser model.Browser, cfg model.Config) (bool, error) {
	return common.NextCandidatePage(ctx, browser, cfg)
}

// ScrollCandidates 通过真实滚轮加载更多猎聘猎头端候选人。
func (r *Runtime) ScrollCandidates(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.ScrollCandidates(ctx, browser, cfg)
}

// stableCandidateText 移除会随浏览和沟通变化的猎聘列表状态文字。
func stableCandidateText(value string) string {
	lines := make([]string, 0)
	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || transientCandidateLine(line) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// transientCandidateLine 判断一行是否为猎聘候选人动态状态。
func transientCandidateLine(value string) bool {
	if strings.HasSuffix(value, "活跃") && len([]rune(value)) <= 8 {
		return true
	}
	switch value {
	case "在线", "活跃状态", "隐藏", "阅", "已查看", "立即沟通", "继续沟通", "已沟通", "获取联系方式":
		return true
	default:
		return false
	}
}
