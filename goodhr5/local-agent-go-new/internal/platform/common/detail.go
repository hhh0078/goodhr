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
func OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (resultErr error) {
	// 弹窗已经出现而正文检查失败时，也必须先清理弹窗；清理失败禁止继续处理列表。
	defer func() {
		if resultErr == nil || contract.IsSessionUnavailable(resultErr) {
			return
		}
		if _, configured := cfg.Selectors["candidate.detail_dialog"]; !configured {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if cleanupErr := CloseCandidateDetail(cleanupCtx, browser, cfg); cleanupErr != nil {
			resultErr = fmt.Errorf("%w：打开检查失败：%v；关闭结果：%w", model.ErrCandidateDetailCleanupFailed, resultErr, cleanupErr)
		}
	}()
	selector, err := CandidateActionSelector(cfg, "candidate.open_target", candidate)
	if err != nil {
		return err
	}
	clickSelectors := []contract.SelectorSpec{selector, selector}
	if _, configured := cfg.Selectors["candidate.open_target_fallback"]; configured {
		fallback, fallbackErr := CandidateActionSelector(cfg, "candidate.open_target_fallback", candidate)
		if fallbackErr != nil {
			return fallbackErr
		}
		clickSelectors[1] = fallback
	}
	var clickErr error
	for attempt := 1; attempt <= 2; attempt++ {
		_, clickErr = browser.Click(ctx, contract.ElementClickRequest{Selector: clickSelectors[attempt-1], ViewportMargin: 16, Diagnostic: true, DiagnosticAttempt: attempt})
		if clickErr != nil {
			continue
		}
		timeoutMS := 1500
		if attempt == 2 {
			timeoutMS = 5000
		}
		opened, probeErr := candidateDetailOpened(ctx, browser, cfg, timeoutMS, attempt)
		if probeErr != nil {
			return probeErr
		}
		if opened {
			return nil
		}
		if dialog, configured := cfg.Selectors["candidate.detail_dialog"]; configured && attempt == 1 {
			dialog.TimeoutMS = 250
			items, dialogErr := browser.FindAll(ctx, contract.ElementFindAllRequest{
				Selector: dialog, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true, Diagnostic: true, DiagnosticAttempt: attempt,
			})
			if dialogErr != nil && !IsElementMissing(dialogErr) {
				return dialogErr
			}
			if len(items) > 0 {
				// 弹窗已经遮住卡片时只等待正文，不再点击背后的候选人入口。
				opened, probeErr = candidateDetailOpened(ctx, browser, cfg, 5000, 2)
				if probeErr != nil {
					return probeErr
				}
				if opened {
					return nil
				}
				break
			}
		}
	}
	if clickErr != nil {
		return clickErr
	}
	return fmt.Errorf("%w：候选人详情入口操作后正文仍未出现", model.ErrCandidateDetailUnavailable)
}

// candidateDetailOpened 只检查详情是否出现，保留预期缺失的逐层证据，避免读取正文干扰打开判断。
func candidateDetailOpened(ctx context.Context, browser model.Browser, cfg model.Config, timeoutMS int, attempt int) (bool, error) {
	selector, err := RequiredSelector(cfg, "candidate.detail")
	if err != nil {
		return false, err
	}
	selector.TimeoutMS = timeoutMS
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{
		Selector: selector, MaxItems: 1, ExpectedMissing: true,
		Diagnostic: true, DiagnosticAttempt: attempt, ProbeOnly: true,
	})
	if IsElementMissing(err) {
		return false, nil
	}
	return err == nil && len(items) > 0, err
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

// CloseCandidateDetail 关闭详情后检查弹窗或正文是否消失，必要时再按一次 Escape。
func CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config) error {
	detailKey := "candidate.detail"
	_, hasDialog := cfg.Selectors["candidate.detail_dialog"]
	if hasDialog {
		detailKey = "candidate.detail_dialog"
	}
	detail, err := RequiredSelector(cfg, detailKey)
	if err != nil {
		return err
	}
	detail.TimeoutMS = 200
	if hasDialog {
		items, probeErr := browser.FindAll(ctx, contract.ElementFindAllRequest{
			Selector: detail, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true, Diagnostic: true,
		})
		if IsElementMissing(probeErr) || probeErr == nil && len(items) == 0 {
			return nil
		}
		if probeErr != nil {
			return probeErr
		}
	}
	var clickErr error
	if _, ok := cfg.Selectors["candidate.detail_close"]; ok {
		closeSelector, selectorErr := RequiredSelector(cfg, "candidate.detail_close")
		if selectorErr != nil {
			return selectorErr
		}
		_, clickErr = browser.Click(ctx, contract.ElementClickRequest{
			Selector:   closeSelector,
			Diagnostic: true,
			Verify: &contract.ClickVerification{
				TargetHidden: &detail,
				TimeoutMS:    2000,
			},
		})
		if clickErr == nil {
			return nil
		}
		if contract.IsSessionUnavailable(clickErr) {
			return clickErr
		}
	} else if !hasDialog {
		_, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120})
		return err
	}
	if _, err = browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120}); err != nil {
		if clickErr != nil {
			return fmt.Errorf("关闭详情按钮没有生效：%w；按 Escape 也失败：%w", clickErr, err)
		}
		return err
	}
	hidden, err := waitSelectorHidden(ctx, browser, cfg, detailKey)
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
