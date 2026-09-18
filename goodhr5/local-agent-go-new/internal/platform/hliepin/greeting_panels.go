// Package hliepin 文件作用：处理开聊前后推广弹框、职位弹框和聊天侧栏，并限制未发送时的恢复重试。
package hliepin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const postGreetPromotionPollAttempts = 4
const postGreetPromotionPollInterval = 150 * time.Millisecond
const postGreetPromotionWindow = 2 * time.Second

// closePostGreetPromotion 在发送后留出两秒观察延迟弹框；单次检查用于动作前清理。
func closePostGreetPromotion(ctx context.Context, browser model.Browser, cfg model.Config, attempts int) error {
	deadline := time.Now().Add(postGreetPromotionWindow)
	for {
		opened, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_promotion_modal")
		if err != nil {
			return fmt.Errorf("检查猎聘开聊后推广弹框失败：%w", err)
		}
		if opened {
			// 已确定最上层是推广弹框，优先按 Esc，且必须验证它确实消失。
			_, pressErr := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", DelayMS: 120})
			if contract.IsSessionUnavailable(pressErr) {
				return pressErr
			}
			stillOpen, probeErr := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_promotion_modal")
			if probeErr != nil {
				return probeErr
			}
			if !stillOpen {
				return nil
			}
			if closeErr := common.CloseOptionalPanel(ctx, browser, cfg, "candidate.greet_promotion_modal", "candidate.greet_promotion_close", "猎聘开聊后推广弹框"); closeErr != nil {
				return errors.Join(pressErr, closeErr)
			}
			return nil
		}
		if attempts <= 1 || !time.Now().Before(deadline) {
			return nil
		}
		timer := time.NewTimer(min(postGreetPromotionPollInterval, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// closeGreetingPanels 按遮挡顺序清理已知弹层，某一步失败也继续尝试清理其余弹层。
func closeGreetingPanels(ctx context.Context, browser model.Browser, cfg model.Config, waitForPromotion bool) error {
	var failures []error
	if err := closeModalWithEscape(ctx, browser, cfg, "candidate.request_confirm_modal", "猎聘索要信息确认弹框"); err != nil {
		if contract.IsSessionUnavailable(err) {
			return err
		}
		failures = append(failures, err)
	}
	attempts := 1
	if waitForPromotion {
		attempts = postGreetPromotionPollAttempts
	}
	if err := closePostGreetPromotion(ctx, browser, cfg, attempts); err != nil {
		if contract.IsSessionUnavailable(err) {
			return err
		}
		failures = append(failures, err)
	}
	if err := closeGreetModalIfPresent(ctx, browser, cfg); err != nil {
		if contract.IsSessionUnavailable(err) {
			return err
		}
		failures = append(failures, err)
	}
	if err := closeCandidatePanels(ctx, browser, cfg); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// openGreetingModal 点击当前候选人的立即沟通，遇到遮挡清理后最多重试一次，已沟通时禁止重发。
func openGreetingModal(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	modal, err := common.RequiredSelector(cfg, "candidate.greet_modal")
	if err != nil {
		return err
	}
	var clickErr error
	for attempt := 0; attempt < 2; attempt++ {
		contacted, err := common.CandidateActionExists(ctx, browser, cfg, candidate, "candidate.continue")
		if err != nil {
			return err
		}
		if contacted {
			return model.ErrCandidateAlreadyContacted
		}
		// 每次重试重新定位同一个候选人，禁止复用页面变化前的元素引用。
		selector, err := common.CandidateActionSelector(cfg, "candidate.greet", candidate)
		if err != nil {
			return err
		}
		_, clickErr = browser.Click(ctx, contract.ElementClickRequest{
			Selector: selector, ViewportMargin: 48,
			Verify: &contract.ClickVerification{TargetVisible: &modal, TimeoutMS: 5000},
		})
		if contract.IsSessionUnavailable(clickErr) {
			return clickErr
		}
		// 验证阶段可能先遇到隐藏的旧节点，仍以当前可见职位弹框为准。
		opened, probeErr := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.greet_modal")
		if probeErr != nil {
			return probeErr
		}
		if opened {
			return nil
		}
		if clickErr == nil {
			clickErr = fmt.Errorf("点击后没有出现请选择职位开聊弹框")
		}
		if err := closeGreetingPanels(ctx, browser, cfg, true); err != nil {
			return errors.Join(clickErr, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fmt.Errorf("点击猎聘立即沟通后未出现职位弹框，清理弹层并重试一次仍未恢复：%w", clickErr)
}
