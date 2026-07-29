// Package common 文件作用：提供所有招聘平台复用的候选人列表滚动、翻页和加载结果验证能力。
package common

import (
	"context"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	candidateListPollAttempts = 20
	candidateListPollInterval = 300 * time.Millisecond
	candidateScrollAttempts   = 3
)

// AdvanceCandidateList 按平台配置使用真实滚轮或下一页按钮推进候选人列表。
// 返回 true 表示页面出现了新候选人，返回 false 表示列表已经没有更多内容。
func AdvanceCandidateList(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
) (bool, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Behavior.CandidateListMode))
	if mode == "" && cfg.Behavior.SupportsPaging {
		mode = "next_page"
	}
	switch mode {
	case "next_page":
		return advanceCandidatePage(ctx, browser, cfg, platformID, before)
	case "", "infinite_scroll":
		return advanceCandidateScroll(ctx, browser, cfg, platformID, before)
	default:
		return false, nil
	}
}

// advanceCandidatePage 点击下一页，并轮询确认候选人列表已经更新。
func advanceCandidatePage(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
) (bool, error) {
	exists, err := SelectorExists(ctx, browser, cfg, "candidate.next_page")
	if err != nil || !exists {
		return false, err
	}
	if err = ClickRequired(ctx, browser, cfg, "candidate.next_page"); err != nil {
		return false, err
	}
	return waitForCandidateListChange(ctx, browser, cfg, platformID, before)
}

// advanceCandidateScroll 在候选人列表区域执行真实滚轮，并确认出现了新候选人。
func advanceCandidateScroll(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
) (bool, error) {
	anchor, err := RequiredSelector(cfg, "candidate.list")
	if err != nil {
		return false, err
	}
	for attempt := 0; attempt < candidateScrollAttempts; attempt++ {
		if _, err = browser.Scroll(ctx, contract.ScrollRequest{
			WheelAnchor: &anchor,
			Distance:    positiveOr(cfg.ScrollDistance, 620),
			MaxAttempts: 1,
			WaitMS:      int(candidateListPollInterval / time.Millisecond),
		}); err != nil {
			return false, err
		}
		changed, waitErr := waitForCandidateListChange(ctx, browser, cfg, platformID, before)
		if waitErr != nil || changed {
			return changed, waitErr
		}
	}
	return false, nil
}

// waitForCandidateListChange 每 300 毫秒读取一次列表，最多尝试 20 次。
func waitForCandidateListChange(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
) (bool, error) {
	beforeSet := candidateFingerprintSet(before)
	for attempt := 0; attempt < candidateListPollAttempts; attempt++ {
		candidates, err := FindCandidates(ctx, browser, cfg, platformID)
		if err != nil {
			return false, err
		}
		for fingerprint := range candidateFingerprintSet(candidates) {
			if _, exists := beforeSet[fingerprint]; !exists {
				return true, nil
			}
		}
		if attempt+1 < candidateListPollAttempts {
			timer := time.NewTimer(candidateListPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return false, nil
}

// candidateFingerprintSet 返回候选人稳定编号集合，兼容页面暂时缺少稳定编号的情况。
func candidateFingerprintSet(candidates []model.Candidate) map[string]struct{} {
	result := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		fingerprint := strings.TrimSpace(candidate.Fingerprint)
		if fingerprint == "" {
			fingerprint = HashText(candidate.Name + "|" + candidate.Summary)
		}
		result[fingerprint] = struct{}{}
	}
	return result
}
