// Package common 文件作用：提供所有招聘平台复用的候选人列表滚动、翻页和加载结果验证能力。
package common

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	candidateListPollAttempts = 20
	candidateListPollInterval = 300 * time.Millisecond
)

// AdvanceCandidateList 按平台配置使用真实滚轮或下一页按钮推进候选人列表。
// 返回 true 表示页面出现了新候选人，返回 false 表示列表已经没有更多内容。
func AdvanceCandidateList(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
	options model.CandidateListOptions,
) (bool, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Behavior.CandidateListMode))
	if mode == "" && cfg.Behavior.SupportsPaging {
		mode = "next_page"
	}
	switch mode {
	case "next_page":
		return advanceCandidatePage(ctx, browser, cfg, platformID, before, options)
	case "", "infinite_scroll":
		return advanceCandidateScroll(ctx, browser, cfg, platformID, before, options)
	default:
		return false, nil
	}
}

// AdvanceCandidateNumberPage 点击指定数字页码，并轮询确认候选人列表已经更新。
func AdvanceCandidateNumberPage(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	selectorKey string,
	pageNumber int,
	before []model.Candidate,
	options model.CandidateListOptions,
) (bool, error) {
	selector, err := numberedPageSelector(cfg, selectorKey, pageNumber)
	if err != nil {
		return false, err
	}
	return AdvanceCandidatePageTarget(ctx, browser, cfg, platformID, selector, pageNumber, before, options)
}

// AdvanceCandidatePageTarget 滚动并点击指定分页入口，确认目标页码和候选人列表均已更新。
func AdvanceCandidatePageTarget(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	selector contract.SelectorSpec,
	pageNumber int,
	before []model.Candidate,
	options model.CandidateListOptions,
) (bool, error) {
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 1, ExpectedMissing: true,
	})
	if err != nil || len(items) == 0 {
		return false, err
	}
	scrollRequest := contract.ScrollRequest{
		Target:         &selector,
		Distance:       320,
		MaxAttempts:    24,
		WaitMS:         120,
		ViewportMargin: 24,
	}
	if anchor, ok := cfg.Selectors["candidate.list"]; ok && len(anchor.Target.Selectors) > 0 {
		scrollRequest.WheelAnchor = &anchor
	}
	if _, err = browser.Scroll(ctx, scrollRequest); err != nil {
		return false, fmt.Errorf("滚动到%s失败：%w", selector.Description, err)
	}
	var verification *contract.ClickVerification
	if _, configured := cfg.Selectors["candidate.current_page"]; configured {
		currentPage, selectorErr := numberedPageSelector(cfg, "candidate.current_page", pageNumber)
		if selectorErr != nil {
			return false, selectorErr
		}
		verification = &contract.ClickVerification{TargetVisible: &currentPage, TimeoutMS: 5000}
	}
	if _, err = browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 24, Verify: verification,
	}); err != nil {
		return false, err
	}
	return waitForCandidateListChange(ctx, browser, cfg, platformID, before, options)
}

// numberedPageSelector 把通用分页选择器收紧为指定的精确数字页码。
func numberedPageSelector(cfg model.Config, selectorKey string, pageNumber int) (contract.SelectorSpec, error) {
	selector, err := RequiredSelector(cfg, selectorKey)
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	if pageNumber < 2 {
		pageNumber = 2
	}
	exact := true
	selector.Target.Text = strconv.Itoa(pageNumber)
	selector.Target.Texts = nil
	selector.Target.ExactText = &exact
	selector.Target.Index = nil
	selector.Description = fmt.Sprintf("%s第 %d 页", firstNonEmpty(cfg.Name, cfg.ID), pageNumber)
	return selector, nil
}

// advanceCandidatePage 点击下一页，并轮询确认候选人列表已经更新。
func advanceCandidatePage(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
	options model.CandidateListOptions,
) (bool, error) {
	exists, err := SelectorExists(ctx, browser, cfg, "candidate.next_page")
	if err != nil || !exists {
		return false, err
	}
	if err = ClickRequired(ctx, browser, cfg, "candidate.next_page"); err != nil {
		return false, err
	}
	return waitForCandidateListChange(ctx, browser, cfg, platformID, before, options)
}

// WaitForCandidateListChange 为平台切岗复用列表更新验证，不执行滚动和点击。
func WaitForCandidateListChange(ctx context.Context, browser model.Browser, cfg model.Config, platformID string, before []model.Candidate) (bool, error) {
	return waitForCandidateListChange(ctx, browser, cfg, platformID, before, model.CandidateListOptions{})
}

// WaitForStableCandidateList 等待候选人名单连续三秒不变，防止切岗多阶段加载产生过期快照。
func WaitForStableCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, platformID string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var previous map[string]struct{}
	var stableSince time.Time
	for {
		candidates, err := FindCandidates(ctx, browser, cfg, platformID, model.CandidateListOptions{})
		if err != nil && !IsElementMissing(err) {
			return err
		}
		current := candidateFingerprintSet(candidates)
		same := len(current) > 0 && len(current) == len(previous)
		for key := range current {
			if _, ok := previous[key]; !ok {
				same = false
				break
			}
		}
		if !same {
			stableSince = time.Now()
		}
		if same && time.Since(stableSince) >= 3*time.Second {
			return nil
		}
		previous = current
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("候选人列表仍在刷新：%w", ctx.Err())
		case <-timer.C:
		}
	}
}

// waitForCandidateListChange 每 300 毫秒读取一次列表，最多尝试 20 次。
func waitForCandidateListChange(
	ctx context.Context,
	browser model.Browser,
	cfg model.Config,
	platformID string,
	before []model.Candidate,
	options model.CandidateListOptions,
) (bool, error) {
	beforeSet := candidateFingerprintSet(before)
	var lastMissingErr error
	foundCandidates := false
	for attempt := 0; attempt < candidateListPollAttempts; attempt++ {
		candidates, err := FindCandidates(ctx, browser, cfg, platformID, options)
		if IsElementMissing(err) {
			lastMissingErr = err
		} else if err != nil {
			return false, err
		} else {
			if len(candidates) > 0 {
				foundCandidates = true
			}
			for fingerprint := range candidateFingerprintSet(candidates) {
				if _, exists := beforeSet[fingerprint]; !exists {
					return true, nil
				}
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
	if !foundCandidates && lastMissingErr != nil {
		return false, lastMissingErr
	}
	return false, nil
}

// candidateFingerprintSet 返回候选人稳定编号集合，兼容页面暂时缺少稳定编号的情况。
func candidateFingerprintSet(candidates []model.Candidate) map[string]struct{} {
	result := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		fingerprint := strings.TrimSpace(candidate.Fields["platform_candidate_id"])
		if fingerprint == "" {
			fingerprint = strings.TrimSpace(candidate.Fingerprint)
		}
		if fingerprint == "" {
			fingerprint = HashText(candidate.Name + "|" + candidate.Summary)
		}
		result[fingerprint] = struct{}{}
	}
	return result
}
