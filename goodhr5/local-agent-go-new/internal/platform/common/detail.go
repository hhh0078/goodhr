// Package common 文件作用：提供所有招聘平台复用的候选人详情打开、提取、浏览和关闭能力。
package common

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const (
	detailReadAttempts = 20
	detailReadInterval = 300 * time.Millisecond
)

// OpenCandidateDetail 点击候选人卡片内的详情入口。
func OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	selector, err := CandidateActionSelector(cfg, "candidate.open_target", candidate)
	if err != nil {
		return err
	}
	_, err = browser.Click(ctx, contract.ElementClickRequest{Selector: selector, ViewportMargin: 16})
	return err
}

// ExtractCandidateDetail 读取当前已经打开的候选人详情文本。
func ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config) (model.CandidateDetail, error) {
	selector, err := RequiredSelector(cfg, "candidate.detail")
	if err != nil {
		return model.CandidateDetail{}, err
	}
	for attempt := 1; attempt <= detailReadAttempts; attempt++ {
		result, readErr := browser.Read(ctx, contract.ElementReadRequest{Selector: selector, Property: "text"})
		if readErr != nil {
			return model.CandidateDetail{}, readErr
		}
		if text := strings.TrimSpace(result.Value); text != "" {
			return model.CandidateDetail{Text: text}, nil
		}
		if attempt == detailReadAttempts {
			break
		}
		timer := time.NewTimer(detailReadInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return model.CandidateDetail{}, ctx.Err()
		case <-timer.C:
		}
	}
	return model.CandidateDetail{}, fmt.Errorf("候选人详情内容为空，已每 300 毫秒检查 20 次")
}

// BrowseCandidateDetail 使用真实鼠标滚轮浏览当前详情区域。
func BrowseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, distance int, attempts int) error {
	anchor, ok := cfg.Selectors["candidate.detail_scroll"]
	if !ok {
		var err error
		anchor, err = RequiredSelector(cfg, "candidate.detail")
		if err != nil {
			return err
		}
	}
	_, err := browser.Scroll(ctx, contract.ScrollRequest{
		WheelAnchor: &anchor,
		Distance:    positiveOr(distance, 320),
		MaxAttempts: positiveOr(attempts, 1),
		WaitMS:      300,
	})
	return err
}

// CloseCandidateDetail 关闭详情后检查正文是否消失，必要时再按一次 Escape。
func CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config) error {
	detail, err := RequiredSelector(cfg, "candidate.detail")
	if err != nil {
		return err
	}
	detail.TimeoutMS = 200
	var clickErr error
	if _, ok := cfg.Selectors["candidate.detail_close"]; ok {
		closeSelector, selectorErr := RequiredSelector(cfg, "candidate.detail_close")
		if selectorErr != nil {
			return selectorErr
		}
		_, clickErr = browser.Click(ctx, contract.ElementClickRequest{
			Selector: closeSelector,
			Verify: &contract.ClickVerification{
				TargetHidden: &detail,
				TimeoutMS:    2000,
			},
		})
		if clickErr == nil {
			return nil
		}
	} else {
		_, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120})
		return err
	}
	if _, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120}); err != nil {
		if clickErr != nil {
			return fmt.Errorf("关闭详情按钮没有生效：%v；按 Escape 也失败：%w", clickErr, err)
		}
		return err
	}
	hidden, err := waitSelectorHidden(ctx, browser, cfg, "candidate.detail")
	if err != nil {
		return err
	}
	if !hidden {
		if clickErr != nil {
			return fmt.Errorf("平台 %s 的候选人详情仍未关闭：%w", cfg.ID, clickErr)
		}
		return fmt.Errorf("平台 %s 的候选人详情仍未关闭", cfg.ID)
	}
	return nil
}
