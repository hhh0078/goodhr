// Package common 文件作用：按平台配置发送消息，安全回车兜底并确认输入框清空。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// sendMessage 按平台配置发送消息，仅在按钮明确尚未点击时使用回车兜底。
func sendMessage(ctx context.Context, browser model.Browser, cfg model.Config, inputKey string, sendKey string, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("发送内容不能为空")
	}
	if err := InputRequired(ctx, browser, cfg, inputKey, message); err != nil {
		return fmt.Errorf("输入消息失败：%w", err)
	}
	var buttonErr error
	if _, configured := cfg.Selectors[sendKey]; configured {
		selector, err := RequiredSelector(cfg, sendKey)
		if err != nil {
			return err
		}
		result, err := browser.Click(ctx, contract.ElementClickRequest{Selector: selector})
		if err == nil && result.Clicked {
			return confirmMessageSent(ctx, browser, cfg, inputKey)
		}
		if err == nil {
			err = errors.New("发送按钮没有完成点击")
		}
		buttonErr = fmt.Errorf("点击发送消息失败：%w", err)
		if ctx.Err() != nil {
			return errors.Join(buttonErr, ctx.Err())
		}
		if result.Clicked || !cfg.Behavior.MessageSendEnterFallback || !messageClickNotStarted(err) {
			return buttonErr
		}
		// 查找或移动失败后重新聚焦原输入框，不重新输入，避免改动或重复消息内容。
		if err := ClickRequired(ctx, browser, cfg, inputKey); err != nil {
			return errors.Join(buttonErr, fmt.Errorf("回车发送前聚焦输入框失败：%w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(buttonErr, err)
	}
	description := "按回车发送消息"
	if buttonErr != nil {
		description = "发送按钮未能点击，改用回车发送消息"
	}
	result, err := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Enter", DelayMS: 80,
		Diagnostic: buttonErr != nil, Description: description})
	if err != nil {
		return errors.Join(buttonErr, fmt.Errorf("按 Enter 发送消息失败：%w", err))
	}
	if !result.Pressed {
		return errors.Join(buttonErr, errors.New("按 Enter 发送消息失败：按键没有执行"))
	}
	if err := confirmMessageSent(ctx, browser, cfg, inputKey); err != nil {
		return errors.Join(buttonErr, err)
	}
	return nil
}

// messageClickNotStarted 根据 Worker 的点击阶段判断是否能安全兜底，浏览器中断和不确定结果不得重发。
func messageClickNotStarted(err error) bool {
	if contract.IsSessionUnavailable(err) {
		return false
	}
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) {
		return false
	}
	if workerErr.Body.Code == "ACTION_CANCELLED" || workerErr.Body.Code == "ACTION_TIMEOUT" {
		return false
	}
	var details struct {
		ClickStarted *bool `json:"click_started"`
	}
	if json.Unmarshal(workerErr.Body.Details, &details) == nil && details.ClickStarted != nil {
		return !*details.ClickStarted
	}
	return workerErr.Body.Code == "ELEMENT_NOT_FOUND" && workerErr.Body.Step == "find"
}

// confirmMessageSent 使用标准读取能力确认输入框清空，不注入脚本，也不重复发送。
func confirmMessageSent(ctx context.Context, browser model.Browser, cfg model.Config, inputKey string) error {
	if !cfg.Behavior.MessageSendVerifyEmpty {
		return waitConversationPoll(ctx)
	}
	selector, err := RequiredSelector(cfg, inputKey)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 10; attempt++ {
		if err := waitConversationPoll(ctx); err != nil {
			return err
		}
		result, err := browser.Read(ctx, contract.ElementReadRequest{Selector: selector})
		if err != nil {
			return fmt.Errorf("确认消息发送结果失败：%w", err)
		}
		if strings.TrimSpace(result.Value) == "" {
			return nil
		}
	}
	return errors.New("消息输入框还没清空，发送结果尚未确认；请检查当前聊天，避免重复发送")
}
