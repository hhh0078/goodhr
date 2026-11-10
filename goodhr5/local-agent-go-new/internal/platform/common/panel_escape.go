// Package common 本文件负责在原页面确认遮层关闭，并在 Esc 未生效时复用原关闭按钮。
package common

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ClosePanelEscapeFirst 只对已核实页面的可见弹层按 Esc，原页上同一弹层仍在时才点备用按钮。
func ClosePanelEscapeFirst(ctx context.Context, browser model.Browser, cfg model.Config, key, closeKey, pageURL string, verifyIdentity func(context.Context) error) error {
	panel, err := RequiredSelector(cfg, key)
	if err != nil {
		return err
	}
	checkPage := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := UniqueCurrentPageURL(ctx, browser)
		if err != nil {
			return err
		}
		if current != pageURL {
			return fmt.Errorf("当前标签页已变化，无法核实%s", panel.Description)
		}
		return nil
	}
	// 找不到元素须同时证明查询的是原页，避免误把切页或 PDF 标签当成原弹层已经关闭。
	visible := func() (bool, error) {
		if err := checkPage(); err != nil {
			return false, err
		}
		found, err := selectorExists(ctx, browser, cfg, key, 80)
		if err != nil || found {
			return found, err
		}
		return false, checkPage()
	}
	checkIdentity := func() error {
		if verifyIdentity != nil {
			if err := verifyIdentity(ctx); err != nil {
				return err
			}
		}
		return checkPage()
	}
	found, err := visible()
	if err != nil || !found {
		return err
	}
	if err := checkIdentity(); err != nil {
		return err
	}
	_, keyErr := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Escape", Target: &panel, ExpectedPageURL: pageURL})
	if contract.IsSessionUnavailable(keyErr) {
		return keyErr
	}
	for attempt := 0; ; attempt++ {
		found, err = visible()
		if err != nil || !found {
			return err
		}
		if keyErr != nil || attempt == 3 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
	}
	button, err := RequiredSelector(cfg, closeKey)
	if err != nil {
		return err
	}
	if err := checkIdentity(); err != nil {
		return err
	}
	_, clickErr := browser.Click(ctx, contract.ElementClickRequest{Selector: button, Verify: &contract.ClickVerification{TargetHidden: &panel, TimeoutMS: 1800}})
	if contract.IsSessionUnavailable(clickErr) {
		return clickErr
	}
	// 按钮返回值不能代替页面证据；按钮动作有可能超时，但目标弹层其实已经关闭。
	found, err = visible()
	if err != nil || !found {
		return err
	}
	if clickErr != nil {
		return clickErr
	}
	return fmt.Errorf("%s仍然可见，暂不继续关闭其他窗口", panel.Description)
}
